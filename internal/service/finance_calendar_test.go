package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
)

func TestFinanceCalendarStockSyncCacheKeyCanonicalizesSymbols(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	left := financeCalendarStockSyncCacheKey([]string{"MSFT", "AAPL", "NVDA"}, from, to)
	right := financeCalendarStockSyncCacheKey([]string{"NVDA", "AAPL", "MSFT"}, from, to)

	if left != right {
		t.Fatalf("financeCalendarStockSyncCacheKey should be order-insensitive: %q != %q", left, right)
	}
}

func TestFinanceCalendarEnsureEconomicCalendarSyncedUsesMarkerCache(t *testing.T) {
	store := cache.NewMemoryStore()
	svc := NewFinanceCalendarService(nil, nil, store)
	fixedNow := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }
	from := fixedNow.AddDate(0, 0, -7)
	to := fixedNow.AddDate(0, 0, 30)

	if err := svc.storeSyncMarker(context.Background(), financeCalendarEconomicSyncCacheKey(from, to)); err != nil {
		t.Fatalf("storeSyncMarker() error = %v", err)
	}
	if err := svc.ensureEconomicCalendarSynced(context.Background(), from, to); err != nil {
		t.Fatalf("ensureEconomicCalendarSynced() with fresh marker error = %v", err)
	}
}
