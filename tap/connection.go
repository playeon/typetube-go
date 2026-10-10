package tap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type pendingRequest struct {
	traceID uint16
	resChan chan []byte
	errChan chan error
}

type Connection struct {
	targetHost       string
	targetPort       int
	targetSocketPath string
	authKey          string
	psk              string
	origin           Origin
	encrypted        bool
	serverIdentity   []byte
	timeout          time.Duration
	compression      bool

	conn          net.Conn
	cryptoSession *CryptoSession
	authenticated bool
	isRevoked     bool
	isDraining    bool

	pending   map[uint16]*pendingRequest
	pendingMu sync.Mutex
	traceSeq  uint32
	writeMu   sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}
}

func ParseTapHost(target string, defaultPort int) (string, int) {
	cleaned := strings.TrimSpace(target)
	if strings.HasPrefix(cleaned, "tap://") {
		cleaned = cleaned[6:]
	}
	if idx := strings.Index(cleaned, "/"); idx != -1 {
		cleaned = cleaned[:idx]
	}
	if strings.HasPrefix(cleaned, "[") && strings.Contains(cleaned, "]") {
		end := strings.Index(cleaned, "]")
		host := cleaned[1:end]
		rest := cleaned[end+1:]
		if strings.HasPrefix(rest, ":") {
			var p int
			fmt.Sscanf(rest[1:], "%d", &p)
			if p > 0 {
				return host, p
			}
		}
		return host, defaultPort
	}
	if strings.Contains(cleaned, ":") {
		parts := strings.Split(cleaned, ":")
		if len(parts) == 2 {
			var p int
			fmt.Sscanf(parts[1], "%d", &p)
			if p > 0 {
				return parts[0], p
			}
		}
	}
	return cleaned, defaultPort
}

func NewConnection(target string, port int, authKey string, origin Origin, timeout time.Duration) *Connection {
	host, p := ParseTapHost(target, port)
	isLocal := host == "localhost" || host == "127.0.0.1" || host == "::1"

	var socketPath string
	if strings.HasPrefix(target, "tap:///") || strings.HasPrefix(target, "unix://") {
		socketPath = strings.TrimPrefix(strings.TrimPrefix(target, "tap:///"), "unix://")
	} else if isLocal {
		defaultSock := "/tmp/typetube-tap.sock"
		if envSock := os.Getenv("TYPETUBE_TAP_SOCKET"); envSock != "" {
			defaultSock = envSock
		}
		if _, err := os.Stat(defaultSock); err == nil {
			socketPath = defaultSock
		}
	}

	return &Connection{
		targetHost:       host,
		targetPort:       p,
		targetSocketPath: socketPath,
		authKey:          authKey,
		origin:           origin,
		encrypted:        !isLocal && socketPath == "",
		timeout:          timeout,
		compression:      true,
		pending:          make(map[uint16]*pendingRequest),
		closed:           make(chan struct{}),
	}
}

func (c *Connection) nextTraceID() uint16 {
	val := atomic.AddUint32(&c.traceSeq, 1)
	tid := uint16(val & 0xFFFF)
	if tid == 0 {
		return c.nextTraceID()
	}
	return tid
}

func (c *Connection) Connect(ctx context.Context) error {
	var conn net.Conn
	var err error

	dialer := &net.Dialer{
		Timeout: c.timeout,
	}

	if c.targetSocketPath != "" {
		conn, err = dialer.DialContext(ctx, "unix", c.targetSocketPath)
	} else {
		// Use tcp4 to force IPv4
		addr := fmt.Sprintf("%s:%d", c.targetHost, c.targetPort)
		conn, err = dialer.DialContext(ctx, "tcp4", addr)
		if err != nil {
			// fallback to standard tcp if tcp4 fails
			conn, err = dialer.DialContext(ctx, "tcp", addr)
		}
	}

	if err != nil {
		return fmt.Errorf("TAP dial failed: %w", err)
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}

	c.conn = conn
	go c.readLoop()

	if c.encrypted {
		if err := c.executeNegotiation(ctx); err != nil {
			c.Close()
			return fmt.Errorf("TAP encryption negotiation failed: %w", err)
		}
	}

	if c.authKey != "" {
		if err := c.executeAuthHandshake(ctx); err != nil {
			c.Close()
			return fmt.Errorf("TAP auth handshake failed: %w", err)
		}
	}

	c.authenticated = true
	return nil
}

func (c *Connection) executeNegotiation(ctx context.Context) error {
	priv, pubBytes, err := GenerateECDHKeyPair()
	if err != nil {
		return err
	}

	traceID := c.nextTraceID()
	req := &pendingRequest{
		traceID: traceID,
		resChan: make(chan []byte, 1),
		errChan: make(chan error, 1),
	}

	c.pendingMu.Lock()
	c.pending[traceID] = req
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, traceID)
		c.pendingMu.Unlock()
	}()

	var payload []byte
	psk := c.psk
	if psk == "" {
		psk = c.authKey
	}
	if psk != "" {
		proof := ComputeHandshakeProof(psk, pubBytes)
		payload = make([]byte, 2+len(pubBytes)+len(proof))
		binary.BigEndian.PutUint16(payload[:2], uint16(len(pubBytes)))
		copy(payload[2:], pubBytes)
		copy(payload[2+len(pubBytes):], proof)
	} else {
		payload = pubBytes
	}

	frame, err := EncodeFrame(IntentNegotiate, traceID, FlagRequest, payload, c.origin)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	_, err = c.conn.Write(frame)
	c.writeMu.Unlock()
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-req.errChan:
		return err
	case respPayload := <-req.resChan:
		var serverPubBytes []byte
		var serverProof []byte

		if len(respPayload) > 70 && binary.BigEndian.Uint16(respPayload[:2]) == 65 {
			pubKeyLen := int(binary.BigEndian.Uint16(respPayload[:2]))
			serverPubBytes = respPayload[2 : 2+pubKeyLen]
			offset := 2 + pubKeyLen
			if len(respPayload) >= offset+2 {
				sigLen := int(binary.BigEndian.Uint16(respPayload[offset : offset+2]))
				offset += 2 + sigLen
				if len(respPayload) >= offset+32 {
					serverProof = respPayload[offset : offset+32]
				}
			} else if len(respPayload) >= offset+32 {
				serverProof = respPayload[offset : offset+32]
			}
		} else if len(respPayload) == 65+32 {
			serverPubBytes = respPayload[:65]
			serverProof = respPayload[65 : 65+32]
		} else if len(respPayload) == 65 {
			serverPubBytes = respPayload
		} else {
			return errors.New("unexpected negotiation payload from server")
		}

		if psk != "" && len(serverProof) == 32 {
			expectedData := make([]byte, 0, len(serverPubBytes)+len(pubBytes))
			expectedData = append(expectedData, serverPubBytes...)
			expectedData = append(expectedData, pubBytes...)
			if !VerifyHandshakeProof(psk, expectedData, serverProof) {
				return errors.New("SECURITY_ALERT: Server handshake PSK proof verification failed")
			}
		}

		sharedSecret, err := ComputeSharedSecret(priv, serverPubBytes)
		if err != nil {
			return err
		}

		var bindingKey string
		if psk != "" && len(serverProof) == 32 {
			bindingKey = psk
		}
		c.cryptoSession, err = NewCryptoSession(sharedSecret, false, bindingKey)
		return err
	}
}

func (c *Connection) executeAuthHandshake(ctx context.Context) error {
	traceID := c.nextTraceID()
	req := &pendingRequest{
		traceID: traceID,
		resChan: make(chan []byte, 1),
		errChan: make(chan error, 1),
	}

	c.pendingMu.Lock()
	c.pending[traceID] = req
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, traceID)
		c.pendingMu.Unlock()
	}()

	payload := []byte(c.authKey)
	if err := c.writeFrame(IntentAuth, traceID, FlagRequest, payload); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-req.errChan:
		return err
	case <-req.resChan:
		return nil
	}
}

func (c *Connection) writeFrame(intent Intent, traceID uint16, flags Flags, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn == nil {
		return errors.New("connection closed")
	}

	outPayload := payload
	outFlags := flags
	if c.cryptoSession != nil {
		outFlags |= FlagEncrypted
		encryptedLen := uint32(len(payload) + 16)
		aad := GetFrameHeaderAAD(c.origin, intent, outFlags, traceID, encryptedLen)
		outPayload = c.cryptoSession.Encrypt(payload, aad)
	}

	frame, err := EncodeFrame(intent, traceID, outFlags, outPayload, c.origin)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(frame)
	return err
}

func (c *Connection) readLoop() {
	headerBuf := make([]byte, HeaderSize)
	for {
		_, err := io.ReadFull(c.conn, headerBuf)
		if err != nil {
			c.cleanupPending(err)
			return
		}

		header, err := ParseHeader(headerBuf)
		if err != nil {
			c.cleanupPending(err)
			return
		}

		payload := make([]byte, header.Length)
		if header.Length > 0 {
			_, err = io.ReadFull(c.conn, payload)
			if err != nil {
				c.cleanupPending(err)
				return
			}
		}

		c.handleIncomingFrame(header, payload)
	}
}

func (c *Connection) handleIncomingFrame(header *Header, payload []byte) {
	intent := header.Intent
	traceID := header.TraceID
	flags := header.Flags

	if (flags & FlagEncrypted) != 0 {
		if c.cryptoSession == nil {
			c.cleanupPending(errors.New("received encrypted frame without crypto session"))
			c.Close()
			return
		}
		aad := GetFrameHeaderAAD(header.Origin, intent, flags, traceID, uint32(len(payload)))
		dec, err := c.cryptoSession.Decrypt(payload, aad)
		if err != nil {
			c.cleanupPending(fmt.Errorf("decryption failed: %w", err))
			c.Close()
			return
		}
		payload = dec
	}

	if intent == IntentGoAway {
		c.isDraining = true
		return
	}

	if intent == IntentDisconnect {
		c.isRevoked = true
		c.cleanupPending(fmt.Errorf("CONNECTION_TERMINATED: %s", string(payload)))
		c.Close()
		return
	}

	c.pendingMu.Lock()
	req, exists := c.pending[traceID]
	if exists {
		delete(c.pending, traceID)
	}
	c.pendingMu.Unlock()

	if !exists {
		return
	}

	if (flags & FlagCompressed) != 0 {
		decompressed, err := DecompressPayload(payload)
		if err != nil {
			req.errChan <- fmt.Errorf("decompression failed: %w", err)
			return
		}
		payload = decompressed
	}

	if (flags & FlagError) != 0 {
		req.errChan <- errors.New(string(payload))
	} else {
		req.resChan <- payload
	}
}

func (c *Connection) cleanupPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for _, req := range c.pending {
		select {
		case req.errChan <- err:
		default:
		}
	}
	c.pending = make(map[uint16]*pendingRequest)
}

func (c *Connection) Invoke(ctx context.Context, method string, payload []byte) ([]byte, error) {
	if c.isRevoked {
		return nil, errors.New("TAP connection revoked by server")
	}

	methodBytes := []byte(method)
	packetPayload := make([]byte, 1+len(methodBytes)+len(payload))
	packetPayload[0] = byte(len(methodBytes))
	copy(packetPayload[1:], methodBytes)
	copy(packetPayload[1+len(methodBytes):], payload)

	traceID := c.nextTraceID()
	req := &pendingRequest{
		traceID: traceID,
		resChan: make(chan []byte, 1),
		errChan: make(chan error, 1),
	}

	c.pendingMu.Lock()
	c.pending[traceID] = req
	c.pendingMu.Unlock()

	flags := FlagRequest
	if c.compression {
		flags |= FlagAcceptCompressed
	}

	if err := c.writeFrame(IntentRPCCall, traceID, flags, packetPayload); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, traceID)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, traceID)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case err := <-req.errChan:
		return nil, err
	case resp := <-req.resChan:
		return resp, nil
	}
}

func (c *Connection) Ping(ctx context.Context) (time.Duration, error) {
	traceID := c.nextTraceID()
	req := &pendingRequest{
		traceID: traceID,
		resChan: make(chan []byte, 1),
		errChan: make(chan error, 1),
	}

	c.pendingMu.Lock()
	c.pending[traceID] = req
	c.pendingMu.Unlock()

	t0 := time.Now()
	if err := c.writeFrame(IntentPing, traceID, FlagRequest, nil); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, traceID)
		c.pendingMu.Unlock()
		return 0, err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, traceID)
		c.pendingMu.Unlock()
		return 0, ctx.Err()
	case err := <-req.errChan:
		return 0, err
	case <-req.resChan:
		return time.Since(t0), nil
	}
}

func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.cleanupPending(errors.New("connection closed"))
	})
	return nil
}
