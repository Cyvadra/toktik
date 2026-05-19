package usmarket

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/pkg/fmp"
	"github.com/Cyvadra/toktik/pkg/tigerapi"
)

func TestTigerPEBackfillProviderName(t *testing.T) {
	provider := NewTigerPEBackfillProvider(tigerapi.Config{})
	if provider.Name() != "tiger" {
		t.Fatalf("expected provider name tiger, got %q", provider.Name())
	}
}

func TestRequestLimiterAllowsZeroQPS(t *testing.T) {
	if err := newRequestLimiter(0).Wait(context.Background()); err != nil {
		t.Fatalf("expected nil limiter wait to succeed, got %v", err)
	}
}

func TestRequestLimiterBackoffDelaysNextSlot(t *testing.T) {
	limiter := newRequestLimiter(1000)
	if err := limiter.Backoff(context.Background(), 25*time.Millisecond); err != nil {
		t.Fatalf("backoff: %v", err)
	}
	start := time.Now()
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("expected backoff delay, elapsed=%v", elapsed)
	}
}

func TestExtractPEObservations(t *testing.T) {
	bars := []tigerapi.KlineBar{
		{Symbol: "AAPL", Time: 1712534400000, Fundamentals: map[string]any{"ttmPeRate": 31.8}},
		{Symbol: "AAPL", Time: 1712620800000, Fundamentals: map[string]any{"ttm_pe_rate": "32.1"}},
		{Symbol: "AAPL", Time: 1712707200000, Fundamentals: map[string]any{"turnoverRate": 0.11}},
	}

	observations := extractPEObservationsFromTigerBars("AAPL", bars)
	if len(observations) != 2 {
		t.Fatalf("expected 2 PE observations, got %d", len(observations))
	}
	if observations[0].Value != 31.8 {
		t.Fatalf("expected first PE value 31.8, got %v", observations[0].Value)
	}
	if observations[1].Value != 32.1 {
		t.Fatalf("expected second PE value 32.1, got %v", observations[1].Value)
	}
	if observations[0].EventTS.UTC() != time.UnixMilli(1712534400000).UTC() {
		t.Fatalf("unexpected event time: %v", observations[0].EventTS)
	}
}

func TestPlanPEObservationInsertsSkipsUnchangedAndRevisesChanged(t *testing.T) {
	observations := []fundamentalObservationInsert{
		{Symbol: "AAPL", FactorCode: usStocksPEFactorCode, EventTS: time.UnixMilli(1712534400000).UTC(), KnownAt: time.UnixMilli(1712534400000).UTC(), Value: 31.8},
		{Symbol: "AAPL", FactorCode: usStocksPEFactorCode, EventTS: time.UnixMilli(1712620800000).UTC(), KnownAt: time.UnixMilli(1712620800000).UTC(), Value: 32.5},
	}
	existing := map[fundamentalObservationKey]existingFundamentalObservation{
		{FactorCode: usStocksPEFactorCode, EventTS: 1712534400000, KnownAt: 1712534400000}: {Value: 31.8, Revision: 0},
		{FactorCode: usStocksPEFactorCode, EventTS: 1712620800000, KnownAt: 1712620800000}: {Value: 32.0, Revision: 2},
	}

	planned, skipped := planFundamentalObservationInserts(observations, existing)
	if skipped != 1 {
		t.Fatalf("expected 1 skipped observation, got %d", skipped)
	}
	if len(planned) != 1 {
		t.Fatalf("expected 1 planned observation, got %d", len(planned))
	}
	if planned[0].Revision != 3 {
		t.Fatalf("expected revision bump to 3, got %d", planned[0].Revision)
	}
}

func TestFilterFMPFundamentalSymbolsExcludesUnitsAndWarrants(t *testing.T) {
	input := []string{"AAPL", "BRK.B", "AAC.U", "AAC.WS", "XYZ.PR", "MSFT"}
	filtered := filterFMPFundamentalSymbols(input)
	want := []string{"AAPL", "BRK.B", "MSFT"}
	if len(filtered) != len(want) {
		t.Fatalf("expected %d symbols, got %d: %#v", len(want), len(filtered), filtered)
	}
	for index := range want {
		if filtered[index] != want[index] {
			t.Fatalf("unexpected filtered symbols: want %#v got %#v", want, filtered)
		}
	}
}

func TestFMPFundamentalsIncrementalModeNormalization(t *testing.T) {
	cases := map[string]string{
		"":                            "",
		"off":                         "",
		"none":                        "",
		"latest":                      "latest-financial-statements",
		"latest_financial_statements": "latest-financial-statements",
		"latest-financial-statements": "latest-financial-statements",
		"LATEST-FINANCIAL-STATEMENTS": "latest-financial-statements",
	}
	for input, want := range cases {
		if got := normalizeFMPFundamentalsIncrementalMode(input); got != want {
			t.Fatalf("normalize %q: want %q got %q", input, want, got)
		}
	}
}

func TestFMPLatestStatementCandidateParsesPeriodAndKnownAt(t *testing.T) {
	candidate, ok := fmpLatestStatementCandidate(fmp.LatestFinancialStatement{Symbol: "aapl", Date: "2026-03-31", FilingDate: "2026-05-01", AcceptedDate: "2026-05-01 16:12:00"})
	if !ok {
		t.Fatal("expected candidate")
	}
	if candidate.Symbol != "AAPL" {
		t.Fatalf("unexpected symbol %q", candidate.Symbol)
	}
	if got := candidate.PeriodDate.Format("2006-01-02"); got != "2026-03-31" {
		t.Fatalf("unexpected period date %s", got)
	}
	if got := candidate.KnownAt.Format("2006-01-02 15:04:05"); got != "2026-05-01 16:12:00" {
		t.Fatalf("unexpected known_at %s", got)
	}
}

func TestFMPFundamentalCandidateFreshnessRequiresPEAndPB(t *testing.T) {
	candidate := fmpFundamentalsDiscoveryCandidate{Symbol: "AAPL", PeriodDate: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 5, 1, 16, 12, 0, 0, time.UTC)}
	fresh := map[string]existingFundamentalFreshness{
		usStocksPEFactorCode: {LatestEventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), LatestKnownAt: time.Date(2026, 5, 1, 16, 12, 0, 0, time.UTC)},
		usStocksPBFactorCode: {LatestEventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), LatestKnownAt: time.Date(2026, 5, 1, 16, 12, 0, 0, time.UTC)},
	}
	if !fmpFundamentalCandidateIsFresh(candidate, fresh) {
		t.Fatal("expected candidate to be fresh")
	}
	delete(fresh, usStocksPBFactorCode)
	if fmpFundamentalCandidateIsFresh(candidate, fresh) {
		t.Fatal("expected missing PB to require update")
	}
}
