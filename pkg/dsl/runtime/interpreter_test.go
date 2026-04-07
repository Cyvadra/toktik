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
