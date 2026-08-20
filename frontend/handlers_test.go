package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexUsesDefaultBacktestRange(t *testing.T) {
	app := testApp(t, "http://example.test")
	mux := http.NewServeMux()
	app.routes(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `name="from" value="2023-01-01"`) ||
		!strings.Contains(body, `name="to" value="2025-12-31"`) {
		t.Fatalf("unexpected default range: %d %s", response.Code, body)
	}
}

func TestBacktestWorkflowAndReportProxy(t *testing.T) {
	var startCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-API-Key") != "secret" {
			t.Fatalf("missing API key for %s", req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/backtests/validate":
			_, _ = w.Write([]byte(`{"strategies":[{"display_name":"Demo","dsl_params":[{"name":"length","title":"Length","type":"int","default":10}]}]}`))
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/backtests/runs":
			startCalls++
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"run_id":"run-1","status":"queued"}`))
		case req.Method == http.MethodGet && req.URL.Path == "/api/v1/backtests/runs/run-1":
			_, _ = w.Write([]byte(`{"run_id":"run-1","status":"completed","report_url":"/api/v1/backtests/runs/run-1/report"}`))
		case req.Method == http.MethodGet && req.URL.Path == "/api/v1/backtests/runs/run-progress":
			_, _ = w.Write([]byte(`{"run_id":"run-progress","status":"running","progress":{"percent":12.345678,"phase":"prepare","message":"loading data"}}`))
		case req.Method == http.MethodGet && req.URL.Path == "/api/v1/backtests/runs/run-1/report":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body>report ready</body></html>`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer upstream.Close()

	app := testApp(t, upstream.URL)
	mux := http.NewServeMux()
	app.routes(mux)

	form := "strategy_file=demo.toktik&market=us&instrument=auto&asset=spy&symbols=SPY&interval=1d&from=2025-01-01&to=2026-01-01&capital=100000"
	validate := httptest.NewRequest(http.MethodPost, "/validate", strings.NewReader(form))
	validate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	validateResponse := httptest.NewRecorder()
	mux.ServeHTTP(validateResponse, validate)
	if validateResponse.Code != http.StatusOK || !strings.Contains(validateResponse.Body.String(), "Length") {
		t.Fatalf("unexpected validation response: %d %s", validateResponse.Code, validateResponse.Body.String())
	}

	start := httptest.NewRequest(http.MethodPost, "/runs", strings.NewReader(form+"&param.length=20&param_type.length=int"))
	start.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	startResponse := httptest.NewRecorder()
	mux.ServeHTTP(startResponse, start)
	if startCalls != 1 || !strings.Contains(startResponse.Body.String(), "run-1") || !strings.Contains(startResponse.Body.String(), `hx-target="this"`) {
		t.Fatalf("unexpected start response: calls=%d body=%s", startCalls, startResponse.Body.String())
	}

	statusResponse := httptest.NewRecorder()
	mux.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/runs/run-1", nil))
	if !strings.Contains(statusResponse.Body.String(), `src="/runs/run-1/report"`) ||
		!strings.Contains(statusResponse.Body.String(), `href="/runs/run-1/report" target="_blank"`) {
		t.Fatalf("completed status did not embed report: %s", statusResponse.Body.String())
	}

	reportResponse := httptest.NewRecorder()
	mux.ServeHTTP(reportResponse, httptest.NewRequest(http.MethodGet, "/runs/run-1/report", nil))
	if reportResponse.Code != http.StatusOK || reportResponse.Body.String() != `<html><body>report ready</body></html>` || reportResponse.Header().Get("Content-Security-Policy") != reportContentSecurityPolicy {
		t.Fatalf("unexpected report response: %d %s", reportResponse.Code, reportResponse.Body.String())
	}

	unknownResponse := httptest.NewRecorder()
	mux.ServeHTTP(unknownResponse, httptest.NewRequest(http.MethodGet, "/runs/unknown", nil))
	if unknownResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown run status = %d", unknownResponse.Code)
	}

	app.runs["run-progress"] = runRecord{ID: "run-progress", StrategyName: "Progress"}
	progressResponse := httptest.NewRecorder()
	mux.ServeHTTP(progressResponse, httptest.NewRequest(http.MethodGet, "/runs/run-progress", nil))
	if progressResponse.Code != http.StatusOK || !strings.Contains(progressResponse.Body.String(), `aria-valuenow="12"`) || !strings.Contains(progressResponse.Body.String(), `width: 12%">12%`) {
		t.Fatalf("progress did not round percentage: %d %s", progressResponse.Code, progressResponse.Body.String())
	}
}

func TestLocalReportProxyOnlyServesHTMLWithinReportDirectory(t *testing.T) {
	app := testApp(t, "http://example.test")
	if err := os.MkdirAll(filepath.Join(app.reportDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.reportDir, "nested", "report.html"), []byte("<html><body>local report</body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	app.routes(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/reports/nested/report.html", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "local report") || response.Header().Get("Content-Security-Policy") != reportContentSecurityPolicy {
		t.Fatalf("unexpected local report response: %d %s", response.Code, response.Body.String())
	}

	if _, err := safeReportPath(app.reportDir, "../handlers.go"); err == nil {
		t.Fatal("safeReportPath accepted a path outside the report directory")
	}

	for _, path := range []string{"/reports/nested/report.txt"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, response.Code)
		}
	}
}

func testApp(t *testing.T, upstreamURL string) *app {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.toktik"), []byte(`strategy("Demo")`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := newStrategyStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	baseURL, _ := url.Parse(upstreamURL)
	reportDir := filepath.Join(dir, "reports")
	if err := os.MkdirAll(reportDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return newApp(store, newAPIClient(baseURL, "secret"), templates, reportDir)
}
