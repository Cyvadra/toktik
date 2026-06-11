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
func RegisterAlphaBuiltins(ip *Interpreter) {

	// rank(x) — cross-sectional rank (0..1) of current value within its own series history.
	// In a single-asset context, this is the percentile rank within the last N bars.
	// We use the full series history available.
	ip.RegisterBuiltin("alpha.rank", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		if args[0].tag == TagSeries && args[0].series != nil {
			s := args[0].series
			n := s.Len()
			if n == 0 {
				return NaVal()
			}
			cur := s.Current()
			if math.IsNaN(cur) {
				return NaVal()
			}
			count := 0
			for i := 0; i < n; i++ {
				v := s.At(i)
				if !math.IsNaN(v) && v < cur {
					count++
				}
			}
			return FloatVal(float64(count) / float64(n))
		}
		return FloatVal(0.5) // scalar → middle rank
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
		vals := lastN(s, w)
		if len(vals) < 2 {
			return NaVal()
		}
		mean := mean(vals)
		std := stddev(vals, mean)
		if std == 0 {
			return FloatVal(0)
		}
		return FloatVal((s.Current() - mean) / std)
	})

	// alpha.decay_linear(x, window) — weighted average with linearly decaying weights.
	// Most recent bar gets weight=window, next gets window-1, ..., oldest gets 1.
	ip.RegisterBuiltin("alpha.decay_linear", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		n := len(vals)
		if n == 0 {
			return NaVal()
		}
		sum := 0.0
		wsum := 0.0
		for i := 0; i < n; i++ {
			weight := float64(n - i) // most recent = highest weight
			if math.IsNaN(vals[i]) {
				continue
			}
			sum += vals[i] * weight
			wsum += weight
		}
		if wsum == 0 {
			return NaVal()
		}
		return FloatVal(sum / wsum)
	})

	// alpha.ts_rank(x, window) — percentile rank of current value within the window.
	ip.RegisterBuiltin("alpha.ts_rank", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		n := len(vals)
		if n == 0 {
			return NaVal()
		}
		cur := vals[0] // vals[0] is most recent
		count := 0
		for _, v := range vals {
			if !math.IsNaN(v) && v < cur {
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
		xv := lastN(sx, w)
		yv := lastN(sy, w)
		n := min(len(xv), len(yv))
		if n < 2 {
			return NaVal()
		}
		xv = xv[:n]
		yv = yv[:n]
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
		xv := lastN(sx, w)
		yv := lastN(sy, w)
		n := min(len(xv), len(yv))
		if n < 2 {
			return NaVal()
		}
		xv = xv[:n]
		yv = yv[:n]
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
		cur := s.Current()
		prev := s.At(p)
		if math.IsNaN(cur) || math.IsNaN(prev) {
			return NaVal()
		}
		return FloatVal(cur - prev)
	})

	// alpha.ts_sum(x, window)
	ip.RegisterBuiltin("alpha.ts_sum", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		sum := 0.0
		for _, v := range vals {
			if !math.IsNaN(v) {
				sum += v
			}
		}
		return FloatVal(sum)
	})

	// alpha.ts_mean(x, window)
	ip.RegisterBuiltin("alpha.ts_mean", func(args []Value) Value {
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
		return FloatVal(mean(vals))
	})

	// alpha.ts_std(x, window)
	ip.RegisterBuiltin("alpha.ts_std", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		if len(vals) < 2 {
			return NaVal()
		}
		return FloatVal(stddev(vals, mean(vals)))
	})

	// alpha.ts_min(x, window)
	ip.RegisterBuiltin("alpha.ts_min", func(args []Value) Value {
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
		m := vals[0]
		for _, v := range vals[1:] {
			if !math.IsNaN(v) && v < m {
				m = v
			}
		}
		return FloatVal(m)
	})

	// alpha.ts_max(x, window)
	ip.RegisterBuiltin("alpha.ts_max", func(args []Value) Value {
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
		m := vals[0]
		for _, v := range vals[1:] {
			if !math.IsNaN(v) && v > m {
				m = v
			}
		}
		return FloatVal(m)
	})

	// alpha.ts_argmin(x, window) — index of min value (0 = most recent).
	ip.RegisterBuiltin("alpha.ts_argmin", func(args []Value) Value {
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
			if !math.IsNaN(vals[i]) && vals[i] < vals[idx] {
				idx = i
			}
		}
		return FloatVal(float64(idx))
	})

	// alpha.ts_argmax(x, window) — index of max value (0 = most recent).
	ip.RegisterBuiltin("alpha.ts_argmax", func(args []Value) Value {
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
			if !math.IsNaN(vals[i]) && vals[i] > vals[idx] {
				idx = i
			}
		}
		return FloatVal(float64(idx))
	})

	// alpha.ts_skewness(x, window)
	ip.RegisterBuiltin("alpha.ts_skewness", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		n := len(vals)
		if n < 3 {
			return NaVal()
		}
		m := mean(vals)
		sd := stddev(vals, m)
		if sd == 0 {
			return FloatVal(0)
		}
		sum := 0.0
		for _, v := range vals {
			d := (v - m) / sd
			sum += d * d * d
		}
		return FloatVal(sum / float64(n))
	})

	// alpha.ts_kurtosis(x, window)
	ip.RegisterBuiltin("alpha.ts_kurtosis", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, w := extractSeriesWindow(args)
		if s == nil || w <= 0 {
			return NaVal()
		}
		vals := lastN(s, w)
		n := len(vals)
		if n < 4 {
			return NaVal()
		}
		m := mean(vals)
		sd := stddev(vals, m)
		if sd == 0 {
			return FloatVal(0)
		}
		sum := 0.0
		for _, v := range vals {
			d := (v - m) / sd
			sum += d * d * d * d
		}
		return FloatVal(sum/float64(n) - 3) // excess kurtosis
	})

	// alpha.signed_power(x, exp) — sign(x) * |x|^exp
	ip.RegisterBuiltin("alpha.signed_power", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		x := args[0].Float()
		e := args[1].Float()
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

	// alpha.log_return(x, period) — ln(x / x[period])
	ip.RegisterBuiltin("alpha.log_return", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s, p := extractSeriesWindow(args)
		if s == nil || p <= 0 || s.Len() <= p {
			return NaVal()
		}
		cur := s.Current()
		prev := s.At(p)
		if math.IsNaN(cur) || math.IsNaN(prev) || prev == 0 {
			return NaVal()
		}
		return FloatVal(math.Log(cur / prev))
	})

	// alpha.ts_median(x, window)
	ip.RegisterBuiltin("alpha.ts_median", func(args []Value) Value {
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
		sorted := make([]float64, len(vals))
		copy(sorted, vals)
		sort.Float64s(sorted)
		n := len(sorted)
		if n%2 == 0 {
			return FloatVal((sorted[n/2-1] + sorted[n/2]) / 2)
		}
		return FloatVal(sorted[n/2])
	})
}

// --- helpers ---

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

// lastN returns the last n values from a series, most-recent first.
// Filters NaN values for most computations.
func lastN(s *Series, n int) []float64 {
	out := make([]float64, 0, n)
	for i := 0; i < n && i < s.Len(); i++ {
		v := s.At(i)
		out = append(out, v)
	}
	return out
}

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
	cov := 0.0
	sx, sy := 0.0, 0.0
	for i := 0; i < n; i++ {
		dx := x[i] - mx
		dy := y[i] - my
		cov += dx * dy
		sx += dx * dx
		sy += dy * dy
	}
	d := math.Sqrt(sx * sy)
	if d == 0 {
		return 0
	}
	return cov / d
}

func cov(x, y []float64) float64 {
	n := len(x)
	if n < 2 || n != len(y) {
		return math.NaN()
	}
	mx, my := mean(x), mean(y)
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += (x[i] - mx) * (y[i] - my)
	}
	return sum / float64(n-1)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
