package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
	"github.com/gin-gonic/gin"
)

// --- mock service ---

type mockQuerier struct {
	barsResp    *dto.BarResponse
	symbolsResp *dto.SymbolResponse
	greeksResp  *dto.GreeksResponse
	btResp      *backtest.Result
	err         error
}

type mockUSStocksQuerier struct {
	barsResp    *dto.USStockBarResponse
	symbolsResp *dto.USStockSymbolResponse
	err         error
}

type mockUSOptionsQuerier struct {
	barsResp    *dto.USOptionBarResponse
	symbolsResp *dto.USOptionSymbolResponse
	greeksResp  *dto.USOptionGreeksResponse
	chainResp   *dto.USOptionChainResponse
	barsReq     dto.USOptionBarRequest
	symbolsReq  dto.USOptionSymbolRequest
	greeksReq   dto.USOptionGreeksRequest
	chainReq    dto.USOptionChainRequest
	err         error
}

type mockInfra struct {
	readyResp    *dto.ReadinessResponse
	marketsResp  *dto.MarketCatalogResponse
	datasetsResp *dto.DatasetCatalogResponse
	err          error
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

func (m *mockUSStocksQuerier) QueryBars(_ context.Context, _ dto.USStockBarRequest) (*dto.USStockBarResponse, error) {
	return m.barsResp, m.err
}

func (m *mockUSStocksQuerier) QuerySymbols(_ context.Context, _ dto.USStockSymbolRequest) (*dto.USStockSymbolResponse, error) {
	return m.symbolsResp, m.err
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

func (m *mockInfra) Readiness(_ context.Context) (*dto.ReadinessResponse, error) {
	return m.readyResp, m.err
}

func (m *mockInfra) ListMarkets(_ context.Context) (*dto.MarketCatalogResponse, error) {
	return m.marketsResp, m.err
}

func (m *mockInfra) ListDatasets(_ context.Context, _ dto.DatasetQueryRequest) (*dto.DatasetCatalogResponse, error) {
	return m.datasetsResp, m.err
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

// --- helpers ---

func setupRouter(q CryptoOptionsQuerier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(q, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil)
}

// --- GetBars ---

func TestGetBars_Success(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mock := &mockQuerier{
		barsResp: &dto.BarResponse{
			Data: []dto.BarRow{{Timestamp: ts, SymbolID: 1, MarkClose: 100, ImpliedVolatility: 0.42}},
		},
	}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/bars?symbol=BTC-1&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.BarResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 bar, got %d", len(resp.Data))
	}
	if resp.Data[0].ImpliedVolatility != 0.42 {
		t.Fatalf("expected implied volatility 0.42, got %v", resp.Data[0].ImpliedVolatility)
	}
}

func TestRunIndicatorSeries_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, &mockIndicatorProvider{
		resp: &dto.IndicatorSeriesResponse{
			Market:     "crypto-spot",
			Symbol:     "BTCUSDT",
			Interval:   "1h",
			Timestamps: []time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			Series:     map[string][]*float64{"ta.sma(close,3)": {func() *float64 { v := 42.0; return &v }()}},
		},
	}, nil, nil, nil, nil, nil, nil)

	body := `{"market":"crypto-spot","symbol":"BTCUSDT","interval":"1h","from":"2024-01-01","to":"2024-01-02","indicators":["ta.sma(close,3)"],"precision":2}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/indicators/series", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.IndicatorSeriesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Market != "crypto-spot" || len(resp.Series) != 1 {
		t.Fatalf("unexpected indicator response: %+v", resp)
	}
	series := resp.Series["ta.sma(close,3)"]
	if len(series) != 1 || series[0] == nil || *series[0] != 42 {
		t.Fatalf("unexpected indicator value: %+v", series)
	}
}

func TestRunIndicatorSeries_NotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, nil, nil, nil, nil, nil, nil, nil)

	body := `{"market":"crypto-spot","symbol":"BTCUSDT","interval":"1h","from":"2024-01-01","to":"2024-01-02","indicators":["close"]}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/indicators/series", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListIndicatorPresets_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{}, &mockIndicatorProvider{
		cat: &dto.IndicatorPresetCatalogResponse{Presets: []dto.IndicatorPresetDefinition{{
			ID:   "classic",
			Name: "Classic Technicals",
			Indicators: []dto.IndicatorPresetIndicator{{
				Key:        "rsi_14",
				Expression: "ta.rsi(close,14)",
			}},
		}}},
	}, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/indicators/presets", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.IndicatorPresetCatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Presets) != 1 || resp.Presets[0].ID != "classic" {
		t.Fatalf("unexpected preset response: %+v", resp)
	}
}

func TestGetBars_MissingParam(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/bars?symbol=BTC-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetBars_ValidationError(t *testing.T) {
	mock := &mockQuerier{err: dto.NewValidationError("bad symbol")}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/bars?symbol=X&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetBars_InternalError(t *testing.T) {
	mock := &mockQuerier{err: errors.New("db down")}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/bars?symbol=X&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var errResp dto.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if errResp.Error != "internal server error" {
		t.Fatalf("expected generic error, got %q", errResp.Error)
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

func TestGetBars_Timeout(t *testing.T) {
	mock := &mockQuerier{err: context.DeadlineExceeded}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/bars?symbol=X&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Code)
	}
}

// --- GetSymbols ---

func TestGetSymbols_Success(t *testing.T) {
	mock := &mockQuerier{
		symbolsResp: &dto.SymbolResponse{
			Data: []dto.SymbolRow{{SymbolID: 1, Symbol: "BTC-CALL-50000"}},
		},
	}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/symbols?base_asset=BTC", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- GetGreeks ---

func TestGetGreeks_Success(t *testing.T) {
	mock := &mockQuerier{
		greeksResp: &dto.GreeksResponse{
			Data: []dto.GreeksRow{{Delta: 0.5}},
		},
	}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/greeks?symbol=X&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetGreeks_MissingSymbol(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/crypto-options/greeks?from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- RunBacktest ---

func TestRunBacktest_Success(t *testing.T) {
	mock := &mockQuerier{
		btResp: &backtest.Result{},
	}
	r := setupRouter(mock)

	body := `{"symbol":"BTC","interval":"1h","from":"2024-01-01","to":"2024-02-01"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/crypto-options/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunBacktest_BadJSON(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/crypto-options/backtest", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRunBacktest_MissingRequired(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	body := `{"symbol":"BTC"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/crypto-options/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRunBacktest_ServiceError(t *testing.T) {
	mock := &mockQuerier{err: errors.New("engine boom")}
	r := setupRouter(mock)

	body := `{"symbol":"BTC","interval":"1h","from":"2024-01-01","to":"2024-02-01"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/crypto-options/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
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
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf(`expected status "ok", got %q`, body["status"])
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

func TestMarketAliasRoute(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mock := &mockQuerier{
		barsResp: &dto.BarResponse{Data: []dto.BarRow{{Timestamp: ts, SymbolID: 1, MarkClose: 100}}},
	}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/bars?symbol=BTC-1&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUSStocksBarsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{barsResp: &dto.USStockBarResponse{Data: []dto.USStockBarRow{{Timestamp: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Symbol: "AAPL", Close: 192.5}}}},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-stocks/bars?symbol=AAPL&interval=1m&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUSStocksSymbolsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{symbolsResp: &dto.USStockSymbolResponse{Data: []dto.USStockSymbolRow{{Symbol: "AAPL"}}}},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-stocks/symbols?search=AA", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUSOptionsBarsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockUSOptionsQuerier{barsResp: &dto.USOptionBarResponse{Data: []dto.USOptionBarRow{{Timestamp: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Symbol: "O:AAPL240119C00190000", Underlying: "AAPL", Close: 4.25}}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		mock,
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/bars?symbol=O:AAPL240119C00190000&interval=1m&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.barsReq.Symbol != "O:AAPL240119C00190000" {
		t.Fatalf("expected symbol to be forwarded, got %q", mock.barsReq.Symbol)
	}
}

func TestUSOptionsSymbolsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockUSOptionsQuerier{symbolsResp: &dto.USOptionSymbolResponse{Data: []dto.USOptionSymbolRow{{Symbol: "O:AAPL240119C00190000", Underlying: "AAPL", OptionType: "C", Strike: 190}}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		mock,
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/symbols?underlying=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.symbolsReq.Underlying != "AAPL" {
		t.Fatalf("expected underlying AAPL, got %q", mock.symbolsReq.Underlying)
	}
}

func TestUSOptionsSymbolsRouteAcceptsRootAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockUSOptionsQuerier{symbolsResp: &dto.USOptionSymbolResponse{Data: []dto.USOptionSymbolRow{{Symbol: "O:AAPL240119C00190000", Underlying: "AAPL", OptionType: "C", Strike: 190}}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		mock,
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/symbols?root=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.symbolsReq.Underlying != "AAPL" {
		t.Fatalf("expected root alias to populate underlying, got %q", mock.symbolsReq.Underlying)
	}
}

func TestUSOptionsGreeksRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockUSOptionsQuerier{greeksResp: &dto.USOptionGreeksResponse{Data: []dto.USOptionGreeksRow{{Timestamp: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Symbol: "O:AAPL240119C00190000", Delta: 0.42}}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		mock,
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/greeks?symbol=O:AAPL240119C00190000&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.greeksReq.Symbol != "O:AAPL240119C00190000" {
		t.Fatalf("expected symbol to be forwarded, got %q", mock.greeksReq.Symbol)
	}
}

func TestUSOptionsChainRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockUSOptionsQuerier{chainResp: &dto.USOptionChainResponse{Data: []dto.USOptionChainSnapshot{{Timestamp: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Underlying: "AAPL", Contracts: []dto.USOptionChainContract{{Symbol: "O:AAPL240119C00190000", Delta: 0.42}}}}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		mock,
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/chain?underlying=AAPL&from=2024-01-01&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.chainReq.Underlying != "AAPL" {
		t.Fatalf("expected underlying AAPL, got %q", mock.chainReq.Underlying)
	}
	if mock.chainReq.From != "2024-01-01" || mock.chainReq.To != "2024-01-04" {
		t.Fatalf("expected from/to to be forwarded, got from=%q to=%q", mock.chainReq.From, mock.chainReq.To)
	}
}

func TestUSOptionsChainRouteAcceptsExpirationWithoutRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockUSOptionsQuerier{chainResp: &dto.USOptionChainResponse{Data: []dto.USOptionChainSnapshot{}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		mock,
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/chain?underlying=AAPL&expiration=2024-01-19", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.chainReq.Expiration != "2024-01-19" {
		t.Fatalf("expected expiration to be forwarded, got %q", mock.chainReq.Expiration)
	}
}

func TestUSOptionsBarsRouteRejectsUnderlyingOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/bars?underlying=AAPL&interval=1d&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "underlying is not supported") {
		t.Fatalf("expected explicit unsupported-underlying message, got %s", w.Body.String())
	}
}

func TestUSOptionsGreeksRouteRejectsUnderlyingOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/greeks?underlying=AAPL&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "underlying is not supported") {
		t.Fatalf("expected explicit unsupported-underlying message, got %s", w.Body.String())
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

func TestStartStrategyBacktestRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockBacktests := &mockStrategyBacktests{startResp: &dto.StrategyBacktestRunAccepted{
		RunID:     "run-123",
		Status:    "queued",
		CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
		StatusURL: "/api/v1/backtests/runs/run-123",
		EventsURL: "/api/v1/backtests/runs/run-123/events",
		ReportURL: "/internal/ignored/by-handler",
	}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		mockBacktests,
		nil, nil, nil, nil, nil,
	)

	body := `{"asset":"BTC","from":"2026-01-01","to":"2026-02-01","capital":5}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/backtests/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.StrategyBacktestRunAccepted
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.RunID != "run-123" || resp.EventsURL == "" {
		t.Fatalf("unexpected start response: %+v", resp)
	}
	if resp.ReportURL != "/api/v1/backtests/runs/run-123/report" {
		t.Fatalf("unexpected report url: %+v", resp)
	}
	if mockBacktests.startReq.Asset != "BTC" || mockBacktests.startReq.Capital != 5 {
		t.Fatalf("unexpected captured request: %+v", mockBacktests.startReq)
	}
}

func TestValidateStrategyBacktestRouteWithDSL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validateResp := &dto.StrategyBacktestValidationResponse{
		StrategyLabel: "runtime-dsl",
		StrategyCount: 1,
		Strategies: []dto.StrategyBacktestValidationItem{{
			DisplayName:   "Runtime DSL",
			ProfileLabel:  "常规交易",
			ProfileSource: "inferred",
			Runtime: &dto.StrategyBacktestValidationRuntime{
				Market:               "crypto",
				Instrument:           "auto",
				CapitalMode:          "usd",
				CapitalUnit:          "USD",
				CapitalExplanation:   "该策略不包含合约逻辑，capital 按 USD 计价。",
				OptionsChainRequired: false,
				RegularTradeSummary:  "Regular trades consume meaningful capital alongside any option legs.",
			},
			Warnings: []string{"dsl_profile not provided; strategy profile was inferred from the DSL AST and may need an explicit override for scripts with indirect option helpers or atypical execution wrappers."},
			DSLParams: []dto.StrategyBacktestDSLParam{{
				Name:    "length",
				Title:   "Length",
				Type:    "int",
				Default: 5,
			}},
		}},
	}
	mockBacktests := &mockStrategyBacktests{validateResp: validateResp}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		mockBacktests,
		nil, nil, nil, nil, nil,
	)

	body := `{"asset":"BTC","from":"2026-01-01","to":"2026-02-01","capital":5,"dsl":"strategy(\"Runtime DSL\")\nlength = input.int(5, title=\"Length\")\nplot(close, title=\"Close\")","dsl_params":{"Length":8}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/backtests/validate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(mockBacktests.validateReq.DSL) == "" {
		t.Fatalf("expected DSL in validate request, got %+v", mockBacktests.validateReq)
	}
	var resp dto.StrategyBacktestValidationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.StrategyCount != 1 || len(resp.Strategies) != 1 {
		t.Fatalf("unexpected validate response: %+v", resp)
	}
	if len(resp.Strategies[0].DSLParams) != 1 || resp.Strategies[0].DSLParams[0].Title != "Length" {
		t.Fatalf("unexpected validate params: %+v", resp.Strategies[0].DSLParams)
	}
	if resp.Strategies[0].ProfileSource != "inferred" || len(resp.Strategies[0].Warnings) != 1 {
		t.Fatalf("unexpected validate warnings/profile source: %+v", resp.Strategies[0])
	}
}

func TestStartStrategyBacktestRouteWithDSL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockBacktests := &mockStrategyBacktests{startResp: &dto.StrategyBacktestRunAccepted{
		RunID:     "run-dsl",
		Status:    "queued",
		CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
		StatusURL: "/api/v1/backtests/runs/run-dsl",
		EventsURL: "/api/v1/backtests/runs/run-dsl/events",
	}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		mockBacktests,
		nil, nil, nil, nil, nil,
	)

	body := `{"asset":"BTC","from":"2026-01-01","to":"2026-02-01","capital":5,"dsl":"strategy(\"Runtime DSL\")\nlength = input.int(5, title=\"Length\")\nif bar_index == 0 {\n  strategy.entry(id=\"long\", direction=strategy.long, qty=1)\n}","dsl_params":{"Length":8},"dsl_profile":{"uses_options":false,"regular_trade":"material"}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/backtests/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(mockBacktests.startReq.DSL) == "" {
		t.Fatalf("expected DSL to be bound, got %+v", mockBacktests.startReq)
	}
	if mockBacktests.startReq.DSLParams["Length"] != float64(8) {
		t.Fatalf("unexpected DSL params: %+v", mockBacktests.startReq.DSLParams)
	}
	if mockBacktests.startReq.DSLProfile == nil || mockBacktests.startReq.DSLProfile.UsesOptions == nil || *mockBacktests.startReq.DSLProfile.UsesOptions {
		t.Fatalf("unexpected DSL profile: %+v", mockBacktests.startReq.DSLProfile)
	}
}

func TestGetStrategyBacktestRunRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		&mockStrategyBacktests{statusResp: &dto.StrategyBacktestRunStatus{
			RunID:     "run-123",
			Status:    "running",
			Request:   dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-02-01", Capital: 5},
			CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 7, 8, 0, 1, 0, time.UTC),
			Progress:  &dto.StrategyBacktestProgress{Phase: "prepare", Current: 10, Total: 100, Percent: 10},
		}},
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/backtests/runs/run-123", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.StrategyBacktestRunStatus
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.RunID != "run-123" || resp.Progress == nil || resp.Progress.Percent != 10 {
		t.Fatalf("unexpected status response: %+v", resp)
	}
	if resp.ReportURL != "/api/v1/backtests/runs/run-123/report" {
		t.Fatalf("unexpected report url: %+v", resp)
	}
}

func TestStreamStrategyBacktestEventsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := make(chan dto.StrategyBacktestSSEvent, 1)
	stream <- dto.StrategyBacktestSSEvent{Event: "progress", Status: &dto.StrategyBacktestRunStatus{
		RunID:     "run-123",
		Status:    "running",
		Request:   dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-02-01", Capital: 5},
		CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 7, 8, 0, 1, 0, time.UTC),
		Progress:  &dto.StrategyBacktestProgress{Phase: "replay", Current: 30, Total: 100, Percent: 30},
	}}
	close(stream)

	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		&mockStrategyBacktests{
			statusResp: &dto.StrategyBacktestRunStatus{
				RunID:     "run-123",
				Status:    "running",
				Request:   dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-02-01", Capital: 5},
				CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
				Progress:  &dto.StrategyBacktestProgress{Phase: "prepare", Current: 0, Total: 100, Percent: 0},
			},
			stream: stream,
		},
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/backtests/runs/run-123/events", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Fatalf("expected status event, got %q", body)
	}
	if !strings.Contains(body, "event: progress") {
		t.Fatalf("expected progress event, got %q", body)
	}
	if !strings.Contains(body, `"run_id":"run-123"`) {
		t.Fatalf("expected run payload, got %q", body)
	}
}

func TestGetStrategyBacktestReportRoutePending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		&mockStrategyBacktests{statusResp: &dto.StrategyBacktestRunStatus{
			RunID:     "run-123",
			Status:    "running",
			Request:   dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-02-01", Capital: 5},
			CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 7, 8, 0, 1, 0, time.UTC),
			Progress:  &dto.StrategyBacktestProgress{Phase: "prepare", Current: 5, Total: 100, Percent: 5},
		}},
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/backtests/runs/run-123/report", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.StrategyBacktestRunStatus
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ReportURL != "/api/v1/backtests/runs/run-123/report" {
		t.Fatalf("unexpected report url: %+v", resp)
	}
}

func TestGetStrategyBacktestReportRouteCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempFile, err := os.CreateTemp(t.TempDir(), "report-*.html")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	html := "<html><body>ok</body></html>"
	if _, err := tempFile.WriteString(html); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		&mockStrategyBacktests{statusResp: &dto.StrategyBacktestRunStatus{
			RunID:     "run-123",
			Status:    "completed",
			Request:   dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-02-01", Capital: 5},
			CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 7, 8, 0, 1, 0, time.UTC),
			Result:    &dto.StrategyBacktestRunResult{Summaries: []dto.StrategyBacktestSummary{{StrategyName: "demo", HTMLPath: tempFile.Name()}}},
		}},
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/backtests/runs/run-123/report", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected html content type, got %q", got)
	}
	if w.Body.String() != html {
		t.Fatalf("unexpected html body: %q", w.Body.String())
	}
}

func TestGetStrategyBacktestNamedReportRouteOverview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempFile, err := os.CreateTemp(t.TempDir(), "overview-*.html")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	html := "<html><body>overview</body></html>"
	if _, err := tempFile.WriteString(html); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		&mockStrategyBacktests{statusResp: &dto.StrategyBacktestRunStatus{
			RunID:     "run-123",
			Status:    "completed",
			Request:   dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-02-01", Capital: 5},
			CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 7, 8, 0, 1, 0, time.UTC),
			Result: &dto.StrategyBacktestRunResult{
				OverviewHTMLPath: tempFile.Name(),
				Summaries:        []dto.StrategyBacktestSummary{{StrategyName: "demo", HTMLPath: tempFile.Name()}},
			},
		}},
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/backtests/runs/run-123/reports/overview", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != html {
		t.Fatalf("unexpected html body: %q", w.Body.String())
	}
}
