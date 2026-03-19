package backtest

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// Indicator computes a derived series from one or more input series.
// All indicators are computed in a preflight pass over the full data set
// before bar-by-bar replay begins.
type Indicator interface {
	// Deps returns the names of input series this indicator requires.
	Deps() []string

	// Compute takes the resolved dependency series and returns the output series.
	// The output must have the same length as the inputs.
	// Use math.NaN() for bars where the indicator is undefined (e.g. warmup period).
	Compute(inputs map[string][]float64) []float64
}

// MultiIndicator produces multiple named output series from one computation.
type MultiIndicator interface {
	Indicator

	// OutputNames returns the names of all output series.
	// The primary registration name maps to the first output.
	OutputNames(baseName string) []string

	// ComputeMulti returns all output series keyed by name.
	ComputeMulti(baseName string, inputs map[string][]float64) map[string][]float64
}

// resolveIndicators computes all registered indicators using topological sort.
// It modifies the data map in-place, adding computed series.
func resolveIndicators(registered map[string]Indicator, data map[string][]float64) error {
	// Build dependency graph
	type node struct {
		name string
		ind  Indicator
	}

	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dep → list of indicators that need it

	for name := range registered {
		if _, exists := inDegree[name]; !exists {
			inDegree[name] = 0
		}
	}

	for name, ind := range registered {
		for _, dep := range ind.Deps() {
			// Only count dependencies on other indicators (not raw data columns)
			if _, isIndicator := registered[dep]; isIndicator {
				inDegree[name]++
				dependents[dep] = append(dependents[dep], name)
			} else if _, hasData := data[dep]; !hasData {
				return fmt.Errorf("indicator %q depends on unknown series %q", name, dep)
			}
		}
	}

	// Topological sort using Kahn's algorithm
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	var order []string
	for len(queue) > 0 {
		// Process all items at current level in parallel
		currentLevel := queue
		queue = nil

		// Compute indicators at this level in parallel
		var wg sync.WaitGroup
		var mu sync.Mutex
		results := make(map[string][]float64)
		multiResults := make(map[string]map[string][]float64)
		var computeErr error

		for _, name := range currentLevel {
			order = append(order, name)
			ind := registered[name]

			wg.Add(1)
			go func(n string, indicator Indicator) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						computeErr = fmt.Errorf("indicator %q panicked: %v", n, r)
						mu.Unlock()
					}
				}()

				// Gather inputs
				inputs := make(map[string][]float64)
				for _, dep := range indicator.Deps() {
					mu.Lock()
					if col, ok := data[dep]; ok {
						inputs[dep] = col
					} else if col, ok := results[dep]; ok {
						inputs[dep] = col
					}
					mu.Unlock()
				}

				if mi, ok := indicator.(MultiIndicator); ok {
					outputs := mi.ComputeMulti(n, inputs)
					mu.Lock()
					multiResults[n] = outputs
					mu.Unlock()
				} else {
					out := indicator.Compute(inputs)
					mu.Lock()
					results[n] = out
					mu.Unlock()
				}
			}(name, ind)
		}
		wg.Wait()

		if computeErr != nil {
			return computeErr
		}

		// Merge results into data
		for name, series := range results {
			data[name] = series
		}
		for _, outputs := range multiResults {
			for name, series := range outputs {
				data[name] = series
			}
		}

		// Update in-degrees
		for _, name := range currentLevel {
			for _, dep := range dependents[name] {
				inDegree[dep]--
				if inDegree[dep] == 0 {
					queue = append(queue, dep)
				}
			}
		}
	}

	if len(order) != len(registered) {
		return fmt.Errorf("indicator dependency cycle detected: resolved %d of %d indicators", len(order), len(registered))
	}

	return nil
}

// --- Built-in Indicator Constructors ---

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

// EMA creates an Exponential Moving Average indicator.
func EMA(source string, period int) Indicator {
	return &emaIndicator{source: source, period: period}
}

type emaIndicator struct {
	source string
	period int
}

func (e *emaIndicator) Deps() []string { return []string{e.source} }

func (e *emaIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[e.source]
	n := len(src)
	out := make([]float64, n)
	k := 2.0 / float64(e.period+1)

	// Seed with SMA of first `period` values
	sum := 0.0
	count := 0
	seedIdx := -1
	for i := 0; i < n && count < e.period; i++ {
		if !math.IsNaN(src[i]) {
			sum += src[i]
			count++
			seedIdx = i
		}
		out[i] = math.NaN()
	}

	if count < e.period {
		// Not enough data
		for i := 0; i < n; i++ {
			out[i] = math.NaN()
		}
		return out
	}

	out[seedIdx] = sum / float64(e.period)
	for i := seedIdx + 1; i < n; i++ {
		if math.IsNaN(src[i]) {
			out[i] = out[i-1]
		} else {
			out[i] = src[i]*k + out[i-1]*(1-k)
		}
	}
	return out
}

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

	// Initial average over first `period` changes
	for i := 1; i <= r.period; i++ {
		out[i-1] = math.NaN()
		change := src[i] - src[i-1]
		if change > 0 {
			avgGain += change
		} else {
			avgLoss -= change
		}
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

// computeEMA is a helper for vectorized EMA computation.
func computeEMA(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	k := 2.0 / float64(period+1)

	sum := 0.0
	count := 0
	seedIdx := -1
	for i := 0; i < n && count < period; i++ {
		if !math.IsNaN(src[i]) {
			sum += src[i]
			count++
			seedIdx = i
		}
		out[i] = math.NaN()
	}

	if count < period {
		for i := 0; i < n; i++ {
			out[i] = math.NaN()
		}
		return out
	}

	out[seedIdx] = sum / float64(period)
	for i := seedIdx + 1; i < n; i++ {
		if math.IsNaN(src[i]) {
			out[i] = out[i-1]
		} else {
			out[i] = src[i]*k + out[i-1]*(1-k)
		}
	}
	return out
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

// Above emits 1.0 on bars where the source is above the threshold.
func Above(source string, threshold float64) Indicator {
	return &thresholdIndicator{source: source, threshold: threshold, above: true}
}

// Below emits 1.0 on bars where the source is below the threshold.
func Below(source string, threshold float64) Indicator {
	return &thresholdIndicator{source: source, threshold: threshold, above: false}
}

type thresholdIndicator struct {
	source    string
	threshold float64
	above     bool
}

func (t *thresholdIndicator) Deps() []string { return []string{t.source} }

func (t *thresholdIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[t.source]
	n := len(src)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(src[i]) {
			out[i] = 0
			continue
		}
		if t.above {
			if src[i] > t.threshold {
				out[i] = 1
			}
		} else {
			if src[i] < t.threshold {
				out[i] = 1
			}
		}
	}
	return out
}

// Highest creates a rolling highest-value indicator over the given period.
func Highest(source string, period int) Indicator {
	return &highestIndicator{source: source, period: period}
}

type highestIndicator struct {
	source string
	period int
}

func (h *highestIndicator) Deps() []string { return []string{h.source} }

func (h *highestIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[h.source]
	n := len(src)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		if i < h.period-1 {
			out[i] = math.NaN()
			continue
		}
		best := math.Inf(-1)
		for j := i - h.period + 1; j <= i; j++ {
			if !math.IsNaN(src[j]) && src[j] > best {
				best = src[j]
			}
		}
		if math.IsInf(best, -1) {
			out[i] = math.NaN()
		} else {
			out[i] = best
		}
	}
	return out
}

// Lowest creates a rolling lowest-value indicator over the given period.
func Lowest(source string, period int) Indicator {
	return &lowestIndicator{source: source, period: period}
}

type lowestIndicator struct {
	source string
	period int
}

func (l *lowestIndicator) Deps() []string { return []string{l.source} }

func (l *lowestIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[l.source]
	n := len(src)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		if i < l.period-1 {
			out[i] = math.NaN()
			continue
		}
		best := math.Inf(1)
		for j := i - l.period + 1; j <= i; j++ {
			if !math.IsNaN(src[j]) && src[j] < best {
				best = src[j]
			}
		}
		if math.IsInf(best, 1) {
			out[i] = math.NaN()
		} else {
			out[i] = best
		}
	}
	return out
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

	for i := 0; i < n; i++ {
		if i < s.kPeriod-1 {
			raw[i] = math.NaN()
			continue
		}
		h := math.Inf(-1)
		l := math.Inf(1)
		valid := true
		for j := i - s.kPeriod + 1; j <= i; j++ {
			if math.IsNaN(high[j]) || math.IsNaN(low[j]) || math.IsNaN(close[j]) {
				valid = false
				break
			}
			if high[j] > h {
				h = high[j]
			}
			if low[j] < l {
				l = low[j]
			}
		}
		if !valid {
			raw[i] = math.NaN()
			continue
		}
		rng := h - l
		if rng == 0 {
			raw[i] = 0
		} else {
			raw[i] = 100 * (close[i] - l) / rng
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

// Quantile creates a rolling quantile indicator over the given period.
// q must be in [0,1], where 0 = min and 1 = max.
func Quantile(source string, period int, q float64) Indicator {
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	return &quantileIndicator{source: source, period: period, q: q}
}

type quantileIndicator struct {
	source string
	period int
	q      float64
}

func (q *quantileIndicator) Deps() []string { return []string{q.source} }

func (q *quantileIndicator) Compute(inputs map[string][]float64) []float64 {
	src := inputs[q.source]
	series := computeRollingQuantiles(src, q.period, []float64{q.q})
	return series[0]
}

func computeRollingQuantiles(src []float64, period int, quantiles []float64) [][]float64 {
	n := len(src)
	out := make([][]float64, len(quantiles))
	for i := range out {
		out[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			out[i][j] = math.NaN()
		}
	}

	if len(quantiles) == 0 {
		return out
	}
	if period <= 0 {
		return out
	}

	qvals := make([]float64, len(quantiles))
	copy(qvals, quantiles)
	for i := range qvals {
		if qvals[i] < 0 {
			qvals[i] = 0
		}
		if qvals[i] > 1 {
			qvals[i] = 1
		}
	}

	if period == 1 {
		for i := 0; i < n; i++ {
			if math.IsNaN(src[i]) {
				continue
			}
			for j := range out {
				out[j][i] = src[i]
			}
		}
		return out
	}

	values := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		if !math.IsNaN(src[i]) {
			values = append(values, src[i])
		}
	}
	if len(values) == 0 {
		return out
	}

	sort.Float64s(values)
	uniq := values[:1]
	for i := 1; i < len(values); i++ {
		if values[i] != values[i-1] {
			uniq = append(uniq, values[i])
		}
	}

	compressed := make([]int, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(src[i]) {
			compressed[i] = -1
			continue
		}
		compressed[i] = sort.SearchFloat64s(uniq, src[i])
	}

	type qReq struct {
		lo     int
		hi     int
		weight float64
	}
	requests := make([]qReq, len(qvals))
	for i, q := range qvals {
		idx := q * float64(period-1)
		lo := int(math.Floor(idx))
		hi := int(math.Ceil(idx))
		requests[i] = qReq{lo: lo, hi: hi, weight: idx - float64(lo)}
	}

	bit := newFenwick(len(uniq))
	nanCount := 0
	windowCount := 0

	for i := 0; i < n; i++ {
		inIdx := compressed[i]
		if inIdx < 0 {
			nanCount++
		} else {
			bit.add(inIdx+1, 1)
		}
		windowCount++

		if windowCount > period {
			outIdx := compressed[i-period]
			if outIdx < 0 {
				nanCount--
			} else {
				bit.add(outIdx+1, -1)
			}
			windowCount--
		}

		if windowCount < period || nanCount > 0 {
			continue
		}

		for qi, req := range requests {
			loVal := uniq[bit.findByOrder(req.lo+1)-1]
			if req.lo == req.hi {
				out[qi][i] = loVal
				continue
			}
			hiVal := uniq[bit.findByOrder(req.hi+1)-1]
			out[qi][i] = loVal*(1-req.weight) + hiVal*req.weight
		}
	}

	return out
}

type fenwickTree struct {
	tree []int
}

func newFenwick(n int) *fenwickTree {
	return &fenwickTree{tree: make([]int, n+1)}
}

func (f *fenwickTree) add(index int, delta int) {
	for i := index; i < len(f.tree); i += i & -i {
		f.tree[i] += delta
	}
}

// findByOrder returns smallest index i such that prefixSum(i) >= order.
// order is 1-based.
func (f *fenwickTree) findByOrder(order int) int {
	idx := 0
	bitMask := 1
	for bitMask < len(f.tree) {
		bitMask <<= 1
	}
	for step := bitMask >> 1; step > 0; step >>= 1 {
		next := idx + step
		if next < len(f.tree) && f.tree[next] < order {
			idx = next
			order -= f.tree[next]
		}
	}
	return idx + 1
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
	high := inputs[d.highSource]
	low := inputs[d.lowSource]
	n := len(high)
	mid := make([]float64, n)
	upper := make([]float64, n)
	lower := make([]float64, n)

	for i := 0; i < n; i++ {
		if i < d.period-1 {
			mid[i] = math.NaN()
			upper[i] = math.NaN()
			lower[i] = math.NaN()
			continue
		}

		h := math.Inf(-1)
		l := math.Inf(1)
		valid := true
		for j := i - d.period + 1; j <= i; j++ {
			if math.IsNaN(high[j]) || math.IsNaN(low[j]) {
				valid = false
				break
			}
			if high[j] > h {
				h = high[j]
			}
			if low[j] < l {
				l = low[j]
			}
		}
		if !valid {
			mid[i] = math.NaN()
			upper[i] = math.NaN()
			lower[i] = math.NaN()
			continue
		}

		upper[i] = h
		lower[i] = l
		mid[i] = (h + l) / 2
	}

	names := d.OutputNames(baseName)
	return map[string][]float64{
		names[0]: mid,
		names[1]: upper,
		names[2]: lower,
	}
}

// Custom creates an indicator from an arbitrary function.
func Custom(deps []string, fn func(inputs map[string][]float64) []float64) Indicator {
	return &customIndicator{deps: deps, fn: fn}
}

type customIndicator struct {
	deps []string
	fn   func(inputs map[string][]float64) []float64
}

func (c *customIndicator) Deps() []string { return c.deps }

func (c *customIndicator) Compute(inputs map[string][]float64) []float64 {
	return c.fn(inputs)
}

func computeSMA(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	if period <= 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
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

		if windowCount > period {
			old := src[i-period]
			if math.IsNaN(old) {
				nanCount--
			} else {
				sum -= old
			}
			windowCount--
		}

		if windowCount < period || nanCount > 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = sum / float64(period)
	}

	return out
}

func computeRMA(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 {
		return out
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

		if windowCount > period {
			old := src[i-period]
			if math.IsNaN(old) {
				nanCount--
			} else {
				sum -= old
			}
			windowCount--
		}

		if windowCount < period || nanCount > 0 {
			continue
		}

		if i == period-1 || math.IsNaN(out[i-1]) {
			out[i] = sum / float64(period)
			continue
		}

		if math.IsNaN(v) {
			out[i] = out[i-1]
		} else {
			out[i] = (out[i-1]*float64(period-1) + v) / float64(period)
		}
	}

	return out
}
