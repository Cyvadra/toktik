package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

type stubUSStockCompanyProfileProvider struct {
	profile  *dto.USStockCompanyProfile
	err      error
	requests []string
}

func (s *stubUSStockCompanyProfileProvider) CompanyProfile(_ context.Context, symbol string) (*dto.USStockCompanyProfile, error) {
	s.requests = append(s.requests, symbol)
	return s.profile, s.err
}

func (s *stubUSStockCompanyProfileProvider) IsETFLike(ctx context.Context, symbol string) (bool, error) {
	profile, err := s.CompanyProfile(ctx, symbol)
	if err != nil {
		return false, err
	}
	return isETFLikeUSStockProfile(profile), nil
}

func (s *stubUSStockCompanyProfileProvider) IsETFLikeBySymbol(ctx context.Context, symbols []string) (map[string]bool, error) {
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

type stubFMPCompanyProfiler struct {
	profile  *dto.USStockCompanyProfile
	profiles map[string]*dto.USStockCompanyProfile
	count    int
}

func (s *stubFMPCompanyProfiler) Profile(_ context.Context, symbol string) (*fmp.Profile, error) {
	s.count++
	profile := s.profileForSymbol(symbol)
	return &fmp.Profile{Symbol: symbol, Sector: profile.Sector, Industry: profile.Industry, IsETF: profile.IsETF, IsFund: profile.IsFund}, nil
}

func (s *stubFMPCompanyProfiler) Profiles(_ context.Context, symbols []string) ([]fmp.Profile, error) {
	s.count++
	profiles := make([]fmp.Profile, 0, len(symbols))
	for _, symbol := range symbols {
		profile := s.profileForSymbol(symbol)
		profiles = append(profiles, fmp.Profile{Symbol: symbol, Sector: profile.Sector, Industry: profile.Industry, IsETF: profile.IsETF, IsFund: profile.IsFund})
	}
	return profiles, nil
}

func (s *stubFMPCompanyProfiler) profileForSymbol(symbol string) *dto.USStockCompanyProfile {
	if s.profiles != nil {
		if profile, ok := s.profiles[symbol]; ok && profile != nil {
			return profile
		}
	}
	if s.profile != nil {
		return s.profile
	}
	return &dto.USStockCompanyProfile{Symbol: symbol}
}

type stubUSStockFundamentals struct {
	snapshotReqs []dto.FundamentalSnapshotRequest
	seriesReqs   []dto.FundamentalSeriesRequest
	snapshotResp *dto.FundamentalSnapshotResponse
	seriesResp   map[string]*dto.FundamentalSeriesResponse
}

func (s *stubUSStockFundamentals) QuerySnapshot(_ context.Context, req dto.FundamentalSnapshotRequest) (*dto.FundamentalSnapshotResponse, error) {
	s.snapshotReqs = append(s.snapshotReqs, req)
	if s.snapshotResp == nil {
		return &dto.FundamentalSnapshotResponse{}, nil
	}
	return s.snapshotResp, nil
}

func (s *stubUSStockFundamentals) QuerySeries(_ context.Context, req dto.FundamentalSeriesRequest) (*dto.FundamentalSeriesResponse, error) {
	s.seriesReqs = append(s.seriesReqs, req)
	if resp, ok := s.seriesResp[req.Factor]; ok {
		return resp, nil
	}
	return &dto.FundamentalSeriesResponse{}, nil
}

func TestUSStocksAttachFundamentalsAlignsPointInTimeValuesToBars(t *testing.T) {
	start := time.Date(2026, 4, 30, 13, 30, 0, 0, time.UTC)
	bars := []dto.USStockBarRow{
		{Timestamp: start, Symbol: "AAPL"},
		{Timestamp: start.Add(24 * time.Hour), Symbol: "AAPL"},
		{Timestamp: start.Add(48 * time.Hour), Symbol: "AAPL"},
	}

	stub := &stubUSStockFundamentals{
		snapshotResp: &dto.FundamentalSnapshotResponse{
			Data: []dto.FundamentalSnapshotEntry{
				{Factor: "pe", EventTS: time.Date(2025, 12, 27, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 1, 30, 6, 1, 32, 0, time.UTC), Value: 34.56, Source: "fmp_quarter_statements"},
			},
		},
		seriesResp: map[string]*dto.FundamentalSeriesResponse{
			"pe": {
				Data: []dto.FundamentalSeriesPoint{
					{EventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), Value: 22.03, Source: "fmp_quarter_statements", Revision: 0},
				},
			},
			"pb": {
				Data: []dto.FundamentalSeriesPoint{
					{EventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), Value: 6.65, Source: "fmp_quarter_statements", Revision: 0},
				},
			},
		},
	}

	svc := NewUSStocksService(nil, stub)
	if err := svc.attachFundamentals(context.Background(), "AAPL", []string{"pe", "pb"}, bars); err != nil {
		t.Fatalf("attachFundamentals returned error: %v", err)
	}

	if got := bars[0].Fundamentals["pe"].Value; got != 34.56 {
		t.Fatalf("expected first bar to use snapshot PE, got %v", got)
	}
	if _, ok := bars[0].Fundamentals["pb"]; ok {
		t.Fatalf("expected first bar to omit PB before first known value, got %#v", bars[0].Fundamentals["pb"])
	}
	if got := bars[1].Fundamentals["pe"].Value; got != 22.03 {
		t.Fatalf("expected second bar to pick up newly known PE, got %v", got)
	}
	if got := bars[1].Fundamentals["pb"].Value; got != 6.65 {
		t.Fatalf("expected second bar to pick up newly known PB, got %v", got)
	}
	if !bars[2].Fundamentals["pe"].Filled || !bars[2].Fundamentals["pb"].Filled {
		t.Fatalf("expected later bars to expose carried-forward fundamentals, got %#v", bars[2].Fundamentals)
	}
	if len(stub.snapshotReqs) != 1 {
		t.Fatalf("expected one snapshot request, got %d", len(stub.snapshotReqs))
	}
	if len(stub.seriesReqs) != 2 {
		t.Fatalf("expected one series request per factor, got %d", len(stub.seriesReqs))
	}
}

func TestNormalizeRequestedFactorsSplitsCommaSeparatedValues(t *testing.T) {
	got := normalizeRequestedFactors([]string{"pe,pb", " pb ", "pe", ""})
	if len(got) != 2 || got[0] != "pe" || got[1] != "pb" {
		t.Fatalf("normalizeRequestedFactors returned %#v", got)
	}
}

func TestPriceDerivedFundamentalValueUsesCurrentBarCloseForPEAndPB(t *testing.T) {
	observation := dto.USStockBarFundamentalValue{
		EventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		Value:   20,
	}
	denominators := map[string]float64{
		fundamentalObservationKey("pe", observation.EventTS): 10,
	}
	got := priceDerivedFundamentalValue(usStockFundamentalBinding{ResponseFactor: "pe", PriceDerived: true}, 250, observation, denominators)
	if got != 25 {
		t.Fatalf("expected price-derived PE to be recomputed from bar close, got %v", got)
	}
	if got := priceDerivedFundamentalValue(usStockFundamentalBinding{ResponseFactor: "market_cap"}, 250, observation, denominators); got != 20 {
		t.Fatalf("expected non price-derived factor to keep stored value, got %v", got)
	}
}

func TestResolveUSStockFundamentalBindingsMapsIndexPEToPE10Live(t *testing.T) {
	bindings := resolveUSStockFundamentalBindings("QQQ", []string{"pe", "pb"})
	if len(bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(bindings))
	}
	if bindings[0].ResponseFactor != "pe" || bindings[0].SourceFactor != virtualFundamentalFactorPE10Live || bindings[0].PriceDerived {
		t.Fatalf("unexpected PE binding: %#v", bindings[0])
	}
	if bindings[1].ResponseFactor != "pb" || bindings[1].SourceFactor != "pb" || !bindings[1].PriceDerived {
		t.Fatalf("unexpected PB binding: %#v", bindings[1])
	}

	nonIndex := resolveUSStockFundamentalBindings("AAPL", []string{"pe"})
	if len(nonIndex) != 1 || nonIndex[0].SourceFactor != "pe" || !nonIndex[0].PriceDerived {
		t.Fatalf("unexpected non-index binding: %#v", nonIndex)
	}
}

func TestUSStocksAttachFundamentalsUsesPE10LiveForIndexPE(t *testing.T) {
	start := time.Date(2026, 4, 30, 13, 30, 0, 0, time.UTC)
	bars := []dto.USStockBarRow{
		{Timestamp: start, Symbol: "QQQ", Close: 510},
		{Timestamp: start.Add(24 * time.Hour), Symbol: "QQQ", Close: 520},
	}

	stub := &stubUSStockFundamentals{
		snapshotResp: &dto.FundamentalSnapshotResponse{
			Data: []dto.FundamentalSnapshotEntry{
				{Factor: virtualFundamentalFactorPE10Live, EventTS: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), KnownAt: start, Value: 31.5, Source: macroVirtualFactorSource},
			},
		},
		seriesResp: map[string]*dto.FundamentalSeriesResponse{
			virtualFundamentalFactorPE10Live: {
				Data: []dto.FundamentalSeriesPoint{{EventTS: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), KnownAt: start.Add(24 * time.Hour), Value: 32.25, Source: macroVirtualFactorSource}},
			},
		},
	}

	svc := NewUSStocksService(nil, stub)
	if err := svc.attachFundamentals(context.Background(), "QQQ", []string{"pe"}, bars); err != nil {
		t.Fatalf("attachFundamentals returned error: %v", err)
	}
	if len(stub.snapshotReqs) != 1 || len(stub.snapshotReqs[0].Factors) != 1 || stub.snapshotReqs[0].Factors[0] != virtualFundamentalFactorPE10Live {
		t.Fatalf("expected snapshot request for pe10_live, got %#v", stub.snapshotReqs)
	}
	if len(stub.seriesReqs) != 1 || stub.seriesReqs[0].Factor != virtualFundamentalFactorPE10Live {
		t.Fatalf("expected series request for pe10_live, got %#v", stub.seriesReqs)
	}
	if got := bars[0].Fundamentals["pe"].Value; got != 31.5 {
		t.Fatalf("expected first index PE bar to use pe10_live, got %v", got)
	}
	if got := bars[1].Fundamentals["pe"].Value; got != 32.25 {
		t.Fatalf("expected later index PE bar to use updated pe10_live, got %v", got)
	}
}

func TestUSStocksAttachCompanyProfileAddsMeta(t *testing.T) {
	provider := &stubUSStockCompanyProfileProvider{profile: &dto.USStockCompanyProfile{Symbol: "AAPL", Sector: "Technology", Industry: "Consumer Electronics"}}
	svc := NewUSStocksService(nil).WithCompanyProfileProvider(provider)
	resp := &dto.USStockBarResponse{Data: []dto.USStockBarRow{{Symbol: "AAPL"}}}

	svc.attachCompanyProfile(context.Background(), "AAPL", resp)

	if resp.Meta == nil || resp.Meta.Profile == nil {
		t.Fatalf("expected company profile metadata to be attached, got %#v", resp.Meta)
	}
	if resp.Meta.Profile.Sector != "Technology" || resp.Meta.Profile.Industry != "Consumer Electronics" {
		t.Fatalf("unexpected company profile metadata: %#v", resp.Meta.Profile)
	}
	if len(provider.requests) != 1 || provider.requests[0] != "AAPL" {
		t.Fatalf("expected provider request for AAPL, got %#v", provider.requests)
	}
}

func TestUSStocksAttachCompanyProfilesToSymbolsAddsProfilesPerRow(t *testing.T) {
	provider := &stubUSStockCompanyProfileProvider{profile: &dto.USStockCompanyProfile{Symbol: "AAPL", Sector: "Technology", Industry: "Consumer Electronics"}}
	svc := NewUSStocksService(nil).WithCompanyProfileProvider(provider)
	rows := []dto.USStockSymbolRow{{Symbol: "AAPL"}, {Symbol: "MSFT"}}

	svc.attachCompanyProfilesToSymbols(context.Background(), rows)

	if rows[0].Profile == nil || rows[1].Profile == nil {
		t.Fatalf("expected profiles on all symbol rows, got %#v", rows)
	}
	if len(provider.requests) != 2 || provider.requests[0] != "AAPL" || provider.requests[1] != "MSFT" {
		t.Fatalf("expected provider requests for each symbol, got %#v", provider.requests)
	}
}

func TestCachedFMPUSStockCompanyProfileProviderUsesCache(t *testing.T) {
	store := cache.NewMemoryStore()
	client := &stubFMPCompanyProfiler{profile: &dto.USStockCompanyProfile{Sector: "Technology", Industry: "Consumer Electronics"}}
	provider := &cachedFMPUSStockCompanyProfileProvider{
		client: client,
		cache:  store,
		ttlValue: func() time.Duration {
			return time.Hour
		},
	}

	first, err := provider.CompanyProfile(context.Background(), "aapl")
	if err != nil {
		t.Fatalf("first CompanyProfile returned error: %v", err)
	}
	second, err := provider.CompanyProfile(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("second CompanyProfile returned error: %v", err)
	}

	if client.count != 1 {
		t.Fatalf("expected one upstream FMP request, got %d", client.count)
	}
	if first == nil || second == nil {
		t.Fatalf("expected cached company profile, got first=%#v second=%#v", first, second)
	}
	if second.Symbol != "AAPL" || second.Sector != "Technology" || second.Industry != "Consumer Electronics" {
		t.Fatalf("unexpected cached profile: %#v", second)
	}
}

func TestCachedFMPUSStockCompanyProfileProviderKeepsETFClassification(t *testing.T) {
	store := cache.NewMemoryStore()
	client := &stubFMPCompanyProfiler{profile: &dto.USStockCompanyProfile{IsETF: true}}
	provider := &cachedFMPUSStockCompanyProfileProvider{
		client: client,
		cache:  store,
		ttlValue: func() time.Duration {
			return time.Hour
		},
	}

	profile, err := provider.CompanyProfile(context.Background(), "SLV")
	if err != nil {
		t.Fatalf("CompanyProfile returned error: %v", err)
	}
	if profile == nil || !profile.IsETF {
		t.Fatalf("expected ETF classification to be cached, got %#v", profile)
	}
	if client.count != 1 {
		t.Fatalf("expected one upstream FMP request, got %d", client.count)
	}
	profile, err = provider.CompanyProfile(context.Background(), "slv")
	if err != nil {
		t.Fatalf("second CompanyProfile returned error: %v", err)
	}
	if profile == nil || !profile.IsETF {
		t.Fatalf("expected cached ETF classification, got %#v", profile)
	}
	if client.count != 1 {
		t.Fatalf("expected cache hit on second request, got %d upstream calls", client.count)
	}
}

func TestCachedFMPUSStockCompanyProfileProviderKeepsFundClassification(t *testing.T) {
	store := cache.NewMemoryStore()
	client := &stubFMPCompanyProfiler{profile: &dto.USStockCompanyProfile{IsFund: true}}
	provider := &cachedFMPUSStockCompanyProfileProvider{
		client: client,
		cache:  store,
		ttlValue: func() time.Duration {
			return time.Hour
		},
	}

	profile, err := provider.CompanyProfile(context.Background(), "SLV")
	if err != nil {
		t.Fatalf("CompanyProfile returned error: %v", err)
	}
	if profile == nil || !profile.IsFund {
		t.Fatalf("expected fund classification to be cached, got %#v", profile)
	}
}

func TestCachedFMPUSStockCompanyProfileProviderIsETFLike(t *testing.T) {
	store := cache.NewMemoryStore()
	client := &stubFMPCompanyProfiler{profile: &dto.USStockCompanyProfile{IsETF: true}}
	provider := &cachedFMPUSStockCompanyProfileProvider{
		client: client,
		cache:  store,
		ttlValue: func() time.Duration {
			return time.Hour
		},
	}

	got, err := provider.IsETFLike(context.Background(), "SLV")
	if err != nil {
		t.Fatalf("IsETFLike returned error: %v", err)
	}
	if !got {
		t.Fatal("expected SLV to be classified as ETF-like")
	}
}

func TestCachedFMPUSStockCompanyProfileProviderIsETFLikeBySymbolUsesBatchRequest(t *testing.T) {
	store := cache.NewMemoryStore()
	client := &stubFMPCompanyProfiler{profiles: map[string]*dto.USStockCompanyProfile{
		"SLV":  {Symbol: "SLV", IsETF: true},
		"AAPL": {Symbol: "AAPL", Sector: "Technology", Industry: "Consumer Electronics"},
		"PSLV": {Symbol: "PSLV", IsFund: true},
	}}
	provider := &cachedFMPUSStockCompanyProfileProvider{
		client: client,
		cache:  store,
		ttlValue: func() time.Duration {
			return time.Hour
		},
	}

	got, err := provider.IsETFLikeBySymbol(context.Background(), []string{"SLV", "AAPL", "PSLV", "AAPL"})
	if err != nil {
		t.Fatalf("IsETFLikeBySymbol returned error: %v", err)
	}
	if !got["SLV"] || got["AAPL"] || !got["PSLV"] {
		t.Fatalf("unexpected ETF-like map: %#v", got)
	}
	if client.count != 1 {
		t.Fatalf("expected one batched upstream request, got %d", client.count)
	}
}

func TestChunkUSStockCompanyProfileSymbols(t *testing.T) {
	batches := chunkUSStockCompanyProfileSymbols([]string{"A", "B", "C", "D", "E"}, 2)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}
	if len(batches[0]) != 2 || len(batches[1]) != 2 || len(batches[2]) != 1 {
		t.Fatalf("unexpected batch sizes: %#v", batches)
	}
	if batches[2][0] != "E" {
		t.Fatalf("unexpected final batch: %#v", batches[2])
	}
}

func TestIsETFLikeUSStockProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile *dto.USStockCompanyProfile
		want    bool
	}{
		{name: "nil", profile: nil, want: false},
		{name: "equity", profile: &dto.USStockCompanyProfile{Symbol: "AAPL"}, want: false},
		{name: "etf", profile: &dto.USStockCompanyProfile{Symbol: "SPY", IsETF: true}, want: true},
		{name: "fund", profile: &dto.USStockCompanyProfile{Symbol: "PSLV", IsFund: true}, want: true},
	}

	for _, tt := range tests {
		if got := isETFLikeUSStockProfile(tt.profile); got != tt.want {
			t.Fatalf("%s: isETFLikeUSStockProfile() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRandomUSStockCompanyProfileTTLStaysWithinRequestedWindow(t *testing.T) {
	for range 64 {
		ttl := randomUSStockCompanyProfileTTL()
		if ttl < usStockCompanyProfileTTLMin || ttl > usStockCompanyProfileTTLMax {
			t.Fatalf("ttl %s out of range [%s, %s]", ttl, usStockCompanyProfileTTLMin, usStockCompanyProfileTTLMax)
		}
	}
}
