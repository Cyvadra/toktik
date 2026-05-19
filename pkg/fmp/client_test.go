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

func TestLatestFinancialStatementsUsesPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest-financial-statements" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("apikey"); got != "test-key" {
			t.Fatalf("unexpected api key %q", got)
		}
		if got := r.URL.Query().Get("page"); got != "2" {
			t.Fatalf("unexpected page %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "250" {
			t.Fatalf("unexpected limit %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"symbol":"AAPL","date":"2026-03-31","filingDate":"2026-05-01","acceptedDate":"2026-05-01 16:12:00","fiscalYear":"2026","period":"Q2"}]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	rows, err := client.LatestFinancialStatements(context.Background(), 2, 250)
	if err != nil {
		t.Fatalf("latest financial statements: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Symbol != "AAPL" || rows[0].Date != "2026-03-31" || rows[0].AcceptedDate != "2026-05-01 16:12:00" {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
}
