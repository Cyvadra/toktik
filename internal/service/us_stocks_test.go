package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chrepo"
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

func (s *stubUSStockCompanyProfileProvider) CompanyProfiles(ctx context.Context, symbols []string) (map[string]*dto.USStockCompanyProfile, error) {
	profiles := make(map[string]*dto.USStockCompanyProfile, len(symbols))
	for _, symbol := range symbols {
		profile, err := s.CompanyProfile(ctx, symbol)
		if err != nil {
			return nil, err
		}
		profiles[symbol] = profile
	}
	return profiles, nil
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
	panelReqs    []dto.FundamentalPanelRequest
	snapshotResp *dto.FundamentalSnapshotResponse
	seriesResp   map[string]*dto.FundamentalSeriesResponse
	panelResp    *dto.FundamentalPanelResponse
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

func (s *stubUSStockFundamentals) QueryPanel(_ context.Context, req dto.FundamentalPanelRequest) (*dto.FundamentalPanelResponse, error) {
	s.panelReqs = append(s.panelReqs, req)
	if s.panelResp == nil {
		return &dto.FundamentalPanelResponse{}, nil
	}
	return s.panelResp, nil
}

func TestUSStocksQuerySplitsReturnsLatestRowsForSymbols(t *testing.T) {
	splitDate := time.Date(2020, 8, 31, 8, 0, 0, 0, time.FixedZone("driver-local", 8*60*60))
	updatedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	rows := &fakeForexRows{data: [][]any{
		{"AAPL", splitDate, 4.0, 1.0, "Stock Split", "fmp", "hash-a", updatedAt},
		{"MSFT", time.Date(2003, 2, 18, 0, 0, 0, 0, time.UTC), 2.0, 1.0, "Stock Split", "fmp", "hash-m", updatedAt},
	}}
	conn := &fakeForexConn{rows: rows}
	svc := NewUSStocksService(chrepo.NewRepo(conn))

	resp, err := svc.QuerySplits(context.Background(), dto.USStockSplitRequest{Symbols: []string{"aapl,MSFT", " AAPL "}})
	if err != nil {
		t.Fatalf("QuerySplits returned error: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected two split rows, got %d", len(resp.Data))
	}
	if resp.Data[0].Symbol != "AAPL" || resp.Data[0].Numerator != 4 || resp.Data[0].Denominator != 1 {
		t.Fatalf("unexpected first split row: %#v", resp.Data[0])
	}
	if !resp.Data[0].SplitDate.Equal(time.Date(2020, 8, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected split date to preserve calendar day in UTC, got %s", resp.Data[0].SplitDate)
	}
	for _, check := range []string{
		"FROM us_stock_splits",
		"argMax(numerator, updated_at)",
		"max(updated_at) AS latest_updated_at",
		"WHERE symbol IN ('AAPL', 'MSFT')",
		"GROUP BY symbol, split_date",
		"ORDER BY symbol, split_date",
	} {
		if !strings.Contains(conn.queryText, check) {
			t.Fatalf("expected split query to contain %q, got %s", check, conn.queryText)
		}
	}
	if strings.Contains(conn.queryText, "AS updated_at") {
		t.Fatalf("expected split query to avoid ClickHouse aggregate alias collision, got %s", conn.queryText)
	}
	if !rows.closed {
		t.Fatalf("expected rows to be closed")
	}
}

func TestUSStocksQuerySplitsRequiresSymbol(t *testing.T) {
	svc := NewUSStocksService(chrepo.NewRepo(&fakeForexConn{}))
	_, err := svc.QuerySplits(context.Background(), dto.USStockSplitRequest{})
	if err == nil || !strings.Contains(err.Error(), "symbol is required") {
		t.Fatalf("expected symbol validation error, got %v", err)
	}
}

func TestUSOptionsQuerySymbolsMergesLatestChainContracts(t *testing.T) {
	rows := &fakeForexRows{data: [][]any{
		{"O:AAPL260619C00190000", "AAPL", "C", time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), 190.0},
	}}
	store := cache.NewMemoryStore()
	latest := NewLatestUSMarketCache(store, time.Hour)
	ctx := context.Background()
	if err := latest.StoreOptionChain(ctx, "AAPL", "polygon", dto.USOptionChainSnapshot{
		Timestamp:  time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
		Underlying: "AAPL",
		Contracts: []dto.USOptionChainContract{
			{Symbol: "O:AAPL260619C00190000", OptionType: "C", Expiration: time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), Strike: 190},
			{Symbol: "O:AAPL260619P00185000", OptionType: "P", Expiration: time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), Strike: 185},
		},
	}); err != nil {
		t.Fatalf("StoreOptionChain failed: %v", err)
	}
	svc := NewUSOptionsService(chrepo.NewRepo(&fakeForexConn{rows: rows})).WithLatestMarketCache(latest)

	resp, err := svc.QuerySymbols(ctx, dto.USOptionSymbolRequest{Underlying: "aapl", IncludeLatest: true, Limit: 10})
	if err != nil {
		t.Fatalf("QuerySymbols returned error: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected ClickHouse and latest symbols, got %#v", resp.Data)
	}
	if resp.Data[0].Symbol != "O:AAPL260619P00185000" || resp.Data[1].Symbol != "O:AAPL260619C00190000" {
		t.Fatalf("unexpected merged symbols: %#v", resp.Data)
	}
}

func TestUSOptionsQuerySymbolsLatestRequiresOptIn(t *testing.T) {
	rows := &fakeForexRows{data: [][]any{
		{"O:AAPL260619C00190000", "AAPL", "C", time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), 190.0},
	}}
	store := cache.NewMemoryStore()
	latest := NewLatestUSMarketCache(store, time.Hour)
	ctx := context.Background()
	if err := latest.StoreOptionChain(ctx, "AAPL", "polygon", dto.USOptionChainSnapshot{
		Timestamp:  time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC),
		Underlying: "AAPL",
		Contracts:  []dto.USOptionChainContract{{Symbol: "O:AAPL260619P00185000", OptionType: "P", Expiration: time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), Strike: 185}},
	}); err != nil {
		t.Fatalf("StoreOptionChain failed: %v", err)
	}
	svc := NewUSOptionsService(chrepo.NewRepo(&fakeForexConn{rows: rows})).WithLatestMarketCache(latest)

	resp, err := svc.QuerySymbols(ctx, dto.USOptionSymbolRequest{Underlying: "AAPL", Limit: 10})
	if err != nil {
		t.Fatalf("QuerySymbols returned error: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Symbol != "O:AAPL260619C00190000" {
		t.Fatalf("expected ClickHouse-only symbols without include_latest, got %#v", resp.Data)
	}
}

func TestClickHouseUSStockCompanyProfileProviderReadsPersistedProfiles(t *testing.T) {
	marketCap := 123456789.0
	rows := &fakeForexRows{data: [][]any{
		{"AAPL", "AAPL", "Apple Inc.", "US", "USD", "NASDAQ", "Nasdaq Global Select", "Technology", "Consumer Electronics", "1980-12-12", &marketCap, (*float64)(nil), "https://www.apple.com", "https://example.com/aapl.png", "fmp", uint8(0), uint8(0)},
	}}
	conn := &fakeForexConn{rows: rows}
	provider := NewClickHouseUSStockCompanyProfileProvider(chrepo.NewRepo(conn))

	profiles, err := provider.CompanyProfiles(context.Background(), []string{" aapl ", "AAPL", ""})
	if err != nil {
		t.Fatalf("CompanyProfiles() error = %v", err)
	}
	profile := profiles["AAPL"]
	if profile == nil {
		t.Fatalf("expected AAPL profile, got %#v", profiles)
	}
	if profile.Name != "Apple Inc." || profile.Ticker != "AAPL" || profile.MarketCapitalization == nil || *profile.MarketCapitalization != marketCap {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	if !strings.Contains(conn.queryText, "FROM us_stock_company_profile FINAL") {
		t.Fatalf("expected profile query to read persisted ClickHouse table, got %s", conn.queryText)
	}
	if !rows.closed {
		t.Fatal("expected rows to be closed")
	}
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
	if err := svc.attachFundamentals(context.Background(), "AAPL", []string{"pe", "pb"}, "1d", bars); err != nil {
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

func TestResolveUSStockFundamentalBindingsKeepsTrailingPEForIndexETFs(t *testing.T) {
	bindings := resolveUSStockFundamentalBindings("QQQ", []string{"pe", "pb"})
	if len(bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(bindings))
	}
	if bindings[0].ResponseFactor != "pe" || bindings[0].SourceFactor != "pe" || bindings[0].PriceDerived {
		t.Fatalf("unexpected PE binding: %#v", bindings[0])
	}
	if bindings[0].SeriesMode != fundamentalSeriesModeFilled {
		t.Fatalf("unexpected PE binding series mode: %#v", bindings[0])
	}
	if bindings[1].ResponseFactor != "pb" || bindings[1].SourceFactor != "pb" || !bindings[1].PriceDerived {
		t.Fatalf("unexpected PB binding: %#v", bindings[1])
	}
	if bindings[1].SeriesMode != fundamentalSeriesModeEvent {
		t.Fatalf("unexpected PB binding series mode: %#v", bindings[1])
	}

	nonIndex := resolveUSStockFundamentalBindings("AAPL", []string{"pe"})
	if len(nonIndex) != 1 || nonIndex[0].SourceFactor != "pe" || !nonIndex[0].PriceDerived {
		t.Fatalf("unexpected non-index binding: %#v", nonIndex)
	}
	if nonIndex[0].SeriesMode != fundamentalSeriesModeEvent {
		t.Fatalf("unexpected non-index binding series mode: %#v", nonIndex[0])
	}
}

func TestResolveUSStockFundamentalBindingsAliasesQQQPE10ToTrailingPE(t *testing.T) {
	bindings := resolveUSStockFundamentalBindings("QQQ", []string{"pe10"})
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings))
	}
	if bindings[0].ResponseFactor != "pe10" || bindings[0].SourceFactor != "pe" || bindings[0].PriceDerived {
		t.Fatalf("unexpected PE10 alias binding: %#v", bindings[0])
	}
	if bindings[0].SeriesMode != fundamentalSeriesModeFilled {
		t.Fatalf("unexpected PE10 alias series mode: %#v", bindings[0])
	}

	nonIndex := resolveUSStockFundamentalBindings("AAPL", []string{"pe10"})
	if len(nonIndex) != 1 || nonIndex[0].SourceFactor != "pe10" {
		t.Fatalf("unexpected non-index pe10 binding: %#v", nonIndex)
	}
}

func TestUSStocksAttachFundamentalsUsesTrailingPEForIndexETFs(t *testing.T) {
	start := time.Date(2026, 4, 30, 13, 30, 0, 0, time.UTC)
	bars := []dto.USStockBarRow{
		{Timestamp: start, Symbol: "QQQ", Close: 510},
		{Timestamp: start.Add(24 * time.Hour), Symbol: "QQQ", Close: 520},
	}

	stub := &stubUSStockFundamentals{
		snapshotResp: &dto.FundamentalSnapshotResponse{
			Data: []dto.FundamentalSnapshotEntry{
				{Factor: "pe", EventTS: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), KnownAt: start, Value: 31.5, Source: "fmp_etf_fundamentals"},
			},
		},
		seriesResp: map[string]*dto.FundamentalSeriesResponse{
			"pe": {
				Data: []dto.FundamentalSeriesPoint{{EventTS: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), KnownAt: start.Add(24 * time.Hour), Value: 32.25, Source: "fmp_etf_fundamentals"}},
			},
		},
	}

	svc := NewUSStocksService(nil, stub)
	if err := svc.attachFundamentals(context.Background(), "QQQ", []string{"pe"}, "1d", bars); err != nil {
		t.Fatalf("attachFundamentals returned error: %v", err)
	}
	if len(stub.snapshotReqs) != 1 || len(stub.snapshotReqs[0].Factors) != 1 || stub.snapshotReqs[0].Factors[0] != "pe" {
		t.Fatalf("expected snapshot request for trailing pe, got %#v", stub.snapshotReqs)
	}
	if len(stub.seriesReqs) != 1 || stub.seriesReqs[0].Factor != "pe" {
		t.Fatalf("expected series request for trailing pe, got %#v", stub.seriesReqs)
	}
	if stub.seriesReqs[0].Mode != fundamentalSeriesModeFilled {
		t.Fatalf("expected trailing pe series request to use filled mode, got %#v", stub.seriesReqs[0])
	}
	if got := bars[0].Fundamentals["pe"].Value; got != 31.5 {
		t.Fatalf("expected first index PE bar to use trailing pe, got %v", got)
	}
	if got := bars[1].Fundamentals["pe"].Value; got != 32.25 {
		t.Fatalf("expected later index PE bar to use updated trailing pe, got %v", got)
	}
}

func TestUSStocksAttachFundamentalsLeavesQQQPE10LiveEmptyWithoutProviderData(t *testing.T) {
	start := time.Date(2026, 4, 30, 13, 30, 0, 0, time.UTC)
	bars := []dto.USStockBarRow{
		{Timestamp: start, Symbol: "QQQ", Close: 510},
		{Timestamp: start.Add(24 * time.Hour), Symbol: "QQQ", Close: 520},
	}

	stub := &stubUSStockFundamentals{}

	svc := NewUSStocksService(nil, stub)
	if err := svc.attachFundamentals(context.Background(), "QQQ", []string{"pe10_live"}, "1d", bars); err != nil {
		t.Fatalf("attachFundamentals returned error: %v", err)
	}
	if len(stub.snapshotReqs) != 1 || len(stub.snapshotReqs[0].Factors) != 1 || stub.snapshotReqs[0].Factors[0] != "pe10_live" {
		t.Fatalf("expected pe10_live request to pass through to fundamentals provider, got %#v", stub.snapshotReqs)
	}
	if len(stub.seriesReqs) != 1 || stub.seriesReqs[0].Factor != "pe10_live" {
		t.Fatalf("expected pe10_live series request to pass through, got %#v", stub.seriesReqs)
	}
	if _, ok := bars[0].Fundamentals["pe10_live"]; ok {
		t.Fatalf("expected no pe10_live attachment for qqq, got %#v", bars[0].Fundamentals["pe10_live"])
	}
	if _, ok := bars[1].Fundamentals["pe10_live"]; ok {
		t.Fatalf("expected no later pe10_live attachment for qqq, got %#v", bars[1].Fundamentals["pe10_live"])
	}
}

func TestUSStocksAttachFundamentalsAliasesQQQPE10ToTrailingPE(t *testing.T) {
	start := time.Date(2026, 4, 30, 13, 30, 0, 0, time.UTC)
	bars := []dto.USStockBarRow{
		{Timestamp: start, Symbol: "QQQ", Close: 510},
		{Timestamp: start.Add(24 * time.Hour), Symbol: "QQQ", Close: 520},
	}

	stub := &stubUSStockFundamentals{
		snapshotResp: &dto.FundamentalSnapshotResponse{
			Data: []dto.FundamentalSnapshotEntry{
				{Factor: "pe", EventTS: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), KnownAt: start, Value: 31.5, Source: "fmp"},
			},
		},
		seriesResp: map[string]*dto.FundamentalSeriesResponse{
			"pe": {
				Data: []dto.FundamentalSeriesPoint{{EventTS: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), KnownAt: start.Add(24 * time.Hour), Value: 32.25, Source: "fmp"}},
			},
		},
	}

	svc := NewUSStocksService(nil, stub)
	if err := svc.attachFundamentals(context.Background(), "QQQ", []string{"pe10"}, "1d", bars); err != nil {
		t.Fatalf("attachFundamentals returned error: %v", err)
	}
	if len(stub.snapshotReqs) != 1 || len(stub.snapshotReqs[0].Factors) != 1 || stub.snapshotReqs[0].Factors[0] != "pe" {
		t.Fatalf("expected pe10 alias snapshot request to use pe, got %#v", stub.snapshotReqs)
	}
	if len(stub.seriesReqs) != 1 || stub.seriesReqs[0].Factor != "pe" {
		t.Fatalf("expected pe10 alias series request to use pe, got %#v", stub.seriesReqs)
	}
	if got := bars[0].Fundamentals["pe10"].Value; got != 31.5 {
		t.Fatalf("expected first qqq pe10 bar to use trailing pe, got %v", got)
	}
	if got := bars[1].Fundamentals["pe10"].Value; got != 32.25 {
		t.Fatalf("expected later qqq pe10 bar to use updated trailing pe, got %v", got)
	}
}

func TestUSStockFundamentalKnownAtCutoffUsesDayEndForDailyBars(t *testing.T) {
	barTS := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if got := usStockFundamentalKnownAtCutoff(barTS, "1d"); !got.Equal(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected 1d known_at cutoff: %s", got)
	}
	if got := usStockFundamentalKnownAtCutoff(barTS, "1h"); !got.Equal(barTS) {
		t.Fatalf("unexpected intraday known_at cutoff: %s", got)
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
