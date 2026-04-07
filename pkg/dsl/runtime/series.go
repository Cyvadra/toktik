package runtime

import "math"

// Series is a bar-indexed time-series. Values are appended with Append() on
// each bar and accessed via Current() (offset 0) and At(n) for history.
type Series struct {
	data []float64
}

// NewSeries creates an empty series.
func NewSeries() *Series {
	return &Series{}
}

// Append adds a value for the current bar.
func (s *Series) Append(v float64) {
	s.data = append(s.data, v)
}

// Set overwrites the most recent bar value.
func (s *Series) Set(v float64) {
	if len(s.data) == 0 {
		s.data = append(s.data, v)
		return
	}
	s.data[len(s.data)-1] = v
}

// Current returns the most recent value.
func (s *Series) Current() float64 {
	if len(s.data) == 0 {
		return math.NaN()
	}
	return s.data[len(s.data)-1]
}

// At returns the value at offset n bars ago (0 = current).
func (s *Series) At(n int) float64 {
	idx := len(s.data) - 1 - n
	if idx < 0 || idx >= len(s.data) {
		return math.NaN()
	}
	return s.data[idx]
}

// Len returns the number of bars stored.
func (s *Series) Len() int {
	return len(s.data)
}

// Data returns the raw underlying slice (read-only usage intended).
func (s *Series) Data() []float64 {
	return s.data
}

// Last returns the n most recent values (oldest first).
func (s *Series) Last(n int) []float64 {
	if n > len(s.data) {
		n = len(s.data)
	}
	start := len(s.data) - n
	out := make([]float64, n)
	copy(out, s.data[start:])
	return out
}
