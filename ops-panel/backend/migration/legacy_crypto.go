package migration

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	legacyNonceSize = 12
	legacyTagSize   = 16
)

// LegacyCipher decrypts values written by the retired Node.js panel. Its wire
// format is nonce || authentication tag || ciphertext.
type LegacyCipher struct {
	aead cipher.AEAD
}

func NewLegacyCipher(encodedKey string) (*LegacyCipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode legacy encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("legacy encryption key must decode to 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create legacy AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create legacy GCM cipher: %w", err)
	}
	if aead.NonceSize() != legacyNonceSize || aead.Overhead() != legacyTagSize {
		return nil, errors.New("unexpected AES-GCM nonce or tag size")
	}
	return &LegacyCipher{aead: aead}, nil
}

func (c *LegacyCipher) Decrypt(encodedCiphertext string) (string, error) {
	if encodedCiphertext == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return "", fmt.Errorf("decode legacy ciphertext: %w", err)
	}
	if len(raw) < legacyNonceSize+legacyTagSize {
		return "", errors.New("legacy ciphertext is too short")
	}

	nonce := raw[:legacyNonceSize]
	tag := raw[legacyNonceSize : legacyNonceSize+legacyTagSize]
	ciphertext := raw[legacyNonceSize+legacyTagSize:]
	sealed := make([]byte, 0, len(ciphertext)+len(tag))
	sealed = append(sealed, ciphertext...)
	sealed = append(sealed, tag...)

	plain, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", errors.New("decrypt legacy ciphertext: authentication failed")
	}
	return string(plain), nil
}
