package runtime

import (
	"math"
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
)

func TestCandidatesSortsByScoreWithDeterministicTies(t *testing.T) {
	prog, errs := parser.Parse(`
items = []
items = candidates.add(items, candidates.new("MSFT", 80, 2))
items = candidates.add(items, candidates.new("NVDA", 90, 1))
items = candidates.add(items, candidates.new("AAPL", 90, 1))
items = candidates.add(items, candidates.new("TSLA", na, 0))
sorted = candidates.sort(items, "desc")
top = candidates.take(sorted, 2)
first = candidates.symbol(top[0])
second = candidates.symbol(top[1])
contains = candidates.contains_symbol(top, "AAPL")
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()

	first, _ := ip.Global.Get("first")
	second, _ := ip.Global.Get("second")
	if first.Str() != "AAPL" || second.Str() != "NVDA" {
		t.Fatalf("sorted candidates = %q, %q; want AAPL, NVDA", first.Str(), second.Str())
	}
	contains, _ := ip.Global.Get("contains")
	if !contains.Bool() {
		t.Fatal("top candidates should contain AAPL")
	}
}

func TestCandidatesTakeAndAccessorsHandleBounds(t *testing.T) {
	prog, errs := parser.Parse(`
items = [candidates.new("AAPL", 12, 3)]
top = candidates.take(items, 10)
symbol = candidates.symbol(top[0])
score = candidates.score(top[0])
secondary = candidates.secondary_score(top[0])
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()

	symbol, _ := ip.Global.Get("symbol")
	score, _ := ip.Global.Get("score")
	secondary, _ := ip.Global.Get("secondary")
	if symbol.Str() != "AAPL" || score.Float() != 12 || secondary.Float() != 3 {
		t.Fatalf("candidate fields = %q/%g/%g", symbol.Str(), score.Float(), secondary.Float())
	}
}

func TestCandidatesRejectsInvalidInput(t *testing.T) {
	ip := NewInterpreter(nil)
	var items diagnostics.List
	ip.Diagnostics = &items
	newCandidate, ok := ip.builtins["candidates.new"]
	if !ok {
		t.Fatal("candidates.new not registered")
	}
	value := newCandidate.FnPtr().Native([]Value{StringVal(""), FloatVal(math.NaN())})
	if !value.IsNa() {
		t.Fatalf("invalid candidate = %#v, want na", value)
	}
	if len(items) != 1 || items[0].Code != "dsl.invalid_candidate_argument" {
		t.Fatalf("diagnostics = %+v, want invalid argument", items)
	}
}

func TestCandidatesPreservesPayload(t *testing.T) {
	prog, errs := parser.Parse(`
item = candidates.new("AAPL", 12, 0, ["BUY_CALL", 2])
payload = candidates.payload(item)
name = payload[0]
qty = payload[1]
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()
	name, _ := ip.Global.Get("name")
	qty, _ := ip.Global.Get("qty")
	if name.Str() != "BUY_CALL" || qty.Float() != 2 {
		t.Fatalf("payload = %q/%g", name.Str(), qty.Float())
	}
}

func TestCandidatesRejectsInvalidSortAndCount(t *testing.T) {
	ip := NewInterpreter(nil)
	var items diagnostics.List
	ip.Diagnostics = &items
	candidate := ObjVal(strategyCandidate{Symbol: "AAPL", Score: 1})
	if got := ip.builtins["candidates.sort"].FnPtr().Native([]Value{ArrayVal([]Value{candidate}), StringVal("sideways")}); !got.IsNa() {
		t.Fatalf("invalid direction = %#v, want na", got)
	}
	if got := ip.builtins["candidates.take"].FnPtr().Native([]Value{ArrayVal([]Value{candidate}), FloatVal(1.5)}); !got.IsNa() {
		t.Fatalf("fractional count = %#v, want na", got)
	}
	if len(items) != 2 {
		t.Fatalf("diagnostics = %+v, want two errors", items)
	}
}

func TestArrayContainsUsesValueEquality(t *testing.T) {
	prog, errs := parser.Parse(`
symbols = ["AAPL", "MSFT"]
has_msft = array.contains(symbols, "MSFT")
has_nvda = array.contains(symbols, "NVDA")
has_nested = array.contains([[1, 2], [3]], [1, 2])
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	ip.Init()
	ip.OnBar()

	hasMSFT, _ := ip.Global.Get("has_msft")
	hasNVDA, _ := ip.Global.Get("has_nvda")
	hasNested, _ := ip.Global.Get("has_nested")
	if !hasMSFT.Bool() || hasNVDA.Bool() || !hasNested.Bool() {
		t.Fatalf("array.contains results = %v/%v/%v", hasMSFT.Bool(), hasNVDA.Bool(), hasNested.Bool())
	}
}
