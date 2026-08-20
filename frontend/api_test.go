package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAPIClientValidatesWithAPIKeyAndDSL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/backtests/validate" || req.Header.Get("X-API-Key") != "secret" {
			t.Fatalf("unexpected request: %s key=%q", req.URL.Path, req.Header.Get("X-API-Key"))
		}
		var payload backtestRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.DSL != `strategy("Demo")` || payload.Asset != "SPY" || payload.Preflight == nil || *payload.Preflight {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"strategies":[{"display_name":"Demo","dsl_params":[{"name":"length","title":"Length","type":"int","default":10}]}]}`))
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	client := newAPIClient(baseURL, "secret")
	response, err := client.validate(context.Background(), backtestRequest{Asset: "SPY", DSL: `strategy("Demo")`})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Strategies) != 1 || len(response.Strategies[0].DSLParams) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestNewAPIClient(t *testing.T) {
	baseURL, err := url.Parse("http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	client := newAPIClient(baseURL, "secret")
	if client.baseURL != baseURL || client.apiKey != "secret" {
		t.Fatalf("unexpected client: %#v", client)
	}
	if client.httpClient.Timeout != apiRequestTimeout {
		t.Fatalf("timeout = %s, want %s", client.httpClient.Timeout, apiRequestTimeout)
	}
}
