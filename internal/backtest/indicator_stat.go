package backtest

import (
	"math"
	"sort"
)

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
	return rollingMax(inputs[h.source], h.period)
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
	return rollingMin(inputs[l.source], l.period)
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
