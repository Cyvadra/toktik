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

// --- US market mock ---

type mockUSQuerier struct {
	stockBarsResp *dto.USStockBarResponse
	optionBarsResp *dto.USOptionBarResponse
	chainResp      *dto.USOptionChainResponse
	err            error
}

func (m *mockUSQuerier) QueryUSStockBars(_ context.Context, _ dto.USStockBarRequest) (*dto.USStockBarResponse, error) {
	return m.stockBarsResp, m.err
}
func (m *mockUSQuerier) QueryUSOptionBars(_ context.Context, _ dto.USOptionBarRequest) (*dto.USOptionBarResponse, error) {
	return m.optionBarsResp, m.err
}
func (m *mockUSQuerier) QueryUSOptionChain(_ context.Context, _ dto.USOptionChainRequest) (*dto.USOptionChainResponse, error) {
	return m.chainResp, m.err
}

// --- helpers ---

func setupRouter(q CryptoOptionsQuerier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(q)
}

func setupRouterWithUS(q CryptoOptionsQuerier, usq USMarketQuerier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouterWithUSMarket(q, usq)
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

// --- GetUSStockBars ---

func TestGetUSStockBars_Success(t *testing.T) {
	ts := time.Date(2024, 1, 3, 14, 30, 0, 0, time.UTC)
	mock := &mockUSQuerier{
		stockBarsResp: &dto.USStockBarResponse{
			Data: []dto.USStockBarRow{{Timestamp: ts, Symbol: "AAPL", Close: 185.0}},
		},
	}
	r := setupRouterWithUS(&mockQuerier{}, mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-stocks/bars?symbol=AAPL&interval=1h&from=2024-01-03&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.USStockBarResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 bar, got %d", len(resp.Data))
	}
}

func TestGetUSStockBars_MissingParam(t *testing.T) {
	r := setupRouterWithUS(&mockQuerier{}, &mockUSQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-stocks/bars?symbol=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetUSStockBars_ServiceError(t *testing.T) {
	mock := &mockUSQuerier{err: errors.New("db down")}
	r := setupRouterWithUS(&mockQuerier{}, mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-stocks/bars?symbol=AAPL&interval=1h&from=2024-01-03&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetUSStockBars_NotConfigured(t *testing.T) {
	// Router without US market service (nil usm)
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-stocks/bars?symbol=AAPL&interval=1h&from=2024-01-03&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- GetUSOptionBars ---

func TestGetUSOptionBars_Success(t *testing.T) {
	ts := time.Date(2024, 1, 3, 14, 30, 0, 0, time.UTC)
	exp := time.Date(2024, 2, 16, 0, 0, 0, 0, time.UTC)
	mock := &mockUSQuerier{
		optionBarsResp: &dto.USOptionBarResponse{
			Data: []dto.USOptionBarRow{
				{Timestamp: ts, Symbol: "O:AAPL240216C00185000", Underlying: "AAPL",
					OptionType: "C", Expiration: exp, Strike: 185.0, Close: 3.5, Delta: 0.5},
			},
		},
	}
	r := setupRouterWithUS(&mockQuerier{}, mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-options/bars?symbol=O:AAPL240216C00185000&interval=1h&from=2024-01-03&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.USOptionBarResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 bar, got %d", len(resp.Data))
	}
}

func TestGetUSOptionBars_MissingParam(t *testing.T) {
	r := setupRouterWithUS(&mockQuerier{}, &mockUSQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-options/bars?symbol=O:AAPL240216C00185000", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetUSOptionBars_ValidationError(t *testing.T) {
	mock := &mockUSQuerier{err: dto.NewValidationError("bad interval")}
	r := setupRouterWithUS(&mockQuerier{}, mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-options/bars?symbol=O:AAPL240216C00185000&interval=bad&from=2024-01-03&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- GetUSOptionChain ---

func TestGetUSOptionChain_Success(t *testing.T) {
	ts := time.Date(2024, 1, 3, 14, 30, 0, 0, time.UTC)
	mock := &mockUSQuerier{
		chainResp: &dto.USOptionChainResponse{
			Data: []dto.USOptionChainRow{
				{Timestamp: ts, Underlying: "AAPL",
					Symbols:     []string{"O:AAPL240216C00185000"},
					OptionTypes: []string{"C"},
					Strikes:     []float64{185.0},
					ClosePrices: []float32{3.5},
					Deltas:      []float32{0.5},
				},
			},
		},
	}
	r := setupRouterWithUS(&mockQuerier{}, mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-options/chain?underlying=AAPL&interval=1h&from=2024-01-03&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dto.USOptionChainResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 chain row, got %d", len(resp.Data))
	}
}

func TestGetUSOptionChain_MissingParam(t *testing.T) {
	r := setupRouterWithUS(&mockQuerier{}, &mockUSQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-options/chain?underlying=AAPL", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetUSOptionChain_ServiceError(t *testing.T) {
	mock := &mockUSQuerier{err: errors.New("db down")}
	r := setupRouterWithUS(&mockQuerier{}, mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/us-options/chain?underlying=AAPL&interval=1h&from=2024-01-03&to=2024-01-04", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}
