package polygon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimeconfig "github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/secrets"
	"github.com/massive-com/client-go/v3/rest"
)

func TestLoadConfigFromEnvRequiresRuntimePolygonAPIKey(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "toktik.yaml")
	t.Setenv(runtimeconfig.EnvConfigPath, configPath)

	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestLoadConfigFromEnvReadsRuntimePolygonConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "toktik.yaml")
	content := []byte("polygon:\n" +
		"  api_key: \"yaml_key\"\n" +
		"  base_url: \"http://localhost:9999\"\n" +
		"  flat_files_base_url: \"http://localhost:7777/files\"\n" +
		"  flat_files_tool: \"mc\"\n" +
		"  flat_files_cache_dir: \"/tmp/polygon-cache\"\n" +
		"  flat_files_access_key: \"flat-access\"\n" +
		"  flat_files_secret_key: \"flat-secret\"\n" +
		"  timeout_seconds: 15\n" +
		"  trace: true\n" +
		"  pagination: false\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", configPath, err)
	}
	t.Setenv(runtimeconfig.EnvConfigPath, configPath)
	t.Setenv("POLYGON_API_KEY", "env_key_should_be_ignored")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv failed: %v", err)
	}
	if cfg.APIKey != "yaml_key" || cfg.BaseURL != "http://localhost:9999" || cfg.FlatFilesBaseURL != "http://localhost:7777/files" || cfg.FlatFilesTool != "mc" || cfg.FlatFilesCacheDir != "/tmp/polygon-cache" || cfg.FlatFilesAccessKey != "flat-access" || cfg.FlatFilesSecretKey != "flat-secret" || cfg.Timeout.Seconds() != 15 || !cfg.Trace || cfg.Pagination {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadConfigFromRuntimeReadsSealedSecrets(t *testing.T) {
	mgr, err := secrets.New("8b52f5ad926b946334dac0a6a07b202dc26dbde82d15b75a500240ef147d04f6")
	if err != nil {
		t.Fatalf("secrets.New failed: %v", err)
	}
	t.Cleanup(mgr.Wipe)

	runtimeCfg := runtimeconfig.DefaultRuntime()
	runtimeCfg.Secrets = mgr
	runtimeCfg.Polygon.BaseURL = "https://api.massive.com"
	runtimeCfg.Polygon.FlatFilesBaseURL = "https://files.massive.com/flatfiles"
	runtimeCfg.Polygon.FlatFilesTool = "rclone"
	runtimeCfg.Polygon.FlatFilesCacheDir = "/tmp/polygon-cache"
	runtimeCfg.Polygon.TimeoutSeconds = 15
	runtimeCfg.Polygon.Trace = true
	runtimeCfg.Polygon.Pagination = false
	mgr.Seal("polygon.api_key", "sealed_key")
	mgr.Seal("polygon.flat_files_access_key", "sealed-access")
	mgr.Seal("polygon.flat_files_secret_key", "sealed-secret")

	cfg, err := LoadConfigFromRuntime(runtimeCfg)
	if err != nil {
		t.Fatalf("LoadConfigFromRuntime failed: %v", err)
	}
	if cfg.APIKey != "sealed_key" || cfg.FlatFilesAccessKey != "sealed-access" || cfg.FlatFilesSecretKey != "sealed-secret" {
		t.Fatalf("unexpected sealed config: %#v", cfg)
	}
}

func TestDownloadMinuteAggregatesFlatFiles(t *testing.T) {
	cacheDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "mc.log")
	installFakeCommand(t, "mc", fmt.Sprintf(`#!/bin/sh
set -eu
if [ "${MC_HOST_s3massive:-}" = "" ]; then
	echo missing alias >&2
	exit 2
fi
echo "$*" >> %q
if [ "$1" != "cat" ]; then
	echo unexpected command >&2
	exit 2
fi
case "$2" in
	s3massive/flatfiles/us_stocks_sip/minute_aggs_v1/2026/04/2026-04-07.csv.gz) printf stock-file ;;
	s3massive/flatfiles/us_options_opra/minute_aggs_v1/2026/04/2026-04-07.csv.gz) printf option-file ;;
	*) echo 'The specified key does not exist.' >&2; exit 1 ;;
esac
`, logPath))

	client, err := New(Config{
		APIKey:             "test_massive_key",
		BaseURL:            "https://api.massive.com",
		FlatFilesBaseURL:   "https://files.massive.com/flatfiles",
		FlatFilesTool:      "mc",
		FlatFilesCacheDir:  cacheDir,
		FlatFilesAccessKey: "flat-access",
		FlatFilesSecretKey: "flat-secret",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stockPath, err := client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 7, 12, 34, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("DownloadStockMinuteAggregates failed: %v", err)
	}
	optionPath, err := client.DownloadOptionMinuteAggregates(context.Background(), time.Date(2026, 4, 7, 9, 30, 0, 0, time.FixedZone("EST", -5*3600)), false)
	if err != nil {
		t.Fatalf("DownloadOptionMinuteAggregates failed: %v", err)
	}

	stockBytes, err := os.ReadFile(stockPath)
	if err != nil {
		t.Fatalf("read stock cache file: %v", err)
	}
	optionBytes, err := os.ReadFile(optionPath)
	if err != nil {
		t.Fatalf("read option cache file: %v", err)
	}
	if string(stockBytes) != "stock-file" || string(optionBytes) != "option-file" {
		t.Fatalf("unexpected cached content: stock=%q option=%q", string(stockBytes), string(optionBytes))
	}
	if !strings.HasSuffix(stockPath, filepath.Join("us_stocks_sip", "minute_aggs_v1", "2026", "04", "2026-04-07.csv.gz")) {
		t.Fatalf("unexpected stock cache path: %s", stockPath)
	}
	if !strings.HasSuffix(optionPath, filepath.Join("us_options_opra", "minute_aggs_v1", "2026", "04", "2026-04-07.csv.gz")) {
		t.Fatalf("unexpected option cache path: %s", optionPath)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read mc log: %v", err)
	}
	requests := strings.FieldsFunc(strings.TrimSpace(string(logBytes)), func(r rune) bool { return r == '\n' })
	if len(requests) != 2 {
		t.Fatalf("expected 2 mc copy requests, got %d (%q)", len(requests), string(logBytes))
	}

	stockPathAgain, err := client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("DownloadStockMinuteAggregates cache hit failed: %v", err)
	}
	if stockPathAgain != stockPath {
		t.Fatalf("unexpected cached stock path: %s", stockPathAgain)
	}
	logBytes, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read mc log after cache hit: %v", err)
	}
	requests = strings.FieldsFunc(strings.TrimSpace(string(logBytes)), func(r rune) bool { return r == '\n' })
	if len(requests) != 2 {
		t.Fatalf("expected no extra mc request on cache hit, got %d", len(requests))
	}

	if _, err := client.DownloadStockMinuteAggregates(context.Background(), time.Time{}, false); err == nil {
		t.Fatal("expected zero date error")
	}

	missingCacheClient, err := New(Config{APIKey: "test_massive_key", BaseURL: "https://api.massive.com", FlatFilesBaseURL: "https://files.massive.com/flatfiles", FlatFilesTool: "mc", FlatFilesAccessKey: "flat-access", FlatFilesSecretKey: "flat-secret"})
	if err != nil {
		t.Fatalf("New missing cache client failed: %v", err)
	}
	if _, err := missingCacheClient.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false); err == nil {
		t.Fatal("expected missing cache directory error")
	}
}

func TestDownloadMinuteAggregatesFlatFilesNotFound(t *testing.T) {
	cacheDir := t.TempDir()
	installFakeCommand(t, "mc", "#!/bin/sh\nset -eu\necho 'The specified key does not exist.' >&2\nexit 1\n")

	client, err := New(Config{
		APIKey:             "test_massive_key",
		BaseURL:            "https://api.massive.com",
		FlatFilesBaseURL:   "https://files.massive.com/flatfiles",
		FlatFilesTool:      "mc",
		FlatFilesCacheDir:  cacheDir,
		FlatFilesAccessKey: "flat-access",
		FlatFilesSecretKey: "flat-secret",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), true)
	if err == nil {
		t.Fatal("expected 404 error")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected HTTPStatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", statusErr.StatusCode)
	}
	if !IsHTTPStatus(statusErr, http.StatusNotFound) {
		t.Fatal("expected IsHTTPStatus to match 404")
	}
	if statusErr.URL == "" {
		t.Fatal("expected request URL in status error")
	}
}

func TestDownloadMinuteAggregatesFlatFilesRetriesTransientMCFailure(t *testing.T) {
	cacheDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "mc.log")
	attemptPath := filepath.Join(t.TempDir(), "attempt.txt")
	installFakeCommand(t, "mc", fmt.Sprintf(`#!/bin/sh
set -eu
if [ "${MC_HOST_s3massive:-}" = "" ]; then
	echo missing alias >&2
	exit 2
fi
count=0
if [ -f %q ]; then
	count=$(cat %q)
fi
count=$((count+1))
printf '%%s' "$count" > %q
echo "$*" >> %q
if [ "$1" != "cat" ]; then
	echo unexpected command >&2
	exit 2
fi
if [ "$count" -eq 1 ]; then
	echo 'mc: <ERROR> Unable to read source. Get "https://files.massive.com/flatfiles/?location=": Connection closed by foreign host. Retry again.' >&2
	exit 1
fi
printf stock-file
`, attemptPath, attemptPath, attemptPath, logPath))

	client, err := New(Config{
		APIKey:             "test_massive_key",
		BaseURL:            "https://api.massive.com",
		FlatFilesBaseURL:   "https://files.massive.com/flatfiles",
		FlatFilesTool:      "mc",
		FlatFilesCacheDir:  cacheDir,
		FlatFilesAccessKey: "flat-access",
		FlatFilesSecretKey: "flat-secret",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stockPath, err := client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 7, 12, 34, 0, 0, time.UTC), true)
	if err != nil {
		t.Fatalf("DownloadStockMinuteAggregates failed after retry: %v", err)
	}

	stockBytes, err := os.ReadFile(stockPath)
	if err != nil {
		t.Fatalf("read stock cache file: %v", err)
	}
	if string(stockBytes) != "stock-file" {
		t.Fatalf("unexpected cached content: %q", string(stockBytes))
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read mc log: %v", err)
	}
	requests := strings.FieldsFunc(strings.TrimSpace(string(logBytes)), func(r rune) bool { return r == '\n' })
	if len(requests) != 2 {
		t.Fatalf("expected 2 mc attempts, got %d (%q)", len(requests), string(logBytes))
	}
}

func TestDownloadMinuteAggregatesFlatFilesWithRclone(t *testing.T) {
	cacheDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "rclone.log")
	installFakeCommand(t, "rclone", fmt.Sprintf(`#!/bin/sh
set -eu
if [ "${RCLONE_CONFIG_S3MASSIVE_TYPE:-}" != "s3" ]; then
	echo missing type >&2
	exit 2
fi
if [ "${RCLONE_CONFIG_S3MASSIVE_PROVIDER:-}" != "Other" ]; then
	echo missing provider >&2
	exit 2
fi
if [ "${RCLONE_CONFIG_S3MASSIVE_ACCESS_KEY_ID:-}" != "flat-access" ]; then
	echo missing access key >&2
	exit 2
fi
if [ "${RCLONE_CONFIG_S3MASSIVE_SECRET_ACCESS_KEY:-}" != "flat-secret" ]; then
	echo missing secret key >&2
	exit 2
fi
if [ "${RCLONE_CONFIG_S3MASSIVE_ENDPOINT:-}" != "https://files.massive.com" ]; then
	echo unexpected endpoint >&2
	exit 2
fi
echo "$*" >> %q
if [ "$1" != "cat" ]; then
	echo unexpected command >&2
	exit 2
fi
case "$2" in
	s3massive:flatfiles/us_stocks_sip/minute_aggs_v1/2026/04/2026-04-07.csv.gz) printf stock-file ;;
	*) echo 'object not found' >&2; exit 1 ;;
esac
`, logPath))

	client, err := New(Config{
		APIKey:             "test_massive_key",
		BaseURL:            "https://api.massive.com",
		FlatFilesBaseURL:   "https://files.massive.com/flatfiles",
		FlatFilesTool:      "rclone",
		FlatFilesCacheDir:  cacheDir,
		FlatFilesAccessKey: "flat-access",
		FlatFilesSecretKey: "flat-secret",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stockPath, err := client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 7, 12, 34, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("DownloadStockMinuteAggregates failed: %v", err)
	}

	stockBytes, err := os.ReadFile(stockPath)
	if err != nil {
		t.Fatalf("read stock cache file: %v", err)
	}
	if string(stockBytes) != "stock-file" {
		t.Fatalf("unexpected cached content: %q", string(stockBytes))
	}
	if !strings.HasSuffix(stockPath, filepath.Join("us_stocks_sip", "minute_aggs_v1", "2026", "04", "2026-04-07.csv.gz")) {
		t.Fatalf("unexpected stock cache path: %s", stockPath)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read rclone log: %v", err)
	}
	requests := strings.FieldsFunc(strings.TrimSpace(string(logBytes)), func(r rune) bool { return r == '\n' })
	if len(requests) != 1 {
		t.Fatalf("expected 1 rclone request, got %d (%q)", len(requests), string(logBytes))
	}

	stockPathAgain, err := client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("DownloadStockMinuteAggregates cache hit failed: %v", err)
	}
	if stockPathAgain != stockPath {
		t.Fatalf("unexpected cached stock path: %s", stockPathAgain)
	}
	logBytes, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read rclone log after cache hit: %v", err)
	}
	requests = strings.FieldsFunc(strings.TrimSpace(string(logBytes)), func(r rune) bool { return r == '\n' })
	if len(requests) != 1 {
		t.Fatalf("expected no extra rclone request on cache hit, got %d", len(requests))
	}
}

func TestDownloadMinuteAggregatesFlatFilesNotFoundWithRclone(t *testing.T) {
	cacheDir := t.TempDir()
	installFakeCommand(t, "rclone", "#!/bin/sh\nset -eu\necho 'object not found' >&2\nexit 1\n")

	client, err := New(Config{
		APIKey:             "test_massive_key",
		BaseURL:            "https://api.massive.com",
		FlatFilesBaseURL:   "https://files.massive.com/flatfiles",
		FlatFilesTool:      "rclone",
		FlatFilesCacheDir:  cacheDir,
		FlatFilesAccessKey: "flat-access",
		FlatFilesSecretKey: "flat-secret",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), true)
	if err == nil {
		t.Fatal("expected 404 error")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected HTTPStatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", statusErr.StatusCode)
	}
	if !IsHTTPStatus(statusErr, http.StatusNotFound) {
		t.Fatal("expected IsHTTPStatus to match 404")
	}
}

func TestDownloadMinuteAggregatesFlatFilesMissingObjectWithRcloneEmptyCat(t *testing.T) {
	cacheDir := t.TempDir()
	installFakeCommand(t, "rclone", `#!/bin/sh
set -eu
case "$1" in
	cat)
		exit 0
		;;
	lsjson)
		printf '[]'
		;;
	*)
		echo unexpected command >&2
		exit 2
		;;
esac
`)

	client, err := New(Config{
		APIKey:             "test_massive_key",
		BaseURL:            "https://api.massive.com",
		FlatFilesBaseURL:   "https://files.massive.com/flatfiles",
		FlatFilesTool:      "rclone",
		FlatFilesCacheDir:  cacheDir,
		FlatFilesAccessKey: "flat-access",
		FlatFilesSecretKey: "flat-secret",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = client.DownloadStockMinuteAggregates(context.Background(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true)
	if err == nil {
		t.Fatal("expected 404 error")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected HTTPStatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", statusErr.StatusCode)
	}
}

func installFakeCommand(t *testing.T, name, script string) {
	t.Helper()
	binDir := t.TempDir()
	path := filepath.Join(binDir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake command %s: %v", name, err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestNewFromEnvAndQueries(t *testing.T) {
	var requests []string
	serverURL := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if got := r.Header.Get("Authorization"); got != "Bearer test_massive_key" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}

		switch {
		case r.URL.Path == "/v2/snapshot/locale/us/markets/stocks/tickers/AAPL":
			writeJSON(t, w, map[string]any{
				"status":     "OK",
				"request_id": "snap-1",
				"ticker": map[string]any{
					"ticker":           "AAPL",
					"todaysChange":     1.25,
					"todaysChangePerc": 0.63,
					"updated":          1712534400123,
					"day":              map[string]any{"o": 190.0, "h": 198.0, "l": 189.5, "c": 197.12, "v": 1000.0, "vw": 196.8},
					"min":              map[string]any{"av": 5000, "o": 196.7, "h": 197.2, "l": 196.6, "c": 197.12, "n": 10, "t": 1712534400000, "v": 200.0, "vw": 196.9},
					"prevDay":          map[string]any{"o": 188.0, "h": 191.0, "l": 187.5, "c": 189.1, "v": 900.0, "vw": 189.0},
					"lastQuote":        map[string]any{"P": 197.2, "S": 2, "p": 197.1, "s": 1, "t": 1712534401000},
					"lastTrade":        map[string]any{"c": []int{1, 2}, "i": "tr-1", "p": 197.12, "s": 100, "t": 1712534402000, "x": 4, "ds": "100"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v2/aggs/ticker/AAPL/range/1/minute/2025-11-03/2025-11-28"):
			assertQueryValue(t, r.URL, "sort", "asc")
			assertQueryValue(t, r.URL, "limit", "2")
			writeJSON(t, w, map[string]any{
				"ticker":       "AAPL",
				"adjusted":     true,
				"queryCount":   2,
				"resultsCount": 2,
				"status":       "OK",
				"results": []map[string]any{
					{"o": 190.0, "h": 191.0, "l": 189.5, "c": 190.5, "v": 1000.0, "vw": 190.4, "n": 5, "t": 1712534400000},
					{"o": 190.5, "h": 192.0, "l": 190.0, "c": 191.2, "v": 1200.0, "vw": 191.0, "n": 6, "t": 1712534460000},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v3/quotes/AAPL"):
			if r.URL.Query().Get("limit") == "1" {
				writeJSON(t, w, map[string]any{
					"status": "OK",
					"results": []map[string]any{{
						"ask_exchange":    11,
						"ask_price":       197.2,
						"ask_size":        3,
						"bid_exchange":    12,
						"bid_price":       197.1,
						"bid_size":        4,
						"sequence_number": 10,
						"sip_timestamp":   1712534403000,
					}},
					"next_url": serverURL + "/next/stock-quotes",
				})
				return
			}
			writeJSON(t, w, map[string]any{"status": "OK", "results": []map[string]any{}})
		case r.URL.Path == "/next/stock-quotes":
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": []map[string]any{{
					"ask_exchange":    13,
					"ask_price":       197.3,
					"ask_size":        5,
					"bid_exchange":    14,
					"bid_price":       197.15,
					"bid_size":        2,
					"sequence_number": 11,
					"sip_timestamp":   1712534404000,
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/v3/trades/AAPL"):
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": []map[string]any{{
					"exchange":        4,
					"id":              "trade-1",
					"price":           197.12,
					"sequence_number": 21,
					"sip_timestamp":   1712534405000,
					"size":            100,
					"decimal_size":    "100",
				}},
				"next_url": serverURL + "/next/stock-trades",
			})
		case r.URL.Path == "/next/stock-trades":
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": []map[string]any{{
					"exchange":        4,
					"id":              "trade-2",
					"price":           197.2,
					"sequence_number": 22,
					"sip_timestamp":   1712534406000,
					"size":            50,
					"decimal_size":    "50",
				}},
			})
		case r.URL.Path == "/v3/reference/options/contracts/O:SPY251219C00650000":
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": map[string]any{
					"ticker":              "O:SPY251219C00650000",
					"underlying_ticker":   "SPY",
					"contract_type":       "call",
					"exercise_style":      "american",
					"expiration_date":     "2025-12-19",
					"shares_per_contract": 100,
					"strike_price":        650,
					"primary_exchange":    "OPRA",
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v3/snapshot/options/SPY"):
			assertQueryValue(t, r.URL, "expiration_date", "2025-12-19")
			assertQueryValue(t, r.URL, "contract_type", "call")
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": []map[string]any{{
					"break_even_price":   660,
					"day":                map[string]any{"change": 0.5, "change_percent": 4.2, "open": 10, "high": 12, "low": 9.5, "close": 11.2, "previous_close": 10.7, "volume": 1000, "vwap": 10.9},
					"details":            map[string]any{"contract_type": "call", "exercise_style": "american", "expiration_date": "2025-12-19", "shares_per_contract": 100, "strike_price": 650, "ticker": "O:SPY251219C00650000"},
					"greeks":             map[string]any{"delta": 0.42, "gamma": 0.03, "theta": -0.11, "vega": 0.09},
					"implied_volatility": 0.22,
					"last_quote":         map[string]any{"ask": 11.3, "ask_size": 2, "bid": 11.1, "bid_size": 3, "midpoint": 11.2, "last_updated": 1712534407000},
					"last_trade":         map[string]any{"exchange": 5, "price": 11.15, "sip_timestamp": 1712534408000, "size": 10},
					"open_interest":      250,
					"underlying_asset":   map[string]any{"ticker": "SPY", "price": 612.4, "change_to_break_even": 47.6},
				}},
				"next_url": serverURL + "/next/option-chain",
			})
		case r.URL.Path == "/next/option-chain":
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": []map[string]any{{
					"break_even_price": 665,
					"day":              map[string]any{"change": 0.2, "change_percent": 2.1, "open": 9.5, "high": 10.5, "low": 9.2, "close": 10.1, "previous_close": 9.9, "volume": 500, "vwap": 10.0},
					"details":          map[string]any{"contract_type": "call", "exercise_style": "american", "expiration_date": "2025-12-19", "shares_per_contract": 100, "strike_price": 655, "ticker": "O:SPY251219C00655000"},
					"last_quote":       map[string]any{"ask": 10.2, "ask_size": 1, "bid": 10.0, "bid_size": 1, "midpoint": 10.1},
					"open_interest":    100,
					"underlying_asset": map[string]any{"ticker": "SPY", "price": 612.4, "change_to_break_even": 52.6},
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/v2/aggs/ticker/O:SPY251219C00650000/range/1/minute/2025-11-03/2025-11-28"):
			writeJSON(t, w, map[string]any{
				"ticker":       "O:SPY251219C00650000",
				"adjusted":     true,
				"queryCount":   1,
				"resultsCount": 1,
				"status":       "OK",
				"results":      []map[string]any{{"o": 10, "h": 12, "l": 9.5, "c": 11.2, "v": 300, "vw": 10.8, "n": 4, "t": 1712534400000}},
			})
		case strings.HasPrefix(r.URL.Path, "/v3/quotes/O:SPY251219C00650000"):
			writeJSON(t, w, map[string]any{
				"status":  "OK",
				"results": []map[string]any{{"ask_exchange": 7, "ask_price": 11.3, "ask_size": 2, "bid_exchange": 8, "bid_price": 11.1, "bid_size": 3, "sequence_number": 31, "sip_timestamp": 1712534409000}},
			})
		case strings.HasPrefix(r.URL.Path, "/v3/trades/O:SPY251219C00650000"):
			writeJSON(t, w, map[string]any{
				"status":  "OK",
				"results": []map[string]any{{"exchange": 5, "price": 11.15, "sip_timestamp": 1712534410000, "size": 10}},
			})
		default:
			t.Fatalf("unexpected request path: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	configPath := filepath.Join(t.TempDir(), "toktik.yaml")
	content := []byte("polygon:\n" +
		fmt.Sprintf("  api_key: \"%s\"\n", "test_massive_key") +
		fmt.Sprintf("  base_url: \"%s\"\n", server.URL) +
		"  pagination: true\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", configPath, err)
	}
	t.Setenv(runtimeconfig.EnvConfigPath, configPath)

	client, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv failed: %v", err)
	}
	ctx := context.Background()

	snapshot, err := client.StockSnapshot(ctx, "aapl")
	if err != nil || snapshot == nil || snapshot.Ticker != "AAPL" || snapshot.LastTrade == nil || snapshot.LastTrade.Price != 197.12 {
		t.Fatalf("StockSnapshot failed: snapshot=%#v err=%v", snapshot, err)
	}

	stockBars, err := client.StockAggregates(ctx, AggregateRequest{Ticker: "AAPL", Multiplier: 1, Timespan: "minute", From: "2025-11-03", To: "2025-11-28", Adjusted: rest.Ptr(true), Sort: "asc", Limit: 2})
	if err != nil || len(stockBars) != 2 || stockBars[1].Close != 191.2 {
		t.Fatalf("StockAggregates failed: bars=%#v err=%v", stockBars, err)
	}

	stockQuotes, err := client.StockQuotes(ctx, "AAPL", QuoteRequest{Limit: 1, Order: "asc", Sort: "timestamp"})
	if err != nil || len(stockQuotes) != 2 || stockQuotes[1].SequenceNumber != 11 {
		t.Fatalf("StockQuotes failed: quotes=%#v err=%v", stockQuotes, err)
	}

	stockTrades, err := client.StockTrades(ctx, "AAPL", TradeRequest{Limit: 1, Order: "asc", Sort: "timestamp"})
	if err != nil || len(stockTrades) != 2 || stockTrades[1].ID != "trade-2" {
		t.Fatalf("StockTrades failed: trades=%#v err=%v", stockTrades, err)
	}

	contract, err := client.OptionContract(ctx, "o:spy251219c00650000")
	if err != nil || contract == nil || contract.Ticker != "O:SPY251219C00650000" || contract.StrikePrice != 650 {
		t.Fatalf("OptionContract failed: contract=%#v err=%v", contract, err)
	}

	chain, err := client.OptionChain(ctx, OptionChainRequest{Underlying: "SPY", ExpirationDate: "2025-12-19", ContractType: "call", Limit: 1})
	if err != nil || len(chain) != 2 || chain[0].Contract.Ticker != "O:SPY251219C00650000" || chain[1].Contract.Ticker != "O:SPY251219C00655000" {
		t.Fatalf("OptionChain failed: chain=%#v err=%v", chain, err)
	}

	optionBars, err := client.OptionAggregates(ctx, AggregateRequest{Ticker: "O:SPY251219C00650000", Multiplier: 1, Timespan: "minute", From: "2025-11-03", To: "2025-11-28", Adjusted: rest.Ptr(true)})
	if err != nil || len(optionBars) != 1 || optionBars[0].Close != 11.2 {
		t.Fatalf("OptionAggregates failed: bars=%#v err=%v", optionBars, err)
	}

	optionQuotes, err := client.OptionQuotes(ctx, "O:SPY251219C00650000", QuoteRequest{Limit: 1})
	if err != nil || len(optionQuotes) != 1 || optionQuotes[0].SequenceNumber != 31 {
		t.Fatalf("OptionQuotes failed: quotes=%#v err=%v", optionQuotes, err)
	}

	optionTrades, err := client.OptionTrades(ctx, "O:SPY251219C00650000", TradeRequest{Limit: 1})
	if err != nil || len(optionTrades) != 1 || optionTrades[0].Price != 11.15 {
		t.Fatalf("OptionTrades failed: trades=%#v err=%v", optionTrades, err)
	}

	if len(requests) < 8 {
		t.Fatalf("expected requests to be recorded, got %v", requests)
	}
}

func TestOptionChainReturnsHTTPStatusErrorOnAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test_massive_key" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		if r.URL.Path != "/v3/snapshot/options/EWH" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"ERROR","request_id":"d4e9e56585b307b4e608c9d97a704ef2","error":"Failed to parse query parameters from URL: Key: 'OptionsChainQueryParam.Limit' Error:Field validation for 'Limit' failed on the 'max' tag"}`))
	}))
	defer server.Close()

	client, err := New(Config{APIKey: "test_massive_key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = client.OptionChain(context.Background(), OptionChainRequest{Underlying: "EWH", Limit: 500})
	if err == nil {
		t.Fatal("expected option chain error")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected HTTPStatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", statusErr.StatusCode)
	}
	if !strings.Contains(statusErr.Body, "OptionsChainQueryParam.Limit") {
		t.Fatalf("expected upstream body detail, got %q", statusErr.Body)
	}
}

func TestOptionChainRetriesInitial429(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/v3/snapshot/options/SPY" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"status":"ERROR","error":"rate limited"}`))
			return
		}
		writeJSON(t, w, map[string]any{
			"status": "OK",
			"results": []map[string]any{{
				"break_even_price": 660,
				"day":              map[string]any{"change": 0.5, "change_percent": 4.2, "open": 10, "high": 12, "low": 9.5, "close": 11.2, "previous_close": 10.7, "volume": 1000, "vwap": 10.9},
				"details":          map[string]any{"contract_type": "call", "exercise_style": "american", "expiration_date": "2025-12-19", "shares_per_contract": 100, "strike_price": 650, "ticker": "O:SPY251219C00650000"},
				"last_quote":       map[string]any{"ask": 11.3, "ask_size": 2, "bid": 11.1, "bid_size": 3, "midpoint": 11.2},
				"open_interest":    250,
				"underlying_asset": map[string]any{"ticker": "SPY", "price": 612.4, "change_to_break_even": 47.6},
			}},
		})
	}))
	defer server.Close()

	client, err := New(Config{APIKey: "test_massive_key", BaseURL: server.URL, RESTQPS: 1000, RetryAttempts: 3, RetryBaseDelay: time.Millisecond, RetryMaxDelay: 2 * time.Millisecond})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	chain, err := client.OptionChain(context.Background(), OptionChainRequest{Underlying: "SPY", ExpirationDate: "2025-12-19"})
	if err != nil {
		t.Fatalf("OptionChain returned error: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("expected one contract, got %d", len(chain))
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestOptionChainRetriesPaginated429(t *testing.T) {
	var nextAttempts int
	serverURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/snapshot/options/SPY":
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": []map[string]any{{
					"break_even_price": 660,
					"day":              map[string]any{"change": 0.5, "change_percent": 4.2, "open": 10, "high": 12, "low": 9.5, "close": 11.2, "previous_close": 10.7, "volume": 1000, "vwap": 10.9},
					"details":          map[string]any{"contract_type": "call", "exercise_style": "american", "expiration_date": "2025-12-19", "shares_per_contract": 100, "strike_price": 650, "ticker": "O:SPY251219C00650000"},
					"last_quote":       map[string]any{"ask": 11.3, "ask_size": 2, "bid": 11.1, "bid_size": 3, "midpoint": 11.2},
					"open_interest":    250,
					"underlying_asset": map[string]any{"ticker": "SPY", "price": 612.4, "change_to_break_even": 47.6},
				}},
				"next_url": serverURL + "/next/option-chain",
			})
		case "/next/option-chain":
			nextAttempts++
			if nextAttempts == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"status":"ERROR","error":"rate limited"}`))
				return
			}
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"results": []map[string]any{{
					"break_even_price": 665,
					"day":              map[string]any{"change": 0.2, "change_percent": 2.1, "open": 9.5, "high": 10.5, "low": 9.2, "close": 10.1, "previous_close": 9.9, "volume": 500, "vwap": 10.0},
					"details":          map[string]any{"contract_type": "call", "exercise_style": "american", "expiration_date": "2025-12-19", "shares_per_contract": 100, "strike_price": 655, "ticker": "O:SPY251219C00655000"},
					"last_quote":       map[string]any{"ask": 10.2, "ask_size": 1, "bid": 10.0, "bid_size": 1, "midpoint": 10.1},
					"open_interest":    100,
					"underlying_asset": map[string]any{"ticker": "SPY", "price": 612.4, "change_to_break_even": 52.6},
				}},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client, err := New(Config{APIKey: "test_massive_key", BaseURL: server.URL, Pagination: true, RESTQPS: 1000, RetryAttempts: 3, RetryBaseDelay: time.Millisecond, RetryMaxDelay: 2 * time.Millisecond})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	chain, err := client.OptionChain(context.Background(), OptionChainRequest{Underlying: "SPY", ExpirationDate: "2025-12-19"})
	if err != nil {
		t.Fatalf("OptionChain returned error: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("expected 2 contracts, got %d", len(chain))
	}
	if nextAttempts != 2 {
		t.Fatalf("expected 2 paginated attempts, got %d", nextAttempts)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func assertQueryValue(t *testing.T, urlValue *url.URL, key, want string) {
	t.Helper()
	if got := urlValue.Query().Get(key); got != want {
		t.Fatalf("query[%s] = %q, want %q", key, got, want)
	}
}
