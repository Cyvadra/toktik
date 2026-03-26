package backtest

import (
	"math"
	"testing"
)

func TestQuantileRollingMedian(t *testing.T) {
	ind := Quantile("x", 3, 0.5)
	out := ind.Compute(map[string][]float64{
		"x": {1, 2, 3, 4, 5},
	})

	if !math.IsNaN(out[0]) || !math.IsNaN(out[1]) {
		t.Fatalf("expected warmup NaN values, got %#v", out[:2])
	}
	if out[2] != 2 || out[3] != 3 || out[4] != 4 {
		t.Fatalf("unexpected rolling median result: %#v", out)
	}
}

func TestBollingerOutputs(t *testing.T) {
	mi := Bollinger("x", 3, 2)
	outs := mi.ComputeMulti("bb", map[string][]float64{
		"x": {1, 2, 3, 4, 5},
	})

	mid := outs["bb"]
	up := outs["bb_upper"]
	lo := outs["bb_lower"]

	if len(mid) != 5 || len(up) != 5 || len(lo) != 5 {
		t.Fatalf("unexpected output lengths")
	}
	if !math.IsNaN(mid[0]) || !math.IsNaN(mid[1]) {
		t.Fatalf("expected warmup NaN in middle band")
	}
	if mid[2] != 2 || mid[3] != 3 || mid[4] != 4 {
		t.Fatalf("unexpected middle band values: %#v", mid)
	}
	if !(up[2] > mid[2] && lo[2] < mid[2]) {
		t.Fatalf("expected upper > mid > lower at first valid bar")
	}
}

func TestDonchianOutputs(t *testing.T) {
	mi := Donchian("h", "l", 3)
	outs := mi.ComputeMulti("dc", map[string][]float64{
		"h": {10, 12, 11, 13, 12},
		"l": {8, 9, 7, 10, 9},
	})

	upper := outs["dc_upper"]
	lower := outs["dc_lower"]
	mid := outs["dc"]

	if !math.IsNaN(upper[0]) || !math.IsNaN(lower[1]) {
		t.Fatalf("expected warmup NaN values")
	}
	if upper[2] != 12 || lower[2] != 7 || mid[2] != 9.5 {
		t.Fatalf("unexpected donchian values at index 2: upper=%v lower=%v mid=%v", upper[2], lower[2], mid[2])
	}
	if upper[3] != 13 || lower[3] != 7 || mid[3] != 10 {
		t.Fatalf("unexpected donchian values at index 3: upper=%v lower=%v mid=%v", upper[3], lower[3], mid[3])
	}
}

func TestWMA(t *testing.T) {
	ind := WMA("x", 3)
	out := ind.Compute(map[string][]float64{
		"x": {1, 2, 3, 4, 5},
	})

	if !math.IsNaN(out[0]) || !math.IsNaN(out[1]) {
		t.Fatalf("expected warmup NaN")
	}
	if out[2] != (1*1+2*2+3*3)/6.0 {
		t.Fatalf("unexpected wma at 2: %v", out[2])
	}
	if out[4] != (3*1+4*2+5*3)/6.0 {
		t.Fatalf("unexpected wma at 4: %v", out[4])
	}
}

func TestVWMA(t *testing.T) {
	ind := VWMA("p", "v", 3)
	out := ind.Compute(map[string][]float64{
		"p": {1, 2, 3, 4},
		"v": {1, 1, 2, 2},
	})

	if !math.IsNaN(out[0]) || !math.IsNaN(out[1]) {
		t.Fatalf("expected warmup NaN")
	}
	want2 := (1*1 + 2*1 + 3*2) / 4.0
	if out[2] != want2 {
		t.Fatalf("unexpected vwma at 2: got %v want %v", out[2], want2)
	}
}

func TestEMASeedsFromFirstValueLikeTradingView(t *testing.T) {
	ind := EMA("x", 3)
	out := ind.Compute(map[string][]float64{
		"x": {10, 11, 12, 13},
	})

	want := []float64{10, 10.5, 11.25, 12.125}
	for i := range want {
		if math.Abs(out[i]-want[i]) > 1e-9 {
			t.Fatalf("unexpected ema at %d: got %v want %v", i, out[i], want[i])
		}
	}
}

func TestEMAHandlesLeadingAndInternalNaNs(t *testing.T) {
	ind := EMA("x", 3)
	out := ind.Compute(map[string][]float64{
		"x": {math.NaN(), math.NaN(), 10, 11, math.NaN(), 13},
	})

	if !math.IsNaN(out[0]) || !math.IsNaN(out[1]) {
		t.Fatalf("expected leading NaN values, got %#v", out[:2])
	}
	want := []float64{10, 10.5, 10.5, 11.75}
	for i, idx := range []int{2, 3, 4, 5} {
		if math.Abs(out[idx]-want[i]) > 1e-9 {
			t.Fatalf("unexpected ema at %d: got %v want %v", idx, out[idx], want[i])
		}
	}
}

func TestATR(t *testing.T) {
	ind := ATR(3)
	out := ind.Compute(map[string][]float64{
		"high":  {11, 12, 13, 14, 15},
		"low":   {10, 11, 12, 13, 14},
		"close": {10.5, 11.5, 12.5, 13.5, 14.5},
	})

	if !math.IsNaN(out[0]) || !math.IsNaN(out[1]) {
		t.Fatalf("expected warmup NaN for ATR")
	}
	if out[4] <= 0 {
		t.Fatalf("expected positive ATR, got %v", out[4])
	}
}

func TestStochasticOutputs(t *testing.T) {
	mi := Stochastic("h", "l", "c", 3, 2, 2)
	outs := mi.ComputeMulti("st", map[string][]float64{
		"h": {10, 11, 12, 13, 14, 15},
		"l": {8, 9, 10, 11, 12, 13},
		"c": {9, 10, 11, 12, 13, 14},
	})

	k := outs["st"]
	d := outs["st_d"]
	raw := outs["st_raw"]

	if len(k) != 6 || len(d) != 6 || len(raw) != 6 {
		t.Fatalf("unexpected stochastic output lengths")
	}
	if raw[5] < 0 || raw[5] > 100 {
		t.Fatalf("raw stochastic should be in [0,100], got %v", raw[5])
	}
	if k[5] < 0 || k[5] > 100 || d[5] < 0 || d[5] > 100 {
		t.Fatalf("smoothed stochastic should be in [0,100], got k=%v d=%v", k[5], d[5])
	}
}

func TestCCI(t *testing.T) {
	ind := CCI("h", "l", "c", 3)
	out := ind.Compute(map[string][]float64{
		"h": {10, 11, 12, 13, 14, 15},
		"l": {8, 9, 10, 11, 12, 13},
		"c": {9, 10, 11, 12, 13, 14},
	})

	if !math.IsNaN(out[1]) {
		t.Fatalf("expected warmup NaN")
	}
	if math.IsNaN(out[5]) || math.IsInf(out[5], 0) {
		t.Fatalf("expected finite CCI at tail, got %v", out[5])
	}
}

func TestADXOutputs(t *testing.T) {
	mi := ADX(3)
	outs := mi.ComputeMulti("adx", map[string][]float64{
		"high":  {11, 12, 13, 14, 15, 16, 17},
		"low":   {10, 11, 12, 13, 14, 15, 16},
		"close": {10.5, 11.5, 12.5, 13.5, 14.5, 15.5, 16.5},
	})

	adx := outs["adx"]
	plus := outs["adx_plus_di"]
	minus := outs["adx_minus_di"]

	if len(adx) != 7 || len(plus) != 7 || len(minus) != 7 {
		t.Fatalf("unexpected ADX output lengths")
	}
	if adx[6] < 0 || adx[6] > 100 {
		t.Fatalf("adx should be in [0,100], got %v", adx[6])
	}
}

func TestOBV(t *testing.T) {
	ind := OBV("c", "v")
	out := ind.Compute(map[string][]float64{
		"c": {10, 11, 10, 10, 12},
		"v": {100, 110, 120, 130, 140},
	})

	if out[0] != 100 {
		t.Fatalf("unexpected obv[0]: %v", out[0])
	}
	if out[1] != 210 { // up day
		t.Fatalf("unexpected obv[1]: %v", out[1])
	}
	if out[2] != 90 { // down day
		t.Fatalf("unexpected obv[2]: %v", out[2])
	}
	if out[3] != 90 { // flat day
		t.Fatalf("unexpected obv[3]: %v", out[3])
	}
}
