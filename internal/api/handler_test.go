package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/apikeyauth"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/dto"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
	"github.com/gin-gonic/gin"
)

// --- mock service ---

type mockQuerier struct {
	barsResp    *dto.BarResponse
	symbolsResp *dto.SymbolResponse
	greeksResp  *dto.GreeksResponse
	ivSmileResp *dto.CryptoIVSmileHistoryResponse
	ivSmileReq  dto.CryptoIVSmileHistoryRequest
	btResp      *backtest.Result
	err         error
}

type mockUSStocksQuerier struct {
	barsResp               *dto.USStockBarResponse
	symbolsResp            *dto.USStockSymbolResponse
	splitsResp             *dto.USStockSplitResponse
	profilesResp           *dto.USStockProfileResponse
	fundamentalMetricsResp *dto.USStockFundamentalMetricsResponse
	barsReq                dto.USStockBarRequest
	splitsReq              dto.USStockSplitRequest
	profilesReq            dto.USStockProfileRequest
	fundamentalMetricsReq  dto.USStockFundamentalMetricsRequest
	err                    error
}

type mockUSOptionsQuerier struct {
	barsResp    *dto.USOptionBarResponse
	symbolsResp *dto.USOptionSymbolResponse
	greeksResp  *dto.USOptionGreeksResponse
	chainResp   *dto.USOptionChainResponse
	wallResp    *dto.USOptionWallResponse
	barsReq     dto.USOptionBarRequest
	symbolsReq  dto.USOptionSymbolRequest
	greeksReq   dto.USOptionGreeksRequest
	chainReq    dto.USOptionChainRequest
	wallReq     dto.USOptionWallRequest
	err         error
}

type mockForexQuerier struct {
	barsResp    *dto.ForexBarResponse
	symbolsResp *dto.ForexSymbolResponse
	barsReq     dto.ForexBarRequest
	err         error
}

type mockInfra struct {
	readyResp    *dto.ReadinessResponse
	marketsResp  *dto.MarketCatalogResponse
	datasetsResp *dto.DatasetCatalogResponse
	err          error
}

type mockDataBrowser struct {
	presetsResp    *dto.BrowserPresetResponse
	schemaResp     *dto.BrowserSchemaResponse
	previewResp    *dto.BrowserPreviewResponse
	coverageResp   *dto.BrowserCoverageResponse
	profileResp    *dto.BrowserFieldProfileResponse
	validCountResp *dto.BrowserValidCountResponse
	valuesResp     *dto.BrowserValueListResponse
	previewReq     dto.BrowserPreviewRequest
	valuesReq      dto.BrowserValueListRequest
	err            error
}

type mockFeature struct {
	volResp             *dto.FeatureVolatilitySnapshotResponse
	historyResp         *dto.FeatureVolatilityHistoryResponse
	termStructureResp   *dto.FeatureTermStructureSnapshotResponse
	skewResp            *dto.FeatureSkewSnapshotResponse
	liquidityResp       *dto.FeatureLiquiditySnapshotResponse
	liquidityHistResp   *dto.FeatureLiquidityHistoryResponse
	eventWindowResp     *dto.FeatureEventWindowSnapshotResponse
	eventWindowHistResp *dto.FeatureEventWindowHistoryResponse
	panelResp           *dto.FeatureDailyPanelResponse
	err                 error
}

type mockIndicatorProvider struct {
	resp *dto.IndicatorSeriesResponse
	cat  *dto.IndicatorPresetCatalogResponse
	err  error
}

type mockStrategyBacktests struct {
	validateResp *dto.StrategyBacktestValidationResponse
	startResp    *dto.StrategyBacktestRunAccepted
	statusResp   *dto.StrategyBacktestRunStatus
	stream       <-chan dto.StrategyBacktestSSEvent
	validateReq  dto.StrategyBacktestRunRequest
	startReq     dto.StrategyBacktestRunRequest
	cancelRunID  string
	err          error
}

type mockPolygonProvider struct {
	snapshotResp  *dto.PolygonStockSnapshotResponse
	aggregateResp *dto.PolygonAggregateResponse
	quoteResp     *dto.PolygonQuoteResponse
	tradeResp     *dto.PolygonTradeResponse
	contractResp  *dto.PolygonOptionContractResponse
	chainResp     *dto.PolygonOptionChainResponse
	err           error
}

type mockScreener struct {
	underlyingsResp *dto.ScreenUnderlyingResponse
	usTurnoverResp  *dto.ScreenUSTurnoverIntersectionResponse
	optionsResp     *dto.ScreenOptionResponse
	underlyingsReq  dto.ScreenUnderlyingRequest
	usTurnoverReq   dto.ScreenUSTurnoverIntersectionRequest
	optionsReq      dto.ScreenOptionRequest
	err             error
}

type mockUniverseProvider struct {
	membersResp *dto.UniverseMembersResponse
	rebuildResp *dto.UniverseRebuildAccepted
	membersReq  dto.UniverseMembersRequest
	rebuildReq  dto.UniverseRebuildRequest
	err         error
}

type mockMacroProvider struct {
	factorsResp *dto.MacroFactorCatalogResponse
	seriesResp  *dto.MacroSeriesResponse
	seriesReq   dto.MacroSeriesRequest
	err         error
}

type mockLogoProvider struct {
	logo       *dto.USStockLogoImage
	err        error
	called     bool
	lastSymbol string
}

func (m *mockQuerier) QueryBars(_ context.Context, _ dto.BarRequest) (*dto.BarResponse, error) {
	return m.barsResp, m.err
}
func (m *mockQuerier) QuerySymbols(_ context.Context, _ dto.SymbolRequest) (*dto.SymbolResponse, error) {
	return m.symbolsResp, m.err
}
func (m *mockQuerier) QueryGreeks(_ context.Context, _ dto.GreeksRequest) (*dto.GreeksResponse, error) {
	return m.greeksResp, m.err
}
func (m *mockQuerier) RunBacktest(_ context.Context, _ dto.BacktestRequest) (*backtest.Result, error) {
	return m.btResp, m.err
}
func (m *mockQuerier) QueryChain(_ context.Context, _ dto.CryptoOptionChainRequest) (*dto.CryptoOptionChainResponse, error) {
	return nil, m.err
}
func (m *mockQuerier) QueryIVSmileHistory(_ context.Context, req dto.CryptoIVSmileHistoryRequest) (*dto.CryptoIVSmileHistoryResponse, error) {
	m.ivSmileReq = req
	return m.ivSmileResp, m.err
}

func TestGetCryptoIVSmileHistoryBindsQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockQuerier{ivSmileResp: &dto.CryptoIVSmileHistoryResponse{BaseAsset: "BTC", Interval: "7d", Data: []dto.CryptoIVSmileSurface{}}}
	r := NewRouter(provider, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/iv-smile-history?base_asset=BTC&from=2026-01-01&to=2026-02-01&interval=7d&max_strike_distance_ratio=0&limit=12", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if provider.ivSmileReq.Interval != "7d" || provider.ivSmileReq.Limit != 12 || provider.ivSmileReq.MaxStrikeDistanceRatio == nil || *provider.ivSmileReq.MaxStrikeDistanceRatio != 0 {
		t.Fatalf("bound request = %+v", provider.ivSmileReq)
	}
}

func (m *mockLogoProvider) GetLogo(_ context.Context, symbol string) (*dto.USStockLogoImage, error) {
	m.called = true
	m.lastSymbol = symbol
	if m.logo != nil && m.logo.Symbol == "" {
		m.logo.Symbol = symbol
	}
	return m.logo, m.err
}

func (m *mockUniverseProvider) Members(_ context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error) {
	m.membersReq = req
	return m.membersResp, m.err
}

func (m *mockUniverseProvider) MemberIntervals(_ context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error) {
	m.membersReq = req
	return m.membersResp, m.err
}

func (m *mockUniverseProvider) StartRebuild(_ context.Context, req dto.UniverseRebuildRequest) (*dto.UniverseRebuildAccepted, error) {
	m.rebuildReq = req
	return m.rebuildResp, m.err
}

func (m *mockUSStocksQuerier) QueryBars(_ context.Context, req dto.USStockBarRequest) (*dto.USStockBarResponse, error) {
	m.barsReq = req
	return m.barsResp, m.err
}

func (m *mockUSStocksQuerier) QuerySymbols(_ context.Context, _ dto.USStockSymbolRequest) (*dto.USStockSymbolResponse, error) {
	return m.symbolsResp, m.err
}

func (m *mockUSStocksQuerier) QuerySplits(_ context.Context, req dto.USStockSplitRequest) (*dto.USStockSplitResponse, error) {
	m.splitsReq = req
	return m.splitsResp, m.err
}

func (m *mockUSStocksQuerier) QueryProfiles(_ context.Context, req dto.USStockProfileRequest) (*dto.USStockProfileResponse, error) {
	m.profilesReq = req
	return m.profilesResp, m.err
}

func (m *mockUSStocksQuerier) QueryFundamentalMetrics(_ context.Context, req dto.USStockFundamentalMetricsRequest) (*dto.USStockFundamentalMetricsResponse, error) {
	m.fundamentalMetricsReq = req
	return m.fundamentalMetricsResp, m.err
}

func (m *mockUSOptionsQuerier) QueryBars(_ context.Context, req dto.USOptionBarRequest) (*dto.USOptionBarResponse, error) {
	m.barsReq = req
	return m.barsResp, m.err
}

func (m *mockUSOptionsQuerier) QuerySymbols(_ context.Context, req dto.USOptionSymbolRequest) (*dto.USOptionSymbolResponse, error) {
	m.symbolsReq = req
	return m.symbolsResp, m.err
}

func (m *mockUSOptionsQuerier) QueryGreeks(_ context.Context, req dto.USOptionGreeksRequest) (*dto.USOptionGreeksResponse, error) {
	m.greeksReq = req
	return m.greeksResp, m.err
}

func (m *mockUSOptionsQuerier) QueryChain(_ context.Context, req dto.USOptionChainRequest) (*dto.USOptionChainResponse, error) {
	m.chainReq = req
	return m.chainResp, m.err
}

func (m *mockUSOptionsQuerier) QueryOptionWall(_ context.Context, req dto.USOptionWallRequest) (*dto.USOptionWallResponse, error) {
	m.wallReq = req
	return m.wallResp, m.err
}

func (m *mockForexQuerier) QueryBars(_ context.Context, req dto.ForexBarRequest) (*dto.ForexBarResponse, error) {
	m.barsReq = req
	return m.barsResp, m.err
}

func (m *mockForexQuerier) QuerySymbols(_ context.Context, _ dto.ForexSymbolRequest) (*dto.ForexSymbolResponse, error) {
	return m.symbolsResp, m.err
}

func (m *mockInfra) Readiness(_ context.Context) (*dto.ReadinessResponse, error) {
	return m.readyResp, m.err
}

func (m *mockInfra) ListMarkets(_ context.Context) (*dto.MarketCatalogResponse, error) {
	return m.marketsResp, m.err
}

func (m *mockInfra) ListDatasets(_ context.Context, _ dto.DatasetQueryRequest) (*dto.DatasetCatalogResponse, error) {
	return m.datasetsResp, m.err
}

func (m *mockDataBrowser) ListBrowserPresets(_ context.Context) (*dto.BrowserPresetResponse, error) {
	return m.presetsResp, m.err
}

func (m *mockDataBrowser) QueryDatasetSchema(_ context.Context, _ dto.BrowserSchemaRequest) (*dto.BrowserSchemaResponse, error) {
	return m.schemaResp, m.err
}

func (m *mockDataBrowser) QueryDatasetPreview(_ context.Context, req dto.BrowserPreviewRequest) (*dto.BrowserPreviewResponse, error) {
	m.previewReq = req
	return m.previewResp, m.err
}

func (m *mockDataBrowser) QueryDatasetCoverage(_ context.Context, _ dto.BrowserCoverageRequest) (*dto.BrowserCoverageResponse, error) {
	return m.coverageResp, m.err
}

func (m *mockDataBrowser) QueryFieldProfile(_ context.Context, _ dto.BrowserFieldProfileRequest) (*dto.BrowserFieldProfileResponse, error) {
	return m.profileResp, m.err
}

func (m *mockDataBrowser) QueryValidCount(_ context.Context, _ dto.BrowserValidCountRequest) (*dto.BrowserValidCountResponse, error) {
	return m.validCountResp, m.err
}

func (m *mockDataBrowser) QueryDatasetValues(_ context.Context, req dto.BrowserValueListRequest) (*dto.BrowserValueListResponse, error) {
	m.valuesReq = req
	return m.valuesResp, m.err
}

func (m *mockFeature) QueryVolatilitySnapshot(_ context.Context, _ dto.FeatureVolatilitySnapshotRequest) (*dto.FeatureVolatilitySnapshotResponse, error) {
	return m.volResp, m.err
}

func (m *mockFeature) QueryVolatilityHistory(_ context.Context, _ dto.FeatureVolatilityHistoryRequest) (*dto.FeatureVolatilityHistoryResponse, error) {
	return m.historyResp, m.err
}

func (m *mockFeature) QueryTermStructureSnapshot(_ context.Context, _ dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureTermStructureSnapshotResponse, error) {
	return m.termStructureResp, m.err
}

func (m *mockFeature) QuerySkewSnapshot(_ context.Context, _ dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureSkewSnapshotResponse, error) {
	return m.skewResp, m.err
}

func (m *mockFeature) QueryLiquiditySnapshot(_ context.Context, _ dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureLiquiditySnapshotResponse, error) {
	return m.liquidityResp, m.err
}

func (m *mockMacroProvider) ListFactors(_ context.Context, _ dto.MacroFactorCatalogRequest) (*dto.MacroFactorCatalogResponse, error) {
	return m.factorsResp, m.err
}

func (m *mockMacroProvider) QuerySeries(_ context.Context, req dto.MacroSeriesRequest) (*dto.MacroSeriesResponse, error) {
	m.seriesReq = req
	return m.seriesResp, m.err
}

func (m *mockFeature) QueryLiquidityHistory(_ context.Context, _ dto.FeatureLiquidityHistoryRequest) (*dto.FeatureLiquidityHistoryResponse, error) {
	return m.liquidityHistResp, m.err
}

func (m *mockFeature) QueryEventWindowSnapshot(_ context.Context, _ dto.FeatureUnderlyingSnapshotRequest) (*dto.FeatureEventWindowSnapshotResponse, error) {
	return m.eventWindowResp, m.err
}

func (m *mockFeature) QueryEventWindowHistory(_ context.Context, _ dto.FeatureUnderlyingHistoryRequest) (*dto.FeatureEventWindowHistoryResponse, error) {
	return m.eventWindowHistResp, m.err
}

func (m *mockFeature) QueryDailyFeaturePanel(_ context.Context, _ dto.FeatureDailyPanelRequest) (*dto.FeatureDailyPanelResponse, error) {
	return m.panelResp, m.err
}

func (m *mockIndicatorProvider) QueryIndicatorSeries(_ context.Context, _ dto.IndicatorSeriesRequest) (*dto.IndicatorSeriesResponse, error) {
	return m.resp, m.err
}

func (m *mockIndicatorProvider) ListIndicatorPresets(_ context.Context) (*dto.IndicatorPresetCatalogResponse, error) {
	return m.cat, m.err
}

func (m *mockFeature) QueryTermStructureHistory(_ context.Context, _ dto.FeatureTermStructureHistoryRequest) (*dto.FeatureTermStructureHistoryResponse, error) {
	return nil, m.err
}

func (m *mockFeature) QuerySkewHistory(_ context.Context, _ dto.FeatureSkewHistoryRequest) (*dto.FeatureSkewHistoryResponse, error) {
	return nil, m.err
}

func (m *mockStrategyBacktests) ValidateStrategyBacktest(_ context.Context, req dto.StrategyBacktestRunRequest) (*dto.StrategyBacktestValidationResponse, error) {
	m.validateReq = req
	return m.validateResp, m.err
}

func (m *mockStrategyBacktests) StartStrategyBacktest(_ context.Context, req dto.StrategyBacktestRunRequest) (*dto.StrategyBacktestRunAccepted, error) {
	m.startReq = req
	return m.startResp, m.err
}

func (m *mockStrategyBacktests) GetStrategyBacktestRun(_ context.Context, _ string) (*dto.StrategyBacktestRunStatus, error) {
	return m.statusResp, m.err
}

func (m *mockStrategyBacktests) CancelStrategyBacktest(_ context.Context, runID string) (*dto.StrategyBacktestRunStatus, error) {
	m.cancelRunID = runID
	return m.statusResp, m.err
}

func (m *mockStrategyBacktests) SubscribeStrategyBacktest(_ context.Context, _ string) (<-chan dto.StrategyBacktestSSEvent, func(), error) {
	if m.err != nil {
		return nil, func() {}, m.err
	}
	if m.stream == nil {
		ch := make(chan dto.StrategyBacktestSSEvent)
		close(ch)
		return ch, func() {}, nil
	}
	return m.stream, func() {}, nil
}

func (m *mockPolygonProvider) QueryStockSnapshot(_ context.Context, _ dto.PolygonStockSnapshotRequest) (*dto.PolygonStockSnapshotResponse, error) {
	return m.snapshotResp, m.err
}

func (m *mockPolygonProvider) QueryStockAggregates(_ context.Context, _ dto.PolygonAggregateRequest) (*dto.PolygonAggregateResponse, error) {
	return m.aggregateResp, m.err
}

func (m *mockPolygonProvider) QueryStockQuotes(_ context.Context, _ dto.PolygonStockQuotesRequest) (*dto.PolygonQuoteResponse, error) {
	return m.quoteResp, m.err
}

func (m *mockPolygonProvider) QueryStockTrades(_ context.Context, _ dto.PolygonStockTradesRequest) (*dto.PolygonTradeResponse, error) {
	return m.tradeResp, m.err
}

func (m *mockPolygonProvider) QueryOptionContract(_ context.Context, _ dto.PolygonOptionContractRequest) (*dto.PolygonOptionContractResponse, error) {
	return m.contractResp, m.err
}

func (m *mockPolygonProvider) QueryOptionChain(_ context.Context, _ dto.PolygonOptionChainRequest) (*dto.PolygonOptionChainResponse, error) {
	return m.chainResp, m.err
}

func (m *mockPolygonProvider) QueryOptionAggregates(_ context.Context, _ dto.PolygonAggregateRequest) (*dto.PolygonAggregateResponse, error) {
	return m.aggregateResp, m.err
}

func (m *mockPolygonProvider) QueryOptionQuotes(_ context.Context, _ dto.PolygonOptionQuotesRequest) (*dto.PolygonQuoteResponse, error) {
	return m.quoteResp, m.err
}

func (m *mockPolygonProvider) QueryOptionTrades(_ context.Context, _ dto.PolygonOptionTradesRequest) (*dto.PolygonTradeResponse, error) {
	return m.tradeResp, m.err
}

func (m *mockScreener) ScreenUnderlyings(_ context.Context, req dto.ScreenUnderlyingRequest) (*dto.ScreenUnderlyingResponse, error) {
	m.underlyingsReq = req
	return m.underlyingsResp, m.err
}

func (m *mockScreener) ScreenUSTurnoverIntersection(_ context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error) {
	m.usTurnoverReq = req
	return m.usTurnoverResp, m.err
}

func (m *mockScreener) ScreenOptions(_ context.Context, req dto.ScreenOptionRequest) (*dto.ScreenOptionResponse, error) {
	m.optionsReq = req
	return m.optionsResp, m.err
}

// --- helpers ---

func setupRouter(q CryptoOptionsQuerier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(q, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil)
}

func TestUtilsUSStockLogoBypassesAPIKeyAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultRuntime()
	r := NewRouterFromDeps(Deps{
		Config:  cfg,
		APIKeys: fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{"secret": {ID: 1, KeyDigest: "secret-digest"}}},
		Logos: &mockLogoProvider{logo: &dto.USStockLogoImage{
			ContentType: "image/png",
			Data:        []byte("png-data"),
		}},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/utils/us-stocks/logos/AAPL.png", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "image/png") {
		t.Fatalf("content type = %q, want image/png", got)
	}
	if got := w.Body.String(); got != "png-data" {
		t.Fatalf("body = %q, want png-data", got)
	}
}

func TestUtilsUSStockLogoRejectsInvalidSymbolBeforeProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockLogoProvider{logo: &dto.USStockLogoImage{ContentType: "image/png", Data: []byte("png-data")}}
	r := NewRouterFromDeps(Deps{Logos: provider})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/utils/us-stocks/logos/BAD_SYMBOL.png", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if provider.called {
		t.Fatalf("logo provider should not be called for invalid public symbol, got %q", provider.lastSymbol)
	}
}

func TestGetPolygonStockSnapshot_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockPolygonProvider{snapshotResp: &dto.PolygonStockSnapshotResponse{Data: &polygonpkg.StockSnapshot{Ticker: "AAPL"}}}
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil, provider)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/polygon/stocks/snapshot?symbol=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "AAPL") {
		t.Fatalf("expected AAPL in response, got %s", w.Body.String())
	}
}

func TestGetPolygonOptionChain_NotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/polygon/options/chain?underlying=SPY", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetPolygonOptionChain_UpstreamErrorDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockPolygonProvider{err: &polygonpkg.HTTPStatusError{
		StatusCode: http.StatusBadRequest,
		Status:     "400 Bad Request",
		Body:       `{"status":"ERROR","request_id":"d4e9e56585b307b4e608c9d97a704ef2","error":"Failed to parse query parameters from URL: Key: 'OptionsChainQueryParam.Limit' Error:Field validation for 'Limit' failed on the 'max' tag"}`,
	}}
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil, provider)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/polygon/options/chain?underlying=EWH&limit=500", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var errResp dto.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.Contains(errResp.Error, "internal server error") {
		t.Fatalf("expected detailed polygon error, got %q", errResp.Error)
	}
	if !strings.Contains(errResp.Error, "OptionsChainQueryParam.Limit") {
		t.Fatalf("expected upstream validation detail, got %q", errResp.Error)
	}
	if !strings.Contains(errResp.Error, "request_id=d4e9e56585b307b4e608c9d97a704ef2") {
		t.Fatalf("expected request id in error, got %q", errResp.Error)
	}
}

func TestGetPolygonStockAggregates_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil, &mockPolygonProvider{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/polygon/stocks/aggregates?ticker=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetUSOptionWall_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &mockUSOptionsQuerier{wallResp: &dto.USOptionWallResponse{
		Symbol:      "AAPL",
		SnapshotDay: time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
		Data: []dto.USOptionWall{{
			Symbol:       "AAPL",
			Expiration:   time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
			SnapshotDay:  time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
			DaysToExpiry: 22,
			Strikes: []dto.USOptionWallStrikeRow{{
				Strike:            200,
				TotalOpenInterest: 215,
			}},
		}},
	}}
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, provider, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/wall?symbol=AAPL&min_dte=20&max_dte=30", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if provider.wallReq.Symbol != "AAPL" || provider.wallReq.MinDTE != 20 || provider.wallReq.MaxDTE != 30 {
		t.Fatalf("unexpected wall request bound: %+v", provider.wallReq)
	}
	if !strings.Contains(w.Body.String(), "215") {
		t.Fatalf("expected wall payload in response, got %s", w.Body.String())
	}
}

func TestGetUSOptionWall_NotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, nil, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/wall?symbol=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

// --- 404 ---

func TestNotFound(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- /health ---

func TestHealthEndpoint(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health?probe=1", nil)
	req.RemoteAddr = "127.0.0.1:43210"
	req.Header.Set("X-Forwarded-For", "203.0.113.25")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Status  string `json:"status"`
		Request struct {
			ClientIP   string              `json:"client_ip"`
			Method     string              `json:"method"`
			URL        string              `json:"url"`
			RemoteAddr string              `json:"remote_addr"`
			Headers    map[string][]string `json:"headers"`
		} `json:"request"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf(`expected status "ok", got %q`, body.Status)
	}
	if body.Request.ClientIP != "127.0.0.1" || body.Request.Method != http.MethodGet || body.Request.URL != "/health?probe=1" || body.Request.RemoteAddr != "127.0.0.1:43210" {
		t.Fatalf("unexpected request echo: %#v", body.Request)
	}
	if got := body.Request.Headers["X-Forwarded-For"]; len(got) != 1 || got[0] != "203.0.113.25" {
		t.Fatalf("expected forwarded header to be echoed, got %#v", got)
	}
}

func TestReadinessEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{readyResp: &dto.ReadinessResponse{Status: "ready"}}, &mockFeature{}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.ReadinessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" {
		t.Fatalf("expected ready, got %q", resp.Status)
	}
}

func TestMarketCatalogEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{marketsResp: &dto.MarketCatalogResponse{Markets: []dto.MarketDescriptor{{Name: "crypto-options", Status: "available"}}}}, &mockFeature{}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/infra/markets", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.MarketCatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Markets) != 1 || resp.Markets[0].Name != "crypto-options" {
		t.Fatalf("unexpected markets response: %+v", resp.Markets)
	}
}

func TestDatasetCatalogEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{datasetsResp: &dto.DatasetCatalogResponse{Summary: dto.DatasetSummary{Total: 1, Ready: 1}, Datasets: []dto.DatasetDescriptor{{Name: "crypto-options-bars", Status: "ready"}}}}, &mockFeature{}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/infra/datasets", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.DatasetCatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Datasets) != 1 || resp.Datasets[0].Name != "crypto-options-bars" {
		t.Fatalf("unexpected datasets response: %+v", resp.Datasets)
	}
	if resp.Summary.Total != 1 || resp.Summary.Ready != 1 {
		t.Fatalf("unexpected dataset summary: %+v", resp.Summary)
	}
}

func TestDatasetCatalogEndpointWithFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{datasetsResp: &dto.DatasetCatalogResponse{Summary: dto.DatasetSummary{Total: 1, Ready: 1}, Datasets: []dto.DatasetDescriptor{{Name: "us-options-bars", Market: "us-options", Status: "ready"}}}}, &mockFeature{}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/infra/datasets?market=us-options&status=ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBrowserPresetsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	browser := &mockDataBrowser{presetsResp: &dto.BrowserPresetResponse{Datasets: []dto.BrowserDatasetDescriptor{{Name: "us-stocks-bars", Market: "us-stocks"}}}}
	r := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), DataBrowser: browser})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/browser/presets", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.BrowserPresetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Datasets) != 1 || resp.Datasets[0].Name != "us-stocks-bars" {
		t.Fatalf("unexpected presets response: %+v", resp.Datasets)
	}
}

func TestBrowserPreviewEndpointBindsParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	browser := &mockDataBrowser{previewResp: &dto.BrowserPreviewResponse{Dataset: dto.BrowserDatasetDescriptor{Name: "us-stocks-bars"}, Columns: []string{"timestamp", "close"}, Data: []map[string]any{{"close": 100.0}}}}
	r := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), DataBrowser: browser})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/browser/datasets/us-stocks-bars/preview?symbol=AAPL&from=2025-01-01&to=2025-01-02&columns=timestamp,close&limit=5", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if browser.previewReq.Dataset != "us-stocks-bars" || browser.previewReq.Symbol != "AAPL" || browser.previewReq.Columns != "timestamp,close" || browser.previewReq.Limit != 5 {
		t.Fatalf("unexpected bound preview request: %+v", browser.previewReq)
	}
}

func TestBrowserEndpointWithoutProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouterFromDeps(Deps{Config: config.DefaultRuntime()})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/browser/presets", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFeatureVolatilitySnapshotRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hv10 := 0.55
	ivPercentile := 74.0
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{volResp: &dto.FeatureVolatilitySnapshotResponse{Market: "us-options", Underlying: "AAPL", LookbackDays: 252, HV10: &hv10, IVPercentile: &ivPercentile}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/volatility-snapshot?market=us-options&underlying=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureVolatilitySnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Market != "us-options" || resp.Underlying != "AAPL" {
		t.Fatalf("unexpected feature response: %+v", resp)
	}
	if resp.HV10 == nil || *resp.HV10 != hv10 {
		t.Fatalf("unexpected hv10: %+v", resp.HV10)
	}
}

func TestFeatureVolatilityHistoryRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hv20 := 0.44
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{historyResp: &dto.FeatureVolatilityHistoryResponse{Market: "crypto-options", Underlying: "BTC", LookbackDays: 252, Data: []dto.FeatureVolatilityHistoryRow{{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), HV20: &hv20}}}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/volatility-history?market=crypto-options&underlying=BTC&from=2024-01-01&to=2024-01-05", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureVolatilityHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Market != "crypto-options" || resp.Underlying != "BTC" || len(resp.Data) != 1 {
		t.Fatalf("unexpected feature history response: %+v", resp)
	}
	if resp.Data[0].HV20 == nil || *resp.Data[0].HV20 != hv20 {
		t.Fatalf("unexpected hv20: %+v", resp.Data[0].HV20)
	}
}

func TestFeatureTermStructureSnapshotRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	atmIV := 0.32
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{termStructureResp: &dto.FeatureTermStructureSnapshotResponse{Market: "us-options", Underlying: "AAPL", Data: []dto.FeatureTermStructureSnapshotRow{{Expiration: time.Date(2024, 2, 16, 0, 0, 0, 0, time.UTC), DaysToExpiry: 45, ATMIV: &atmIV}}}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/term-structure-snapshot?market=us-options&underlying=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureTermStructureSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Underlying != "AAPL" || len(resp.Data) != 1 || resp.Data[0].ATMIV == nil || *resp.Data[0].ATMIV != atmIV {
		t.Fatalf("unexpected term structure response: %+v", resp)
	}
}

func TestFeatureSkewSnapshotRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	skew := 0.07
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{skewResp: &dto.FeatureSkewSnapshotResponse{Market: "us-options", Underlying: "AAPL", Data: []dto.FeatureSkewSnapshotRow{{Expiration: time.Date(2024, 2, 16, 0, 0, 0, 0, time.UTC), DaysToExpiry: 45, PutCallSkew: &skew}}}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/skew-snapshot?market=us-options&underlying=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureSkewSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Underlying != "AAPL" || len(resp.Data) != 1 || resp.Data[0].PutCallSkew == nil || *resp.Data[0].PutCallSkew != skew {
		t.Fatalf("unexpected skew response: %+v", resp)
	}
}

func TestFeatureLiquiditySnapshotRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	spread := 0.031
	ratio := 0.75
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{liquidityResp: &dto.FeatureLiquiditySnapshotResponse{Market: "crypto-options", Underlying: "BTC", Data: []dto.FeatureLiquiditySnapshotRow{{Expiration: time.Date(2024, 2, 16, 0, 0, 0, 0, time.UTC), DaysToExpiry: 45, RelativeSpread: &spread, TradabilityRatio: &ratio, ContractCount: 8}}}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/liquidity-snapshot?market=crypto-options&underlying=BTC", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureLiquiditySnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Underlying != "BTC" || len(resp.Data) != 1 || resp.Data[0].RelativeSpread == nil || *resp.Data[0].RelativeSpread != spread {
		t.Fatalf("unexpected liquidity response: %+v", resp)
	}
}

func TestFeatureLiquidityHistoryRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ratio := 0.5
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{liquidityHistResp: &dto.FeatureLiquidityHistoryResponse{Market: "us-options", Underlying: "AAPL", Data: []dto.FeatureLiquidityHistoryRow{{AsOfDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), FeatureLiquiditySnapshotRow: dto.FeatureLiquiditySnapshotRow{Expiration: time.Date(2024, 2, 16, 0, 0, 0, 0, time.UTC), ActivityRatio: &ratio, Volume: 1200, ContractCount: 10}}}}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/liquidity-history?market=us-options&underlying=AAPL&from=2024-01-01&to=2024-01-05", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureLiquidityHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Market != "us-options" || len(resp.Data) != 1 || resp.Data[0].ActivityRatio == nil || *resp.Data[0].ActivityRatio != ratio {
		t.Fatalf("unexpected liquidity history response: %+v", resp)
	}
}

func TestFeatureEventWindowSnapshotRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	daysToNext := 3
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{eventWindowResp: &dto.FeatureEventWindowSnapshotResponse{Market: "us-options", Underlying: "AAPL", DaysToNextHoliday: &daysToNext}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/event-window-snapshot?market=us-options&underlying=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureEventWindowSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DaysToNextHoliday == nil || *resp.DaysToNextHoliday != daysToNext {
		t.Fatalf("unexpected event window response: %+v", resp)
	}
}

func TestFeatureEventWindowHistoryRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	daysFromPrev := 2
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{eventWindowHistResp: &dto.FeatureEventWindowHistoryResponse{Market: "us-options", Underlying: "AAPL", Data: []dto.FeatureEventWindowHistoryRow{{Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), FeatureEventWindowSnapshotResponse: dto.FeatureEventWindowSnapshotResponse{Market: "us-options", Underlying: "AAPL", DaysFromPrevHoliday: &daysFromPrev}}}}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/event-window-history?market=us-options&underlying=AAPL&from=2024-01-01&to=2024-01-05", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureEventWindowHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].DaysFromPrevHoliday == nil || *resp.Data[0].DaysFromPrevHoliday != daysFromPrev {
		t.Fatalf("unexpected event window history response: %+v", resp)
	}
}

func TestFeatureDailyPanelRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hv20 := 0.21
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{panelResp: &dto.FeatureDailyPanelResponse{Market: "us-options", Underlying: "AAPL", LookbackDays: 252, Data: []dto.FeatureDailyPanelRow{{Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), HV20: &hv20, LiquidityVolume: 1200}}}},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/features/daily-feature-panel?market=us-options&underlying=AAPL&from=2024-01-01&to=2024-01-05", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.FeatureDailyPanelResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].HV20 == nil || *resp.Data[0].HV20 != hv20 || resp.Data[0].LiquidityVolume != 1200 {
		t.Fatalf("unexpected daily panel response: %+v", resp)
	}
}

func TestGetMacroSeriesRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	macro := &mockMacroProvider{seriesResp: &dto.MacroSeriesResponse{
		Dataset:         "gurufocus-shiller",
		Interval:        "1m",
		ReferenceMarket: "us-stocks",
		ReferenceSymbol: "SPY",
		AsOf:            time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Data: []dto.MacroSeriesPoint{{
			Factor:          "pe10",
			Timestamp:       time.Date(2026, 4, 30, 19, 59, 0, 0, time.UTC),
			EventTS:         time.Date(2026, 4, 30, 19, 59, 0, 0, time.UTC),
			KnownAt:         time.Date(2026, 5, 1, 13, 30, 0, 0, time.UTC),
			Value:           35.2,
			Filled:          true,
			Realtime:        true,
			ReferenceMarket: "us-stocks",
			ReferenceSymbol: "SPY",
		}},
	}}

	r := NewRouterFromDeps(Deps{
		Config:        config.DefaultRuntime(),
		CryptoOptions: &mockQuerier{},
		USStocks:      &mockUSStocksQuerier{},
		USOptions:     &mockUSOptionsQuerier{},
		Infra:         &mockInfra{},
		Features:      &mockFeature{},
		Macro:         macro,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/macro/series?dataset=gurufocus-shiller&factor=pe10&from=2026-04-01&to=2026-05-01&interval=1m&reference_symbol=SPY", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if macro.seriesReq.Dataset != "gurufocus-shiller" {
		t.Fatalf("expected dataset to bind, got %+v", macro.seriesReq)
	}
	if macro.seriesReq.ReferenceSymbol != "SPY" {
		t.Fatalf("expected reference symbol to bind, got %+v", macro.seriesReq)
	}
	var resp dto.MacroSeriesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Factor != "pe10" {
		t.Fatalf("unexpected response payload: %+v", resp)
	}
}

func TestRebuildUniverseAcceptsDateOnlyJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universes := &mockUniverseProvider{rebuildResp: &dto.UniverseRebuildAccepted{Market: "us-stocks", Code: "strong_momentum", Accepted: true, Status: "queued"}}
	cfg := config.DefaultRuntime()
	r := NewRouterFromDeps(Deps{
		Config:    cfg,
		Universes: universes,
		APIKeys: fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
			"test-key": {ID: 1, KeyDigest: "test-digest"},
		}},
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/universes/rebuild", strings.NewReader(`{"market":"us-stocks","code":"strong_momentum","dry_run":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if !universes.rebuildReq.ForceRefresh {
		t.Fatal("force_refresh should default to true")
	}
}

func TestRebuildUniverseRejectsDeprecatedDates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universes := &mockUniverseProvider{rebuildResp: &dto.UniverseRebuildAccepted{Market: "us-stocks", Code: "strong_momentum", Accepted: true, Status: "queued"}}
	r := NewRouterFromDeps(Deps{
		Config:    config.DefaultRuntime(),
		Universes: universes,
		APIKeys: fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
			"test-key": {ID: 1, KeyDigest: "test-digest"},
		}},
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/universes/rebuild", strings.NewReader(`{"market":"us-stocks","code":"strong_momentum","from":"2024-01-01"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if universes.rebuildReq.Code != "" {
		t.Fatalf("rebuild should not be called for deprecated dates: %+v", universes.rebuildReq)
	}
}

func TestRebuildUniverseAcceptsForceRefreshFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universes := &mockUniverseProvider{rebuildResp: &dto.UniverseRebuildAccepted{Market: "us-stocks", Code: "strong_momentum", Accepted: true, Status: "queued"}}
	r := NewRouterFromDeps(Deps{
		Config:    config.DefaultRuntime(),
		Universes: universes,
		APIKeys: fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
			"test-key": {ID: 1, KeyDigest: "test-digest"},
		}},
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/universes/rebuild", strings.NewReader(`{"market":"us-stocks","code":"strong_momentum","force_refresh":false,"dry_run":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if universes.rebuildReq.ForceRefresh {
		t.Fatal("force_refresh=false was not preserved")
	}
}

func TestRebuildUniverseRequiresAPIKeyAuthenticator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universes := &mockUniverseProvider{rebuildResp: &dto.UniverseRebuildAccepted{Market: "us-stocks", Code: "strong_momentum", Accepted: true, Status: "queued"}}
	r := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Universes: universes})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/universes/rebuild", strings.NewReader(`{"market":"us-stocks","code":"strong_momentum"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if universes.rebuildReq.Code != "" {
		t.Fatalf("rebuild should not be called without API key auth: %+v", universes.rebuildReq)
	}
}

func TestGetUniverseMembersRoutesAsOfToMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universes := &mockUniverseProvider{membersResp: &dto.UniverseMembersResponse{Market: "us-stocks", Code: "strong_momentum"}}
	r := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Universes: universes})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/universes/strong_momentum/members?market=us-stocks&as_of=2024-02-01&limit=25", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := universes.membersReq.AsOf.Format("2006-01-02"); got != "2024-02-01" {
		t.Fatalf("as_of = %s, want 2024-02-01", got)
	}
	if universes.membersReq.From.IsZero() == false || universes.membersReq.To.IsZero() == false {
		t.Fatalf("as_of query should route to Members without range: %+v", universes.membersReq)
	}
	if universes.membersReq.Limit != 25 {
		t.Fatalf("limit = %d, want 25", universes.membersReq.Limit)
	}
}

func TestGetUniverseMembersRoutesRangeToMemberIntervals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	universes := &mockUniverseProvider{membersResp: &dto.UniverseMembersResponse{Market: "us-stocks", Code: "strong_momentum"}}
	r := NewRouterFromDeps(Deps{Config: config.DefaultRuntime(), Universes: universes})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/universes/strong_momentum/members?from=2024-02-01&to=2024-03-01", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := universes.membersReq.From.Format("2006-01-02"); got != "2024-02-01" {
		t.Fatalf("from = %s, want 2024-02-01", got)
	}
	if got := universes.membersReq.To.Format("2006-01-02"); got != "2024-03-01" {
		t.Fatalf("to = %s, want 2024-03-01", got)
	}
}
