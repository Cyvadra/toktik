package tigerapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tigerconfig "github.com/tigerfintech/openapi-go-sdk/config"
)

type recordedRequest struct {
	Method     string
	BizContent map[string]any
}

func TestLoadConfigFromEnvRequiresVariables(t *testing.T) {
	t.Setenv(EnvTigerID, "")
	t.Setenv(EnvPrivateKey, "")
	t.Setenv(EnvAccount, "")
	t.Setenv(EnvLicense, "")
	t.Setenv(EnvEnvironment, "")

	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected missing environment variables error")
	}
}

func TestLoadConfigFromEnvLoadsTokenFromDefaultFile(t *testing.T) {
	t.Setenv(EnvTigerID, "test_tiger_id")
	t.Setenv(EnvPrivateKey, mustGeneratePrivateKeyPEM(t))
	t.Setenv(EnvAccount, "test_account")
	t.Setenv(EnvLicense, "TBNZ")
	t.Setenv(EnvEnvironment, string(EnvironmentProd))
	t.Setenv(EnvToken, "")
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, defaultTokenFile), []byte("token=file_token_123\n"), 0644); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv failed: %v", err)
	}
	if cfg.Token != "file_token_123" {
		t.Fatalf("expected token from default file, got %q", cfg.Token)
	}
}

func TestLoadConfigFromEnvLoadsTokenFromExplicitFile(t *testing.T) {
	t.Setenv(EnvTigerID, "test_tiger_id")
	t.Setenv(EnvPrivateKey, mustGeneratePrivateKeyPEM(t))
	t.Setenv(EnvAccount, "test_account")
	t.Setenv(EnvLicense, "TBNZ")
	t.Setenv(EnvEnvironment, string(EnvironmentProd))
	t.Setenv(EnvToken, "")
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "custom_token.properties")
	t.Setenv(EnvTokenFile, tokenPath)
	if err := os.WriteFile(tokenPath, []byte("token=explicit_token_456\n"), 0644); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv failed: %v", err)
	}
	if cfg.Token != "explicit_token_456" {
		t.Fatalf("expected token from explicit file, got %q", cfg.Token)
	}
}

func TestNewFromEnvAndQueries(t *testing.T) {
	testKey := mustGeneratePrivateKeyPEM(t)

	responses := map[string]any{
		"grab_quote_permission": []map[string]any{{"name": "usOptionQuote", "expireAt": -1}},
		"market_state":          []map[string]any{{"market": "US", "status": "Trading"}},
		"quote_real_time":       []map[string]any{{"symbol": "AAPL", "latestPrice": 197.12}},
		"kline":                 []map[string]any{{"symbol": "AAPL", "items": []map[string]any{{"time": 1712534400000, "open": 190.0, "high": 198.0, "low": 189.5, "close": 197.12, "volume": 1000.0}}}},
		"timeline":              []map[string]any{{"symbol": "AAPL", "time": 1712534400000, "price": 197.12, "avgPrice": 196.8, "volume": 1000.0}},
		"trade_tick":            []map[string]any{{"symbol": "AAPL", "time": 1712534400000, "price": 197.12, "size": 10.0, "direction": "BUY"}},
		"quote_depth":           map[string]any{"symbol": "AAPL", "bids": [][]float64{{197.1, 100}}, "asks": [][]float64{{197.2, 200}}},
		"option_expiration":     []map[string]any{{"symbol": "AAPL", "dates": []string{"2026-04-17", "2026-05-15"}}},
		"option_chain":          []map[string]any{{"identifier": "AAPL 260417C00200000", "symbol": "AAPL", "expiry": "2026-04-17", "strike": 200.0, "putCall": "CALL"}},
		"option_brief":          []map[string]any{{"symbol": "AAPL", "identifier": "AAPL 260417C00200000", "latest_price": 12.4, "bid_price": 12.3, "ask_price": 12.5, "volume": 10, "open_interest": 20, "delta": 0.51}},
		"option_kline":          []map[string]any{{"symbol": "AAPL 260417C00200000", "items": []map[string]any{{"time": 1712534400000, "open": 10.0, "high": 12.5, "low": 9.8, "close": 12.4, "volume": 123.0}}}},
		"custom_history":        []map[string]any{{"symbol": "AAPL", "time": 1712534400000, "close": 197.12}},
	}

	var (
		mu      sync.Mutex
		records []recordedRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var envelope map[string]string
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode request envelope: %v", err)
		}

		var bizContent map[string]any
		if err := json.Unmarshal([]byte(envelope["biz_content"]), &bizContent); err != nil {
			t.Fatalf("decode biz_content: %v", err)
		}

		mu.Lock()
		records = append(records, recordedRequest{Method: envelope["method"], BizContent: bizContent})
		mu.Unlock()

		payload := map[string]any{
			"code":      0,
			"message":   "success",
			"data":      responses[envelope["method"]],
			"timestamp": 1712534400,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	t.Setenv(EnvTigerID, "test_tiger_id")
	t.Setenv(EnvPrivateKey, testKey)
	t.Setenv(EnvAccount, "test_account")
	t.Setenv(EnvLicense, "TBNZ")
	t.Setenv(EnvEnvironment, string(EnvironmentSandbox))
	t.Setenv(EnvEnableDynamicDomain, "false")
	t.Setenv(EnvServerURL, server.URL)

	client, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv failed: %v", err)
	}

	if client.SDKConfig().ServerURL != server.URL {
		t.Fatalf("expected overridden server URL %q, got %q", server.URL, client.SDKConfig().ServerURL)
	}
	if !client.SDKConfig().SandboxDebug {
		t.Fatal("expected sandbox mode from environment")
	}

	states, err := client.USMarketState()
	if err != nil || len(states) != 1 || states[0].Status != "Trading" {
		t.Fatalf("USMarketState failed: %#v, err=%v", states, err)
	}

	quotes, err := client.StockQuotes([]string{"aapl", "AAPL"})
	if err != nil || len(quotes) != 1 || quotes[0].Symbol != "AAPL" {
		t.Fatalf("StockQuotes failed: %#v, err=%v", quotes, err)
	}

	stockBars, err := client.StockKlines(StockKlineRequest{Symbol: "AAPL", Period: "day"})
	if err != nil || len(stockBars) != 1 || stockBars[0].Close != 197.12 {
		t.Fatalf("StockKlines failed: %#v, err=%v", stockBars, err)
	}

	timeline, err := client.StockTimeline([]string{"AAPL"})
	if err != nil || len(timeline) != 1 {
		t.Fatalf("StockTimeline failed: %#v, err=%v", timeline, err)
	}

	ticks, err := client.StockTradeTicks([]string{"AAPL"})
	if err != nil || len(ticks) != 1 || ticks[0].Direction != "BUY" {
		t.Fatalf("StockTradeTicks failed: %#v, err=%v", ticks, err)
	}

	depth, err := client.StockDepth("AAPL")
	if err != nil || depth == nil || depth.Symbol != "AAPL" {
		t.Fatalf("StockDepth failed: %#v, err=%v", depth, err)
	}

	expirations, err := client.OptionExpirations("AAPL")
	if err != nil || len(expirations) != 2 {
		t.Fatalf("OptionExpirations failed: %#v, err=%v", expirations, err)
	}

	chain, err := client.OptionChain("AAPL", "2026-04-17")
	if err != nil || len(chain) != 1 || chain[0].Identifier == "" {
		t.Fatalf("OptionChain failed: %#v, err=%v", chain, err)
	}

	optionQuotes, err := client.OptionQuotes([]string{"AAPL 260417C00200000"})
	if err != nil || len(optionQuotes) != 1 || optionQuotes[0].Delta != 0.51 {
		t.Fatalf("OptionQuotes failed: %#v, err=%v", optionQuotes, err)
	}

	optionBars, err := client.OptionKlines(OptionKlineRequest{Identifier: "AAPL 260417C00200000", Period: "day"})
	if err != nil || len(optionBars) != 1 || optionBars[0].Close != 12.4 {
		t.Fatalf("OptionKlines failed: %#v, err=%v", optionBars, err)
	}

	var rawBars []KlineBar
	if err := client.Execute("custom_history", map[string]any{"symbol": "AAPL", "period": "day"}, &rawBars); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(rawBars) != 1 || rawBars[0].Symbol != "AAPL" {
		t.Fatalf("unexpected Execute payload: %#v", rawBars)
	}

	rawResponse, err := client.ExecuteRawResponse("custom_history", map[string]any{"symbol": "AAPL"})
	if err != nil {
		t.Fatalf("ExecuteRawResponse failed: %v", err)
	}
	if !strings.Contains(rawResponse, "197.12") {
		t.Fatalf("unexpected raw response: %s", rawResponse)
	}

	mu.Lock()
	defer mu.Unlock()
	assertRecordedMethod(t, records, "market_state", map[string]any{"market": "US"})
	assertRecordedMethod(t, records, "quote_real_time", map[string]any{"symbols": []any{"AAPL"}})
	assertRecordedMethod(t, records, "kline", map[string]any{"symbols": []any{"AAPL"}, "period": "day"})
	assertRecordedMethod(t, records, "option_expiration", map[string]any{"symbols": []any{"AAPL"}})
	assertRecordedMethod(t, records, "option_chain", map[string]any{
		"option_basic":       []any{map[string]any{"symbol": "AAPL", "expiry": float64(1776398400000)}},
		"return_greek_value": false,
	})
	assertRecordedMethod(t, records, "option_brief", map[string]any{
		"option_basic": []any{map[string]any{
			"symbol": "AAPL",
			"expiry": float64(1776398400000),
			"right":  "CALL",
			"strike": 200,
		}},
	})
	assertRecordedMethod(t, records, "option_kline", map[string]any{
		"option_query": []any{map[string]any{
			"symbol":     "AAPL",
			"expiry":     float64(1776398400000),
			"right":      "CALL",
			"strike":     200,
			"period":     "day",
			"begin_time": -1,
			"end_time":   float64(4070880000000),
		}},
	})
	assertRecordedMethod(t, records, "custom_history", map[string]any{"symbol": "AAPL"})
}

func TestConfigToSDKOptionsIncludesLicense(t *testing.T) {
	cfg := Config{
		TigerID:             "id",
		PrivateKey:          mustGeneratePrivateKeyPEM(t),
		Account:             "account",
		License:             "TBNZ",
		Environment:         EnvironmentProd,
		EnableDynamicDomain: false,
	}
	sdkCfg, err := tigerconfig.NewClientConfig(cfg.toSDKOptions()...)
	if err != nil {
		t.Fatalf("NewClientConfig failed: %v", err)
	}
	if sdkCfg.License != "TBNZ" {
		t.Fatalf("expected license to propagate, got %q", sdkCfg.License)
	}
}

func TestNewFromEnvCurrentEnvironment(t *testing.T) {
	if os.Getenv(EnvTigerID) == "" {
		t.Skip("Tiger environment variables are not set in the current shell")
	}

	client, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv failed with current environment: %v", err)
	}
	if client.SDKConfig() == nil {
		t.Fatal("expected SDK config to be initialized")
	}
}

func mustGeneratePrivateKeyPEM(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(privateKey)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	return string(pem.EncodeToMemory(block))
}

func assertRecordedMethod(t *testing.T, records []recordedRequest, method string, expected map[string]any) {
	t.Helper()
	for _, item := range records {
		if item.Method != method {
			continue
		}
		for key, value := range expected {
			got, ok := item.BizContent[key]
			if !ok {
				t.Fatalf("method %s missing key %s in biz_content", method, key)
			}
			if canonicalJSON(t, got) != canonicalJSON(t, value) {
				t.Fatalf("method %s key %s mismatch: got=%v want=%v", method, key, got, value)
			}
		}
		return
	}
	t.Fatalf("method %s was not recorded", method)
}

func canonicalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal expected value: %v", err)
	}
	return string(encoded)
}
