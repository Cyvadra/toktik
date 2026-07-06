package fmp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProfilesUsesCommaSeparatedSymbolList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profile" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("symbol"); got != "AAPL,SLV" {
			t.Fatalf("unexpected symbol query %q", got)
		}
		if got := r.URL.Query().Get("apikey"); got != "test-key" {
			t.Fatalf("unexpected api key %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"symbol":"AAPL","sector":"Technology"},{"symbol":"SLV","isEtf":true}]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	profiles, err := client.Profiles(context.Background(), []string{"AAPL", "SLV", "AAPL", " "})
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Symbol != "AAPL" || profiles[1].Symbol != "SLV" || !profiles[1].IsETF {
		t.Fatalf("unexpected profiles: %#v", profiles)
	}
	if strings.TrimSpace(profiles[0].Sector) != "Technology" {
		t.Fatalf("unexpected sector: %#v", profiles[0])
	}
}

func TestProfilesAcceptsFractionalVolumeFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"symbol":"MDRX","volume":0.6339,"averageVolume":266095.9}]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	profiles, err := client.Profiles(context.Background(), []string{"MDRX"})
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Volume != 1 || profiles[0].AverageVolume != 266096 {
		t.Fatalf("unexpected rounded volume fields: volume=%d averageVolume=%d", profiles[0].Volume, profiles[0].AverageVolume)
	}
}

func TestProfilesAcceptsFractionalMarketCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{
			"symbol":"GOOG",
			"marketCap":4353064766971.0005,
			"image":"https://images.financialmodelingprep.com/symbol/GOOG.png"
		}]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	profile, err := client.Profile(context.Background(), "GOOG")
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if profile.MarketCap != 4353064766971 {
		t.Fatalf("marketCap = %d, want 4353064766971", profile.MarketCap)
	}
}

func TestClientUsesDiskCache(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL), WithCacheDir(t.TempDir()))
	if _, err := client.IntradayPrices(context.Background(), "AAPL", Interval1Min, "2026-01-01", "2026-01-02"); err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	if _, err := client.IntradayPrices(context.Background(), "AAPL", Interval1Min, "2026-01-01", "2026-01-02"); err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected cached second fetch, got %d upstream requests", got)
	}
}

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

func TestEarningsCalendarUsesDateWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/earnings-calendar" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("apikey"); got != "test-key" {
			t.Fatalf("unexpected api key %q", got)
		}
		if got := r.URL.Query().Get("from"); got != "2026-05-12" {
			t.Fatalf("unexpected from %q", got)
		}
		if got := r.URL.Query().Get("to"); got != "2026-05-19" {
			t.Fatalf("unexpected to %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"symbol":"AAPL","date":"2026-05-15","lastUpdated":"2026-05-15"}]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	rows, err := client.EarningsCalendar(context.Background(), "2026-05-12", "2026-05-19")
	if err != nil {
		t.Fatalf("earnings calendar: %v", err)
	}
	if len(rows) != 1 || rows[0].Symbol != "AAPL" || rows[0].LastUpdated != "2026-05-15" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestSecFilingsFinancialsUsesPaginationAndWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sec-filings-financials" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("apikey"); got != "test-key" {
			t.Fatalf("unexpected api key %q", got)
		}
		if got := r.URL.Query().Get("from"); got != "2026-05-12" {
			t.Fatalf("unexpected from %q", got)
		}
		if got := r.URL.Query().Get("to"); got != "2026-05-19" {
			t.Fatalf("unexpected to %q", got)
		}
		if got := r.URL.Query().Get("page"); got != "2" {
			t.Fatalf("unexpected page %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "250" {
			t.Fatalf("unexpected limit %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"symbol":"AAPL","filingDate":"2026-05-01 00:00:00","acceptedDate":"2026-05-01 16:12:00","formType":"10-Q","hasFinancials":true}]`))
	}))
	defer server.Close()

	client := New("test-key", WithHTTPClient(server.Client()), WithBaseURL(server.URL))
	rows, err := client.SecFilingsFinancials(context.Background(), "2026-05-12", "2026-05-19", 2, 250)
	if err != nil {
		t.Fatalf("sec filings financials: %v", err)
	}
	if len(rows) != 1 || rows[0].Symbol != "AAPL" || !rows[0].HasFinancials {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}
