package migration

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
)

func encodeLegacyTestValue(t *testing.T, key, nonce, plain []byte) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	sealed := aead.Seal(nil, nonce, plain, nil)
	ciphertext := sealed[:len(sealed)-aead.Overhead()]
	tag := sealed[len(sealed)-aead.Overhead():]
	raw := make([]byte, 0, len(nonce)+len(tag)+len(ciphertext))
	raw = append(raw, nonce...)
	raw = append(raw, tag...)
	raw = append(raw, ciphertext...)
	return base64.StdEncoding.EncodeToString(raw)
}

func TestLegacyCipherDecryptsNodeWireFormat(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	nonce := []byte{1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23}
	ciphertext := encodeLegacyTestValue(t, key, nonce, []byte("legacy secret"))

	legacy, err := NewLegacyCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("new legacy cipher: %v", err)
	}
	got, err := legacy.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "legacy secret" {
		t.Fatalf("plaintext = %q", got)
	}
}

func TestLegacyCipherRejectsInvalidInputs(t *testing.T) {
	if _, err := NewLegacyCipher(base64.StdEncoding.EncodeToString(make([]byte, 31))); err == nil {
		t.Fatal("expected invalid key length error")
	}
	legacy, err := NewLegacyCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Decrypt("not-base64"); err == nil {
		t.Fatal("expected malformed ciphertext error")
	}
	if _, err := legacy.Decrypt(base64.StdEncoding.EncodeToString(make([]byte, 27))); err == nil {
		t.Fatal("expected short ciphertext error")
	}

	valid := encodeLegacyTestValue(t, make([]byte, 32), make([]byte, 12), []byte("secret"))
	raw, err := base64.StdEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if _, err := legacy.Decrypt(base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("expected authentication error")
	}
}

func TestLegacyCipherEmptyValue(t *testing.T) {
	legacy, err := NewLegacyCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := legacy.Decrypt("")
	if err != nil || got != "" {
		t.Fatalf("empty decrypt = %q, %v", got, err)
	}
}
