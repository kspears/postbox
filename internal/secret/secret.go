// Package secret seals a short secret, such as an account password, with a 32-byte key
// (AES-256-GCM), binding it to the context it belongs to.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// Seal encrypts plain under key. bound is authenticated but not stored: Open must be given the
// same bytes, so a sealed value cannot be moved to another row.
func Seal(key, bound []byte, plain string) ([]byte, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	return aead.Seal(nonce, nonce, []byte(plain), bound), nil
}

// Open reverses Seal; it fails when key or bound differ.
func Open(key, bound, sealed []byte) (string, error) {
	aead, err := gcm(key)
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", errors.New("sealed value too short")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, bound)
	return string(plain), err
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("sealing key: %w", err)
	}
	return cipher.NewGCM(block)
}
