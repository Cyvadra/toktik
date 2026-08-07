package deribit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestClientOptionChain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/public/get_book_summary_by_currency" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("currency"); got != "BTC" {
			t.Fatalf("currency=%q want BTC", got)
		}
		if got := r.URL.Query().Get("kind"); got != "option" {
			t.Fatalf("kind=%q want option", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"instrument_name":"BTC-28AUG26-110000-P","base_currency":"BTC","quote_currency":"USD","creation_timestamp":1786100000000,"bid_price":0.638,"ask_price":null,"mark_iv":65.75,"open_interest":0.2,"underlying_price":64892.68}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	rows, err := client.OptionChain(context.Background(), " btc ")
	if err != nil {
		t.Fatalf("OptionChain: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows)=%d want 1", len(rows))
	}
	row := rows[0]
	if row.InstrumentName != "BTC-28AUG26-110000-P" || row.BidPrice == nil || *row.BidPrice != 0.638 {
		t.Fatalf("unexpected row: %#v", row)
	}
	if row.AskPrice != nil {
		t.Fatalf("AskPrice=%v want nil", row.AskPrice)
	}
	if row.MarkIV == nil || *row.MarkIV != 65.75 {
		t.Fatalf("MarkIV=%v want 65.75", row.MarkIV)
	}
}

func TestClientOptionChainErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		assertType func(error) bool
	}{
		{
			name:   "http status",
			status: http.StatusBadGateway,
			body:   `upstream unavailable`,
			assertType: func(err error) bool {
				var target *HTTPStatusError
				return errors.As(err, &target) && target.StatusCode == http.StatusBadGateway
			},
		},
		{
			name:   "json rpc",
			status: http.StatusOK,
			body:   `{"jsonrpc":"2.0","error":{"code":11050,"message":"bad_request"}}`,
			assertType: func(err error) bool {
				var target *RPCError
				return errors.As(err, &target) && target.Code == 11050
			},
		},
		{
			name:   "malformed json",
			status: http.StatusOK,
			body:   `{`,
			assertType: func(err error) bool {
				var target *ResponseError
				return errors.As(err, &target)
			},
		},
		{
			name:   "missing result",
			status: http.StatusOK,
			body:   `{"jsonrpc":"2.0"}`,
			assertType: func(err error) bool {
				var target *ResponseError
				return errors.As(err, &target)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client, err := NewClient(Config{BaseURL: server.URL})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = client.OptionChain(context.Background(), "BTC")
			if err == nil || !tt.assertType(err) {
				t.Fatalf("unexpected error: %T %v", err, err)
			}
		})
	}
}

func TestNewClientUsesExplicitProxy(t *testing.T) {
	client, err := NewClient(Config{
		BaseURL:  "https://www.deribit.com",
		ProxyURL: "http://127.0.0.1:17892",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.httpClient.Transport)
	}
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "www.deribit.com"}}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:17892" {
		t.Fatalf("proxy=%v want http://127.0.0.1:17892", proxyURL)
	}
}

func TestNewClientRejectsInvalidURLs(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "://bad"}); err == nil {
		t.Fatal("expected invalid base URL error")
	}
	if _, err := NewClient(Config{ProxyURL: "://bad"}); err == nil {
		t.Fatal("expected invalid proxy URL error")
	}
}
