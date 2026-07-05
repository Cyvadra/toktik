package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

func TestLatestUSMarketCacheMergeStockBars(t *testing.T) {
	store := cache.NewMemoryStore()
	latest := NewLatestUSMarketCache(store, time.Hour)
	ctx := context.Background()

	if err := latest.StoreStockBars(ctx, "SPY", "fmp", false, []LatestUSStockDailyBar{{
		Timestamp:    time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Symbol:       "SPY",
		Open:         733.39,
		High:         737.36,
		Low:          731.5,
		Close:        737.21,
		Volume:       9333887,
		Transactions: 0,
	}}); err != nil {
		t.Fatalf("StoreStockBars failed: %v", err)
	}

	rows := []dto.USStockBarRow{{
		Timestamp: time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC),
		Symbol:    "SPY",
		Close:     737.05,
	}}
	merged, changed, err := latest.MergeStockBars(ctx, "SPY", time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC), false, rows)
	if err != nil {
		t.Fatalf("MergeStockBars failed: %v", err)
	}
	if !changed || len(merged) != 2 {
		t.Fatalf("expected merged latest bar, changed=%v rows=%#v", changed, merged)
	}
	if !merged[1].Timestamp.Equal(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)) || merged[1].Close != 737.21 {
		t.Fatalf("unexpected latest row: %#v", merged[1])
	}
}

func TestLatestUSMarketCacheMergeStockBarsSkipsMismatchedAdjustment(t *testing.T) {
	store := cache.NewMemoryStore()
	latest := NewLatestUSMarketCache(store, time.Hour)
	ctx := context.Background()

	if err := latest.StoreStockBars(ctx, "SPY", "fmp", false, []LatestUSStockDailyBar{{
		Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Symbol:    "SPY",
		Close:     737.21,
	}}); err != nil {
		t.Fatalf("StoreStockBars failed: %v", err)
	}

	rows := []dto.USStockBarRow{{
		Timestamp: time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC),
		Symbol:    "SPY",
		Close:     368.52,
	}}
	merged, changed, err := latest.MergeStockBars(ctx, "SPY", time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC), true, rows)
	if err != nil {
		t.Fatalf("MergeStockBars failed: %v", err)
	}
	if changed || len(merged) != 1 || merged[0].Close != rows[0].Close {
		t.Fatalf("expected mismatched adjusted cache to be skipped, changed=%v rows=%#v", changed, merged)
	}
}

func TestAggregateFMPIntradayStockBarsUsesSessionOrder(t *testing.T) {
	bars := aggregateFMPIntradayStockBars("spy", []fmp.IntradayBar{
		{Date: "2026-06-10 15:30:00", Open: 736, High: 738, Low: 735, Close: 737, Volume: 300},
		{Date: "2026-06-10 09:30:00", Open: 733, High: 734, Low: 732, Close: 733.5, Volume: 100},
		{Date: "2026-06-10 12:30:00", Open: 734, High: 736.5, Low: 733.25, Close: 736, Volume: 200},
		{Date: "2026-06-11 09:30:00", Open: 739, High: 740, Low: 738.5, Close: 739.5, Volume: 400},
	})
	if len(bars) != 2 {
		t.Fatalf("expected two daily bars, got %#v", bars)
	}
	first := bars[0]
	if !first.Timestamp.Equal(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)) || first.Symbol != "SPY" {
		t.Fatalf("unexpected first bar identity: %#v", first)
	}
	if first.Open != 733 || first.High != 738 || first.Low != 732 || first.Close != 737 || first.Volume != 600 {
		t.Fatalf("unexpected first daily aggregation: %#v", first)
	}
	if !bars[1].Timestamp.Equal(time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)) || bars[1].Close != 739.5 {
		t.Fatalf("unexpected second daily aggregation: %#v", bars[1])
	}
}

func TestLatestUSMarketPrewarmPoolUnion(t *testing.T) {
	screener := &fakeLatestPoolScreener{responses: map[int][]string{
		7:   {"AAPL", "SPY"},
		20:  {"SPY", "MSFT"},
		60:  {"NVDA"},
		120: {"AAPL", "QQQ"},
	}}
	pool, err := ResolveLatestUSMarketPrewarmPool(context.Background(), screener)
	if err != nil {
		t.Fatalf("ResolveLatestUSMarketPrewarmPool failed: %v", err)
	}
	want := []string{"AAPL", "SPY", "MSFT", "NVDA", "QQQ"}
	if len(pool) != len(want) {
		t.Fatalf("unexpected pool length: got %#v want %#v", pool, want)
	}
	for i := range want {
		if pool[i] != want[i] {
			t.Fatalf("unexpected pool: got %#v want %#v", pool, want)
		}
	}
}

func TestLatestUSMarketPrewarmPoolRequestsETFInclusiveCandidates(t *testing.T) {
	screener := &fakeLatestPoolScreener{responses: map[int][]string{
		7: {"AAPL", "SPY"},
	}}
	_, err := ResolveLatestUSMarketPrewarmPool(context.Background(), screener)
	if err != nil {
		t.Fatalf("ResolveLatestUSMarketPrewarmPool failed: %v", err)
	}
	seen := map[bool]bool{}
	for _, req := range screener.requests {
		if req.LookbackDays == 7 {
			seen[req.NonETFOnly] = true
		}
	}
	if !seen[true] || !seen[false] {
		t.Fatalf("expected non-ETF and ETF-inclusive requests for latest pool, got %#v", screener.requests)
	}
}

func TestPrioritizeLatestUSMarketPoolMovesSmokeSymbolsFirst(t *testing.T) {
	pool := []string{"AAPL", "MSFT", "NVDA", "QQQ", "SPY"}
	got := prioritizeLatestUSMarketPool(pool, []string{"SPY", "QQQ"})
	want := []string{"SPY", "QQQ", "AAPL", "MSFT", "NVDA"}
	if len(got) != len(want) {
		t.Fatalf("unexpected pool length: got %#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected priority pool: got %#v want %#v", got, want)
		}
	}
}

func TestPrioritizeLatestUSMarketPoolAddsAlwaysRefreshSymbols(t *testing.T) {
	pool := []string{"AAPL", "MSFT"}
	got := prioritizeLatestUSMarketPool(pool, []string{"SPY", "QQQ"})
	want := []string{"SPY", "QQQ", "AAPL", "MSFT"}
	if len(got) != len(want) {
		t.Fatalf("unexpected pool length: got %#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected priority pool: got %#v want %#v", got, want)
		}
	}
}

func TestPrioritizeLatestUSMarketPoolKeepsAlwaysRefreshSymbolsWhenPoolEmpty(t *testing.T) {
	got := prioritizeLatestUSMarketPool(nil, []string{" spy ", "QQQ", "spy", ""})
	want := []string{"SPY", "QQQ"}
	if len(got) != len(want) {
		t.Fatalf("unexpected pool length: got %#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected priority pool: got %#v want %#v", got, want)
		}
	}
}

func TestLatestUSMarketCacheMergeOptionChainReplacesSameDaySnapshot(t *testing.T) {
	store := cache.NewMemoryStore()
	latest := NewLatestUSMarketCache(store, time.Hour)
	ctx := context.Background()
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)

	if err := latest.StoreOptionChain(ctx, "SPY", "polygon", dto.USOptionChainSnapshot{
		Timestamp:  day.Add(16 * time.Hour),
		Underlying: "SPY",
		Contracts:  []dto.USOptionChainContract{{Symbol: "O:SPY260619C00740000", OptionType: "C", Expiration: day.AddDate(0, 0, 9), Strike: 740, Volume: 900}},
	}); err != nil {
		t.Fatalf("StoreOptionChain failed: %v", err)
	}

	rows := []dto.USOptionChainSnapshot{{
		Timestamp:  day,
		Underlying: "SPY",
		Contracts:  []dto.USOptionChainContract{{Symbol: "O:SPY260619C00735000", OptionType: "C", Expiration: day.AddDate(0, 0, 9), Strike: 735, Volume: 100}},
	}}
	merged, changed, err := latest.MergeOptionChain(ctx, "SPY", time.Time{}, day, day.AddDate(0, 0, 1), rows)
	if err != nil {
		t.Fatalf("MergeOptionChain failed: %v", err)
	}
	if !changed || len(merged) != 1 {
		t.Fatalf("expected same-day latest chain to replace historical, changed=%v rows=%#v", changed, merged)
	}
	if merged[0].Contracts[0].Symbol != "O:SPY260619C00740000" {
		t.Fatalf("expected latest snapshot contract, got %#v", merged[0].Contracts)
	}
}

func TestLatestUSMarketStageResultRecordsPartialSuccessAsRunSuccess(t *testing.T) {
	store := cache.NewMemoryStore()
	latest := NewLatestUSMarketCache(store, time.Hour)
	ctx := context.Background()
	now := time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)
	result := LatestUSMarketRefreshResult{StockSymbols: 1, Partial: true, Errors: []string{"provider failed"}}

	if err := latest.recordStageResult(ctx, latestUSMarketStageStockBars, now.Add(-time.Minute), now, 1, 1, context.DeadlineExceeded); err != nil {
		t.Fatalf("recordStageResult failed: %v", err)
	}
	if err := latest.recordRunResult(ctx, now, result, context.DeadlineExceeded); err != nil {
		t.Fatalf("recordRunResult failed: %v", err)
	}
	state, ok := latest.loadState(ctx)
	if !ok {
		t.Fatalf("expected refresh state")
	}
	if !state.LastSuccessAt.Equal(now) || state.ConsecutiveFailures != 0 {
		t.Fatalf("expected partial success to update success state, got %#v", state)
	}
	stage := state.StageResults[latestUSMarketStageStockBars]
	if stage.SuccessCount != 1 || stage.FailureCount != 1 || stage.LastError == "" {
		t.Fatalf("unexpected stage result: %#v", stage)
	}
}

func TestLatestUSMarketCacheLatestOptionChainSnapshotFiltersExpiration(t *testing.T) {
	store := cache.NewMemoryStore()
	latest := NewLatestUSMarketCache(store, time.Hour)
	ctx := context.Background()
	expiration := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)

	err := latest.StoreOptionChain(ctx, "AAPL", "polygon", dto.USOptionChainSnapshot{
		Timestamp:  time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
		Underlying: "AAPL",
		Contracts: []dto.USOptionChainContract{
			{Symbol: "O:AAPL260619C00200000", OptionType: "C", Expiration: expiration, Strike: 200},
			{Symbol: "O:AAPL260626P00190000", OptionType: "P", Expiration: expiration.AddDate(0, 0, 7), Strike: 190},
		},
	})
	if err != nil {
		t.Fatalf("StoreOptionChain failed: %v", err)
	}

	snapshot, ok, err := latest.LatestOptionChainSnapshot(ctx, "aapl", expiration)
	if err != nil {
		t.Fatalf("LatestOptionChainSnapshot failed: %v", err)
	}
	if !ok || len(snapshot.Contracts) != 1 || snapshot.Contracts[0].Symbol != "O:AAPL260619C00200000" {
		t.Fatalf("unexpected snapshot: ok=%v snapshot=%#v", ok, snapshot)
	}
}

type fakeLatestPoolScreener struct {
	responses map[int][]string
	requests  []dto.ScreenUSTurnoverIntersectionRequest
}

func (s *fakeLatestPoolScreener) ScreenUSTurnoverIntersection(_ context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error) {
	s.requests = append(s.requests, req)
	resp := &dto.ScreenUSTurnoverIntersectionResponse{Data: make([]dto.ScreenedUSTurnoverIntersectionRow, 0)}
	for _, symbol := range s.responses[req.LookbackDays] {
		resp.Data = append(resp.Data, dto.ScreenedUSTurnoverIntersectionRow{Underlying: symbol})
	}
	return resp, nil
}
