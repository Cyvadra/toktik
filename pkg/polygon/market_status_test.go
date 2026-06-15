package polygon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMarketStatusNow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/marketstatus/now" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_, _ = w.Write([]byte(`{"market":"closed","serverTime":"2026-06-10T14:30:00Z","exchanges":{"nyse":"open","nasdaq":"closed"}}`))
	}))
	defer server.Close()

	client, err := New(Config{APIKey: "test-key", BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	status, err := client.MarketStatusNow(context.Background())
	if err != nil {
		t.Fatalf("MarketStatusNow failed: %v", err)
	}
	if status == nil || !status.IsUSStocksOpen() || status.Exchanges["nyse"] != "open" {
		t.Fatalf("unexpected status: %#v", status)
	}
}
