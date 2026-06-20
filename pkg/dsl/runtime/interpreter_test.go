package runtime

import (
	"math"
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/parser"
)

func TestInterpreterArithmetic(t *testing.T) {
	src := "x = 2 + 3 * 4"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterMathBuiltins(ip)
	ip.Init()
	ip.OnBar()
	v, ok := ip.Global.Get("x")
	if !ok {
		t.Fatal("x not found")
	}
	if v.Float() != 14 {
		t.Errorf("expected 14, got %g", v.Float())
	}
}

func TestInterpreterVarPersist(t *testing.T) {
	src := "var count = 0\ncount := count + 1"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	ip.OnBar()
	ip.OnBar()
	v, _ := ip.Global.Get("count")
	if v.Float() != 3 {
		t.Errorf("expected 3, got %g", v.Float())
	}
}

func TestInterpreterIf(t *testing.T) {
	src := "x = 10\nif x > 5 {\n  y = 1\n} else {\n  y = 0\n}"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("y")
	if v.Float() != 1 {
		t.Errorf("expected 1, got %g", v.Float())
	}
}

func TestInterpreterPineIndentedIfElse(t *testing.T) {
	src := "x = 10\nif x > 5\n    y = 1\nelse\n    y = 0"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("y")
	if v.Float() != 1 {
		t.Errorf("expected 1, got %g", v.Float())
	}
}

func TestInterpreterPineTabIndentedIfElse(t *testing.T) {
	src := "x = 10\nif x > 5\n\ty = 1\nelse\n\ty = 0"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("y")
	if v.Float() != 1 {
		t.Errorf("expected 1, got %g", v.Float())
	}
}

func TestInterpreterPineNestedIndentedBlocks(t *testing.T) {
	src := "x = 10\ny = 0\nif x > 5\n    if x < 20\n        y = 2\nz = y + 1"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	y, _ := ip.Global.Get("y")
	if y.Float() != 2 {
		t.Errorf("expected y=2, got %g", y.Float())
	}
	z, _ := ip.Global.Get("z")
	if z.Float() != 3 {
		t.Errorf("expected z=3, got %g", z.Float())
	}
}

func TestInterpreterFor(t *testing.T) {
	src := "var sum = 0\nfor i = 1 to 5 {\n  sum := sum + i\n}"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("sum")
	if v.Float() != 15 {
		t.Errorf("expected 15, got %g", v.Float())
	}
}

func TestInterpreterPineIndentedFor(t *testing.T) {
	src := "var sum = 0\nfor i = 1 to 5\n    sum := sum + i\nout = sum"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("out")
	if v.Float() != 15 {
		t.Errorf("expected 15, got %g", v.Float())
	}
}

func TestInterpreterWhileAssignmentPersistsAcrossIterations(t *testing.T) {
	src := "i = 0\nout = 0\nwhile i < 3 {\n  out = i + 1\n  i = i + 1\n}"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("out")
	if v.Float() != 3 {
		t.Errorf("expected 3, got %g", v.Float())
	}
	i, _ := ip.Global.Get("i")
	if i.Float() != 3 {
		t.Errorf("expected i=3, got %g", i.Float())
	}
}

func TestNaBuiltinTreatsSeriesNaNAsNa(t *testing.T) {
	src := "x = na\nout = 0\nif na(x) {\n  out = 1\n}"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterMathBuiltins(ip)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("out")
	if v.Float() != 1 {
		t.Errorf("expected 1, got %g", v.Float())
	}
}

func TestInterpreterEqAssignUpdatesVaripStorage(t *testing.T) {
	src := "varip x = 0\nif 1 {\n  x = 7\n}\nout = x"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("out")
	if v.Float() != 7 {
		t.Fatalf("expected out=7, got %g", v.Float())
	}
	stored, ok := ip.varip["x"]
	if !ok || stored.Float() != 7 {
		t.Fatalf("expected varip x to persist as 7, got %#v (exists=%v)", stored, ok)
	}
}

func TestInterpreterFnCall(t *testing.T) {
	src := "fn double(x) {\n  return x * 2\n}\ny = double(21)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("y")
	if v.Float() != 42 {
		t.Errorf("expected 42, got %g", v.Float())
	}
}

func TestInterpreterPineArrowFnCall(t *testing.T) {
	src := "indicator(\"Arrow\")\ndouble(float x) => x * 2\ny = double(21)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("y")
	if v.Float() != 42 {
		t.Errorf("expected 42, got %g", v.Float())
	}
}

func TestInterpreterPineArrowFnIndentedBody(t *testing.T) {
	src := "bump(x) =>\n    y = x + 1\n    return y\nout = bump(2)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("out")
	if v.Float() != 3 {
		t.Errorf("expected 3, got %g", v.Float())
	}
}

func TestInterpreterPineTypedVarAndIncrement(t *testing.T) {
	src := "var float count = 0\ncount++\ncount--\ncount++"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("count")
	if v.Float() != 1 {
		t.Errorf("expected 1, got %g", v.Float())
	}
}

func TestInterpreterShortCircuitLogicalOps(t *testing.T) {
	src := "x = 0\nout = false and (1 / x > 0)\nout2 = true or (1 / x > 0)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	out, _ := ip.Global.Get("out")
	if out.Bool() {
		t.Fatalf("expected out=false")
	}
	out2, _ := ip.Global.Get("out2")
	if !out2.Bool() {
		t.Fatalf("expected out2=true")
	}
}

func TestInterpreterMathBuiltins(t *testing.T) {
	src := "x = math.abs(-5)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterMathBuiltins(ip)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("x")
	if v.Float() != 5 {
		t.Errorf("expected 5, got %g", v.Float())
	}
}

func TestInterpreterLenBuiltinOnArray(t *testing.T) {
	src := "arr = [1, 2, 3]\nout = len(arr)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("out")
	if v.Float() != 3 {
		t.Errorf("expected 3, got %g", v.Float())
	}
}

func TestInterpreterArrayConcatenation(t *testing.T) {
	src := "arr = [1, 2]\narr2 = arr + [3]\nout = len(arr2)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("out")
	if v.Float() != 3 {
		t.Errorf("expected 3, got %g", v.Float())
	}
	a, _ := ip.Global.Get("arr2")
	if len(a.Array()) != 3 || a.Array()[2].Float() != 3 {
		t.Fatalf("unexpected concatenated array: %#v", a.Array())
	}
}

func TestInterpreterArrayConcatenationSnapshotsLoopValues(t *testing.T) {
	src := "arr = [1, 2, 3]\nout = []\ni = 0\nwhile i < len(arr) {\n  x = arr[i]\n  if x < 3 {\n    out = out + [x]\n  }\n  i = i + 1\n}"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	out, _ := ip.Global.Get("out")
	if len(out.Array()) != 2 {
		t.Fatalf("expected 2 values, got %#v", out.Array())
	}
	if out.Array()[0].Float() != 1 || out.Array()[1].Float() != 2 {
		t.Fatalf("expected [1 2], got %#v", out.Array())
	}
	if out.Array()[0].Tag() == TagSeries || out.Array()[1].Tag() == TagSeries {
		t.Fatalf("expected snapshot values, got %#v", out.Array())
	}
}

func TestInterpreterArrayFilteringPreservesDistinctIndexedValues(t *testing.T) {
	src := "ids = [1, 2, 3]\nopen = []\ni = 0\nwhile i < len(ids) {\n  sid = ids[i]\n  if sid > 1 {\n    open = open + [sid]\n  }\n  i = i + 1\n}\nout0 = open[0]\nout1 = open[1]"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	open, _ := ip.Global.Get("open")
	if len(open.Array()) != 2 {
		t.Fatalf("expected filtered array len 2, got %#v", open.Array())
	}
	if open.Array()[0].Float() != 2 || open.Array()[1].Float() != 3 {
		t.Fatalf("expected [2 3], got %#v", open.Array())
	}
	if open.Array()[0].Tag() == TagSeries || open.Array()[1].Tag() == TagSeries {
		t.Fatalf("expected scalar snapshots, got %#v", open.Array())
	}
	if out0, _ := ip.Global.Get("out0"); out0.Float() != 2 {
		t.Fatalf("expected out0=2, got %g", out0.Float())
	}
	if out1, _ := ip.Global.Get("out1"); out1.Float() != 3 {
		t.Fatalf("expected out1=3, got %g", out1.Float())
	}
}

func TestInterpreterTernary(t *testing.T) {
	src := "x = 10\ny = x > 5 ? 1 : 0"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("y")
	if v.Float() != 1 {
		t.Errorf("expected 1, got %g", v.Float())
	}
}

func TestInputReturnsDefaultValue(t *testing.T) {
	src := `length = input(14, title="Length")
mult = input.float(2.0, title="Multiplier")`
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterInputBuiltins(ip)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("length")
	if v.Float() != 14 {
		t.Errorf("length: expected 14, got %g", v.Float())
	}
	m, _ := ip.Global.Get("mult")
	if m.Float() != 2.0 {
		t.Errorf("mult: expected 2.0, got %g", m.Float())
	}
}

func TestInputRespectsOverrides(t *testing.T) {
	src := `length = input(14, title="Length")`
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Inputs = map[string]float64{"Length": 30}
	RegisterInputBuiltins(ip)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("length")
	if v.Float() != 30 {
		t.Errorf("expected override 30, got %g", v.Float())
	}
}

func TestTABollingerBands(t *testing.T) {
	src := `bb = ta.bb(close, 3, 2.0)
upper = bb[0]
basis = bb[1]
lower = bb[2]
`
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterTABuiltins(ip)
	ip.Init()

	// Feed 5 bars via mock bridge: close values [10, 12, 14, 16, 18].
	for _, p := range []float64{10, 12, 14, 16, 18} {
		ip.Bridge = &mockBridge{closeVal: p}
		ip.OnBar()
	}
	// At bar 5, close.At(0)=18, At(1)=16, At(2)=14; basis = 16
	basisV, _ := ip.Global.Get("basis")
	if basisV.Float() != 16 {
		t.Errorf("basis: expected 16, got %g", basisV.Float())
	}
	upperV, _ := ip.Global.Get("upper")
	lowerV, _ := ip.Global.Get("lower")
	if upperV.Float() <= basisV.Float() {
		t.Errorf("upper band should be above basis: upper=%g basis=%g", upperV.Float(), basisV.Float())
	}
	if lowerV.Float() >= basisV.Float() {
		t.Errorf("lower band should be below basis: lower=%g basis=%g", lowerV.Float(), basisV.Float())
	}
}

func TestTAWMAComputesWeightedAverage(t *testing.T) {
	src := `wma3 = ta.wma(close, 3)`
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterTABuiltins(ip)
	ip.Init()

	for _, p := range []float64{10, 20, 30} {
		ip.Bridge = &mockBridge{closeVal: p}
		ip.OnBar()
	}
	// close: At(0)=30, At(1)=20, At(2)=10
	// WMA(3) = (30*3 + 20*2 + 10*1) / 6 = 140/6
	v, _ := ip.Global.Get("wma3")
	want := (30.0*3 + 20.0*2 + 10.0*1) / 6.0
	if v.Float() != want {
		t.Errorf("wma3: expected %g, got %g", want, v.Float())
	}
}

func TestTARSIUsesWilderSmoothing(t *testing.T) {
	src := `rsi = ta.rsi(close, 3)
rsi_again = ta.rsi(close, 3)`
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterTABuiltins(ip)
	ip.Init()

	for _, price := range []float64{10, 12, 11, 13, 14, 13} {
		ip.Bridge = &mockBridge{closeVal: price}
		ip.OnBar()
	}

	value, _ := ip.Global.Get("rsi")
	const want = 62.85714285714286
	if math.Abs(value.Float()-want) > 1e-9 {
		t.Fatalf("rsi = %.12f, want %.12f", value.Float(), want)
	}
	again, _ := ip.Global.Get("rsi_again")
	if math.Abs(again.Float()-want) > 1e-9 {
		t.Fatalf("second rsi call = %.12f, want %.12f", again.Float(), want)
	}
}

func TestTABarsSinceCountsCorrectly(t *testing.T) {
	src := `sig = close > 15
bs = ta.barssince(sig)
`
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	RegisterTABuiltins(ip)
	ip.Init()

	for _, p := range []float64{10, 20, 12, 13} {
		ip.Bridge = &mockBridge{closeVal: p}
		ip.OnBar()
	}
	// close: [10,20,12,13], sig: [false,true,false,false]
	// barssince: At(0)=false, At(1)=false, At(2)=true → 2 bars ago
	v, _ := ip.Global.Get("bs")
	if v.Float() != 2 {
		t.Errorf("barssince: expected 2, got %g", v.Float())
	}
}

// mockBridge is a minimal bridge stub for unit tests.
type mockBridge struct {
	closeVal float64
	barIdx   int
	orders   []OrderIntent
}

func (m *mockBridge) BarIndex() int                     { m.barIdx++; return m.barIdx }
func (m *mockBridge) Close() float64                    { return m.closeVal }
func (m *mockBridge) Open() float64                     { return m.closeVal }
func (m *mockBridge) High() float64                     { return m.closeVal }
func (m *mockBridge) Low() float64                      { return m.closeVal }
func (m *mockBridge) Volume() float64                   { return 1000 }
func (m *mockBridge) Field(n string) float64            { return m.closeVal }
func (m *mockBridge) FieldAt(n string, o int) float64   { return m.closeVal }
func (m *mockBridge) Buy(qty float64)                   {}
func (m *mockBridge) Sell(qty float64)                  {}
func (m *mockBridge) EntryLong(id string, qty float64)  {}
func (m *mockBridge) EntryShort(id string, qty float64) {}
func (m *mockBridge) ExitLong(id string)                {}
func (m *mockBridge) ExitShort(id string)               {}
func (m *mockBridge) PositionSize() float64             { return 0 }
func (m *mockBridge) PositionAvgPrice() float64         { return m.closeVal }
func (m *mockBridge) Equity() float64                   { return 100000 }
func (m *mockBridge) Cash() float64                     { return 100000 }
func (m *mockBridge) Ind(name string) float64           { return m.closeVal }
func (m *mockBridge) IndAt(name string, o int) float64  { return m.closeVal }
func (m *mockBridge) SubmitOrder(intent OrderIntent) int {
	m.orders = append(m.orders, intent)
	return len(m.orders)
}

func TestOrderSubmitBuildsStructuredIntent(t *testing.T) {
	src := "oid = order.submit(id=\"entry-1\", side=order.sell, qty=2, type=\"stop_limit\", limit=105, stop=100, immediate=true, note=\"protective\", ref=\"sig-1\", group_ref=\"cycle-1\", schedule_at=3)"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	bridge := &mockBridge{}
	ip := NewInterpreter(prog)
	ip.Bridge = bridge
	RegisterOrderBuiltins(ip)
	ip.Init()
	ip.OnBar()
	if len(bridge.orders) != 1 {
		t.Fatalf("expected 1 submitted order, got %d", len(bridge.orders))
	}
	got := bridge.orders[0]
	if got.ID != "entry-1" || got.Side != SideSell || got.Type != OrderStopLimit {
		t.Fatalf("unexpected order intent identity: %#v", got)
	}
	if got.Qty != 2 || got.LimitPrice != 105 || got.StopPrice != 100 {
		t.Fatalf("unexpected order prices/qty: %#v", got)
	}
	if !got.Immediate || got.Note != "protective" || got.Ref != "sig-1" || got.GroupRef != "cycle-1" || got.ScheduleAt != 3 {
		t.Fatalf("unexpected order metadata: %#v", got)
	}
	v, ok := ip.Global.Get("oid")
	if !ok || v.Float() != 1 {
		t.Fatalf("expected oid=1, got %#v (exists=%v)", v, ok)
	}
}

func TestBuiltinDocsIncludeCoreBacktestEntries(t *testing.T) {
	docs := BuiltinDocs(ProfileBacktest)
	byName := make(map[string]BuiltinDoc, len(docs))
	for _, doc := range docs {
		byName[doc.Name] = doc
	}

	for _, name := range []string{"strategy.entry", "order.submit", "request.security", "ta.sma", "strategy.long"} {
		doc, ok := byName[name]
		if !ok {
			t.Fatalf("expected builtin docs to include %q", name)
		}
		if doc.Summary == "" || doc.Example == "" || doc.ReturnValue == "" {
			t.Fatalf("expected complete doc metadata for %q, got %#v", name, doc)
		}
	}
}

func TestProfileBuiltinBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		want    []string
		deny    []string
	}{
		{
			name:    "indicator",
			profile: ProfileIndicator,
			want:    []string{"ta.sma", "math.abs", "strategy.entry", "input"},
			deny:    []string{"request.security", "order.submit", "options.chain", "portfolio.symbols"},
		},
		{
			name:    "backtest",
			profile: ProfileBacktest,
			want:    []string{"order.submit", "options.chain", "portfolio.symbols"},
			deny:    []string{"request.security"},
		},
		{
			name:    "alert",
			profile: ProfileAlert,
			want:    []string{"order.submit", "signal.active", "config.get"},
			deny:    []string{"request.security", "options.chain", "portfolio.symbols"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := NewInterpreter(nil)
			RegisterProfile(ip, tt.profile)
			for _, name := range tt.want {
				if _, ok := ip.builtins[name]; !ok {
					t.Fatalf("expected %s profile to include %q", tt.profile, name)
				}
			}
			for _, name := range tt.deny {
				if _, ok := ip.builtins[name]; ok {
					t.Fatalf("expected %s profile to exclude %q", tt.profile, name)
				}
			}
		})
	}
}

func TestBacktestProfileWithRequestProvidersIncludesRequestBuiltins(t *testing.T) {
	stub := func(args []Value) Value { return NaVal() }
	ip := NewInterpreter(nil)
	RegisterBacktestProfile(ip, stub, stub, stub)
	for _, name := range []string{"request.security", "request.factor", "request.fundamental"} {
		if _, ok := ip.builtins[name]; !ok {
			t.Fatalf("expected injected backtest profile to include %q", name)
		}
	}
}

func TestStrategyEntrySubmitsStructuredIntent(t *testing.T) {
	src := "strategy.entry(id=\"long\", direction=strategy.long, qty=3, note=\"pine\")"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	bridge := &mockBridge{}
	ip := NewInterpreter(prog)
	ip.Bridge = bridge
	RegisterStrategyBuiltins(ip)
	ip.Init()
	ip.OnBar()
	if len(bridge.orders) != 1 {
		t.Fatalf("expected 1 submitted order, got %d", len(bridge.orders))
	}
	got := bridge.orders[0]
	if got.ID != "long" || got.Note != "pine" || got.Side != SideBuy || got.Type != OrderMarket || got.Qty != 3 {
		t.Fatalf("unexpected strategy.entry order intent: %#v", got)
	}
}

func TestStrategyEntryShortStopLimitIntent(t *testing.T) {
	src := "strategy.entry(id=\"short\", direction=strategy.short, qty=2, limit=90, stop=95, immediate=true, note=\"risk\")"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	bridge := &mockBridge{}
	ip := NewInterpreter(prog)
	ip.Bridge = bridge
	RegisterStrategyBuiltins(ip)
	ip.Init()
	ip.OnBar()
	if len(bridge.orders) != 1 {
		t.Fatalf("expected 1 submitted order, got %d", len(bridge.orders))
	}
	got := bridge.orders[0]
	if got.ID != "short" || got.Side != SideSell || got.Type != OrderStopLimit || got.Qty != 2 {
		t.Fatalf("unexpected strategy.entry identity: %#v", got)
	}
	if got.LimitPrice != 90 || got.StopPrice != 95 || !got.Immediate || got.Note != "risk" {
		t.Fatalf("unexpected strategy.entry advanced fields: %#v", got)
	}
}
