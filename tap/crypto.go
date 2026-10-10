package tap

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"errors"
)

const (
	GCMTagLength = 16
	GCMIVLength  = 12
)

type CryptoSession struct {
	sendCipher       cipher.AEAD
	recvCipher       cipher.AEAD
	baseIV           []byte
	sendNonceCounter uint32
	recvNonceCounter uint32
}

func NewCryptoSession(sharedSecret []byte, isServer bool, psk string) (*CryptoSession, error) {
	salt := []byte("TAP_CRYPTO_SALT_v1")
	if len(psk) > 0 {
		salt = []byte(psk)
	}

	h := hmac.New(sha256.New, salt)
	h.Write(sharedSecret)
	prk := h.Sum(nil)

	h = hmac.New(sha256.New, prk)
	h.Write([]byte("TAP_C2S_KEY"))
	c2sKey := h.Sum(nil)

	h = hmac.New(sha256.New, prk)
	h.Write([]byte("TAP_S2C_KEY"))
	s2cKey := h.Sum(nil)

	h = hmac.New(sha256.New, prk)
	h.Write([]byte("TAP_BASE_IV"))
	baseIV := h.Sum(nil)[:8]

	var sendKey, recvKey []byte
	if isServer {
		sendKey = s2cKey
		recvKey = c2sKey
	} else {
		sendKey = c2sKey
		recvKey = s2cKey
	}

	sendBlock, err := aes.NewCipher(sendKey)
	if err != nil {
		return nil, err
	}
	sendGCM, err := cipher.NewGCM(sendBlock)
	if err != nil {
		return nil, err
	}

	recvBlock, err := aes.NewCipher(recvKey)
	if err != nil {
		return nil, err
	}
	recvGCM, err := cipher.NewGCM(recvBlock)
	if err != nil {
		return nil, err
	}

	return &CryptoSession{
		sendCipher: sendGCM,
		recvCipher: recvGCM,
		baseIV:     baseIV,
	}, nil
}

func (s *CryptoSession) makeIV(counter uint32) []byte {
	iv := make([]byte, GCMIVLength)
	copy(iv[:8], s.baseIV)
	binary.BigEndian.PutUint32(iv[8:], counter)
	return iv
}

func (s *CryptoSession) Encrypt(plaintext []byte, aad []byte) []byte {
	s.sendNonceCounter++
	iv := s.makeIV(s.sendNonceCounter)
	// Go's cipher.NewGCM Seal appends the 16-byte tag to the ciphertext: [ciphertext][tag]
	ciphertextAndTag := s.sendCipher.Seal(nil, iv, plaintext, aad)
	tag := ciphertextAndTag[len(ciphertextAndTag)-GCMTagLength:]
	ciphertext := ciphertextAndTag[:len(ciphertextAndTag)-GCMTagLength]
	// TAP wire format expects: [tag (16 bytes)][ciphertext]
	out := make([]byte, len(ciphertextAndTag))
	copy(out[:GCMTagLength], tag)
	copy(out[GCMTagLength:], ciphertext)
	return out
}

func (s *CryptoSession) Decrypt(payload []byte, aad []byte) ([]byte, error) {
	if len(payload) < GCMTagLength {
		return nil, errors.New("payload too short to contain AES-GCM tag")
	}
	tag := payload[:GCMTagLength]
	ciphertext := payload[GCMTagLength:]
	s.recvNonceCounter++
	iv := s.makeIV(s.recvNonceCounter)

	// Reorder to [ciphertext][tag] for Go GCM Open
	dataToDecrypt := make([]byte, len(payload))
	copy(dataToDecrypt[:len(ciphertext)], ciphertext)
	copy(dataToDecrypt[len(ciphertext):], tag)

	return s.recvCipher.Open(nil, iv, dataToDecrypt, aad)
}

func GenerateECDHKeyPair() (*ecdh.PrivateKey, []byte, error) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	pubBytes := priv.PublicKey().Bytes()
	return priv, pubBytes, nil
}

func ComputeSharedSecret(priv *ecdh.PrivateKey, remotePubBytes []byte) ([]byte, error) {
	peerPub, err := ecdh.P256().NewPublicKey(remotePubBytes)
	if err != nil {
		return nil, err
	}
	return priv.ECDH(peerPub)
}

func ComputeHandshakeProof(key string, data []byte) []byte {
	h := hmac.New(sha256.New, []byte(key))
	h.Write(data)
	return h.Sum(nil)
}

func VerifyHandshakeProof(key string, data []byte, expectedProof []byte) bool {
	actual := ComputeHandshakeProof(key, data)
	return hmac.Equal(actual, expectedProof)
}

func VerifyServerSignature(pubKeyDER []byte, data []byte, signature []byte) bool {
	pub, err := x509.ParsePKIXPublicKey(pubKeyDER)
	if err != nil {
		return false
	}
	if edPub, ok := pub.(ed25519.PublicKey); ok {
		return ed25519.Verify(edPub, data, signature)
	}
	return false
}
