package vix

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseCSV(t *testing.T) {
	rows, err := ParseCSV(strings.NewReader("DATE,OPEN,HIGH,LOW,CLOSE\n01/02/1990,17.24,18.00,16.50,17.75\n"))
	if err != nil {
		t.Fatalf("ParseCSV() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0].Date.Format("2006-01-02"), "1990-01-02"; got != want {
		t.Fatalf("Date = %s, want %s", got, want)
	}
	if got, want := rows[0].Close, 17.75; got != want {
		t.Fatalf("Close = %v, want %v", got, want)
	}
}

func TestFetchHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/history.csv" {
			t.Fatalf("path = %s, want /history.csv", r.URL.Path)
		}
		_, _ = w.Write([]byte("DATE,OPEN,HIGH,LOW,CLOSE\n05/23/2026,20.00,21.00,19.00,20.50\n"))
	}))
	defer server.Close()

	client := New(WithHTTPClient(server.Client()), WithHistoryURL(server.URL+"/history.csv"))
	rows, err := client.FetchHistory(context.Background())
	if err != nil {
		t.Fatalf("FetchHistory() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0].Date, time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Date = %v, want %v", got, want)
	}
	if got, want := rows[0].Open, 20.0; got != want {
		t.Fatalf("Open = %v, want %v", got, want)
	}
	if got, want := rows[0].High, 21.0; got != want {
		t.Fatalf("High = %v, want %v", got, want)
	}
	if got, want := rows[0].Low, 19.0; got != want {
		t.Fatalf("Low = %v, want %v", got, want)
	}
	if got, want := rows[0].Close, 20.5; got != want {
		t.Fatalf("Close = %v, want %v", got, want)
	}
}
