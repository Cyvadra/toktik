package backtest

import "math"

// RSI creates a Relative Strength Index indicator.
func RSI(source string, period int) Indicator {
	return &rsiIndicator{source: source, period: period}
}

type rsiIndicator struct {
	source string
	period int
}

func (r *rsiIndicator) Deps() []string { return []string{r.source} }

func (r *rsiIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[r.source]
	n := len(src)
	out := make([]float64, n)

	if n <= r.period {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	avgGain := 0.0
	avgLoss := 0.0

	// Initial average over first `period` changes, NaN-safe
	validChanges := 0
	for i := 1; i <= r.period; i++ {
		out[i-1] = math.NaN()
		if math.IsNaN(src[i]) || math.IsNaN(src[i-1]) {
			continue
		}
		change := src[i] - src[i-1]
		if change > 0 {
			avgGain += change
		} else {
			avgLoss -= change
		}
		validChanges++
	}
	if validChanges < r.period {
		// Not enough valid data in seed window — mark all NaN
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	avgGain /= float64(r.period)
	avgLoss /= float64(r.period)

	if avgLoss == 0 {
		out[r.period] = 100
	} else {
		rs := avgGain / avgLoss
		out[r.period] = 100 - 100/(1+rs)
	}

	for i := r.period + 1; i < n; i++ {
		if math.IsNaN(src[i]) || math.IsNaN(src[i-1]) {
			out[i] = out[i-1] // carry forward through NaN gaps
			continue
		}
		change := src[i] - src[i-1]
		gain, loss := 0.0, 0.0
		if change > 0 {
			gain = change
		} else {
			loss = -change
		}
		avgGain = (avgGain*float64(r.period-1) + gain) / float64(r.period)
		avgLoss = (avgLoss*float64(r.period-1) + loss) / float64(r.period)

		if avgLoss == 0 {
			out[i] = 100
		} else {
			rs := avgGain / avgLoss
			out[i] = 100 - 100/(1+rs)
		}
	}
	return out
}

// Stochastic creates a classic stochastic oscillator.
// Outputs are: "{name}" (smoothed %K), "{name}_d" (%D), "{name}_raw" (raw %K).
func Stochastic(highSource, lowSource, closeSource string, kPeriod, kSmooth, dPeriod int) MultiIndicator {
	return &stochasticIndicator{
		highSource:  highSource,
		lowSource:   lowSource,
		closeSource: closeSource,
		kPeriod:     kPeriod,
		kSmooth:     kSmooth,
		dPeriod:     dPeriod,
	}
}

type stochasticIndicator struct {
	highSource  string
	lowSource   string
	closeSource string
	kPeriod     int
	kSmooth     int
	dPeriod     int
}

func (s *stochasticIndicator) Deps() []string {
	return []string{s.highSource, s.lowSource, s.closeSource}
}

func (s *stochasticIndicator) OutputNames(baseName string) []string {
	return []string{baseName, baseName + "_d", baseName + "_raw"}
}

func (s *stochasticIndicator) Compute(inputs map[string][]float64) []float64 {
	out := s.ComputeMulti("stoch", inputs)
	return out["stoch"]
}

func (s *stochasticIndicator) ComputeMulti(baseName string, inputs map[string][]float64) map[string][]float64 {
	high := inputs[s.highSource]
	low := inputs[s.lowSource]
	close := inputs[s.closeSource]
	n := len(high)
	raw := make([]float64, n)

	if s.kPeriod <= 0 || s.kSmooth <= 0 || s.dPeriod <= 0 {
		for i := 0; i < n; i++ {
			raw[i] = math.NaN()
		}
		names := s.OutputNames(baseName)
		return map[string][]float64{
			names[0]: raw,
			names[1]: append([]float64(nil), raw...),
			names[2]: raw,
		}
	}

	hh := rollingMax(high, s.kPeriod)
	ll := rollingMin(low, s.kPeriod)
	for i := 0; i < n; i++ {
		if math.IsNaN(hh[i]) || math.IsNaN(ll[i]) || math.IsNaN(close[i]) {
			raw[i] = math.NaN()
			continue
		}
		rng := hh[i] - ll[i]
		if rng == 0 {
			raw[i] = 0
		} else {
			raw[i] = 100 * (close[i] - ll[i]) / rng
		}
	}

	k := computeSMA(raw, s.kSmooth)
	d := computeSMA(k, s.dPeriod)
	names := s.OutputNames(baseName)
	return map[string][]float64{
		names[0]: k,
		names[1]: d,
		names[2]: raw,
	}
}

// CCI creates Commodity Channel Index.
func CCI(highSource, lowSource, closeSource string, period int) Indicator {
	return &cciIndicator{highSource: highSource, lowSource: lowSource, closeSource: closeSource, period: period}
}

type cciIndicator struct {
	highSource  string
	lowSource   string
	closeSource string
	period      int
}

func (c *cciIndicator) Deps() []string { return []string{c.highSource, c.lowSource, c.closeSource} }

func (c *cciIndicator) Compute(inputs map[string][]float64) []float64 {
	high := inputs[c.highSource]
	low := inputs[c.lowSource]
	close := inputs[c.closeSource]
	n := len(high)
	out := make([]float64, n)

	if c.period <= 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	tp := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(high[i]) || math.IsNaN(low[i]) || math.IsNaN(close[i]) {
			tp[i] = math.NaN()
			continue
		}
		tp[i] = (high[i] + low[i] + close[i]) / 3
	}
	smaTP := computeSMA(tp, c.period)

	for i := 0; i < n; i++ {
		if i < c.period-1 || math.IsNaN(tp[i]) || math.IsNaN(smaTP[i]) {
			out[i] = math.NaN()
			continue
		}
		meanDev := 0.0
		valid := true
		for j := i - c.period + 1; j <= i; j++ {
			if math.IsNaN(tp[j]) {
				valid = false
				break
			}
			meanDev += math.Abs(tp[j] - smaTP[i])
		}
		if !valid {
			out[i] = math.NaN()
			continue
		}
		meanDev /= float64(c.period)
		if meanDev == 0 {
			out[i] = 0
		} else {
			out[i] = (tp[i] - smaTP[i]) / (0.015 * meanDev)
		}
	}

	return out
}

// ADX creates Average Directional Index with Wilder smoothing.
// Outputs are: "{name}" (ADX), "{name}_plus_di", "{name}_minus_di".
func ADX(period int) MultiIndicator {
	return &adxIndicator{period: period}
}

type adxIndicator struct {
	period int
}

func (a *adxIndicator) Deps() []string { return []string{"high", "low", "close"} }

func (a *adxIndicator) OutputNames(baseName string) []string {
	return []string{baseName, baseName + "_plus_di", baseName + "_minus_di"}
}

func (a *adxIndicator) Compute(inputs map[string][]float64) []float64 {
	out := a.ComputeMulti("adx", inputs)
	return out["adx"]
}

func (a *adxIndicator) ComputeMulti(baseName string, inputs map[string][]float64) map[string][]float64 {
	high := inputs["high"]
	low := inputs["low"]
	close := inputs["close"]
	n := len(high)

	tr := make([]float64, n)
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)
	plusDI := make([]float64, n)
	minusDI := make([]float64, n)
	dx := make([]float64, n)

	if a.period <= 0 {
		for i := 0; i < n; i++ {
			plusDI[i] = math.NaN()
			minusDI[i] = math.NaN()
			dx[i] = math.NaN()
		}
		names := a.OutputNames(baseName)
		return map[string][]float64{
			names[0]: dx,
			names[1]: plusDI,
			names[2]: minusDI,
		}
	}

	for i := 0; i < n; i++ {
		plusDM[i] = 0
		minusDM[i] = 0
		if math.IsNaN(high[i]) || math.IsNaN(low[i]) || math.IsNaN(close[i]) {
			tr[i] = math.NaN()
			continue
		}
		if i == 0 || math.IsNaN(high[i-1]) || math.IsNaN(low[i-1]) || math.IsNaN(close[i-1]) {
			tr[i] = high[i] - low[i]
			continue
		}

		upMove := high[i] - high[i-1]
		downMove := low[i-1] - low[i]
		if upMove > downMove && upMove > 0 {
			plusDM[i] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i] = downMove
		}

		a1 := high[i] - low[i]
		a2 := math.Abs(high[i] - close[i-1])
		a3 := math.Abs(low[i] - close[i-1])
		tr[i] = math.Max(a1, math.Max(a2, a3))
	}

	atr := computeRMA(tr, a.period)
	plusRMA := computeRMA(plusDM, a.period)
	minusRMA := computeRMA(minusDM, a.period)

	for i := 0; i < n; i++ {
		if math.IsNaN(atr[i]) || atr[i] == 0 {
			plusDI[i] = math.NaN()
			minusDI[i] = math.NaN()
			dx[i] = math.NaN()
			continue
		}

		plusDI[i] = 100 * plusRMA[i] / atr[i]
		minusDI[i] = 100 * minusRMA[i] / atr[i]
		sum := plusDI[i] + minusDI[i]
		if sum == 0 {
			dx[i] = 0
		} else {
			dx[i] = 100 * math.Abs(plusDI[i]-minusDI[i]) / sum
		}
	}

	adx := computeRMA(dx, a.period)
	names := a.OutputNames(baseName)
	return map[string][]float64{
		names[0]: adx,
		names[1]: plusDI,
		names[2]: minusDI,
	}
}

// OBV creates On-Balance Volume.
func OBV(closeSource, volumeSource string) Indicator {
	return &obvIndicator{closeSource: closeSource, volumeSource: volumeSource}
}

type obvIndicator struct {
	closeSource  string
	volumeSource string
}

func (o *obvIndicator) Deps() []string { return []string{o.closeSource, o.volumeSource} }

func (o *obvIndicator) Compute(inputs map[string][]float64) []float64 {
	close := inputs[o.closeSource]
	volume := inputs[o.volumeSource]
	n := len(close)
	out := make([]float64, n)
	if n == 0 {
		return out
	}

	if math.IsNaN(volume[0]) {
		out[0] = math.NaN()
	} else {
		out[0] = volume[0]
	}

	for i := 1; i < n; i++ {
		if math.IsNaN(close[i]) || math.IsNaN(close[i-1]) || math.IsNaN(volume[i]) || math.IsNaN(out[i-1]) {
			out[i] = math.NaN()
			continue
		}
		switch {
		case close[i] > close[i-1]:
			out[i] = out[i-1] + volume[i]
		case close[i] < close[i-1]:
			out[i] = out[i-1] - volume[i]
		default:
			out[i] = out[i-1]
		}
	}

	return out
}
