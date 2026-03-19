package dvol

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetHistoryPaginatesAndDedups(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts1 := base.UnixMilli()
	ts2 := base.Add(1 * time.Minute).UnixMilli()
	ts3 := base.Add(2 * time.Minute).UnixMilli()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		end := r.URL.Query().Get("end_timestamp")
		if end == fmt.Sprintf("%d", ts3) {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","result":{"data":[[%d,1,2,0.5,1.5],[%d,2,3,1.5,2.5]],"continuation":%d}}`, ts2, ts3, ts2)))
			return
		}
		if end == fmt.Sprintf("%d", ts2) {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","result":{"data":[[%d,0.8,1.1,0.7,1.0],[%d,1,2,0.5,1.5]],"continuation":null}}`, ts1, ts2)))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unexpected end_timestamp"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	rows, err := client.GetHistory(context.Background(), "BTC", "60", base, base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("expected 3 unique rows, got %d", len(rows))
	}
	if !rows[0].Timestamp.Before(rows[1].Timestamp) || !rows[1].Timestamp.Before(rows[2].Timestamp) {
		t.Fatalf("rows are not sorted ascending by timestamp")
	}
	if rows[0].Timestamp.UnixMilli() != ts1 || rows[1].Timestamp.UnixMilli() != ts2 || rows[2].Timestamp.UnixMilli() != ts3 {
		t.Fatalf("unexpected timestamps: %v %v %v", rows[0].Timestamp, rows[1].Timestamp, rows[2].Timestamp)
	}
}

func TestSupportsCurrencyInvalidCurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params","data":{"reason":"invalid currency","param":"currency"}}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	supported, err := client.SupportsCurrency(context.Background(), "XXX", "60")
	if err != nil {
		t.Fatalf("SupportsCurrency returned unexpected error: %v", err)
	}
	if supported {
		t.Fatalf("expected unsupported currency")
	}
}

func TestNormalizeResolution(t *testing.T) {
	tests := []struct {
		in      string
		expect  string
		errWant bool
	}{
		{in: "", expect: "60"},
		{in: "1", expect: "1"},
		{in: "1s", expect: "1"},
		{in: "60", expect: "60"},
		{in: "1m", expect: "60"},
		{in: "3600", expect: "3600"},
		{in: "1h", expect: "3600"},
		{in: "43200", expect: "43200"},
		{in: "12h", expect: "43200"},
		{in: "86400", expect: "86400"},
		{in: "1d", expect: "86400"},
		{in: "5m", errWant: true},
		{in: "15m", errWant: true},
	}

	for _, tc := range tests {
		got, err := normalizeResolution(tc.in)
		if tc.errWant {
			if err == nil {
				t.Fatalf("normalizeResolution(%q) expected error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeResolution(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.expect {
			t.Fatalf("normalizeResolution(%q) = %q, want %q", tc.in, got, tc.expect)
		}
	}
}
