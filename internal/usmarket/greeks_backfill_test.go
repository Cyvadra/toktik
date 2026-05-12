package usmarket

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestApplyCalculatedGreeks(t *testing.T) {
	marketDate := time.Date(2025, 12, 23, 0, 0, 0, 0, time.UTC)
	expiration := time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC)
	rows := []OptionBar1m{
		{
			Timestamp:  time.Date(2025, 12, 23, 15, 0, 0, 0, time.UTC),
			Symbol:     "O:SPY251226C00480000",
			Underlying: "SPY",
			OptionType: "C",
			Expiration: expiration,
			Strike:     480,
			Close:      6.34,
			MarketDate: marketDate,
		},
		{
			Timestamp:  time.Date(2025, 12, 23, 15, 1, 0, 0, time.UTC),
			Symbol:     "O:QQQ251226P00470000",
			Underlying: "QQQ",
			OptionType: "P",
			Expiration: expiration,
			Strike:     470,
			Close:      5.15,
			MarketDate: marketDate,
		},
	}

	stockCloses := stockCloseSeries{
		"SPY": {{timestamp: rows[0].Timestamp.Unix(), close: 482.25}},
	}

	updatedRows, originalRows, matchedContracts, unmatchedContracts := ApplyCalculatedGreeks(rows, stockCloses, GreeksConfig{RiskFreeRate: 0.05})
	if len(updatedRows) != 1 {
		t.Fatalf("updated rows: got %d, want 1", len(updatedRows))
	}
	if len(originalRows) != 1 {
		t.Fatalf("original rows: got %d, want 1", len(originalRows))
	}
	if len(matchedContracts) != 1 {
		t.Fatalf("matched contracts: got %d, want 1", len(matchedContracts))
	}
	if len(unmatchedContracts) != 1 {
		t.Fatalf("unmatched contracts: got %d, want 1", len(unmatchedContracts))
	}

	bar := updatedRows[0]
	if bar.UnderlyingClose != 482.25 {
		t.Fatalf("underlying close: got %v, want 482.25", bar.UnderlyingClose)
	}
	if math.IsNaN(float64(bar.Delta)) || math.IsNaN(float64(bar.Gamma)) {
		t.Fatalf("expected finite greeks, got delta=%v gamma=%v", bar.Delta, bar.Gamma)
	}
}

func TestResolveCSVDateRange(t *testing.T) {
	from, to, ok, err := ResolveCSVDateRange([]string{
		"/tmp/2025-12-24.csv.gz",
		"/tmp/2025-12-23.csv",
		"/tmp/2025-12-26.csv.gz",
	})
	if err != nil {
		t.Fatalf("ResolveCSVDateRange returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got := from.Format("2006-01-02"); got != "2025-12-23" {
		t.Fatalf("unexpected start date: %s", got)
	}
	if got := to.Format("2006-01-02"); got != "2025-12-26" {
		t.Fatalf("unexpected end date: %s", got)
	}

	_, _, ok, err = ResolveCSVDateRange(nil)
	if err != nil {
		t.Fatalf("ResolveCSVDateRange(nil) returned error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for empty input")
	}
	_, _, _, err = ResolveCSVDateRange([]string{"/tmp/not-a-date.csv"})
	if err == nil || !strings.Contains(err.Error(), "cannot parse date") {
		t.Fatalf("expected parse error, got %v", err)
	}
}
