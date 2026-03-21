package backtest

import (
	"sort"
	"time"
)

var _ = alignTimestamps

// alignSeries maps each primary timestamp to the index of the latest
// secondary bar whose timestamp is ≤ the primary timestamp.
// Returns -1 for primary bars that precede all secondary data.
//
// Both inputs must be sorted ascending by timestamp.
func alignSeries(primary, secondary *DataSet) []int {
	mapping := make([]int, primary.Len)
	if secondary.Len == 0 {
		for i := range mapping {
			mapping[i] = -1
		}
		return mapping
	}

	secTimes := secondary.Timestamps
	for i, pt := range primary.Timestamps {
		// Binary search: find largest j where secTimes[j] <= pt
		j := sort.Search(len(secTimes), func(k int) bool {
			return secTimes[k].After(pt)
		}) - 1

		if j < 0 {
			mapping[i] = -1
		} else {
			mapping[i] = j
		}
	}
	return mapping
}

// alignTimestamps returns the alignment mapping between two timestamp slices.
// This is a convenience wrapper when you don't have full DataSets.
func alignTimestamps(primary, secondary []time.Time) []int {
	p := &DataSet{Timestamps: primary, Len: len(primary)}
	s := &DataSet{Timestamps: secondary, Len: len(secondary)}
	return alignSeries(p, s)
}
