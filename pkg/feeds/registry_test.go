package feeds

import (
	"context"
	"testing"
)

type mockFeed struct{}

func (m *mockFeed) Name() string                                           { return "mock" }
func (m *mockFeed) Fields() []string                                       { return []string{"open", "high", "low", "close"} }
func (m *mockFeed) Symbols() []string                                      { return []string{"BTC"} }
func (m *mockFeed) SourceWindows() []Window                                { return []Window{PredefinedWindows[0]} }
func (m *mockFeed) Fetch(_ context.Context, _ FetchRequest) ([]Bar, error) { return nil, nil }

func TestRegisterAndGet(t *testing.T) {
	// Reset for test isolation
	mu.Lock()
	saved := registry
	registry = make(map[string]Feed)
	mu.Unlock()
	defer func() {
		mu.Lock()
		registry = saved
		mu.Unlock()
	}()

	f := &mockFeed{}
	Register(f)

	got := Get("mock")
	if got == nil {
		t.Fatal("Get returned nil for registered feed")
	}
	if got.Name() != "mock" {
		t.Fatalf("got %q, want %q", got.Name(), "mock")
	}

	if Get("nonexistent") != nil {
		t.Fatal("Get returned non-nil for unregistered feed")
	}

	names := List()
	if len(names) != 1 || names[0] != "mock" {
		t.Fatalf("List() = %v, want [mock]", names)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	mu.Lock()
	saved := registry
	registry = make(map[string]Feed)
	mu.Unlock()
	defer func() {
		mu.Lock()
		registry = saved
		mu.Unlock()
	}()

	Register(&mockFeed{})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	Register(&mockFeed{})
}
