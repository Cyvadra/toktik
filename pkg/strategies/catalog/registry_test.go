package catalog

import (
	"fmt"
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

func TestResolveDetailedProvidesFreshStrategyInstances(t *testing.T) {
	name := "fresh-instance-test"
	counter := 0
	err := TryRegister(Registration{
		Name: name,
		Factory: func(Config) (backtest.Strategy, error) {
			counter++
			return &namedTestStrategy{name: fmt.Sprintf("fresh-%d", counter)}, nil
		},
	})
	if err != nil {
		t.Fatalf("TryRegister failed: %v", err)
	}

	resolved, err := ResolveDetailed(name, DefaultConfig(), "")
	if err != nil {
		t.Fatalf("ResolveDetailed failed: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	first, err := resolved[0].NewStrategy()
	if err != nil {
		t.Fatalf("NewStrategy first failed: %v", err)
	}
	second, err := resolved[0].NewStrategy()
	if err != nil {
		t.Fatalf("NewStrategy second failed: %v", err)
	}
	if first == second {
		t.Fatal("NewStrategy returned the same strategy instance twice")
	}
	if first.Name() == second.Name() {
		t.Fatalf("strategy names should reflect distinct factory calls, got %q", first.Name())
	}
}

type namedTestStrategy struct{ name string }

func (s *namedTestStrategy) Name() string { return s.name }

func (s *namedTestStrategy) Init(*backtest.SetupContext) error { return nil }

func (s *namedTestStrategy) OnBar(*backtest.BarContext) {}
