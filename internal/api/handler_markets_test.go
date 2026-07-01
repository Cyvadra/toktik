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
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

func TestGetBars_Success(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mock := &mockQuerier{
		barsResp: &dto.BarResponse{
			Data: []dto.BarRow{{Timestamp: ts, SymbolID: 1, MarkClose: 100, ImpliedVolatility: 0.42}},
		},
	}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/bars?symbol=BTC-1&interval=1m&from=2024-01-01&to=2024-01-02", nil)
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

func TestGetBars_MissingParam(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/bars?symbol=BTC-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetBars_ValidationError(t *testing.T) {
	mock := &mockQuerier{err: dto.NewValidationError("bad symbol")}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/bars?symbol=X&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetBars_InternalError(t *testing.T) {
	mock := &mockQuerier{err: errors.New("db down")}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/bars?symbol=X&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var errResp dto.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if errResp.Error != "internal server error" {
		t.Fatalf("expected generic prod error, got %q", errResp.Error)
	}
}

func TestGetBars_InternalErrorDevEnvironment(t *testing.T) {
	g := config.DefaultRuntime()
	g.API.Environment = "dev"
	r := NewRouterFromDeps(Deps{
		Config:        g,
		CryptoOptions: &mockQuerier{err: errors.New("db down")},
		USStocks:      &mockUSStocksQuerier{},
		USOptions:     &mockUSOptionsQuerier{},
		Infra:         &mockInfra{},
		Features:      &mockFeature{},
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/bars?symbol=X&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var errResp dto.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if errResp.Error != "db down" {
		t.Fatalf("expected propagated dev error, got %q", errResp.Error)
	}
}

func TestGetBars_Timeout(t *testing.T) {
	mock := &mockQuerier{err: context.DeadlineExceeded}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/bars?symbol=X&interval=1m&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Code)
	}
}

func TestGetSymbols_Success(t *testing.T) {
	mock := &mockQuerier{
		symbolsResp: &dto.SymbolResponse{
			Data: []dto.SymbolRow{{SymbolID: 1, Symbol: "BTC-CALL-50000"}},
		},
	}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/symbols?base_asset=BTC", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetGreeks_Success(t *testing.T) {
	mock := &mockQuerier{
		greeksResp: &dto.GreeksResponse{
			Data: []dto.GreeksRow{{Delta: 0.5}},
		},
	}
	r := setupRouter(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/greeks?symbol=X&from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetGreeks_MissingSymbol(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/crypto-options/greeks?from=2024-01-01&to=2024-01-02", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRunBacktest_Success(t *testing.T) {
	mock := &mockQuerier{btResp: &backtest.Result{}}
	r := setupRouter(mock)

	body := `{"symbol":"BTC","interval":"1h","from":"2024-01-01","to":"2024-02-01"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/markets/crypto-options/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunBacktest_BadJSON(t *testing.T) {
	r := setupRouter(&mockQuerier{})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/markets/crypto-options/backtest", strings.NewReader("{"))
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
	req, _ := http.NewRequest("POST", "/api/v1/markets/crypto-options/backtest", strings.NewReader(body))
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
	req, _ := http.NewRequest("POST", "/api/v1/markets/crypto-options/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
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
	mock := &mockUSStocksQuerier{barsResp: &dto.USStockBarResponse{Data: []dto.USStockBarRow{{Timestamp: time.Date(2024, 1, 2, 14, 30, 0, 0, time.UTC), Symbol: "AAPL", Close: 192.5}}, Meta: &dto.USStockBarMeta{Profile: &dto.USStockCompanyProfile{Symbol: "AAPL", Sector: "Technology", Industry: "Consumer Electronics"}}}}
	r := NewRouter(
		&mockQuerier{},
		mock,
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-stocks/bars?symbol=AAPL&interval=1m&from=2024-01-02&to=2024-01-03&factor=pe&factor=pb&adjusted=false", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(mock.barsReq.Factors) != 2 || mock.barsReq.Factors[0] != "pe" || mock.barsReq.Factors[1] != "pb" {
		t.Fatalf("expected factor query params to be forwarded, got %#v", mock.barsReq.Factors)
	}
	if mock.barsReq.Adjusted == nil || *mock.barsReq.Adjusted {
		t.Fatalf("expected adjusted=false query param to be forwarded, got %#v", mock.barsReq.Adjusted)
	}
	if !strings.Contains(w.Body.String(), `"meta":{"profile":{"symbol":"AAPL","sector":"Technology","industry":"Consumer Electronics"}}`) {
		t.Fatalf("expected company profile metadata in response body, got %s", w.Body.String())
	}
}

func TestForexBarsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockForexQuerier{barsResp: &dto.ForexBarResponse{Data: []dto.ForexBarRow{{Timestamp: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Symbol: "EURUSD", Close: 1.101}}}}
	r := NewRouterFromDeps(Deps{
		Config:        config.DefaultRuntime(),
		Forex:         mock,
		CryptoOptions: &mockQuerier{},
		USStocks:      &mockUSStocksQuerier{},
		USOptions:     &mockUSOptionsQuerier{},
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/forex/bars?symbol=EURUSD&interval=1h&from=2024-01-02&to=2024-01-03", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.barsReq.Symbol != "EURUSD" || mock.barsReq.Interval != "1h" {
		t.Fatalf("expected forwarded forex request, got %#v", mock.barsReq)
	}
}

func TestUSStocksSymbolsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{symbolsResp: &dto.USStockSymbolResponse{Data: []dto.USStockSymbolRow{{Symbol: "AAPL", Profile: &dto.USStockCompanyProfile{Symbol: "AAPL", Sector: "Technology", Industry: "Consumer Electronics"}}}}},
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
	if !strings.Contains(w.Body.String(), `"profile":{"symbol":"AAPL","sector":"Technology","industry":"Consumer Electronics"}`) {
		t.Fatalf("expected company profile metadata in symbols response, got %s", w.Body.String())
	}
}

func TestUSStocksSplitsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockUSStocksQuerier{splitsResp: &dto.USStockSplitResponse{Data: []dto.USStockSplitRow{{
		Symbol:      "AAPL",
		SplitDate:   time.Date(2020, 8, 31, 0, 0, 0, 0, time.UTC),
		Numerator:   4,
		Denominator: 1,
		SplitType:   "Stock Split",
		Source:      "fmp",
		UpdatedAt:   time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}}}}
	r := NewRouter(
		&mockQuerier{},
		mock,
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-stocks/splits?symbol=AAPL&symbol=MSFT", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(mock.splitsReq.Symbols) != 2 || mock.splitsReq.Symbols[0] != "AAPL" || mock.splitsReq.Symbols[1] != "MSFT" {
		t.Fatalf("expected split symbols to be forwarded, got %#v", mock.splitsReq.Symbols)
	}
	if !strings.Contains(w.Body.String(), `"symbol":"AAPL"`) || !strings.Contains(w.Body.String(), `"numerator":4`) {
		t.Fatalf("expected split response body, got %s", w.Body.String())
	}
}

func TestUSStocksSplitsRouteRejectsMissingSymbol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{err: dto.NewValidationError("symbol is required")},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/markets/us-stocks/splits", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
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

func TestUSMarketRoutesBindIncludeLatest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stocks := &mockUSStocksQuerier{barsResp: &dto.USStockBarResponse{Data: []dto.USStockBarRow{}}}
	options := &mockUSOptionsQuerier{
		barsResp:    &dto.USOptionBarResponse{Data: []dto.USOptionBarRow{}},
		symbolsResp: &dto.USOptionSymbolResponse{Data: []dto.USOptionSymbolRow{}},
		chainResp:   &dto.USOptionChainResponse{Data: []dto.USOptionChainSnapshot{}},
	}
	r := NewRouter(
		&mockQuerier{},
		stocks,
		options,
		&mockInfra{},
		&mockFeature{},
		nil,
		nil, nil, nil, nil, nil,
	)

	requests := []struct {
		url   string
		check func()
	}{
		{url: "/api/v1/markets/us-stocks/bars?symbol=AAPL&interval=1d&from=2026-06-09&to=2026-06-11&include_latest=true", check: func() {
			if !stocks.barsReq.IncludeLatest {
				t.Fatalf("expected us-stocks bars include_latest to bind true")
			}
		}},
		{url: "/api/v1/markets/us-options/bars?symbol=O:AAPL260619C00190000&interval=1d&from=2026-06-09&to=2026-06-11&include_latest=true", check: func() {
			if !options.barsReq.IncludeLatest {
				t.Fatalf("expected us-options bars include_latest to bind true")
			}
		}},
		{url: "/api/v1/markets/us-options/symbols?underlying=AAPL&include_latest=true", check: func() {
			if !options.symbolsReq.IncludeLatest {
				t.Fatalf("expected us-options symbols include_latest to bind true")
			}
		}},
		{url: "/api/v1/markets/us-options/chain?underlying=AAPL&include_latest=true", check: func() {
			if !options.chainReq.IncludeLatest {
				t.Fatalf("expected us-options chain include_latest to bind true")
			}
		}},
	}
	for _, tc := range requests {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", tc.url, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", tc.url, w.Code, w.Body.String())
		}
		tc.check()
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

func TestScreenOptionsRouteSupportsMinDTEAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockScreener{optionsResp: &dto.ScreenOptionResponse{Data: []dto.ScreenedOption{}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil,
		nil,
		mock,
		nil,
		nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/screener/options?market=us-options&underlying=AAPL&min_dte=14&max_dte=60&limit=10", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.optionsReq.DTEMin == nil || *mock.optionsReq.DTEMin != 14 {
		t.Fatalf("expected min_dte alias to populate DTEMin, got %+v", mock.optionsReq)
	}
	if mock.optionsReq.DTEMax == nil || *mock.optionsReq.DTEMax != 60 {
		t.Fatalf("expected max_dte alias to populate DTEMax, got %+v", mock.optionsReq)
	}
	if mock.optionsReq.MinDTE == nil || *mock.optionsReq.MinDTE != 14 {
		t.Fatalf("expected normalized MinDTE, got %+v", mock.optionsReq)
	}
	if mock.optionsReq.MaxDTE == nil || *mock.optionsReq.MaxDTE != 60 {
		t.Fatalf("expected normalized MaxDTE, got %+v", mock.optionsReq)
	}
	if mock.optionsReq.IncludeLatest == nil || !*mock.optionsReq.IncludeLatest {
		t.Fatalf("expected screener options include_latest to default true, got %+v", mock.optionsReq.IncludeLatest)
	}
}

func TestScreenUSTurnoverIntersectionRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockScreener{usTurnoverResp: &dto.ScreenUSTurnoverIntersectionResponse{Data: []dto.ScreenedUSTurnoverIntersectionRow{{Underlying: "AAPL", CombinedTurnoverUSD: 123}}}}
	r := NewRouter(
		&mockQuerier{},
		&mockUSStocksQuerier{},
		&mockUSOptionsQuerier{},
		&mockInfra{},
		&mockFeature{},
		nil,
		nil,
		nil,
		mock,
		nil,
		nil,
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/screener/us-underlyings/turnover-intersection?limit=25&lookback_days=30&non_etf_only=true", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if mock.usTurnoverReq.Limit != 25 || mock.usTurnoverReq.LookbackDays != 30 || !mock.usTurnoverReq.NonETFOnly {
		t.Fatalf("unexpected request bind: %+v", mock.usTurnoverReq)
	}
	if mock.usTurnoverReq.IncludeLatest == nil || !*mock.usTurnoverReq.IncludeLatest {
		t.Fatalf("expected turnover intersection include_latest to default true, got %+v", mock.usTurnoverReq.IncludeLatest)
	}
	var resp dto.ScreenUSTurnoverIntersectionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Underlying != "AAPL" {
		t.Fatalf("unexpected response: %+v", resp)
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
