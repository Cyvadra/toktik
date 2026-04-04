package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
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

func (m *mockUSStocksQuerier) QueryBars(_ context.Context, _ dto.USStockBarRequest) (*dto.USStockBarResponse, error) {
	return m.barsResp, m.err
}

func (m *mockUSStocksQuerier) QuerySymbols(_ context.Context, _ dto.USStockSymbolRequest) (*dto.USStockSymbolResponse, error) {
	return m.symbolsResp, m.err
}

func (m *mockUSOptionsQuerier) QueryBars(_ context.Context, _ dto.USOptionBarRequest) (*dto.USOptionBarResponse, error) {
	return m.barsResp, m.err
}

func (m *mockUSOptionsQuerier) QuerySymbols(_ context.Context, _ dto.USOptionSymbolRequest) (*dto.USOptionSymbolResponse, error) {
	return m.symbolsResp, m.err
}

func (m *mockUSOptionsQuerier) QueryGreeks(_ context.Context, _ dto.USOptionGreeksRequest) (*dto.USOptionGreeksResponse, error) {
	return m.greeksResp, m.err
}

func (m *mockUSOptionsQuerier) QueryChain(_ context.Context, _ dto.USOptionChainRequest) (*dto.USOptionChainResponse, error) {
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

// --- helpers ---

func setupRouter(q CryptoOptionsQuerier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(q, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{}, &mockFeature{})
}

// --- GetBars ---

func TestGetBars_Success(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mock := &mockQuerier{
		barsResp: &dto.BarResponse{
			Data: []dto.BarRow{{Timestamp: ts, SymbolID: 1, MarkClose: 100}},
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
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{readyResp: &dto.ReadinessResponse{Status: "ready"}}, &mockFeature{})

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
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{marketsResp: &dto.MarketCatalogResponse{Markets: []dto.MarketDescriptor{{Name: "crypto-options", Status: "available"}}}}, &mockFeature{})

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
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{datasetsResp: &dto.DatasetCatalogResponse{Summary: dto.DatasetSummary{Total: 1, Ready: 1}, Datasets: []dto.DatasetDescriptor{{Name: "crypto-options-bars", Status: "ready"}}}}, &mockFeature{})

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
	r := NewRouter(&mockQuerier{}, &mockUSStocksQuerier{}, &mockUSOptionsQuerier{}, &mockInfra{datasetsResp: &dto.DatasetCatalogResponse{Summary: dto.DatasetSummary{Total: 1, Ready: 1}, Datasets: []dto.DatasetDescriptor{{Name: "us-options-bars", Market: "us-options", Status: "ready"}}}}, &mockFeature{})

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
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{barsResp: &dto.USOptionBarResponse{Data: []dto.USOptionBarRow{{Timestamp: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Symbol: "O:AAPL240119C00190000", Underlying: "AAPL", Close: 4.25}}}},
		&mockInfra{},
		&mockFeature{},
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/bars?symbol=O:AAPL240119C00190000&interval=1m&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUSOptionsSymbolsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{symbolsResp: &dto.USOptionSymbolResponse{Data: []dto.USOptionSymbolRow{{Symbol: "O:AAPL240119C00190000", Underlying: "AAPL", OptionType: "C", Strike: 190}}}},
		&mockInfra{},
		&mockFeature{},
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/symbols?underlying=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUSOptionsGreeksRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{greeksResp: &dto.USOptionGreeksResponse{Data: []dto.USOptionGreeksRow{{Timestamp: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Symbol: "O:AAPL240119C00190000", Delta: 0.42}}}},
		&mockInfra{},
		&mockFeature{},
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/greeks?symbol=O:AAPL240119C00190000&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUSOptionsChainRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{chainResp: &dto.USOptionChainResponse{Data: []dto.USOptionChainSnapshot{{Timestamp: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Underlying: "AAPL", Contracts: []dto.USOptionChainContract{{Symbol: "O:AAPL240119C00190000", Delta: 0.42}}}}}},
		&mockInfra{},
		&mockFeature{},
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-options/chain?underlying=AAPL&from=2024-01-01&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
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
