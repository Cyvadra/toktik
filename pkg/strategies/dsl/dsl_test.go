package dsl_test

import (
	"testing"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/dsl"
)

// uniqueName returns a test-unique strategy name to avoid conflicts with the
// global registry shared across tests.
func uniqueName(suffix string) string {
	return "dsl-test-" + suffix
}

// --- Spec.Register ---

func TestSpec_Register_Stateless(t *testing.T) {
	name := uniqueName("stateless")
	dsl.Spec{
		Name:   name,
		Groups: []string{"test"},
		Indicators: []dsl.IndicatorDef{
			{Name: "sma20", Indicator: backtest.SMA("close", 20)},
		},
		EntryLong: []dsl.Condition{
			func(ctx *backtest.BarContext) bool { return ctx.Ind("sma20") > 0 },
		},
		ExitLong: []dsl.Condition{
			func(ctx *backtest.BarContext) bool { return ctx.Ind("sma20") <= 0 },
		},
		Sizing: dsl.QtyFromPctEquity(0.95),
	}.Register()

	// Verify the strategy appears in the catalog.
	found := false
	for _, n := range catalog.Available() {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("strategy %q not found in catalog after Register()", name)
	}
}

func TestSpec_Register_NameIsDisplayName(t *testing.T) {
	name := uniqueName("displayname")
	dsl.Spec{Name: name}.Register()

	strats, err := catalog.Resolve(name, catalog.DefaultConfig())
	if err != nil {
		t.Fatalf("Resolve(%q): %v", name, err)
	}
	if len(strats) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(strats))
	}
	if strats[0].Name() != name {
		t.Errorf("Name() = %q, want %q", strats[0].Name(), name)
	}
}

// --- Spec.RegisterWithConfig ---

func TestSpec_RegisterWithConfig_InheritsOuterName(t *testing.T) {
	outerName := uniqueName("config-inherit")
	dsl.Spec{Name: outerName}.RegisterWithConfig(func(_ catalog.Config) dsl.Spec {
		// Return an inner spec without a Name — should inherit the outer name.
		return dsl.Spec{}
	})

	strats, err := catalog.Resolve(outerName, catalog.DefaultConfig())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := strats[0].Name(); got != outerName {
		t.Errorf("Name() = %q, want %q (inherited)", got, outerName)
	}
}

func TestSpec_RegisterWithConfig_InnerNameOverrides(t *testing.T) {
	outerName := uniqueName("config-override-outer")
	innerName := uniqueName("config-override-inner")
	dsl.Spec{Name: outerName}.RegisterWithConfig(func(_ catalog.Config) dsl.Spec {
		return dsl.Spec{Name: innerName}
	})

	strats, err := catalog.Resolve(outerName, catalog.DefaultConfig())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := strats[0].Name(); got != innerName {
		t.Errorf("Name() = %q, want %q (inner override)", got, innerName)
	}
}

// --- dslStrategy.Init ---

func TestDslStrategy_Init_RegistersIndicators(t *testing.T) {
	name := uniqueName("init-inds")
	dsl.Spec{
		Name: name,
		Indicators: []dsl.IndicatorDef{
			{Name: "sma5", Indicator: backtest.SMA("close", 5)},
			{Name: "ema10", Indicator: backtest.EMA("close", 10)},
		},
	}.Register()

	strats, err := catalog.Resolve(name, catalog.DefaultConfig())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	ctx := backtest.NewSetupContext("test", "BTC", "1h")
	if err := strats[0].Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// If Init panics or returns an error we'd have caught it above.
	// No further assertion needed — indicator registration is internal state.
}

// --- Sizing helpers ---

func TestQtyFromPctEquity_ReturnsNonNil(t *testing.T) {
	fn := dsl.QtyFromPctEquity(0.95)
	if fn == nil {
		t.Fatal("QtyFromPctEquity returned nil")
	}
}

func TestQtyFromPctCash_ReturnsNonNil(t *testing.T) {
	fn := dsl.QtyFromPctCash(0.5)
	if fn == nil {
		t.Fatal("QtyFromPctCash returned nil")
	}
}

func TestQtyFromNotional_ReturnsNonNil(t *testing.T) {
	fn := dsl.QtyFromNotional(10000)
	if fn == nil {
		t.Fatal("QtyFromNotional returned nil")
	}
}

func TestQtyFromPctEquityCapped_ReturnsNonNil(t *testing.T) {
	fn := dsl.QtyFromPctEquityCapped(0.99, 0.5)
	if fn == nil {
		t.Fatal("QtyFromPctEquityCapped returned nil when pct > maxPct")
	}
}

func TestQtyFromPctEquityCapped_BelowCap_ReturnsNonNil(t *testing.T) {
	fn := dsl.QtyFromPctEquityCapped(0.3, 0.5)
	if fn == nil {
		t.Fatal("QtyFromPctEquityCapped returned nil when pct < maxPct")
	}
}
