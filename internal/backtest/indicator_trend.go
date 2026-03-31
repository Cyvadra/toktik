package backtest

import "math"

// SMA creates a Simple Moving Average indicator.
func SMA(source string, period int) Indicator {
	return &smaIndicator{source: source, period: period}
}

type smaIndicator struct {
	source string
	period int
}

func (s *smaIndicator) Deps() []string { return []string{s.source} }

func (s *smaIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[s.source]
	n := len(src)
	out := make([]float64, n)
	sum := 0.0
	nanCount := 0 // track NaN values inside the window

	for i := 0; i < n; i++ {
		isNaN := math.IsNaN(src[i])
		if isNaN {
			nanCount++
		} else {
			sum += src[i]
		}
		if i >= s.period {
			if math.IsNaN(src[i-s.period]) {
				nanCount--
			} else {
				sum -= src[i-s.period]
			}
		}
		if i < s.period-1 || nanCount > 0 {
			out[i] = math.NaN()
		} else {
			out[i] = sum / float64(s.period)
		}
	}
	return out
}

// EMA creates an Exponential Moving Average indicator compatible with TradingView ta.ema.
func EMA(source string, period int) Indicator {
	return &emaIndicator{source: source, period: period}
}

type emaIndicator struct {
	source string
	period int
}

func (e *emaIndicator) Deps() []string { return []string{e.source} }

func (e *emaIndicator) Compute(inputs map[string][]float64) []float64 {
	return computeEMA(inputs[e.source], e.period)
}

// WMA creates a linearly weighted moving average indicator.
func WMA(source string, period int) Indicator {
	return &wmaIndicator{source: source, period: period}
}

type wmaIndicator struct {
	source string
	period int
}

func (w *wmaIndicator) Deps() []string { return []string{w.source} }

func (w *wmaIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[w.source]
	n := len(src)
	out := make([]float64, n)
	if w.period <= 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	denom := float64(w.period*(w.period+1)) / 2
	for i := 0; i < n; i++ {
		if i < w.period-1 {
			out[i] = math.NaN()
			continue
		}
		weighted := 0.0
		valid := true
		for j := 0; j < w.period; j++ {
			v := src[i-w.period+1+j]
			if math.IsNaN(v) {
				valid = false
				break
			}
			weighted += float64(j+1) * v
		}
		if !valid {
			out[i] = math.NaN()
			continue
		}
		out[i] = weighted / denom
	}
	return out
}

// VWMA creates a volume-weighted moving average over a rolling window.
func VWMA(priceSource, volumeSource string, period int) Indicator {
	return &vwmaIndicator{priceSource: priceSource, volumeSource: volumeSource, period: period}
}

type vwmaIndicator struct {
	priceSource  string
	volumeSource string
	period       int
}

func (v *vwmaIndicator) Deps() []string { return []string{v.priceSource, v.volumeSource} }

func (v *vwmaIndicator) Compute(inputs map[string][]float64) []float64 {
	price := inputs[v.priceSource]
	vol := inputs[v.volumeSource]
	n := len(price)
	out := make([]float64, n)
	if v.period <= 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	pvSum := 0.0
	volSum := 0.0
	nanCount := 0
	windowCount := 0

	for i := 0; i < n; i++ {
		p := price[i]
		vv := vol[i]
		if math.IsNaN(p) || math.IsNaN(vv) {
			nanCount++
		} else {
			pvSum += p * vv
			volSum += vv
		}
		windowCount++

		if windowCount > v.period {
			op := price[i-v.period]
			ov := vol[i-v.period]
			if math.IsNaN(op) || math.IsNaN(ov) {
				nanCount--
			} else {
				pvSum -= op * ov
				volSum -= ov
			}
			windowCount--
		}

		if windowCount < v.period || nanCount > 0 || volSum == 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = pvSum / volSum
	}

	return out
}

// RMA creates Wilder's moving average (TradingView ta.rma).
func RMA(source string, period int) Indicator {
	return &rmaIndicator{source: source, period: period}
}

type rmaIndicator struct {
	source string
	period int
}

func (r *rmaIndicator) Deps() []string { return []string{r.source} }

func (r *rmaIndicator) Compute(inputs map[string][]float64) []float64 {
	return computeRMA(inputs[r.source], r.period)
}

// MACD creates a MACD indicator that produces three series:
// "{name}", "{name}_signal", "{name}_hist".
func MACD(source string, fast, slow, signal int) MultiIndicator {
	return &macdIndicator{source: source, fast: fast, slow: slow, signal: signal}
}

type macdIndicator struct {
	source string
	fast   int
	slow   int
	signal int
}

func (m *macdIndicator) Deps() []string { return []string{m.source} }

func (m *macdIndicator) OutputNames(baseName string) []string {
	return []string{baseName, baseName + "_signal", baseName + "_hist"}
}

func (m *macdIndicator) Compute(inputs map[string][]float64) []float64 {
	// Returns the MACD line only (for simple Indicator interface compatibility)
	src := inputs[m.source]
	fastEMA := computeEMA(src, m.fast)
	slowEMA := computeEMA(src, m.slow)
	n := len(src)
	macdLine := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(fastEMA[i]) || math.IsNaN(slowEMA[i]) {
			macdLine[i] = math.NaN()
		} else {
			macdLine[i] = fastEMA[i] - slowEMA[i]
		}
	}
	return macdLine
}

func (m *macdIndicator) ComputeMulti(baseName string, inputs map[string][]float64) map[string][]float64 {
	src := inputs[m.source]
	fastEMA := computeEMA(src, m.fast)
	slowEMA := computeEMA(src, m.slow)
	n := len(src)

	macdLine := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(fastEMA[i]) || math.IsNaN(slowEMA[i]) {
			macdLine[i] = math.NaN()
		} else {
			macdLine[i] = fastEMA[i] - slowEMA[i]
		}
	}

	signalLine := computeEMA(macdLine, m.signal)
	hist := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(macdLine[i]) || math.IsNaN(signalLine[i]) {
			hist[i] = math.NaN()
		} else {
			hist[i] = macdLine[i] - signalLine[i]
		}
	}

	names := m.OutputNames(baseName)
	return map[string][]float64{
		names[0]: macdLine,
		names[1]: signalLine,
		names[2]: hist,
	}
}

// Crossover emits 1.0 on bars where series `a` crosses above series `b`.
func Crossover(a, b string) Indicator {
	return &crossIndicator{a: a, b: b, above: true}
}

// Crossunder emits 1.0 on bars where series `a` crosses below series `b`.
func Crossunder(a, b string) Indicator {
	return &crossIndicator{a: a, b: b, above: false}
}

type crossIndicator struct {
	a, b  string
	above bool
}

func (c *crossIndicator) Deps() []string { return []string{c.a, c.b} }

func (c *crossIndicator) Compute(inputs map[string][]float64) []float64 {
	sa, sb := inputs[c.a], inputs[c.b]
	n := len(sa)
	out := make([]float64, n)
	out[0] = 0
	for i := 1; i < n; i++ {
		if math.IsNaN(sa[i]) || math.IsNaN(sb[i]) || math.IsNaN(sa[i-1]) || math.IsNaN(sb[i-1]) {
			out[i] = 0
			continue
		}
		if c.above {
			if sa[i-1] <= sb[i-1] && sa[i] > sb[i] {
				out[i] = 1
			}
		} else {
			if sa[i-1] >= sb[i-1] && sa[i] < sb[i] {
				out[i] = 1
			}
		}
	}
	return out
}
