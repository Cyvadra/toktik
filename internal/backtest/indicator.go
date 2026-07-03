package backtest

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

var ErrUnknownIndicatorSeries = errors.New("unknown indicator series")

type unknownIndicatorSeriesError struct {
	message string
}

func (e unknownIndicatorSeriesError) Error() string { return e.message }

func (e unknownIndicatorSeriesError) Is(target error) bool {
	return target == ErrUnknownIndicatorSeries
}

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

// OptionalDepsProvider may be implemented by an Indicator to declare
// dependencies that are not required to be present. When an optional dep
// is absent from the data, an all-NaN slice of the data length is injected
// into the inputs map instead of returning an error.
type OptionalDepsProvider interface {
	OptionalDeps() []string
}

// resolveIndicators computes all registered indicators using topological sort.
// It modifies the data map in-place, adding computed series.
func resolveIndicators(registered map[string]Indicator, data map[string][]float64) error {
	// Build dependency graph
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
				return unknownIndicatorSeriesError{message: fmt.Sprintf("indicator %q depends on unknown series %q", name, dep)}
			}
		}
		// Optional deps: if present and from another indicator, wire them into the
		// graph so they are computed before this indicator. Missing optional deps
		// are silently injected as all-NaN at compute time.
		if op, ok := ind.(OptionalDepsProvider); ok {
			for _, dep := range op.OptionalDeps() {
				if _, isIndicator := registered[dep]; isIndicator {
					inDegree[name]++
					dependents[dep] = append(dependents[dep], name)
				}
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
				// Optional deps: inject what exists, or an all-NaN slice.
				if op, ok := indicator.(OptionalDepsProvider); ok {
					mu.Lock()
					n := dataLen(data)
					mu.Unlock()
					for _, dep := range op.OptionalDeps() {
						mu.Lock()
						if col, ok := data[dep]; ok {
							inputs[dep] = col
						} else if col, ok := results[dep]; ok {
							inputs[dep] = col
						} else {
							nan := make([]float64, n)
							for i := range nan {
								nan[i] = math.NaN()
							}
							inputs[dep] = nan
						}
						mu.Unlock()
					}
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

// computeEMA is a helper for vectorized EMA computation compatible with TradingView ta.ema.
func computeEMA(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	if period <= 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}

	k := 2.0 / float64(period+1)
	seeded := false
	for i := 0; i < n; i++ {
		if math.IsNaN(src[i]) {
			if seeded {
				out[i] = out[i-1]
			} else {
				out[i] = math.NaN()
			}
			continue
		}
		if !seeded {
			out[i] = src[i]
			seeded = true
			continue
		}
		out[i] = src[i]*k + out[i-1]*(1-k)
	}
	if !seeded {
		for i := range out {
			out[i] = math.NaN()
		}
	}
	return out
}

// dataLen returns the length of any series currently in data, or 0.
func dataLen(data map[string][]float64) int {
	for _, col := range data {
		return len(col)
	}
	return 0
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

		out[i] = (out[i-1]*float64(period-1) + v) / float64(period)
	}

	return out
}

// rollingMax computes the rolling maximum over a window of size `period` using
// a monotonic decreasing deque — O(n) total.
func rollingMax(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	// deque stores indices; front has the index of the current window max.
	deque := make([]int, 0, period)
	nanCount := 0

	for i := 0; i < n; i++ {
		// Track NaN entering the window
		if math.IsNaN(src[i]) {
			nanCount++
		}
		// Evict index that fell out of window
		if i >= period {
			if len(deque) > 0 && deque[0] <= i-period {
				deque = deque[1:]
			}
			if math.IsNaN(src[i-period]) {
				nanCount--
			}
		}
		// Remove smaller elements from the back
		for len(deque) > 0 && !math.IsNaN(src[i]) && src[deque[len(deque)-1]] <= src[i] {
			deque = deque[:len(deque)-1]
		}
		if !math.IsNaN(src[i]) {
			deque = append(deque, i)
		}

		if i < period-1 || nanCount > 0 || len(deque) == 0 {
			out[i] = math.NaN()
		} else {
			out[i] = src[deque[0]]
		}
	}
	return out
}

// rollingMin computes the rolling minimum over a window of size `period` using
// a monotonic increasing deque — O(n) total.
func rollingMin(src []float64, period int) []float64 {
	n := len(src)
	out := make([]float64, n)
	deque := make([]int, 0, period)
	nanCount := 0

	for i := 0; i < n; i++ {
		if math.IsNaN(src[i]) {
			nanCount++
		}
		if i >= period {
			if len(deque) > 0 && deque[0] <= i-period {
				deque = deque[1:]
			}
			if math.IsNaN(src[i-period]) {
				nanCount--
			}
		}
		for len(deque) > 0 && !math.IsNaN(src[i]) && src[deque[len(deque)-1]] >= src[i] {
			deque = deque[:len(deque)-1]
		}
		if !math.IsNaN(src[i]) {
			deque = append(deque, i)
		}

		if i < period-1 || nanCount > 0 || len(deque) == 0 {
			out[i] = math.NaN()
		} else {
			out[i] = src[deque[0]]
		}
	}
	return out
}
