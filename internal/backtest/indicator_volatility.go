package backtest

import "math"

// ATR creates Average True Range using Wilder's smoothing.
func ATR(period int) Indicator {
	return &atrIndicator{period: period}
}

type atrIndicator struct {
	period int
}

func (a *atrIndicator) Deps() []string { return []string{"high", "low", "close"} }

func (a *atrIndicator) Compute(inputs map[string][]float64) []float64 {
	high := inputs["high"]
	low := inputs["low"]
	close := inputs["close"]
	n := len(high)
	tr := make([]float64, n)

	for i := 0; i < n; i++ {
		if math.IsNaN(high[i]) || math.IsNaN(low[i]) || math.IsNaN(close[i]) {
			tr[i] = math.NaN()
			continue
		}
		if i == 0 || math.IsNaN(close[i-1]) {
			tr[i] = high[i] - low[i]
			continue
		}
		a1 := high[i] - low[i]
		a2 := math.Abs(high[i] - close[i-1])
		a3 := math.Abs(low[i] - close[i-1])
		tr[i] = math.Max(a1, math.Max(a2, a3))
	}

	return computeRMA(tr, a.period)
}

// Bollinger creates Bollinger Bands as a multi-output indicator.
// Outputs are: "{name}" (middle/SMA), "{name}_upper", "{name}_lower".
func Bollinger(source string, period int, k float64) MultiIndicator {
	return &bollingerIndicator{source: source, period: period, k: k}
}

type bollingerIndicator struct {
	source string
	period int
	k      float64
}

func (b *bollingerIndicator) Deps() []string { return []string{b.source} }

func (b *bollingerIndicator) OutputNames(baseName string) []string {
	return []string{baseName, baseName + "_upper", baseName + "_lower"}
}

func (b *bollingerIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[b.source]
	n := len(src)
	mid := make([]float64, n)
	if b.period <= 0 {
		for i := range mid {
			mid[i] = math.NaN()
		}
		return mid
	}

	sum := 0.0
	nanCount := 0
	windowCount := 0
	for i := 0; i < n; i++ {
		v := src[i]
		if math.IsNaN(v) {
			nanCount++
		} else {
			sum += v
		}
		windowCount++

		if windowCount > b.period {
			old := src[i-b.period]
			if math.IsNaN(old) {
				nanCount--
			} else {
				sum -= old
			}
			windowCount--
		}

		if windowCount < b.period || nanCount > 0 {
			mid[i] = math.NaN()
			continue
		}
		mid[i] = sum / float64(b.period)
	}

	return mid
}

func (b *bollingerIndicator) ComputeMulti(baseName string, inputs map[string][]float64) map[string][]float64 {
	src := inputs[b.source]
	n := len(src)
	mid := make([]float64, n)
	upper := make([]float64, n)
	lower := make([]float64, n)
	if b.period <= 0 {
		for i := 0; i < n; i++ {
			mid[i] = math.NaN()
			upper[i] = math.NaN()
			lower[i] = math.NaN()
		}
		names := b.OutputNames(baseName)
		return map[string][]float64{
			names[0]: mid,
			names[1]: upper,
			names[2]: lower,
		}
	}

	sum := 0.0
	sumSq := 0.0
	nanCount := 0
	windowCount := 0

	for i := 0; i < n; i++ {
		v := src[i]
		if math.IsNaN(v) {
			nanCount++
		} else {
			sum += v
			sumSq += v * v
		}
		windowCount++

		if windowCount > b.period {
			old := src[i-b.period]
			if math.IsNaN(old) {
				nanCount--
			} else {
				sum -= old
				sumSq -= old * old
			}
			windowCount--
		}

		if windowCount < b.period || nanCount > 0 {
			mid[i] = math.NaN()
			upper[i] = math.NaN()
			lower[i] = math.NaN()
			continue
		}

		mean := sum / float64(b.period)
		variance := sumSq/float64(b.period) - mean*mean
		if variance < 0 {
			// Clamp tiny negatives caused by floating-point cancellation.
			variance = 0
		}
		std := math.Sqrt(variance)

		mid[i] = mean
		upper[i] = mean + b.k*std
		lower[i] = mean - b.k*std
	}

	names := b.OutputNames(baseName)
	return map[string][]float64{
		names[0]: mid,
		names[1]: upper,
		names[2]: lower,
	}
}

// Donchian creates Donchian Channel as a multi-output indicator.
// Outputs are: "{name}" (middle), "{name}_upper", "{name}_lower".
func Donchian(highSource, lowSource string, period int) MultiIndicator {
	return &donchianIndicator{highSource: highSource, lowSource: lowSource, period: period}
}

type donchianIndicator struct {
	highSource string
	lowSource  string
	period     int
}

func (d *donchianIndicator) Deps() []string { return []string{d.highSource, d.lowSource} }

func (d *donchianIndicator) OutputNames(baseName string) []string {
	return []string{baseName, baseName + "_upper", baseName + "_lower"}
}

func (d *donchianIndicator) Compute(inputs map[string][]float64) []float64 {
	outs := d.ComputeMulti("donchian", inputs)
	return outs["donchian"]
}

func (d *donchianIndicator) ComputeMulti(baseName string, inputs map[string][]float64) map[string][]float64 {
	upper := rollingMax(inputs[d.highSource], d.period)
	lower := rollingMin(inputs[d.lowSource], d.period)
	n := len(upper)
	mid := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(upper[i]) || math.IsNaN(lower[i]) {
			mid[i] = math.NaN()
		} else {
			mid[i] = (upper[i] + lower[i]) / 2
		}
	}
	names := d.OutputNames(baseName)
	return map[string][]float64{
		names[0]: mid,
		names[1]: upper,
		names[2]: lower,
	}
}
