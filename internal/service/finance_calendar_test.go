package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
)

type failingCacheStore struct {
	err error
}

func (s *failingCacheStore) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}

func (s *failingCacheStore) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return s.err
}

func (s *failingCacheStore) Close() error {
	return nil
}

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

func TestFinanceCalendarEnsureEconomicCalendarSyncedPropagatesMarkerWriteFailure(t *testing.T) {
	storeErr := errors.New("cache write failed")
	svc := NewFinanceCalendarService(nil, nil, &failingCacheStore{err: storeErr})
	svc.syncEconomicCalendarFn = func(context.Context, time.Time, time.Time) (int, error) {
		return 2, nil
	}
	from := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	err := svc.ensureEconomicCalendarSynced(context.Background(), from, to)
	if err == nil {
		t.Fatal("expected marker write failure")
	}
	if !strings.Contains(err.Error(), "store economic calendar sync marker") {
		t.Fatalf("error = %v, want economic marker context", err)
	}
	if !errors.Is(err, storeErr) {
		t.Fatalf("error = %v, want cache error %v", err, storeErr)
	}
}
