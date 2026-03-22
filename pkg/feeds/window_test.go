package feeds

import (
	"testing"
	"time"
)

func TestParseWindow(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"1m", "1m", false},
		{"5m", "5m", false},
		{"15m", "15m", false},
		{"1h", "1h", false},
		{"4h", "4h", false},
		{"12h", "12h", false},
		{"1d", "1d", false},
		{"99x", "", true},
	}

	for _, tc := range tests {
		w, err := ParseWindow(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseWindow(%q) expected error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseWindow(%q) unexpected error: %v", tc.input, err)
		}
		if w.Label != tc.want {
			t.Fatalf("ParseWindow(%q) = %q, want %q", tc.input, w.Label, tc.want)
		}
	}
}

func TestTableName(t *testing.T) {
	w, _ := ParseWindow("1m")
	name := TableName("dvol", w)
	if name != "feed_dvol_1m" {
		t.Fatalf("got %q, want %q", name, "feed_dvol_1m")
	}

	w, _ = ParseWindow("1d")
	name = TableName("dvol", w)
	if name != "feed_dvol_1d" {
		t.Fatalf("got %q, want %q", name, "feed_dvol_1d")
	}
}

func TestAggregateBars(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	bars := []Bar{
		{Symbol: "BTC", Timestamp: base, Open: 50, High: 55, Low: 48, Close: 53},
		{Symbol: "BTC", Timestamp: base.Add(1 * time.Minute), Open: 53, High: 58, Low: 51, Close: 56},
		{Symbol: "BTC", Timestamp: base.Add(2 * time.Minute), Open: 56, High: 60, Low: 54, Close: 59},
		{Symbol: "BTC", Timestamp: base.Add(3 * time.Minute), Open: 59, High: 61, Low: 57, Close: 58},
		{Symbol: "BTC", Timestamp: base.Add(4 * time.Minute), Open: 58, High: 62, Low: 55, Close: 60},
	}

	target, _ := ParseWindow("5m")
	agg := AggregateBars(bars, target)

	if len(agg) != 1 {
		t.Fatalf("expected 1 aggregated bar, got %d", len(agg))
	}

	b := agg[0]
	if b.Open != 50 {
		t.Fatalf("open: got %f, want 50", b.Open)
	}
	if b.High != 62 {
		t.Fatalf("high: got %f, want 62", b.High)
	}
	if b.Low != 48 {
		t.Fatalf("low: got %f, want 48", b.Low)
	}
	if b.Close != 60 {
		t.Fatalf("close: got %f, want 60", b.Close)
	}
}

func TestAggregateBarsMultipleBuckets(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	bars := []Bar{
		{Symbol: "BTC", Timestamp: base, Open: 50, High: 55, Low: 48, Close: 53},
		{Symbol: "BTC", Timestamp: base.Add(1 * time.Minute), Open: 53, High: 58, Low: 51, Close: 56},
		// Next 5m bucket
		{Symbol: "BTC", Timestamp: base.Add(5 * time.Minute), Open: 56, High: 60, Low: 54, Close: 59},
		{Symbol: "BTC", Timestamp: base.Add(6 * time.Minute), Open: 59, High: 61, Low: 57, Close: 58},
	}

	target, _ := ParseWindow("5m")
	agg := AggregateBars(bars, target)

	if len(agg) != 2 {
		t.Fatalf("expected 2 aggregated bars, got %d", len(agg))
	}

	if agg[0].Open != 50 || agg[0].Close != 56 {
		t.Fatalf("first bucket: open=%f close=%f, want 50/56", agg[0].Open, agg[0].Close)
	}
	if agg[1].Open != 56 || agg[1].Close != 58 {
		t.Fatalf("second bucket: open=%f close=%f, want 56/58", agg[1].Open, agg[1].Close)
	}
}

func TestWindowsAbove(t *testing.T) {
	w1m, _ := ParseWindow("1m")
	above := WindowsAbove(w1m)
	if len(above) != len(PredefinedWindows) {
		t.Fatalf("expected %d windows above 1m, got %d", len(PredefinedWindows), len(above))
	}

	w1h, _ := ParseWindow("1h")
	above = WindowsAbove(w1h)
	// 1h, 2h, 3h, 4h, 6h, 8h, 12h, 1d = 8
	if len(above) != 8 {
		t.Fatalf("expected 8 windows above/equal 1h, got %d", len(above))
	}
}

func TestFloorTimestamp(t *testing.T) {
	ts := time.Date(2026, 1, 1, 14, 37, 22, 0, time.UTC)
	w5m, _ := ParseWindow("5m")
	floored := FloorTimestamp(ts, w5m)
	want := time.Date(2026, 1, 1, 14, 35, 0, 0, time.UTC)
	if !floored.Equal(want) {
		t.Fatalf("FloorTimestamp got %v, want %v", floored, want)
	}
}
