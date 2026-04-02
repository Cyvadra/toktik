package dsl_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	"github.com/Cyvadra/toktik/pkg/strategies/dsl"
)

// --- helpers ----------------------------------------------------------

// syntheticFeed returns a simple 100-bar feed with monotonically increasing
// close prices.
type syntheticFeed struct{}

func (f *syntheticFeed) Fields() []string { return []string{"open", "high", "low", "close", "volume"} }

func (f *syntheticFeed) Load(_ context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	n := 100
	ds := backtest.NewDataSet(n)
	ts := make([]time.Time, n)
	open := make([]float64, n)
	high := make([]float64, n)
	low := make([]float64, n)
	cls := make([]float64, n)
	vol := make([]float64, n)
	base := req.From
	for i := 0; i < n; i++ {
		ts[i] = base.Add(time.Duration(i) * time.Hour)
		p := 100.0 + float64(i)
		open[i] = p
		high[i] = p + 1
		low[i] = p - 1
		cls[i] = p + 0.5
		vol[i] = 1000
	}
	ds.SetTimestamps(ts)
	ds.AddColumn("open", open)
	ds.AddColumn("high", high)
	ds.AddColumn("low", low)
	ds.AddColumn("close", cls)
	ds.AddColumn("volume", vol)
	return ds, nil
}

// runSpec compiles the Spec, wires it through the backtest Engine, and returns
// the result. It fails the test if any step errors.
func runSpec(t *testing.T, spec *dsl.Spec) *backtest.Result {
	t.Helper()
	strategy := spec.Compile()

	eng := backtest.NewEngine(backtest.Config{InitialCapital: 100_000})
	eng.RegisterDataFeed("test", &syntheticFeed{})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)

	result, err := eng.Run(context.Background(), "test", "TEST", "1h", from, to, strategy, nil)
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}
	return result
}

// --- Spec.Compile tests -----------------------------------------------

// TestCompile_StatelessOnBar verifies that a Spec with a stateless OnBar
// creates a valid backtest.Strategy that runs to completion.
func TestCompile_StatelessOnBar(t *testing.T) {
	bought := 0
	spec := &dsl.Spec{
		Name:   "test-stateless",
		Groups: []string{"test"},
		Indicators: []dsl.IndicatorSpec{
			{Name: "sma10", Ind: backtest.SMA("close", 10)},
		},
		OnBar: func(ctx *backtest.BarContext) {
			ref := ctx.PrimaryRef()
			sma := ctx.Ind("sma10")
			if !math.IsNaN(sma) && ctx.Close() > sma && ctx.Position(ref) == 0 {
				ctx.Buy(ref, 1)
				bought++
			}
		},
	}

	result := runSpec(t, spec)
	if result.InitialCapital <= 0 {
		t.Errorf("unexpected zero initial capital")
	}
	if bought == 0 {
		t.Errorf("expected at least one buy, got 0")
	}
}

// TestCompile_StatefulNew verifies that a Spec using New creates a per-instance
// state that is independent across compilations.
func TestCompile_StatefulNew(t *testing.T) {
	newCalls := 0
	spec := &dsl.Spec{
		Name:   "test-stateful",
		Groups: []string{"test"},
		New: func() *dsl.Instance {
			newCalls++
			var callCount int
			return &dsl.Instance{
				OnBar: func(ctx *backtest.BarContext) {
					callCount++
					_ = callCount
				},
			}
		},
	}

	_ = runSpec(t, spec)
	// Each Compile() call should invoke New() exactly once.
	if newCalls != 1 {
		t.Errorf("expected New to be called once per Compile, got %d", newCalls)
	}

	// A second Compile should call New again with a fresh state.
	before := newCalls
	_ = spec.Compile()
	if newCalls != before+1 {
		t.Errorf("expected second Compile to call New again, got %d total calls", newCalls)
	}
}

// TestCompile_NewTakesPrecedence verifies that when both New and OnBar are set,
// New takes precedence.
func TestCompile_NewTakesPrecedence(t *testing.T) {
	onBarCalled := false
	staticOnBarCalled := false

	spec := &dsl.Spec{
		Name:   "test-precedence",
		Groups: []string{"test"},
		New: func() *dsl.Instance {
			return &dsl.Instance{
				OnBar: func(_ *backtest.BarContext) {
					onBarCalled = true
				},
			}
		},
		OnBar: func(_ *backtest.BarContext) {
			staticOnBarCalled = true
		},
	}

	_ = runSpec(t, spec)

	if !onBarCalled {
		t.Error("expected Instance.OnBar to be called when New is set")
	}
	if staticOnBarCalled {
		t.Error("expected static OnBar NOT to be called when New is set")
	}
}

// TestCompile_Params verifies that Params are accessible in OnBar via
// ctx.Param().
func TestCompile_Params(t *testing.T) {
	spec := &dsl.Spec{
		Name:   "test-params",
		Groups: []string{"test"},
		Params: map[string]interface{}{
			"fast_period": 10,
			"threshold":   0.02,
		},
		OnBar: func(ctx *backtest.BarContext) {
			fp := ctx.ParamInt("fast_period", 0)
			if fp != 10 {
				panic("fast_period mismatch")
			}
			th := ctx.ParamFloat("threshold", 0)
			if th != 0.02 {
				panic("threshold mismatch")
			}
		},
	}

	_ = runSpec(t, spec)
}

// TestCompile_MultipleIndicators verifies that multiple indicators on the
// primary security are all resolved correctly.
func TestCompile_MultipleIndicators(t *testing.T) {
	var gotSMA, gotEMA, gotATR float64

	spec := &dsl.Spec{
		Name:   "test-multi-ind",
		Groups: []string{"test"},
		Indicators: []dsl.IndicatorSpec{
			{Name: "sma20", Ind: backtest.SMA("close", 20)},
			{Name: "ema20", Ind: backtest.EMA("close", 20)},
			{Name: "atr14", Ind: backtest.ATR(14)},
		},
		OnBar: func(ctx *backtest.BarContext) {
			if ctx.BarIndex() == 50 {
				gotSMA = ctx.Ind("sma20")
				gotEMA = ctx.Ind("ema20")
				gotATR = ctx.Ind("atr14")
			}
		},
	}

	_ = runSpec(t, spec)

	if math.IsNaN(gotSMA) {
		t.Error("sma20 is NaN at bar 50")
	}
	if math.IsNaN(gotEMA) {
		t.Error("ema20 is NaN at bar 50")
	}
	if math.IsNaN(gotATR) {
		t.Error("atr14 is NaN at bar 50")
	}
}

// TestCompile_InitHook verifies that Instance.Init is called with valid refs
// and can set up extra indicators.
func TestCompile_InitHook(t *testing.T) {
	initCalled := false
	spec := &dsl.Spec{
		Name:   "test-init-hook",
		Groups: []string{"test"},
		New: func() *dsl.Instance {
			return &dsl.Instance{
				Init: func(ctx *backtest.SetupContext, refs dsl.RefSet) error {
					initCalled = true
					// Register an extra indicator inside the Init hook.
					ctx.Register("extra_sma", backtest.SMA("close", 5))
					return nil
				},
				OnBar: func(ctx *backtest.BarContext) {
					// Ensure the extra indicator registered in Init is available.
					_ = ctx.Ind("extra_sma")
				},
			}
		},
	}

	_ = runSpec(t, spec)

	if !initCalled {
		t.Error("expected Instance.Init to be called")
	}
}

// TestCompile_ReportColumns verifies that report columns declared in Spec are
// surfaced via the ReportColumnProvider interface.
func TestCompile_ReportColumns(t *testing.T) {
	spec := &dsl.Spec{
		Name:   "test-report-cols",
		Groups: []string{"test"},
		ReportColumns: []backtest.ReportColumn{
			{Source: "sma10", Label: "SMA10", Decimals: 2, Overlay: true},
		},
		Indicators: []dsl.IndicatorSpec{
			{Name: "sma10", Ind: backtest.SMA("close", 10)},
		},
		OnBar: func(_ *backtest.BarContext) {},
	}

	s := spec.Compile()
	provider, ok := s.(backtest.ReportColumnProvider)
	if !ok {
		t.Fatal("compiled strategy does not implement ReportColumnProvider")
	}
	cols := provider.ReportColumns()
	if len(cols) != 1 || cols[0].Source != "sma10" {
		t.Errorf("unexpected report columns: %v", cols)
	}
}

// TestCompile_EmptyReportColumns verifies that a Spec with no ReportColumns
// still implements the interface and returns an empty (not nil) slice.
func TestCompile_EmptyReportColumns(t *testing.T) {
	spec := &dsl.Spec{
		Name:   "test-no-report-cols",
		Groups: []string{"test"},
		OnBar:  func(_ *backtest.BarContext) {},
	}

	s := spec.Compile()
	provider, ok := s.(backtest.ReportColumnProvider)
	if !ok {
		t.Fatal("compiled strategy does not implement ReportColumnProvider")
	}
	if cols := provider.ReportColumns(); cols == nil {
		t.Error("expected non-nil (empty) slice from ReportColumns()")
	}
}

// TestRefSet_MustSecurityPanic verifies that MustSecurity panics for unknown keys.
func TestRefSet_MustSecurityPanic(t *testing.T) {
	var refs dsl.RefSet // zero value → empty maps
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown security key")
		}
	}()
	refs.MustSecurity("nonexistent")
}

// TestRefSet_MustFactorPanic verifies that MustFactor panics for unknown keys.
func TestRefSet_MustFactorPanic(t *testing.T) {
	var refs dsl.RefSet
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown factor key")
		}
	}()
	refs.MustFactor("nonexistent")
}

// --- Register tests ---------------------------------------------------

// TestRegister_AddsToAvailable verifies that Register() makes the strategy
// appear in catalog.Available().
func TestRegister_AddsToAvailable(t *testing.T) {
	// catalog normalizes names to lowercase, so use a lowercase test name.
	name := "dsl-test-register-addstoavailable"
	spec := &dsl.Spec{
		Name:   name,
		Groups: []string{"test"},
		OnBar:  func(_ *backtest.BarContext) {},
	}
	spec.Register()

	found := false
	for _, n := range catalog.Available() {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("strategy %q not found in catalog.Available() after Register()", name)
	}
}

// TestRegisterWithConfig verifies that RegisterWithConfig allows config-driven
// parameterisation.
func TestRegisterWithConfig(t *testing.T) {
	name := "dsl-test-register-withconfig"
	base := &dsl.Spec{
		Name:   name,
		Groups: []string{"test"},
	}
	base.RegisterWithConfig(func(cfg catalog.Config) (*dsl.Spec, error) {
		period := catalog.IntOrDefault(cfg.FastPeriod, 10)
		return &dsl.Spec{
			Name:   name,
			Groups: []string{"test"},
			Indicators: []dsl.IndicatorSpec{
				{Name: "sma", Ind: backtest.SMA("close", period)},
			},
			OnBar: func(ctx *backtest.BarContext) { _ = ctx.Ind("sma") },
		}, nil
	})

	strategies, err := catalog.Resolve(name, catalog.DefaultConfig())
	if err != nil {
		t.Fatalf("catalog.Resolve: %v", err)
	}
	if len(strategies) == 0 {
		t.Error("expected at least one strategy from catalog.Resolve")
	}
}

// --- Sizing helpers ---------------------------------------------------

// TestQtyFromPctEquity verifies the basic calculation.
func TestQtyFromPctEquity(t *testing.T) {
	// Build a minimal BarContext via the engine so we have a real cash/equity.
	var capturedQty float64

	spec := &dsl.Spec{
		Name:   "test-sizing-equity",
		Groups: []string{"test"},
		OnBar: func(ctx *backtest.BarContext) {
			if ctx.BarIndex() == 20 {
				capturedQty = dsl.QtyFromPctEquity(ctx, 0.50)
			}
		},
	}

	_ = runSpec(t, spec)

	// We cannot know the exact value without replicating the engine's equity
	// calculation, but it must be positive.
	if capturedQty <= 0 {
		t.Errorf("expected positive qty from QtyFromPctEquity, got %f", capturedQty)
	}
}

// TestQtyFromPctCash verifies that cash-based sizing is positive.
func TestQtyFromPctCash(t *testing.T) {
	var capturedQty float64

	spec := &dsl.Spec{
		Name:   "test-sizing-cash",
		Groups: []string{"test"},
		OnBar: func(ctx *backtest.BarContext) {
			if ctx.BarIndex() == 10 {
				capturedQty = dsl.QtyFromPctCash(ctx, 0.50)
			}
		},
	}

	_ = runSpec(t, spec)

	if capturedQty <= 0 {
		t.Errorf("expected positive qty from QtyFromPctCash, got %f", capturedQty)
	}
}

// TestQtyFromNotional verifies notional-based sizing.
func TestQtyFromNotional(t *testing.T) {
	var capturedQty float64

	spec := &dsl.Spec{
		Name:   "test-sizing-notional",
		Groups: []string{"test"},
		OnBar: func(ctx *backtest.BarContext) {
			if ctx.BarIndex() == 10 {
				capturedQty = dsl.QtyFromNotional(ctx, 1000.0)
			}
		},
	}

	_ = runSpec(t, spec)

	if capturedQty <= 0 {
		t.Errorf("expected positive qty from QtyFromNotional, got %f", capturedQty)
	}
}

// TestQtyFromPctEquityCapped verifies capped sizing is positive and ≤ the
// uncapped version when cash is the binding constraint.
func TestQtyFromPctEquityCapped(t *testing.T) {
	var capped, uncapped float64

	spec := &dsl.Spec{
		Name:   "test-sizing-capped",
		Groups: []string{"test"},
		OnBar: func(ctx *backtest.BarContext) {
			if ctx.BarIndex() == 10 {
				capped = dsl.QtyFromPctEquityCapped(ctx, 0.95)
				uncapped = dsl.QtyFromPctEquity(ctx, 0.95)
			}
		},
	}

	_ = runSpec(t, spec)

	if capped <= 0 {
		t.Errorf("expected positive capped qty, got %f", capped)
	}
	if capped > uncapped+1e-9 {
		t.Errorf("capped (%f) should be ≤ uncapped (%f)", capped, uncapped)
	}
}
