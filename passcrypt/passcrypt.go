package passcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// DecodePasswordKey decodes PASSWORD_KEY: whitespace is stripped, then base64 (Std or Raw) is decoded.
// The decoded key must be 16, 24, or 32 bytes for AES-128/192/256.
func DecodePasswordKey(raw string) ([]byte, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("empty PASSWORD_KEY")
	}
	var b strings.Builder
	for _, r := range raw {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	compact := b.String()
	key, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(compact)
	}
	if err != nil {
		return nil, fmt.Errorf("PASSWORD_KEY must be valid base64: %w", err)
	}
	switch len(key) {
	case 16, 24, 32:
		return key, nil
	default:
		return nil, fmt.Errorf("PASSWORD_KEY decodes to %d bytes; need 16, 24, or 32", len(key))
	}
}

// EncryptAESGCM encrypts plaintext with AES-GCM using the given key.
// Returns base64 ciphertext (without tag), nonce (IV), and authentication tag.
func EncryptAESGCM(plaintext string, key []byte) (ciphertextB64, nonceB64, tagB64 string, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", "", fmt.Errorf("aes key: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", "", fmt.Errorf("nonce: %w", err)
	}
	// Seal appends tag to ciphertext
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	tagSize := gcm.Overhead()
	if len(sealed) < tagSize {
		return "", "", "", fmt.Errorf("unexpected seal output length")
	}
	ct := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]
	return base64.StdEncoding.EncodeToString(ct),
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(tag),
		nil
}

// DecryptAESGCM decrypts base64 ciphertext, nonce (IV), and tag produced by EncryptAESGCM.
// Returns the original plaintext for use by workers or other callers holding the same key.
func DecryptAESGCM(ciphertextB64, nonceB64, tagB64 string, key []byte) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("empty key")
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("ciphertext base64: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return "", fmt.Errorf("nonce base64: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(tagB64)
	if err != nil {
		return "", fmt.Errorf("tag base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes key: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return "", fmt.Errorf("nonce length %d, want %d", len(nonce), gcm.NonceSize())
	}
	tagSize := gcm.Overhead()
	if len(tag) != tagSize {
		return "", fmt.Errorf("tag length %d, want %d", len(tag), tagSize)
	}

	// Open expects ciphertext||tag as produced by Seal
	sealed := append(append(make([]byte, 0, len(ct)+len(tag)), ct...), tag...)
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("gcm open: %w", err)
	}
	return string(plain), nil
}
