package service

import (
	"context"
	"strings"
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
	key := usTurnoverIntersectionCacheKey(100, 20, false)
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
	key := usTurnoverIntersectionCacheKey(100, 20, false)
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

func TestUSTurnoverIntersectionCacheKeyIncludesUniverseFilter(t *testing.T) {
	withETF := usTurnoverIntersectionCacheKey(100, 20, false)
	nonETFOnly := usTurnoverIntersectionCacheKey(100, 20, true)

	if withETF == nonETFOnly {
		t.Fatalf("cache key should differ by universe filter: %q", withETF)
	}
}

func TestUSStocksFundamentalsUniverseFilterClauseRequiresPEAndPB(t *testing.T) {
	clause := usStocksFundamentalsUniverseFilterClause("symbol")
	if !strings.Contains(clause, "factor_code IN ('pe', 'pb')") {
		t.Fatalf("expected pe/pb filter in clause, got %q", clause)
	}
	if !strings.Contains(clause, "HAVING countDistinct(factor_code) = 2") {
		t.Fatalf("expected both factors requirement in clause, got %q", clause)
	}
	if !strings.Contains(clause, "AND symbol IN") {
		t.Fatalf("expected symbol column to be interpolated, got %q", clause)
	}
}

func TestFilterNonETFUSTurnoverResultsDropsETFProfiles(t *testing.T) {
	provider := &stubUSStockCompanyProfileProvider{}
	provider.requests = nil
	svc := NewScreenerService(nil).WithCompanyProfileProvider(provider)
	rows := []dto.ScreenedUSTurnoverIntersectionRow{{Underlying: "AAPL"}, {Underlying: "SLV"}, {Underlying: "MSFT"}}
	providerBySymbol := map[string]*dto.USStockCompanyProfile{
		"AAPL": {Symbol: "AAPL"},
		"SLV":  {Symbol: "SLV", IsETF: true},
		"MSFT": {Symbol: "MSFT"},
	}
	providerFn := &stubUSStockCompanyProfileProviderFunc{fn: func(_ context.Context, symbol string) (*dto.USStockCompanyProfile, error) {
		return providerBySymbol[symbol], nil
	}}
	svc.companyInfo = providerFn

	got := svc.filterNonETFUSTurnoverResults(context.Background(), rows)
	if len(got) != 2 || got[0].Underlying != "AAPL" || got[1].Underlying != "MSFT" {
		t.Fatalf("unexpected filtered rows: %+v", got)
	}
}

type stubUSStockCompanyProfileProviderFunc struct {
	fn func(context.Context, string) (*dto.USStockCompanyProfile, error)
}

func (s *stubUSStockCompanyProfileProviderFunc) CompanyProfile(ctx context.Context, symbol string) (*dto.USStockCompanyProfile, error) {
	return s.fn(ctx, symbol)
}

func (s *stubUSStockCompanyProfileProviderFunc) IsETFLike(ctx context.Context, symbol string) (bool, error) {
	profile, err := s.CompanyProfile(ctx, symbol)
	if err != nil {
		return false, err
	}
	return isETFLikeUSStockProfile(profile), nil
}

func (s *stubUSStockCompanyProfileProviderFunc) IsETFLikeBySymbol(ctx context.Context, symbols []string) (map[string]bool, error) {
	result := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		isETFLike, err := s.IsETFLike(ctx, symbol)
		if err != nil {
			return nil, err
		}
		result[symbol] = isETFLike
	}
	return result, nil
}
