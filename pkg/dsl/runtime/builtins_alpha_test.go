package runtime

import (
	"math"
	"testing"
)

// TestAlphaWindowReducersSkipNaN guards against a regression where a single
// NaN observation (a warm-up bar or data gap) inside the window poisoned
// min/max/skewness/kurtosis, silently turning a valid rolling statistic into
// NaN and killing any downstream condition built on it.
func TestAlphaWindowReducersSkipNaN(t *testing.T) {
	ip := NewInterpreter(nil)
	RegisterAlphaBuiltins(ip)

	s := NewSeries()
	// Oldest first: a NaN warm-up bar followed by five valid observations.
	for _, v := range []float64{math.NaN(), 1, 2, 3, 4, 5} {
		s.Append(v)
	}
	series := SeriesVal(s)
	window := FloatVal(6)

	call := func(name string) Value {
		fn, ok := ip.builtins[name]
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		return fn.FnPtr().Native([]Value{series, window})
	}

	if got := call("alpha.ts_min"); got.IsNa() || got.Float() != 1 {
		t.Fatalf("alpha.ts_min = %#v, want 1 (NaN bar should be skipped)", got)
	}
	if got := call("alpha.ts_max"); got.IsNa() || got.Float() != 5 {
		t.Fatalf("alpha.ts_max = %#v, want 5 (NaN bar should be skipped)", got)
	}
	if got := call("alpha.ts_mean"); got.IsNa() || got.Float() != 3 {
		t.Fatalf("alpha.ts_mean = %#v, want 3 (NaN bar should be skipped)", got)
	}
	if got := call("alpha.ts_skewness"); got.IsNa() {
		t.Fatal("alpha.ts_skewness should not be na when 5 valid observations remain after excluding one NaN bar")
	}
	if got := call("alpha.ts_kurtosis"); got.IsNa() {
		t.Fatal("alpha.ts_kurtosis should not be na when 5 valid observations remain after excluding one NaN bar")
	}
}

// TestAlphaTsMinMaxNaNAtMostRecentBar guards the specific regression where
// the *most recent* bar (index 0, used to seed the min/max accumulator) was
// NaN: the old implementation seeded the accumulator with vals[0] and every
// `v < m`/`v > m` comparison against NaN is false, so the loop returned NaN
// even though four valid values existed in the window.
func TestAlphaTsMinMaxNaNAtMostRecentBar(t *testing.T) {
	ip := NewInterpreter(nil)
	RegisterAlphaBuiltins(ip)

	s := NewSeries()
	for _, v := range []float64{1, 2, 3, 4, math.NaN()} { // most recent (last appended) is NaN
		s.Append(v)
	}
	series := SeriesVal(s)
	window := FloatVal(5)

	minFn := ip.builtins["alpha.ts_min"].FnPtr().Native([]Value{series, window})
	if minFn.IsNa() || minFn.Float() != 1 {
		t.Fatalf("alpha.ts_min = %#v, want 1", minFn)
	}
	maxFn := ip.builtins["alpha.ts_max"].FnPtr().Native([]Value{series, window})
	if maxFn.IsNa() || maxFn.Float() != 4 {
		t.Fatalf("alpha.ts_max = %#v, want 4", maxFn)
	}
}

// TestAlphaRankExcludesNaNFromDenominator guards against alpha.rank being
// biased low during warm-up by counting unfilled (NaN) history bars in the
// denominator.
func TestAlphaRankExcludesNaNFromDenominator(t *testing.T) {
	ip := NewInterpreter(nil)
	RegisterAlphaBuiltins(ip)

	s := NewSeries()
	for _, v := range []float64{math.NaN(), math.NaN(), math.NaN(), 1, 2, 3} {
		s.Append(v)
	}
	got := ip.builtins["alpha.rank"].FnPtr().Native([]Value{SeriesVal(s)})
	// Current value (3) is the max of the 3 valid observations {1,2,3}:
	// 2 of 3 are strictly less, so rank should be 2/3, not 2/6.
	want := 2.0 / 3.0
	if got.IsNa() || math.Abs(got.Float()-want) > 1e-9 {
		t.Fatalf("alpha.rank = %#v, want %g", got, want)
	}
}

func TestAlphaArgExtremeReturnsRawBarOffsetAcrossNaNGap(t *testing.T) {
	ip := NewInterpreter(nil)
	RegisterAlphaBuiltins(ip)

	s := NewSeries()
	for _, value := range []float64{1, 9, math.NaN(), 4} {
		s.Append(value)
	}
	args := []Value{SeriesVal(s), FloatVal(4)}

	argMin := ip.builtins["alpha.ts_argmin"].FnPtr().Native(args)
	if argMin.IsNa() || argMin.Float() != 3 {
		t.Fatalf("alpha.ts_argmin = %#v, want raw bar offset 3", argMin)
	}
	argMax := ip.builtins["alpha.ts_argmax"].FnPtr().Native(args)
	if argMax.IsNa() || argMax.Float() != 2 {
		t.Fatalf("alpha.ts_argmax = %#v, want raw bar offset 2", argMax)
	}
}
