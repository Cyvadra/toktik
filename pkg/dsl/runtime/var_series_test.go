package runtime

import (
	"math"
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/parser"
)

// TestCompoundAssignDivideByZero verifies that /= and %= by zero yield na,
// matching the binary / and % operators instead of leaking Inf/NaN silently.
func TestCompoundAssignDivideByZero(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"slash_eq", "x = 10\nx /= 0\nout = x"},
		{"percent_eq", "x = 10\nx %= 0\nout = x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, errs := parser.Parse(tc.src)
			if len(errs) > 0 {
				t.Fatal(errs)
			}
			ip := NewInterpreter(prog)
			ip.Init()
			ip.OnBar()
			v, _ := ip.Global.Get("out")
			if math.IsInf(v.Float(), 0) {
				t.Fatalf("expected na (NaN) after divide-by-zero, got Inf")
			}
			if !math.IsNaN(v.Float()) {
				t.Fatalf("expected na (NaN) after divide-by-zero, got %v", v.Float())
			}
		})
	}
}

// TestVarHistorySubscript verifies that a var accumulator is series-backed so
// history subscript (x[n]) resolves the previous bar's value.
func TestVarHistorySubscript(t *testing.T) {
	src := "var counter = 0\ncounter := counter + 1\nprev = counter[1]"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar() // counter=1, prev=na
	ip.OnBar() // counter=2, prev=1
	ip.OnBar() // counter=3, prev=2

	cur, _ := ip.Global.Get("counter")
	if cur.Float() != 3 {
		t.Fatalf("counter = %g, want 3", cur.Float())
	}
	prev, _ := ip.Global.Get("prev")
	if prev.Float() != 2 {
		t.Fatalf("counter[1] = %g, want 2", prev.Float())
	}
}

// TestVaripHistorySubscript verifies varip accumulators are also series-backed.
func TestVaripHistorySubscript(t *testing.T) {
	src := "varip total = 0\ntotal := total + 2\nprev = total[1]"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar() // total=2, prev=na
	ip.OnBar() // total=4, prev=2

	cur, _ := ip.Global.Get("total")
	if cur.Float() != 4 {
		t.Fatalf("total = %g, want 4", cur.Float())
	}
	prev, _ := ip.Global.Get("prev")
	if prev.Float() != 2 {
		t.Fatalf("total[1] = %g, want 2", prev.Float())
	}
}

// TestReassignToStringKeepsValue verifies that reassigning a previously
// series-backed numeric variable to a string does not clobber the string with
// a series wrapper (i.e. non-scalar reassignments are not series-backed).
func TestReassignToStringKeepsValue(t *testing.T) {
	src := "x = 5\nx := \"hi\"\nout = x"
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	v, _ := ip.Global.Get("x")
	if v.Str() != "hi" {
		t.Fatalf("x = %q, want \"hi\"", v.Str())
	}
}
