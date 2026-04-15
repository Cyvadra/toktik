package secrets

import (
	"encoding/hex"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	// 32-byte AES-256 key
	key := hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	m, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Wipe()

	m.Seal("api_key", "sk-secret-12345")
	got, err := m.Open("api_key")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != "sk-secret-12345" {
		t.Fatalf("got %q, want %q", got, "sk-secret-12345")
	}
}

func TestPassthrough(t *testing.T) {
	m, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.Seal("token", "plain-value")
	got, err := m.Open("token")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != "plain-value" {
		t.Fatalf("got %q, want %q", got, "plain-value")
	}
}

func TestMissingField(t *testing.T) {
	m, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := m.Open("nonexistent")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestInvalidKey(t *testing.T) {
	_, err := New("not-hex")
	if err == nil {
		t.Fatal("expected error for invalid hex key")
	}
}

func TestBadKeyLength(t *testing.T) {
	_, err := New("0102030405") // 5 bytes — invalid for AES
	if err == nil {
		t.Fatal("expected error for bad key length")
	}
}

func TestWipe(t *testing.T) {
	key := hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	m, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.Seal("secret", "value")
	m.Wipe()
	got, err := m.Open("secret")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != "" {
		t.Fatalf("after Wipe got %q, want empty", got)
	}
}
