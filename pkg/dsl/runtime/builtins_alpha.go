package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

import (
	"math"
	"sort"
)

// RegisterAlphaBuiltins adds WorldQuant-style Alpha operators:
// rank, zscore, decay_linear, ts_rank, ts_corr, ts_cov, ts_delta,
// ts_sum, ts_mean, ts_std, ts_min, ts_max, ts_argmin, ts_argmax,
// ts_skewness, ts_kurtosis, signed_power, scale, log_return.
//
// NOTE ON `alpha.rank`: this interpreter executes one bar/one asset at a
// time, so there is no cross-sectional universe available at the point this
// builtin runs. `alpha.rank` therefore ranks the current value within its
// own time-series history (a temporal percentile rank), NOT across assets as
// in the original WorldQuant definition. Callers porting published alphas
// must account for this deviation; a genuine cross-sectional rank requires
// pre-computing scores for the full universe (see candidates.* builtins) and
// ranking that collection instead.
func RegisterAlphaBuiltins(ip *Interpreter) {

	// alpha.rank(x) — temporal percentile rank (0..1) of the current value
	// within its own series history, excluding NaN observations from both
	// the current-value check and the denominator.
	ip.RegisterBuiltin("alpha.rank", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		if args[0].tag != TagSeries || args[0].series == nil {
			return FloatVal(0.5) // scalar → middle rank
		}
		s := args[0].series
		cur := s.Current()
		if math.IsNaN(cur) {
			return NaVal()
		}
		n := s.Len()
		validCount, lessCount := 0, 0
		for i := 0; i < n; i++ {
			v := s.At(i)
			if math.IsNaN(v) {
				continue
			}
			validCount++
			if v < cur {
				lessCount++
			}
		}
		if validCount == 0 {
			return NaVal()
		}
		return FloatVal(float64(lessCount) / float64(validCount))
	})

	// alpha.zscore(x, window) — z-score over rolling window.
	ip.RegisterBuiltin("alpha.zscore", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		cur := s.Current()
		if math.IsNaN(cur) {
			return NaVal()
		}
		vals := lastN(s, w)
		if len(vals) < 2 {
			return NaVal()
		}
		m := mean(vals)
		sd := stddev(vals, m)
		if sd == 0 {
			return FloatVal(0)
		}
		return FloatVal((cur - m) / sd)
	})

	// alpha.decay_linear(x, window) — weighted average with linearly decaying
	// weights. Most recent bar gets weight=window, next gets window-1, ...,
	// oldest gets 1. NaN observations are skipped from both the sum and the
	// weight total so a gap does not bias the average toward zero.
	ip.RegisterBuiltin("alpha.decay_linear", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		rawVals := rawLastN(s, w)
		n := len(rawVals)
		if n == 0 {
			return NaVal()
		}
		sum, wsum := 0.0, 0.0
		for i := 0; i < n; i++ {
			if math.IsNaN(rawVals[i]) {
				continue
			}
			weight := float64(n - i) // most recent = highest weight
			sum += rawVals[i] * weight
			wsum += weight
		}
		if wsum == 0 {
			return NaVal()
		}
		return FloatVal(sum / wsum)
	})

	// alpha.ts_rank(x, window) — percentile rank of the current value within
	// the window, excluding NaN observations from the denominator.
	ip.RegisterBuiltin("alpha.ts_rank", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		cur := s.Current()
		if math.IsNaN(cur) {
			return NaVal()
		}
		vals := lastN(s, w) // NaN-filtered; cur is included since it is non-NaN.
		n := len(vals)
		if n == 0 {
			return NaVal()
		}
		count := 0
		for _, v := range vals {
			if v < cur {
				count++
			}
		}
		return FloatVal(float64(count) / float64(n))
	})

	// alpha.ts_corr(x, y, window) — rolling Pearson correlation.
	ip.RegisterBuiltin("alpha.ts_corr", func(args []Value) Value {
		if len(args) < 3 {
			return NaVal()
		}
		sx, sy := extractTwoSeries(args)
		w := int(args[2].Float())
		if sx == nil || sy == nil || w <= 0 {
			return NaVal()
		}
		xv, yv := pairedLastN(sx, sy, w)
		if len(xv) < 2 {
			return NaVal()
		}
		return FloatVal(pearson(xv, yv))
	})

	// alpha.ts_cov(x, y, window) — rolling covariance.
	ip.RegisterBuiltin("alpha.ts_cov", func(args []Value) Value {
		if len(args) < 3 {
			return NaVal()
		}
		sx, sy := extractTwoSeries(args)
		w := int(args[2].Float())
		if sx == nil || sy == nil || w <= 0 {
			return NaVal()
		}
		xv, yv := pairedLastN(sx, sy, w)
		if len(xv) < 2 {
			return NaVal()
		}
		return FloatVal(cov(xv, yv))
	})

	// alpha.ts_delta(x, period) — x - x[period].
	ip.RegisterBuiltin("alpha.ts_delta", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, p := extractSeriesWindow(args)
		if s == nil || p <= 0 || s.Len() <= p {
			return NaVal()
		}
		cur, prev := s.Current(), s.At(p)
		if math.IsNaN(cur) || math.IsNaN(prev) {
			return NaVal()
		}
		return FloatVal(cur - prev)
	})

	// alpha.log_return(x, period) — ln(x / x[period]).
	ip.RegisterBuiltin("alpha.log_return", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, p := extractSeriesWindow(args)
		if s == nil || p <= 0 || s.Len() <= p {
			return NaVal()
		}
		cur, prev := s.Current(), s.At(p)
		if math.IsNaN(cur) || math.IsNaN(prev) || prev == 0 {
			return NaVal()
		}
		return FloatVal(math.Log(cur / prev))
	})

	// alpha.signed_power(x, exp) — sign(x) * |x|^exp
	ip.RegisterBuiltin("alpha.signed_power", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		x, e := args[0].Float(), args[1].Float()
		if math.IsNaN(x) {
			return NaVal()
		}
		return FloatVal(math.Copysign(math.Pow(math.Abs(x), e), x))
	})

	// alpha.scale(x, target_sum) — scale so |values| sum to target_sum.
	// For single-asset: returns x * target_sum / |x| (i.e. sign(x)*target_sum).
	ip.RegisterBuiltin("alpha.scale", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		x := args[0].Float()
		target := 1.0
		if len(args) >= 2 {
			target = args[1].Float()
		}
		if math.IsNaN(x) || x == 0 {
			return NaVal()
		}
		return FloatVal(x / math.Abs(x) * target)
	})

	// Simple windowed reducers over NaN-filtered values, sharing one
	// validate-extract-reduce skeleton instead of duplicating it per builtin.
	registerWindowReducer(ip, "alpha.ts_sum", 1, sum)
	registerWindowReducer(ip, "alpha.ts_mean", 1, mean)
	registerWindowReducer(ip, "alpha.ts_std", 2, func(vals []float64) float64 { return stddev(vals, mean(vals)) })
	registerWindowReducer(ip, "alpha.ts_min", 1, minOf)
	registerWindowReducer(ip, "alpha.ts_max", 1, maxOf)
	registerWindowReducer(ip, "alpha.ts_median", 1, median)
	registerWindowReducer(ip, "alpha.ts_skewness", 3, skewness)
	registerWindowReducer(ip, "alpha.ts_kurtosis", 4, kurtosis)

	// Index-returning reducers (0 = most recent within the NaN-filtered window).
	registerWindowIndexReducer(ip, "alpha.ts_argmin", func(a, b float64) bool { return a < b })
	registerWindowIndexReducer(ip, "alpha.ts_argmax", func(a, b float64) bool { return a > b })
}

// --- registration helpers ---

// registerWindowReducer registers a builtin of the form fn(series, window)
// that reduces a NaN-filtered rolling window to a single float. minLen is
// the minimum number of non-NaN observations required to produce a result.
func registerWindowReducer(ip *Interpreter, name string, minLen int, reduce func([]float64) float64) {
	ip.RegisterBuiltin(name, func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		if len(vals) < minLen {
			return NaVal()
		}
		return FloatVal(reduce(vals))
	})
}

// registerWindowIndexReducer registers a builtin of the form fn(series,
// window) that returns the index (0 = most recent) of the extreme value
// within a NaN-filtered rolling window, per the better(candidate, current)
// comparator.
func registerWindowIndexReducer(ip *Interpreter, name string, better func(candidate, current float64) bool) {
	ip.RegisterBuiltin(name, func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		if len(vals) == 0 {
			return NaVal()
		}
		idx := 0
		for i := 1; i < len(vals); i++ {
			if better(vals[i], vals[idx]) {
				idx = i
			}
		}
		return FloatVal(float64(idx))
	})
}

// --- extraction helpers ---

func extractSeriesWindow(args []Value) (*Series, int) {
	var s *Series
	if args[0].tag == TagSeries && args[0].series != nil {
		s = args[0].series
	}
	w := int(args[1].Float())
	return s, w
}

func extractTwoSeries(args []Value) (*Series, *Series) {
	var sx, sy *Series
	if args[0].tag == TagSeries && args[0].series != nil {
		sx = args[0].series
	}
	if args[1].tag == TagSeries && args[1].series != nil {
		sy = args[1].series
	}
	return sx, sy
}

// rawLastN returns the last n raw values from a series, most-recent first,
// including any NaN observations. Use this when a caller needs to weight or
// index bars positionally (e.g. decay_linear, where a gap's position still
// matters even though its value is skipped).
func rawLastN(s *Series, n int) []float64 {
	out := make([]float64, 0, n)
	for i := 0; i < n && i < s.Len(); i++ {
		out = append(out, s.At(i))
	}
	return out
}

// lastN returns up to n most-recent non-NaN values from a series
// (most-recent first). NaN observations (warm-up bars, gaps) are excluded
// so a single missing bar cannot poison an otherwise-valid reducer (min,
// max, mean, skewness, ...). Note this is intentionally distinct from
// Series.Last, which returns raw (oldest-first, NaN-inclusive) history for
// callers that need bar-aligned data.
func lastN(s *Series, n int) []float64 {
	raw := rawLastN(s, n)
	out := make([]float64, 0, len(raw))
	for _, v := range raw {
		if !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	return out
}

// pairedLastN aligns the last min(len(x), len(y)) NaN-filtered observations
// from two raw (positionally-aligned) windows so correlation/covariance
// only consider bars where both series have a valid value.
func pairedLastN(sx, sy *Series, w int) ([]float64, []float64) {
	xv := rawLastN(sx, w)
	yv := rawLastN(sy, w)
	n := len(xv)
	if len(yv) < n {
		n = len(yv)
	}
	outX := make([]float64, 0, n)
	outY := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(xv[i]) || math.IsNaN(yv[i]) {
			continue
		}
		outX = append(outX, xv[i])
		outY = append(outY, yv[i])
	}
	return outX, outY
}

// --- statistics helpers ---

func sum(vals []float64) float64 {
	total := 0.0
	for _, v := range vals {
		total += v
	}
	return total
}

func minOf(vals []float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxOf(vals []float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func median(vals []float64) float64 {
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func skewness(vals []float64) float64 {
	n := len(vals)
	m := mean(vals)
	sd := stddev(vals, m)
	if sd == 0 {
		return 0
	}
	total := 0.0
	for _, v := range vals {
		d := (v - m) / sd
		total += d * d * d
	}
	return total / float64(n)
}

func kurtosis(vals []float64) float64 {
	n := len(vals)
	m := mean(vals)
	sd := stddev(vals, m)
	if sd == 0 {
		return 0
	}
	total := 0.0
	for _, v := range vals {
		d := (v - m) / sd
		total += d * d * d * d
	}
	return total/float64(n) - 3 // excess kurtosis
}

// mean/stddev accept possibly-NaN-containing slices for callers (zscore,
// decay_linear) that may pass in a mixed window; windowed reducers
// registered via registerWindowReducer already receive NaN-free input from
// lastN, so the NaN checks below are a defensive no-op in that path.
func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return math.NaN()
	}
	sum := 0.0
	count := 0
	for _, v := range vals {
		if !math.IsNaN(v) {
			sum += v
			count++
		}
	}
	if count == 0 {
		return math.NaN()
	}
	return sum / float64(count)
}

func stddev(vals []float64, m float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	sum := 0.0
	count := 0
	for _, v := range vals {
		if !math.IsNaN(v) {
			d := v - m
			sum += d * d
			count++
		}
	}
	if count < 2 {
		return 0
	}
	return math.Sqrt(sum / float64(count-1))
}

func pearson(x, y []float64) float64 {
	n := len(x)
	if n < 2 || n != len(y) {
		return math.NaN()
	}
	mx, my := mean(x), mean(y)
	covar, sx, sy := 0.0, 0.0, 0.0
	for i := 0; i < n; i++ {
		dx := x[i] - mx
		dy := y[i] - my
		covar += dx * dy
		sx += dx * dx
		sy += dy * dy
	}
	d := math.Sqrt(sx * sy)
	if d == 0 {
		return 0
	}
	return covar / d
}

func cov(x, y []float64) float64 {
	n := len(x)
	if n < 2 || n != len(y) {
		return math.NaN()
	}
	mx, my := mean(x), mean(y)
	total := 0.0
	for i := 0; i < n; i++ {
		total += (x[i] - mx) * (y[i] - my)
	}
	return total / float64(n-1)
}
