package feeds

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Window represents a predefined aggregation time window.
type Window struct {
	Label    string        // e.g. "1m", "5m", "1h", "1d"
	Duration time.Duration // wall-clock duration
}

func (w Window) String() string { return w.Label }

// TableSuffix returns the label used in ClickHouse table names (e.g., "1m").
func (w Window) TableSuffix() string { return w.Label }

// PredefinedWindows are all supported aggregation windows, ordered by duration.
var PredefinedWindows = []Window{
	{Label: "1m", Duration: 1 * time.Minute},
	{Label: "5m", Duration: 5 * time.Minute},
	{Label: "15m", Duration: 15 * time.Minute},
	{Label: "30m", Duration: 30 * time.Minute},
	{Label: "1h", Duration: 1 * time.Hour},
	{Label: "2h", Duration: 2 * time.Hour},
	{Label: "3h", Duration: 3 * time.Hour},
	{Label: "4h", Duration: 4 * time.Hour},
	{Label: "6h", Duration: 6 * time.Hour},
	{Label: "8h", Duration: 8 * time.Hour},
	{Label: "12h", Duration: 12 * time.Hour},
	{Label: "1d", Duration: 24 * time.Hour},
}

// ParseWindow looks up a window by label. Returns error if not found.
func ParseWindow(label string) (Window, error) {
	l := strings.TrimSpace(strings.ToLower(label))
	for _, w := range PredefinedWindows {
		if w.Label == l {
			return w, nil
		}
	}
	return Window{}, fmt.Errorf("unknown window %q", label)
}

// WindowsAbove returns predefined windows whose duration is >= the given window.
func WindowsAbove(src Window) []Window {
	out := make([]Window, 0, len(PredefinedWindows))
	for _, w := range PredefinedWindows {
		if w.Duration >= src.Duration {
			out = append(out, w)
		}
	}
	return out
}

// SmallestSourceWindow returns the shortest window in the slice.
func SmallestSourceWindow(ws []Window) Window {
	if len(ws) == 0 {
		return PredefinedWindows[0]
	}
	smallest := ws[0]
	for _, w := range ws[1:] {
		if w.Duration < smallest.Duration {
			smallest = w
		}
	}
	return smallest
}

// TableName returns the ClickHouse table name for a given feed and window.
// Convention: feed_<name>_<window>  e.g. feed_dvol_1m
func TableName(feedName string, w Window) string {
	return fmt.Sprintf("feed_%s_%s", strings.ToLower(feedName), w.Label)
}

// FloorTimestamp truncates a timestamp to the start of the given window.
func FloorTimestamp(t time.Time, w Window) time.Time {
	t = t.UTC()
	d := w.Duration
	return t.Truncate(d)
}

// AggregateBars aggregates fine-grained bars into a coarser window using
// standard OHLC rules: first open, max high, min low, last close.
// Input bars must be sorted ascending by timestamp.
func AggregateBars(bars []Bar, target Window) []Bar {
	if len(bars) == 0 {
		return nil
	}

	type bucket struct {
		symbol string
		ts     time.Time
		open   float64
		high   float64
		low    float64
		close  float64
	}

	buckets := make(map[string]*bucket) // key = "symbol|truncated_ts"

	for _, b := range bars {
		floored := FloorTimestamp(b.Timestamp, target)
		key := b.Symbol + "|" + floored.Format(time.RFC3339Nano)

		if existing, ok := buckets[key]; ok {
			if b.High > existing.high {
				existing.high = b.High
			}
			if b.Low < existing.low {
				existing.low = b.Low
			}
			existing.close = b.Close
		} else {
			buckets[key] = &bucket{
				symbol: b.Symbol,
				ts:     floored,
				open:   b.Open,
				high:   b.High,
				low:    b.Low,
				close:  b.Close,
			}
		}
	}

	out := make([]Bar, 0, len(buckets))
	for _, bkt := range buckets {
		out = append(out, Bar{
			Symbol:    bkt.symbol,
			Timestamp: bkt.ts,
			Open:      bkt.open,
			High:      bkt.high,
			Low:       bkt.low,
			Close:     bkt.close,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol != out[j].Symbol {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})

	return out
}
