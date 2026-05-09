package service

import (
	"context"
	"testing"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
)

func TestExpirationColUsesUSArrayJoinAlias(t *testing.T) {
	if got := expirationCol(false); got != "expiration_val" {
		t.Fatalf("expirationCol(false) = %q, want expiration_val", got)
	}
	if got := expirationCol(true); got != "m.expiration" {
		t.Fatalf("expirationCol(true) = %q, want m.expiration", got)
	}
}

func TestTurnoverIntersectionCandidateLimit(t *testing.T) {
	tests := []struct {
		limit int
		want  int
	}{
		{limit: 0, want: 0},
		{limit: 1, want: 2},
		{limit: 5, want: 7},
		{limit: 100, want: 135},
	}

	for _, tt := range tests {
		if got := turnoverIntersectionCandidateLimit(tt.limit); got != tt.want {
			t.Fatalf("turnoverIntersectionCandidateLimit(%d) = %d, want %d", tt.limit, got, tt.want)
		}
	}
}

func TestStoreUSTurnoverIntersectionInCacheSkipsEmptyResponses(t *testing.T) {
	store := cache.NewMemoryStore()
	svc := NewScreenerService(nil, store)
	key := usTurnoverIntersectionCacheKey(100, 20)
	resp := &dto.ScreenUSTurnoverIntersectionResponse{}

	if err := svc.storeUSTurnoverIntersectionInCache(context.Background(), key, resp); err != nil {
		t.Fatalf("storeUSTurnoverIntersectionInCache() error = %v", err)
	}
	if _, ok, err := store.Get(context.Background(), key); err != nil {
		t.Fatalf("cache.Get() error = %v", err)
	} else if ok {
		t.Fatalf("cache.Get() ok = true, want false for empty response")
	}
}

func TestUSTurnoverIntersectionCacheRoundTrip(t *testing.T) {
	store := cache.NewMemoryStore()
	svc := NewScreenerService(nil, store)
	key := usTurnoverIntersectionCacheKey(100, 20)
	want := &dto.ScreenUSTurnoverIntersectionResponse{
		LookbackDays:   20,
		Limit:          100,
		CandidateLimit: 135,
		Data: []dto.ScreenedUSTurnoverIntersectionRow{{
			Underlying:          "AAPL",
			StockTurnoverUSD:    1,
			OptionTurnoverUSD:   2,
			CombinedTurnoverUSD: 3,
		}},
	}

	if err := svc.storeUSTurnoverIntersectionInCache(context.Background(), key, want); err != nil {
		t.Fatalf("storeUSTurnoverIntersectionInCache() error = %v", err)
	}
	got, ok, err := svc.loadUSTurnoverIntersectionFromCache(context.Background(), key)
	if err != nil {
		t.Fatalf("loadUSTurnoverIntersectionFromCache() error = %v", err)
	}
	if !ok {
		t.Fatalf("loadUSTurnoverIntersectionFromCache() ok = false, want true")
	}
	if len(got.Data) != 1 || got.Data[0].Underlying != "AAPL" || got.CandidateLimit != 135 {
		t.Fatalf("unexpected cached response: %+v", got)
	}
}
