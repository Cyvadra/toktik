package polygon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/massive-com/client-go/v3/rest"
)

func TestLoadConfigFromEnvRequiresAPIKey(t *testing.T) {
	t.Setenv(EnvMassiveAPIKey, "")
	t.Setenv(EnvPolygonAPIKey, "")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestLoadConfigFromEnvSupportsPolygonAliases(t *testing.T) {
	t.Setenv(EnvMassiveAPIKey, "")
	t.Setenv(EnvPolygonAPIKey, "poly_key")
	t.Setenv(EnvPolygonBaseURL, "http://localhost:9999")
	t.Setenv(EnvPolygonFlatFilesBaseURL, "http://localhost:7777/files")
	t.Setenv(EnvPolygonFlatFilesCacheDir, "/tmp/polygon-cache")
	t.Setenv(EnvPolygonTimeoutSeconds, "15")
	t.Setenv(EnvPolygonTrace, "true")
	t.Setenv(EnvPolygonPagination, "false")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv failed: %v", err)
	}
	if cfg.APIKey != "poly_key" || cfg.BaseURL != "http://localhost:9999" || cfg.FlatFilesBaseURL != "http://localhost:7777/files" || cfg.FlatFilesCacheDir != "/tmp/polygon-cache" || cfg.Timeout.Seconds() != 15 || !cfg.Trace || cfg.Pagination {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestDownloadMinuteAggregatesFlatFiles(t *testing.T) {
	cacheDir := t.TempDir()
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if got := r.Header.Get("Authorization"); got != "Bearer test_massive_key" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		switch r.URL.Path {
		case "/flatfiles/us_stocks_sip/minute_aggs_v1/2026/2026-04-07.csv.gz":
			_, _ = w.Write([]byte("stock-file"))
		case "/flatfiles/us_options_opra/minute_aggs_v1/2026/2026-04-07.csv.gz":
			_, _ = w.Write([]byte("option-file"))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Config{
		APIKey:            "test_massive_key",
		BaseURL:           server.URL,
		FlatFilesBaseURL:  server.URL + "/flatfiles",
		FlatFilesCacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stockPath, err := client.DownloadStockMinuteAggregates(time.Date(2026, 4, 7, 12, 34, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("DownloadStockMinuteAggregates failed: %v", err)
	}
	optionPath, err := client.DownloadOptionMinuteAggregates(time.Date(2026, 4, 7, 9, 30, 0, 0, time.FixedZone("EST", -5*3600)), false)
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
	if !strings.HasSuffix(stockPath, filepath.Join("us_stocks_sip", "minute_aggs_v1", "2026", "2026-04-07.csv.gz")) {
		t.Fatalf("unexpected stock cache path: %s", stockPath)
	}
	if !strings.HasSuffix(optionPath, filepath.Join("us_options_opra", "minute_aggs_v1", "2026", "2026-04-07.csv.gz")) {
		t.Fatalf("unexpected option cache path: %s", optionPath)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 download requests, got %d", len(requests))
	}

	stockPathAgain, err := client.DownloadStockMinuteAggregates(time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("DownloadStockMinuteAggregates cache hit failed: %v", err)
	}
	if stockPathAgain != stockPath {
		t.Fatalf("unexpected cached stock path: %s", stockPathAgain)
	}
	if len(requests) != 2 {
		t.Fatalf("expected no extra request on cache hit, got %d", len(requests))
	}

	if _, err := client.DownloadStockMinuteAggregates(time.Time{}, false); err == nil {
		t.Fatal("expected zero date error")
	}

	missingCacheClient, err := New(Config{APIKey: "test_massive_key", BaseURL: server.URL, FlatFilesBaseURL: server.URL + "/flatfiles"})
	if err != nil {
		t.Fatalf("New missing cache client failed: %v", err)
	}
	if _, err := missingCacheClient.DownloadStockMinuteAggregates(time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false); err == nil {
		t.Fatal("expected missing cache directory error")
	}
}

func TestDownloadMinuteAggregatesFlatFilesNotFound(t *testing.T) {
	cacheDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(Config{
		APIKey:            "test_massive_key",
		BaseURL:           server.URL,
		FlatFilesBaseURL:  server.URL + "/flatfiles",
		FlatFilesCacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = client.DownloadStockMinuteAggregates(time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), true)
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

	t.Setenv(EnvMassiveAPIKey, "test_massive_key")
	t.Setenv(EnvMassiveBaseURL, server.URL)
	t.Setenv(EnvMassivePagination, "true")

	client, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv failed: %v", err)
	}

	snapshot, err := client.StockSnapshot("aapl")
	if err != nil || snapshot == nil || snapshot.Ticker != "AAPL" || snapshot.LastTrade == nil || snapshot.LastTrade.Price != 197.12 {
		t.Fatalf("StockSnapshot failed: snapshot=%#v err=%v", snapshot, err)
	}

	stockBars, err := client.StockAggregates(AggregateRequest{Ticker: "AAPL", Multiplier: 1, Timespan: "minute", From: "2025-11-03", To: "2025-11-28", Adjusted: rest.Ptr(true), Sort: "asc", Limit: 2})
	if err != nil || len(stockBars) != 2 || stockBars[1].Close != 191.2 {
		t.Fatalf("StockAggregates failed: bars=%#v err=%v", stockBars, err)
	}

	stockQuotes, err := client.StockQuotes("AAPL", QuoteRequest{Limit: 1, Order: "asc", Sort: "timestamp"})
	if err != nil || len(stockQuotes) != 2 || stockQuotes[1].SequenceNumber != 11 {
		t.Fatalf("StockQuotes failed: quotes=%#v err=%v", stockQuotes, err)
	}

	stockTrades, err := client.StockTrades("AAPL", TradeRequest{Limit: 1, Order: "asc", Sort: "timestamp"})
	if err != nil || len(stockTrades) != 2 || stockTrades[1].ID != "trade-2" {
		t.Fatalf("StockTrades failed: trades=%#v err=%v", stockTrades, err)
	}

	contract, err := client.OptionContract("o:spy251219c00650000")
	if err != nil || contract == nil || contract.Ticker != "O:SPY251219C00650000" || contract.StrikePrice != 650 {
		t.Fatalf("OptionContract failed: contract=%#v err=%v", contract, err)
	}

	chain, err := client.OptionChain(OptionChainRequest{Underlying: "SPY", ExpirationDate: "2025-12-19", ContractType: "call", Limit: 1})
	if err != nil || len(chain) != 2 || chain[0].Contract.Ticker != "O:SPY251219C00650000" || chain[1].Contract.Ticker != "O:SPY251219C00655000" {
		t.Fatalf("OptionChain failed: chain=%#v err=%v", chain, err)
	}

	optionBars, err := client.OptionAggregates(AggregateRequest{Ticker: "O:SPY251219C00650000", Multiplier: 1, Timespan: "minute", From: "2025-11-03", To: "2025-11-28", Adjusted: rest.Ptr(true)})
	if err != nil || len(optionBars) != 1 || optionBars[0].Close != 11.2 {
		t.Fatalf("OptionAggregates failed: bars=%#v err=%v", optionBars, err)
	}

	optionQuotes, err := client.OptionQuotes("O:SPY251219C00650000", QuoteRequest{Limit: 1})
	if err != nil || len(optionQuotes) != 1 || optionQuotes[0].SequenceNumber != 31 {
		t.Fatalf("OptionQuotes failed: quotes=%#v err=%v", optionQuotes, err)
	}

	optionTrades, err := client.OptionTrades("O:SPY251219C00650000", TradeRequest{Limit: 1})
	if err != nil || len(optionTrades) != 1 || optionTrades[0].Price != 11.15 {
		t.Fatalf("OptionTrades failed: trades=%#v err=%v", optionTrades, err)
	}

	if len(requests) < 8 {
		t.Fatalf("expected requests to be recorded, got %v", requests)
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
