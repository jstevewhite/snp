package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Key is a 32-byte AES-256-GCM key (spec §4 "Encryption").
type Key struct {
	key []byte
}

// LoadOrCreateKey reads the key from path, creating it (random, mode
// 0600) if it does not exist.
func LoadOrCreateKey(path string) (*Key, error) {
	if b, err := os.ReadFile(path); err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("key file %s: expected 32 bytes, got %d", path, len(b))
		}
		return &Key{key: b}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return &Key{key: b}, nil
}

// Seal encrypts plaintext under the snippet id (used as AAD so a
// ciphertext cannot be moved between rows). Output is
// nonce||ciphertext.
func (k *Key) Seal(id string, pt []byte) ([]byte, error) {
	block, err := aes.NewCipher(k.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, pt, []byte(id)), nil
}

// Open decrypts data sealed under id.
func (k *Key) Open(id string, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(k.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	n := gcm.NonceSize()
	if len(data) < n {
		return nil, ErrDecrypt
	}
	pt, err := gcm.Open(nil, data[:n], data[n:], []byte(id))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
