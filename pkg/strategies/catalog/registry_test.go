package catalog

import (
	"testing"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func dummyFactory(_ Config) (backtest.Strategy, error) {
	return nil, nil
}

func TestTryRegister_EmptyName(t *testing.T) {
	err := TryRegister(Registration{Name: "", Factory: dummyFactory})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestTryRegister_NilFactory(t *testing.T) {
	err := TryRegister(Registration{Name: "nil-factory-test", Factory: nil})
	if err == nil {
		t.Fatal("expected error for nil factory")
	}
}

func TestTryRegister_Duplicate(t *testing.T) {
	// Register a unique name for this test
	name := "try-register-dup-test"
	err := TryRegister(Registration{Name: name, Factory: dummyFactory})
	if err != nil {
		t.Fatalf("first TryRegister failed: %v", err)
	}

	// Second registration with the same name should fail
	err = TryRegister(Registration{Name: name, Factory: dummyFactory})
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestTryRegister_Success(t *testing.T) {
	name := "try-register-ok-test"
	err := TryRegister(Registration{
		Name:    name,
		Aliases: []string{"try-register-ok-alias"},
		Groups:  []string{"test-group"},
		Factory: dummyFactory,
	})
	if err != nil {
		t.Fatalf("TryRegister failed: %v", err)
	}

	// Verify it's in the Available list
	found := false
	for _, n := range Available() {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("strategy %q not in Available() after TryRegister", name)
	}
}
