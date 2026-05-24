package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

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
	tempDir := t.TempDir()
	tempFile, err := os.CreateTemp(tempDir, "report-*.html")
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

	cfg := config.DefaultRuntime()
	cfg.Paths.ReportsRoot = tempDir
	r := NewRouterFromDeps(Deps{
		Config:        cfg,
		CryptoOptions: &mockQuerier{},
		USStocks:      &mockUSStocksQuerier{},
		USOptions:     &mockUSOptionsQuerier{},
		Infra:         &mockInfra{},
		Features:      &mockFeature{},
		StrategyBacktests: &mockStrategyBacktests{statusResp: &dto.StrategyBacktestRunStatus{
			RunID:     "run-123",
			Status:    "completed",
			Request:   dto.StrategyBacktestRunRequest{Asset: "BTC", From: "2026-01-01", To: "2026-02-01", Capital: 5},
			CreatedAt: time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 7, 8, 0, 1, 0, time.UTC),
			Result:    &dto.StrategyBacktestRunResult{Summaries: []dto.StrategyBacktestSummary{{StrategyName: "demo", HTMLPath: tempFile.Name()}}},
		}},
	})

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
	tempDir := t.TempDir()
	tempFile, err := os.CreateTemp(tempDir, "overview-*.html")
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

	cfg := config.DefaultRuntime()
	cfg.Paths.ReportsRoot = tempDir
	r := NewRouterFromDeps(Deps{
		Config:        cfg,
		CryptoOptions: &mockQuerier{},
		USStocks:      &mockUSStocksQuerier{},
		USOptions:     &mockUSOptionsQuerier{},
		Infra:         &mockInfra{},
		Features:      &mockFeature{},
		StrategyBacktests: &mockStrategyBacktests{statusResp: &dto.StrategyBacktestRunStatus{
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
	})

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
