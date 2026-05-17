package fmp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientRetriesOnServerErrors(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Error Message":"temporary"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	_, err := client.IntradayPrices(context.Background(), "AAPL", Interval1Min, "2026-01-01", "2026-01-02")
	if err != nil {
		t.Fatalf("expected retry to eventually succeed, got error: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestClientRetriesOn429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"Error Message":"rate limit"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	_, err := client.IntradayPrices(context.Background(), "AAPL", Interval1Min, "2026-01-01", "2026-01-02")
	if err != nil {
		t.Fatalf("expected retry to eventually succeed, got error: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected 3 attempts on 429 retry, got %d", got)
	}
}

func TestClientReturns429AfterRetryBudgetExhausted(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"Error Message":"rate limit"}`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	_, err := client.IntradayPrices(context.Background(), "AAPL", Interval1Min, "2026-01-01", "2026-01-02")
	if err == nil {
		t.Fatal("expected final 429 error, got nil")
	}
	if !IsHTTPStatus(err, http.StatusTooManyRequests) {
		t.Fatalf("expected IsHTTPStatus 429 to be true, got err=%v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 4 {
		t.Fatalf("expected 4 attempts after retry budget, got %d", got)
	}
}
