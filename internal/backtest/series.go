package backtest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/pkg/feeds"
)

// alignSeries maps each primary bar to the latest secondary bar that is
// confirmed by the primary bar close.
//
// Confirmation rule:
// secondary_bar_close_time <= primary_bar_close_time
//
// Returns -1 for primary bars that precede all secondary data.
//
// Both inputs must be sorted ascending by timestamp.
func alignSeries(primary, secondary *DataSet, primaryInterval, secondaryInterval string) ([]int, error) {
	mapping := make([]int, primary.Len)
	if secondary.Len == 0 {
		for i := range mapping {
			mapping[i] = -1
		}
		return mapping, nil
	}

	primaryDur, primaryDurOK := parseIntervalDuration(primaryInterval)
	secondaryDur, secondaryDurOK := parseIntervalDuration(secondaryInterval)
	if !primaryDurOK || !secondaryDurOK {
		return nil, fmt.Errorf(
			"cannot align series with unparseable intervals (primary=%q secondary=%q): open-time fallback would introduce look-ahead bias",
			primaryInterval,
			secondaryInterval,
		)
	}

	secTimes := secondary.Timestamps
	for i, pt := range primary.Timestamps {
		target := pt.Add(primaryDur)

		// Binary search: find largest j where secondaryTime(j) <= target
		j := sort.Search(len(secTimes), func(k int) bool {
			secTime := secTimes[k].Add(secondaryDur)
			return secTime.After(target)
		}) - 1

		if j < 0 {
			mapping[i] = -1
		} else {
			mapping[i] = j
		}
	}
	return mapping, nil
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
