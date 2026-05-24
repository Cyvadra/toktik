package fmp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSplitsFetchesSymbolSplits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/splits" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("symbol"); got != "AAPL" {
			t.Fatalf("symbol query = %q, want AAPL", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"symbol":"AAPL","date":"2020-08-31","numerator":4,"denominator":1,"splitType":"Stock Split"}]`))
	}))
	defer server.Close()

	client := New("test-key", WithBaseURL(server.URL), WithCacheDir(""))
	splits, err := client.Splits(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("Splits returned error: %v", err)
	}
	if len(splits) != 1 {
		t.Fatalf("expected one split, got %#v", splits)
	}
	if splits[0].Symbol != "AAPL" || splits[0].Date != "2020-08-31" || splits[0].Numerator != 4 || splits[0].Denominator != 1 {
		t.Fatalf("unexpected split: %#v", splits[0])
	}
}
