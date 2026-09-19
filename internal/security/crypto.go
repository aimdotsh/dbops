package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(masterKey string) (*Cipher, error) {
	if len(masterKey) < 16 {
		return nil, errors.New("master key must be at least 16 characters")
	}
	key := sha256.Sum256([]byte(masterKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) EncryptString(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plain), nil)
	buf := append(nonce, sealed...)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (c *Cipher) DecryptString(encoded string) (string, error) {
	buf, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	n := c.aead.NonceSize()
	if len(buf) <= n {
		return "", errors.New("invalid encrypted value")
	}
	plain, err := c.aead.Open(nil, buf[:n], buf[n:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func RandomPassword(length int) (string, error) {
	if length < 20 {
		length = 20
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789_-"
	b := make([]byte, length)
	raw := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(b), nil
}
