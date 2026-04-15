package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
)

// Manager holds an AES-GCM cipher for encrypting and decrypting
// sensitive strings in memory. A zero-value Manager (no key) is a
// no-op passthrough.
type Manager struct {
	mu    sync.RWMutex
	gcm   cipher.AEAD
	store map[string][]byte // field name → ciphertext
}

// New creates a Manager from a hex-encoded AES key (16, 24, or 32 bytes).
// An empty key disables encryption (passthrough mode).
func New(hexKey string) (*Manager, error) {
	m := &Manager{store: make(map[string][]byte)}
	if hexKey == "" {
		return m, nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("secrets: invalid hex key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	m.gcm = gcm
	return m, nil
}

// Seal encrypts plaintext, stores it under the given field name, and
// returns the hex-encoded ciphertext. If encryption is disabled the
// plaintext is stored as-is.
func (m *Manager) Seal(field, plaintext string) string {
	if plaintext == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gcm == nil {
		m.store[field] = []byte(plaintext)
		return plaintext
	}

	nonce := make([]byte, m.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		// Fallback: store plaintext if randomness fails.
		m.store[field] = []byte(plaintext)
		return plaintext
	}
	ct := m.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	m.store[field] = ct
	return hex.EncodeToString(ct)
}

// Open decrypts the value stored under field and returns the plaintext.
// Returns empty string if the field was never sealed.
func (m *Manager) Open(field string) (string, error) {
	m.mu.RLock()
	ct, ok := m.store[field]
	m.mu.RUnlock()
	if !ok || len(ct) == 0 {
		return "", nil
	}

	if m.gcm == nil {
		return string(ct), nil
	}

	nonceSize := m.gcm.NonceSize()
	if len(ct) < nonceSize {
		return "", fmt.Errorf("secrets: ciphertext too short for field %q", field)
	}
	plaintext, err := m.gcm.Open(nil, ct[:nonceSize], ct[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("secrets: decrypt field %q: %w", field, err)
	}
	return string(plaintext), nil
}

// Wipe zeroes all stored ciphertexts.
func (m *Manager) Wipe() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.store {
		for i := range v {
			v[i] = 0
		}
		delete(m.store, k)
	}
}
