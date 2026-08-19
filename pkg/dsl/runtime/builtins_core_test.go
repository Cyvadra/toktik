package runtime

import (
	"math"
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/parser"
)

func TestArrayPercentileUsesLinearInterpolationAndIgnoresInvalidValues(t *testing.T) {
	prog, errs := parser.Parse(`
values = [40, na, 10, 20, 30]
p25 = array.percentile(values, 25)
first = values[0]
empty = array.percentile([], 50)
invalid = array.percentile([1], 101)
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()

	p25, _ := ip.Global.Get("p25")
	if p25.Float() != 17.5 {
		t.Fatalf("array.percentile(..., 25) = %v, want 17.5", p25.Float())
	}
	first, _ := ip.Global.Get("first")
	if first.Float() != 40 {
		t.Fatalf("array.percentile modified input: first = %v, want 40", first.Float())
	}
	empty, _ := ip.Global.Get("empty")
	invalid, _ := ip.Global.Get("invalid")
	if !math.IsNaN(empty.Float()) || !math.IsNaN(invalid.Float()) {
		t.Fatalf("invalid percentile results = %v/%v, want na/na", empty, invalid)
	}
}

func TestSnapshotDetachesCurrentSeriesValue(t *testing.T) {
	prog, errs := parser.Parse(`
varip clock = 0
clock += 1
varip captured = na
if clock == 2
	captured := snapshot(clock)
out = captured
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	ip.OnBar()
	ip.OnBar()

	out, _ := ip.Global.Get("out")
	if out.Float() != 2 {
		t.Fatalf("snapshot value = %v, want 2", out.Float())
	}
}
