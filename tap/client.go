package tap

import (
	"context"
	"sync"
	"time"
)

type Client struct {
	target      string
	port        int
	authKey     string
	origin      Origin
	timeout     time.Duration
	autoRetry   bool

	conn *Connection
	mu   sync.Mutex
}

func NewClient(target string, port int, authKey string, timeout time.Duration) *Client {
	if target == "" {
		target = "sg.clusters.typetube.xyz"
	}
	if port == 0 {
		port = 8443
	}
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		target:    target,
		port:      port,
		authKey:   authKey,
		origin:    OriginTypetubeGo,
		timeout:   timeout,
		autoRetry: true,
	}
}

func (c *Client) acquireConnection(ctx context.Context) (*Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil && c.conn.authenticated && !c.conn.isDraining {
		return c.conn, nil
	}

	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}

	conn := NewConnection(c.target, c.port, c.authKey, c.origin, c.timeout)
	if err := conn.Connect(ctx); err != nil {
		return nil, err
	}
	c.conn = conn
	return conn, nil
}

func (c *Client) Invoke(ctx context.Context, method string, payload []byte) ([]byte, error) {
	conn, err := c.acquireConnection(ctx)
	if err != nil {
		return nil, err
	}

	res, err := conn.Invoke(ctx, method, payload)
	if err != nil && c.autoRetry {
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()

		retryConn, err2 := c.acquireConnection(ctx)
		if err2 == nil {
			return retryConn.Invoke(ctx, method, payload)
		}
	}
	return res, err
}

func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	conn, err := c.acquireConnection(ctx)
	if err != nil {
		return 0, err
	}
	lat, err := conn.Ping(ctx)
	if err != nil && c.autoRetry {
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()

		retryConn, err2 := c.acquireConnection(ctx)
		if err2 == nil {
			return retryConn.Ping(ctx)
		}
	}
	return lat, err
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}
