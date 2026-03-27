package backtest

import (
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/pkg/feeds"
)

var _ = alignTimestamps

// alignSeries maps each primary bar to the latest secondary bar that is
// confirmed by the primary bar close.
//
// Confirmation rule:
// secondary_bar_close_time <= primary_bar_close_time
//
// When interval parsing fails, it falls back to open-time mapping:
// secondary_bar_open_time <= primary_bar_open_time
// Returns -1 for primary bars that precede all secondary data.
//
// Both inputs must be sorted ascending by timestamp.
func alignSeries(primary, secondary *DataSet, primaryInterval, secondaryInterval string) []int {
	mapping := make([]int, primary.Len)
	if secondary.Len == 0 {
		for i := range mapping {
			mapping[i] = -1
		}
		return mapping
	}

	primaryDur, primaryDurOK := parseIntervalDuration(primaryInterval)
	secondaryDur, secondaryDurOK := parseIntervalDuration(secondaryInterval)
	useCloseConfirmed := primaryDurOK && secondaryDurOK

	secTimes := secondary.Timestamps
	for i, pt := range primary.Timestamps {
		target := pt
		if useCloseConfirmed {
			target = target.Add(primaryDur)
		}

		// Binary search: find largest j where secondaryTime(j) <= target
		j := sort.Search(len(secTimes), func(k int) bool {
			secTime := secTimes[k]
			if useCloseConfirmed {
				secTime = secTime.Add(secondaryDur)
			}
			return secTime.After(target)
		}) - 1

		if j < 0 {
			mapping[i] = -1
		} else {
			mapping[i] = j
		}
	}
	return mapping
}

func parseIntervalDuration(interval string) (time.Duration, bool) {
	if interval == "" {
		return 0, false
	}

	normalized := strings.ToLower(strings.TrimSpace(interval))
	if normalized == "" {
		return 0, false
	}

	if w, err := feeds.ParseWindow(normalized); err == nil {
		return w.Duration, true
	}

	// Pine-style bare minute strings, e.g. "60", "240".
	if allDigits(normalized) {
		minutes, err := time.ParseDuration(normalized + "m")
		if err == nil {
			return minutes, true
		}
	}

	return 0, false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// alignTimestamps returns the alignment mapping between two timestamp slices.
// This is a convenience wrapper when you don't have full DataSets.
func alignTimestamps(primary, secondary []time.Time) []int {
	p := &DataSet{Timestamps: primary, Len: len(primary)}
	s := &DataSet{Timestamps: secondary, Len: len(secondary)}
	return alignSeries(p, s, "", "")
}
