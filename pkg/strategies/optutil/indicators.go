package optutil

import (
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// PercentileRank returns an indicator that computes the rolling percentile
// rank of a source column over the given period. Equivalent to
// ta.percentrank(source, period) in Pine Script.
func PercentileRank(source string, period int) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			series := inputs[source]
			n := len(series)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if i < period || math.IsNaN(series[i]) {
					out[i] = math.NaN()
					continue
				}
				count := 0
				valid := 0
				for j := i - period; j < i; j++ {
					if math.IsNaN(series[j]) {
						continue
					}
					valid++
					if series[j] < series[i] {
						count++
					}
				}
				if valid == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = float64(count) / float64(valid) * 100
			}
			return out
		},
	)
}

// RollingStdDev computes a rolling standard deviation of src over the given
// period. The first (period-1) values are NaN.
func RollingStdDev(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	if period <= 1 {
		return out
	}

	for i := 0; i < n; i++ {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		sum := 0.0
		sumSq := 0.0
		valid := 0
		for j := i - period + 1; j <= i; j++ {
			v := src[j]
			if math.IsNaN(v) {
				out[i] = math.NaN()
				break
			}
			sum += v
			sumSq += v * v
			valid++
		}
		if valid == period {
			mean := sum / float64(period)
			variance := sumSq/float64(period) - mean*mean
			if variance < 0 {
				variance = 0
			}
			out[i] = math.Sqrt(variance)
		} else if !math.IsNaN(out[i]) {
			out[i] = math.NaN()
		}
	}
	return out
}

// RollingStdDevIndicator wraps RollingStdDev as a backtest.Indicator that
// reads from the named source column.
func RollingStdDevIndicator(source string, period int) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			return RollingStdDev(inputs[source], period)
		},
	)
}

// PercentChange returns an indicator computing bar-over-bar percent change:
// (current / previous) - 1.
func PercentChange(source string) backtest.Indicator {
	return backtest.Custom(
		[]string{source},
		func(inputs map[string][]float64) []float64 {
			series := inputs[source]
			out := make([]float64, len(series))
			for i := range out {
				out[i] = math.NaN()
			}
			for i := 1; i < len(series); i++ {
				prev := series[i-1]
				curr := series[i]
				if math.IsNaN(prev) || math.IsNaN(curr) || prev == 0 {
					continue
				}
				out[i] = curr/prev - 1
			}
			return out
		},
	)
}
