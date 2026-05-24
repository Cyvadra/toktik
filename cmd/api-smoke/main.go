package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const maxResponseBodyBytes = 8 << 20

type queryParam struct {
	key   string
	value string
}

type smokeCase struct {
	name         string
	suite        string
	method       string
	path         string
	query        []queryParam
	body         string
	contentType  string
	expectStatus int
	validate     func(body any) error
}

type runner struct {
	stdout  io.Writer
	stderr  io.Writer
	client  *http.Client
	baseURL string
	apiKey  string
	verbose bool
	results []caseResult
}

type caseResult struct {
	name     string
	suite    string
	status   string
	code     int
	duration time.Duration
	err      error
	url      string
	body     string
}

func main() {
	app := runner{
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
	os.Exit(app.run(os.Args[1:]))
}

func (r *runner) run(args []string) int {
	fs := flag.NewFlagSet("api-smoke", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	baseURL := fs.String("base-url", "http://127.0.0.1:9010", "Base URL of the running api-server")
	apiKey := fs.String("api-key", "", "Optional API key sent as X-API-Key")
	timeout := fs.Duration("timeout", 15*time.Second, "Per-request timeout")
	suites := fs.String("suite", "", "Comma-separated suite filter, e.g. core,markets")
	only := fs.String("only", "", "Comma-separated case names to run")
	failFast := fs.Bool("fail-fast", false, "Stop after the first failed case")
	listOnly := fs.Bool("list", false, "List available cases and exit")
	verbose := fs.Bool("verbose", false, "Print response snippets for each case")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r.baseURL = strings.TrimRight(strings.TrimSpace(*baseURL), "/")
	r.apiKey = strings.TrimSpace(*apiKey)
	r.verbose = *verbose
	r.client = &http.Client{Timeout: *timeout}

	allCases := defaultCases()
	if *listOnly {
		r.printCaseList(allCases)
		return 0
	}

	selected := filterCases(allCases, splitCSV(*suites), splitCSV(*only))
	if len(selected) == 0 {
		fmt.Fprintln(r.stderr, "no smoke cases selected")
		return 2
	}

	for _, tc := range selected {
		result := r.runCase(tc)
		r.results = append(r.results, result)
		r.printCaseResult(result)
		if result.err != nil && *failFast {
			break
		}
	}

	return r.printSummary()
}

func (r *runner) runCase(tc smokeCase) caseResult {
	caseURL, err := buildCaseURL(r.baseURL, tc)
	if err != nil {
		return caseResult{name: tc.name, suite: tc.suite, status: "FAIL", err: err}
	}

	var requestBody io.Reader
	if tc.body != "" {
		requestBody = strings.NewReader(tc.body)
	}
	req, err := http.NewRequest(tc.method, caseURL, requestBody)
	if err != nil {
		return caseResult{name: tc.name, suite: tc.suite, status: "FAIL", err: err, url: caseURL}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "toktik-api-smoke/1.0")
	if tc.contentType != "" {
		req.Header.Set("Content-Type", tc.contentType)
	}
	if r.apiKey != "" {
		req.Header.Set("X-API-Key", r.apiKey)
	}

	started := time.Now()
	resp, err := r.client.Do(req)
	if err != nil {
		return caseResult{name: tc.name, suite: tc.suite, status: "FAIL", err: err, url: caseURL, duration: time.Since(started)}
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if readErr != nil {
		return caseResult{name: tc.name, suite: tc.suite, status: "FAIL", code: resp.StatusCode, err: fmt.Errorf("read body: %w", readErr), url: caseURL, duration: time.Since(started)}
	}

	result := caseResult{
		name:     tc.name,
		suite:    tc.suite,
		status:   "PASS",
		code:     resp.StatusCode,
		url:      caseURL,
		duration: time.Since(started),
		body:     truncateForLog(string(bodyBytes), 240),
	}

	if resp.StatusCode != tc.expectStatus {
		result.status = "FAIL"
		result.err = fmt.Errorf("unexpected status: got %d want %d", resp.StatusCode, tc.expectStatus)
		return result
	}

	if tc.validate == nil {
		return result
	}

	decoded, err := decodeJSON(bodyBytes)
	if err != nil {
		result.status = "FAIL"
		result.err = err
		return result
	}
	if err := tc.validate(decoded); err != nil {
		result.status = "FAIL"
		result.err = err
	}
	return result
}

func (r *runner) printCaseList(cases []smokeCase) {
	rows := make([]string, 0, len(cases))
	for _, tc := range cases {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s %s", tc.suite, tc.name, tc.method, tc.path))
	}
	sort.Strings(rows)
	for _, row := range rows {
		fmt.Fprintln(r.stdout, row)
	}
}

func (r *runner) printCaseResult(result caseResult) {
	line := fmt.Sprintf("[%s] %s/%s (%d, %s)", result.status, result.suite, result.name, result.code, result.duration.Round(time.Millisecond))
	if result.err != nil {
		line += ": " + result.err.Error()
	}
	fmt.Fprintln(r.stdout, line)
	if r.verbose {
		fmt.Fprintf(r.stdout, "  %s\n", result.url)
		if result.body != "" {
			fmt.Fprintf(r.stdout, "  body: %s\n", result.body)
		}
	}
}

func (r *runner) printSummary() int {
	passed := 0
	failed := 0
	for _, result := range r.results {
		if result.err == nil {
			passed++
			continue
		}
		failed++
	}
	fmt.Fprintf(r.stdout, "summary: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func defaultCases() []smokeCase {
	return []smokeCase{
		{
			name:         "health",
			suite:        "core",
			method:       http.MethodGet,
			path:         "/health",
			expectStatus: http.StatusOK,
			validate: func(body any) error {
				obj, err := requireObject(body)
				if err != nil {
					return err
				}
				return requireStringEquals(obj, "status", "ok")
			},
		},
		{
			name:         "ready",
			suite:        "core",
			method:       http.MethodGet,
			path:         "/ready",
			expectStatus: http.StatusOK,
			validate: func(body any) error {
				obj, err := requireObject(body)
				if err != nil {
					return err
				}
				status, err := requireString(obj, "status")
				if err != nil {
					return err
				}
				switch status {
				case "ok", "ready", "degraded":
					return nil
				default:
					return fmt.Errorf("unexpected ready status %q", status)
				}
			},
		},
		{
			name:         "infra-markets",
			suite:        "core",
			method:       http.MethodGet,
			path:         "/api/v1/infra/markets",
			expectStatus: http.StatusOK,
			validate: func(body any) error {
				obj, err := requireObject(body)
				if err != nil {
					return err
				}
				_, err = requireArrayMinLen(obj, "markets", 1)
				return err
			},
		},
		{
			name:         "infra-datasets",
			suite:        "core",
			method:       http.MethodGet,
			path:         "/api/v1/infra/datasets",
			expectStatus: http.StatusOK,
			validate: func(body any) error {
				obj, err := requireObject(body)
				if err != nil {
					return err
				}
				_, err = requireArrayMinLen(obj, "datasets", 1)
				return err
			},
		},
		{
			name:         "crypto-options-symbols",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/crypto-options/symbols",
			query:        []queryParam{{key: "base_asset", value: "BTC"}, {key: "limit", value: "5"}},
			expectStatus: http.StatusOK,
			validate:     validateCryptoOptionSymbols("BTC"),
		},
		{
			name:         "crypto-options-bars",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/crypto-options/bars",
			query:        []queryParam{{key: "symbol", value: "BTC-28MAR25-100000-C"}, {key: "interval", value: "1h"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-03"}, {key: "limit", value: "2"}},
			expectStatus: http.StatusOK,
			validate:     validateCryptoOptionBars("BTC"),
		},
		{
			name:         "crypto-options-greeks",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/crypto-options/greeks",
			query:        []queryParam{{key: "symbol", value: "BTC-28MAR25-100000-C"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-03"}, {key: "limit", value: "2"}},
			expectStatus: http.StatusOK,
			validate:     validateCryptoOptionGreeks(),
		},
		{
			name:         "crypto-options-chain",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/crypto-options/chain",
			query:        []queryParam{{key: "base_asset", value: "BTC"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-03"}, {key: "interval", value: "1d"}, {key: "limit", value: "1"}},
			expectStatus: http.StatusOK,
			validate:     validateCryptoOptionChain("BTC"),
		},
		{
			name:         "us-stocks-symbols",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/us-stocks/symbols",
			query:        []queryParam{{key: "search", value: "AA"}, {key: "limit", value: "5"}},
			expectStatus: http.StatusOK,
			validate:     validateSymbolData("AA"),
		},
		{
			name:         "us-stocks-bars",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/us-stocks/bars",
			query:        []queryParam{{key: "symbol", value: "AAPL"}, {key: "interval", value: "1d"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-15"}, {key: "limit", value: "5"}},
			expectStatus: http.StatusOK,
			validate:     validateUSStockBars("AAPL"),
		},
		{
			name:         "us-options-symbols",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/us-options/symbols",
			query:        []queryParam{{key: "underlying", value: "SPY"}, {key: "limit", value: "5"}},
			expectStatus: http.StatusOK,
			validate:     validateOptionSymbolData("SPY"),
		},
		{
			name:         "us-options-bars",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/us-options/bars",
			query:        []queryParam{{key: "symbol", value: "O:SPY250102C00400000"}, {key: "interval", value: "1d"}, {key: "from", value: "2025-01-02"}, {key: "to", value: "2025-01-03"}, {key: "limit", value: "2"}},
			expectStatus: http.StatusOK,
			validate:     validateUSOptionBars("O:SPY250102C00400000", "SPY"),
		},
		{
			name:         "us-options-greeks",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/us-options/greeks",
			query:        []queryParam{{key: "symbol", value: "O:SPY250102C00400000"}, {key: "from", value: "2025-01-02"}, {key: "to", value: "2025-01-03"}, {key: "limit", value: "2"}},
			expectStatus: http.StatusOK,
			validate:     validateUSOptionGreeks("O:SPY250102C00400000", "SPY"),
		},
		{
			name:         "us-options-chain",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/us-options/chain",
			query:        []queryParam{{key: "underlying", value: "SPY"}, {key: "from", value: "2025-01-02"}, {key: "to", value: "2025-01-03"}, {key: "interval", value: "1d"}, {key: "limit", value: "1"}},
			expectStatus: http.StatusOK,
			validate:     validateUSOptionChain("SPY"),
		},
		{
			name:         "crypto-spot-symbols",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/crypto-spot/symbols",
			query:        []queryParam{{key: "search", value: "BTC"}, {key: "limit", value: "5"}},
			expectStatus: http.StatusOK,
			validate:     validateSymbolData("BTC"),
		},
		{
			name:         "crypto-spot-bars",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/crypto-spot/bars",
			query:        []queryParam{{key: "symbol", value: "BTC"}, {key: "interval", value: "1d"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-05"}, {key: "limit", value: "2"}},
			expectStatus: http.StatusOK,
			validate:     validateBasicBars("BTC"),
		},
		{
			name:         "forex-symbols",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/forex/symbols",
			query:        []queryParam{{key: "search", value: "EUR"}, {key: "limit", value: "5"}},
			expectStatus: http.StatusOK,
			validate:     validateSymbolData("EUR"),
		},
		{
			name:         "forex-bars",
			suite:        "markets",
			method:       http.MethodGet,
			path:         "/api/v1/markets/forex/bars",
			query:        []queryParam{{key: "symbol", value: "EURUSD"}, {key: "interval", value: "1d"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-05"}, {key: "limit", value: "2"}},
			expectStatus: http.StatusOK,
			validate:     validateBasicBars("EURUSD"),
		},
		{
			name:         "feature-volatility-snapshot",
			suite:        "features",
			method:       http.MethodGet,
			path:         "/api/v1/features/volatility-snapshot",
			query:        []queryParam{{key: "market", value: "us-options"}, {key: "underlying", value: "SPY"}},
			expectStatus: http.StatusOK,
			validate:     validateVolatilitySnapshot("us-options", "SPY"),
		},
		{
			name:         "fundamental-factors",
			suite:        "fundamentals",
			method:       http.MethodGet,
			path:         "/api/v1/fundamentals/factors",
			expectStatus: http.StatusOK,
			validate:     validateFundamentalFactors(),
		},
		{
			name:         "fundamental-series",
			suite:        "fundamentals",
			method:       http.MethodGet,
			path:         "/api/v1/fundamentals/series",
			query:        []queryParam{{key: "market", value: "us-stocks"}, {key: "symbol", value: "AAPL"}, {key: "factor", value: "pb"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-15"}, {key: "mode", value: "filled"}},
			expectStatus: http.StatusOK,
			validate:     validateFundamentalSeries("us-stocks", "AAPL", "pb"),
		},
		{
			name:         "fundamental-snapshot",
			suite:        "fundamentals",
			method:       http.MethodGet,
			path:         "/api/v1/fundamentals/snapshot",
			query:        []queryParam{{key: "market", value: "us-stocks"}, {key: "symbol", value: "AAPL"}, {key: "factor", value: "pb"}, {key: "factor", value: "pe"}},
			expectStatus: http.StatusOK,
			validate:     validateFundamentalSnapshot("us-stocks", "AAPL"),
		},
		{
			name:         "fundamental-panel",
			suite:        "fundamentals",
			method:       http.MethodGet,
			path:         "/api/v1/fundamentals/panel",
			query:        []queryParam{{key: "market", value: "us-stocks"}, {key: "symbol", value: "AAPL"}, {key: "symbol", value: "MSFT"}, {key: "factor", value: "pb"}},
			expectStatus: http.StatusOK,
			validate:     validateFundamentalPanel("us-stocks"),
		},
		{
			name:         "fundamental-freshness",
			suite:        "fundamentals",
			method:       http.MethodGet,
			path:         "/api/v1/fundamentals/freshness",
			query:        []queryParam{{key: "market", value: "us-stocks"}},
			expectStatus: http.StatusOK,
			validate:     validateFundamentalFreshness("us-stocks"),
		},
		{
			name:         "macro-factors",
			suite:        "macro",
			method:       http.MethodGet,
			path:         "/api/v1/macro/factors",
			expectStatus: http.StatusOK,
			validate:     validateMacroFactors(),
		},
		{
			name:         "macro-series",
			suite:        "macro",
			method:       http.MethodGet,
			path:         "/api/v1/macro/series",
			query:        []queryParam{{key: "dataset", value: "gurufocus-shiller"}, {key: "factor", value: "pe10"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-03-01"}, {key: "interval", value: "event"}, {key: "limit", value: "5"}},
			expectStatus: http.StatusOK,
			validate:     validateMacroSeries("gurufocus-shiller", "pe10"),
		},
		{
			name:         "factor-catalog",
			suite:        "factors",
			method:       http.MethodGet,
			path:         "/api/v1/factors",
			expectStatus: http.StatusOK,
			validate:     validateFactorCatalog(),
		},
		{
			name:         "factor-bars",
			suite:        "factors",
			method:       http.MethodGet,
			path:         "/api/v1/factors/bars",
			query:        []queryParam{{key: "name", value: "dvol"}, {key: "symbol", value: "BTC"}, {key: "window", value: "1d"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-10"}, {key: "limit", value: "3"}},
			expectStatus: http.StatusOK,
			validate:     validateFactorBars("BTC"),
		},
		{
			name:         "calendar-economic",
			suite:        "calendar",
			method:       http.MethodGet,
			path:         "/api/v1/calendar/economic",
			expectStatus: http.StatusOK,
			validate:     validateCalendarData(),
		},
		{
			name:         "calendar-stocks",
			suite:        "calendar",
			method:       http.MethodPost,
			path:         "/api/v1/calendar/stocks",
			body:         `{"symbols":["AAPL","MSFT"]}`,
			contentType:  "application/json",
			expectStatus: http.StatusOK,
			validate:     validateStockCalendar(),
		},
		{
			name:         "browser-presets",
			suite:        "browser",
			method:       http.MethodGet,
			path:         "/api/v1/browser/presets",
			expectStatus: http.StatusOK,
			validate:     validateBrowserPresets(),
		},
		{
			name:         "browser-schema",
			suite:        "browser",
			method:       http.MethodGet,
			path:         "/api/v1/browser/datasets/feature-volatility-snapshots/schema",
			expectStatus: http.StatusOK,
			validate:     validateBrowserSchema("feature-volatility-snapshots"),
		},
		{
			name:         "browser-preview",
			suite:        "browser",
			method:       http.MethodGet,
			path:         "/api/v1/browser/datasets/feature-volatility-snapshots/preview",
			query:        []queryParam{{key: "underlying", value: "SPY"}, {key: "limit", value: "2"}},
			expectStatus: http.StatusOK,
			validate:     validateBrowserPreview("feature-volatility-snapshots"),
		},
		{
			name:         "browser-coverage",
			suite:        "browser",
			method:       http.MethodGet,
			path:         "/api/v1/browser/datasets/feature-volatility-snapshots/coverage",
			query:        []queryParam{{key: "underlying", value: "SPY"}},
			expectStatus: http.StatusOK,
			validate:     validateBrowserCoverage("feature-volatility-snapshots"),
		},
		{
			name:         "browser-field-profile",
			suite:        "browser",
			method:       http.MethodGet,
			path:         "/api/v1/browser/datasets/feature-volatility-snapshots/field-profile",
			query:        []queryParam{{key: "field", value: "hv20"}},
			expectStatus: http.StatusOK,
			validate:     validateBrowserFieldProfile("feature-volatility-snapshots", "hv20"),
		},
		{
			name:         "browser-valid-count",
			suite:        "browser",
			method:       http.MethodGet,
			path:         "/api/v1/browser/datasets/feature-volatility-snapshots/valid-count",
			expectStatus: http.StatusOK,
			validate:     validateBrowserValidCount("feature-volatility-snapshots"),
		},
		{
			name:         "browser-symbols",
			suite:        "browser",
			method:       http.MethodGet,
			path:         "/api/v1/browser/datasets/feature-volatility-snapshots/symbols",
			query:        []queryParam{{key: "search", value: "SPY"}, {key: "limit", value: "3"}},
			expectStatus: http.StatusOK,
			validate:     validateBrowserSymbols("feature-volatility-snapshots"),
		},
		{
			name:         "validation-us-stocks-bars-missing-symbol",
			suite:        "validation",
			method:       http.MethodGet,
			path:         "/api/v1/markets/us-stocks/bars",
			query:        []queryParam{{key: "interval", value: "1d"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-15"}},
			expectStatus: http.StatusBadRequest,
			validate:     validateErrorContains("Symbol"),
		},
		{
			name:         "validation-fundamental-series-missing-factor",
			suite:        "validation",
			method:       http.MethodGet,
			path:         "/api/v1/fundamentals/series",
			query:        []queryParam{{key: "market", value: "us-stocks"}, {key: "symbol", value: "AAPL"}, {key: "from", value: "2025-01-01"}, {key: "to", value: "2025-01-15"}},
			expectStatus: http.StatusBadRequest,
			validate:     validateErrorContains("Factor"),
		},
		{
			name:         "validation-calendar-stocks-empty-body",
			suite:        "validation",
			method:       http.MethodPost,
			path:         "/api/v1/calendar/stocks",
			body:         `{}`,
			contentType:  "application/json",
			expectStatus: http.StatusBadRequest,
			validate:     validateErrorContains("Symbols"),
		},
	}
}

func filterCases(cases []smokeCase, suites, only []string) []smokeCase {
	suiteSet := make(map[string]struct{}, len(suites))
	for _, suite := range suites {
		suiteSet[suite] = struct{}{}
	}
	onlySet := make(map[string]struct{}, len(only))
	for _, name := range only {
		onlySet[name] = struct{}{}
	}

	selected := make([]smokeCase, 0, len(cases))
	for _, tc := range cases {
		if len(suiteSet) > 0 {
			if _, ok := suiteSet[tc.suite]; !ok {
				continue
			}
		}
		if len(onlySet) > 0 {
			if _, ok := onlySet[tc.name]; !ok {
				continue
			}
		}
		selected = append(selected, tc)
	}
	return selected
}

func buildCaseURL(baseURL string, tc smokeCase) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	ref, err := url.Parse(tc.path)
	if err != nil {
		return "", fmt.Errorf("parse path %q: %w", tc.path, err)
	}
	finalURL := parsed.ResolveReference(ref)
	q := finalURL.Query()
	for _, param := range tc.query {
		q.Add(param.key, param.value)
	}
	finalURL.RawQuery = q.Encode()
	return finalURL.String(), nil
}

func decodeJSON(body []byte) (any, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode json body: %w", err)
	}
	return decoded, nil
}

func requireObject(body any) (map[string]any, error) {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", body)
	}
	return obj, nil
}

func requireArrayMinLen(obj map[string]any, key string, min int) ([]any, error) {
	value, ok := obj[key]
	if !ok {
		return nil, fmt.Errorf("missing field %q", key)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("field %q is %T, want array", key, value)
	}
	if len(items) < min {
		return nil, fmt.Errorf("field %q has %d items, want at least %d", key, len(items), min)
	}
	return items, nil
}

func requireString(obj map[string]any, key string) (string, error) {
	value, ok := obj[key]
	if !ok {
		return "", fmt.Errorf("missing field %q", key)
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("field %q is %v, want non-empty string", key, value)
	}
	return text, nil
}

func requireStringEquals(obj map[string]any, key, want string) error {
	got, err := requireString(obj, key)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("field %q = %q, want %q", key, got, want)
	}
	return nil
}

func requireNumber(obj map[string]any, key string) error {
	value, ok := obj[key]
	if !ok {
		return fmt.Errorf("missing field %q", key)
	}
	if _, ok := value.(float64); !ok {
		return fmt.Errorf("field %q is %T, want number", key, value)
	}
	return nil
}

func requireArrayObject(items []any, index int) (map[string]any, error) {
	if len(items) <= index {
		return nil, fmt.Errorf("array has %d items, index %d out of range", len(items), index)
	}
	obj, ok := items[index].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("array item %d is %T, want object", index, items[index])
	}
	return obj, nil
}

func firstDataObject(body any) (map[string]any, error) {
	obj, err := requireObject(body)
	if err != nil {
		return nil, err
	}
	items, err := requireArrayMinLen(obj, "data", 1)
	if err != nil {
		return nil, err
	}
	return requireArrayObject(items, 0)
}

func requireArrayFieldObject(obj map[string]any, key string, index int) (map[string]any, error) {
	items, err := requireArrayMinLen(obj, key, index+1)
	if err != nil {
		return nil, err
	}
	return requireArrayObject(items, index)
}

func requireNumberAtLeast(obj map[string]any, key string, min float64) error {
	value, ok := obj[key]
	if !ok {
		return fmt.Errorf("missing field %q", key)
	}
	number, ok := value.(float64)
	if !ok {
		return fmt.Errorf("field %q is %T, want number", key, value)
	}
	if number < min {
		return fmt.Errorf("field %q = %v, want at least %v", key, number, min)
	}
	return nil
}

func validateSymbolData(search string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		symbol, err := requireString(first, "symbol")
		if err != nil {
			return err
		}
		if !strings.Contains(strings.ToUpper(symbol), strings.ToUpper(search)) {
			return fmt.Errorf("symbol %q does not contain search token %q", symbol, search)
		}
		return nil
	}
}

func validateCryptoOptionSymbols(baseAsset string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "base_asset", baseAsset); err != nil {
			return err
		}
		_, err = requireString(first, "symbol")
		return err
	}
}

func validateUSStockBars(symbol string) func(body any) error {
	return validateBasicBars(symbol)
}

func validateBasicBars(symbol string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "symbol", symbol); err != nil {
			return err
		}
		if _, err := requireString(first, "timestamp"); err != nil {
			return err
		}
		if err := requireNumber(first, "open"); err != nil {
			return err
		}
		if err := requireNumber(first, "close"); err != nil {
			return err
		}
		return nil
	}
}

func validateCryptoOptionBars(baseAsset string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "base_asset", baseAsset); err != nil {
			return err
		}
		if err := requireNumber(first, "mark_close"); err != nil {
			return err
		}
		return requireNumber(first, "implied_volatility")
	}
}

func validateCryptoOptionGreeks() func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "timestamp"); err != nil {
			return err
		}
		if err := requireNumber(first, "delta"); err != nil {
			return err
		}
		return requireNumber(first, "implied_volatility")
	}
}

func validateCryptoOptionChain(baseAsset string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "base_asset", baseAsset); err != nil {
			return err
		}
		contracts, err := requireArrayMinLen(first, "contracts", 1)
		if err != nil {
			return err
		}
		contract, err := requireArrayObject(contracts, 0)
		if err != nil {
			return err
		}
		if _, err := requireString(contract, "symbol"); err != nil {
			return err
		}
		return requireNumber(contract, "strike")
	}
}

func validateOptionSymbolData(underlying string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "underlying", underlying); err != nil {
			return err
		}
		_, err = requireString(first, "symbol")
		return err
	}
}

func validateUSOptionBars(symbol, underlying string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "symbol", symbol); err != nil {
			return err
		}
		if err := requireStringEquals(first, "underlying", underlying); err != nil {
			return err
		}
		if err := requireNumber(first, "close"); err != nil {
			return err
		}
		return requireNumber(first, "volume")
	}
}

func validateUSOptionGreeks(symbol, underlying string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "symbol", symbol); err != nil {
			return err
		}
		if err := requireStringEquals(first, "underlying", underlying); err != nil {
			return err
		}
		if err := requireNumber(first, "delta"); err != nil {
			return err
		}
		return requireNumber(first, "underlying_close")
	}
}

func validateUSOptionChain(underlying string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "underlying", underlying); err != nil {
			return err
		}
		contracts, err := requireArrayMinLen(first, "contracts", 1)
		if err != nil {
			return err
		}
		contract, err := requireArrayObject(contracts, 0)
		if err != nil {
			return err
		}
		if _, err := requireString(contract, "symbol"); err != nil {
			return err
		}
		return requireNumber(contract, "strike")
	}
}

func validateVolatilitySnapshot(market, underlying string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(obj, "market", market); err != nil {
			return err
		}
		if err := requireStringEquals(obj, "underlying", underlying); err != nil {
			return err
		}
		if err := requireNumber(obj, "price_observations"); err != nil {
			return err
		}
		if err := requireNumber(obj, "iv_observations"); err != nil {
			return err
		}
		return nil
	}
}

func validateFundamentalFactors() func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "factor_code"); err != nil {
			return err
		}
		_, err = requireString(first, "market")
		return err
	}
}

func validateFundamentalSeries(market, symbol, factor string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(obj, "market", market); err != nil {
			return err
		}
		if err := requireStringEquals(obj, "symbol", symbol); err != nil {
			return err
		}
		if err := requireStringEquals(obj, "factor", factor); err != nil {
			return err
		}
		first, err := requireArrayFieldObject(obj, "data", 0)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "event_ts"); err != nil {
			return err
		}
		if _, err := requireString(first, "known_at"); err != nil {
			return err
		}
		return requireNumber(first, "value")
	}
}

func validateFundamentalSnapshot(market, symbol string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(obj, "market", market); err != nil {
			return err
		}
		if err := requireStringEquals(obj, "symbol", symbol); err != nil {
			return err
		}
		first, err := requireArrayFieldObject(obj, "data", 0)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "factor"); err != nil {
			return err
		}
		return requireNumber(first, "value")
	}
}

func validateFundamentalPanel(market string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(obj, "market", market); err != nil {
			return err
		}
		first, err := requireArrayFieldObject(obj, "data", 0)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "symbol"); err != nil {
			return err
		}
		if _, err := requireString(first, "factor"); err != nil {
			return err
		}
		return requireNumber(first, "value")
	}
}

func validateFundamentalFreshness(market string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "market", market); err != nil {
			return err
		}
		if _, err := requireString(first, "factor"); err != nil {
			return err
		}
		_, err = requireString(first, "last_known_at")
		return err
	}
}

func validateMacroFactors() func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "dataset"); err != nil {
			return err
		}
		_, err = requireString(first, "factor_code")
		return err
	}
}

func validateMacroSeries(dataset, factor string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(obj, "dataset", dataset); err != nil {
			return err
		}
		first, err := requireArrayFieldObject(obj, "data", 0)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "factor", factor); err != nil {
			return err
		}
		return requireNumber(first, "value")
	}
}

func validateFactorCatalog() func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "name"); err != nil {
			return err
		}
		_, err = requireArrayMinLen(first, "source_windows", 1)
		return err
	}
}

func validateFactorBars(symbol string) func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if err := requireStringEquals(first, "symbol", symbol); err != nil {
			return err
		}
		if err := requireNumber(first, "open"); err != nil {
			return err
		}
		return requireNumber(first, "close")
	}
}

func validateCalendarData() func(body any) error {
	return func(body any) error {
		first, err := firstDataObject(body)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "type"); err != nil {
			return err
		}
		if _, err := requireString(first, "date"); err != nil {
			return err
		}
		_, err = requireString(first, "title")
		return err
	}
}

func validateStockCalendar() func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		if _, err := requireArrayMinLen(obj, "symbols", 2); err != nil {
			return err
		}
		first, err := requireArrayFieldObject(obj, "data", 0)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "symbol"); err != nil {
			return err
		}
		_, err = requireString(first, "type")
		return err
	}
}

func validateBrowserPresets() func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		items, err := requireArrayMinLen(obj, "datasets", 1)
		if err != nil {
			return err
		}
		first, err := requireArrayObject(items, 0)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "name"); err != nil {
			return err
		}
		_, err = requireString(first, "relation")
		return err
	}
}

func validateBrowserSchema(dataset string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		datasetObj, err := requireObjectField(obj, "dataset")
		if err != nil {
			return err
		}
		if err := requireStringEquals(datasetObj, "name", dataset); err != nil {
			return err
		}
		columns, err := requireArrayMinLen(obj, "columns", 1)
		if err != nil {
			return err
		}
		first, err := requireArrayObject(columns, 0)
		if err != nil {
			return err
		}
		if _, err := requireString(first, "name"); err != nil {
			return err
		}
		_, err = requireString(first, "type")
		return err
	}
}

func validateBrowserPreview(dataset string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		datasetObj, err := requireObjectField(obj, "dataset")
		if err != nil {
			return err
		}
		if err := requireStringEquals(datasetObj, "name", dataset); err != nil {
			return err
		}
		if _, err := requireArrayMinLen(obj, "columns", 1); err != nil {
			return err
		}
		data, err := requireArrayMinLen(obj, "data", 1)
		if err != nil {
			return err
		}
		_, err = requireArrayObject(data, 0)
		return err
	}
}

func validateBrowserCoverage(dataset string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		datasetObj, err := requireObjectField(obj, "dataset")
		if err != nil {
			return err
		}
		if err := requireStringEquals(datasetObj, "name", dataset); err != nil {
			return err
		}
		if err := requireNumberAtLeast(obj, "row_count", 1); err != nil {
			return err
		}
		_, err = requireArrayMinLen(obj, "daily", 1)
		return err
	}
}

func validateBrowserFieldProfile(dataset, field string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		datasetObj, err := requireObjectField(obj, "dataset")
		if err != nil {
			return err
		}
		if err := requireStringEquals(datasetObj, "name", dataset); err != nil {
			return err
		}
		if err := requireStringEquals(obj, "field", field); err != nil {
			return err
		}
		if err := requireNumberAtLeast(obj, "row_count", 1); err != nil {
			return err
		}
		if err := requireNumber(obj, "distinct_count"); err != nil {
			return err
		}
		if _, ok := obj["min"]; !ok {
			return fmt.Errorf("missing field %q", "min")
		}
		if _, ok := obj["max"]; !ok {
			return fmt.Errorf("missing field %q", "max")
		}
		return nil
	}
}

func validateBrowserValidCount(dataset string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		datasetObj, err := requireObjectField(obj, "dataset")
		if err != nil {
			return err
		}
		if err := requireStringEquals(datasetObj, "name", dataset); err != nil {
			return err
		}
		if err := requireNumberAtLeast(obj, "row_count", 1); err != nil {
			return err
		}
		if err := requireNumber(obj, "valid_count"); err != nil {
			return err
		}
		return requireNumber(obj, "invalid_count")
	}
}

func validateBrowserSymbols(dataset string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		datasetObj, err := requireObjectField(obj, "dataset")
		if err != nil {
			return err
		}
		if err := requireStringEquals(datasetObj, "name", dataset); err != nil {
			return err
		}
		fields, err := requireArrayMinLen(obj, "fields", 1)
		if err != nil {
			return err
		}
		firstField, err := requireArrayObject(fields, 0)
		if err != nil {
			return err
		}
		if _, err := requireString(firstField, "field"); err != nil {
			return err
		}
		values, err := requireArrayMinLen(firstField, "values", 1)
		if err != nil {
			return err
		}
		firstValue, err := requireArrayObject(values, 0)
		if err != nil {
			return err
		}
		_, err = requireString(firstValue, "value")
		return err
	}
}

func validateErrorContains(fragment string) func(body any) error {
	return func(body any) error {
		obj, err := requireObject(body)
		if err != nil {
			return err
		}
		message, err := requireString(obj, "error")
		if err != nil {
			return err
		}
		if !strings.Contains(strings.ToLower(message), strings.ToLower(fragment)) {
			return fmt.Errorf("error %q does not contain %q", message, fragment)
		}
		return nil
	}
}

func requireObjectField(obj map[string]any, key string) (map[string]any, error) {
	value, ok := obj[key]
	if !ok {
		return nil, fmt.Errorf("missing field %q", key)
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("field %q is %T, want object", key, value)
	}
	return nested, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func truncateForLog(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
