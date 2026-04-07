package runtime

import (
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
func (m *mockBridge) Ind(name string) float64           { return m.closeVal }
func (m *mockBridge) IndAt(name string, o int) float64  { return m.closeVal }
