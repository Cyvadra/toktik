package backtest

import (
	"fmt"
	"math"
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

	for i := 0; i < n; i++ {
		if math.IsNaN(src[i]) {
			out[i] = math.NaN()
			continue
		}
		sum += src[i]
		if i >= s.period {
			sum -= src[i-s.period]
		}
		if i < s.period-1 {
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
