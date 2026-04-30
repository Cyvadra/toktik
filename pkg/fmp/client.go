// Package fmp provides a Go client for the Financial Modeling Prep stable API.
// All methods are generated from live endpoint probing; re-run generate.py to refresh.
// Docs: https://site.financialmodelingprep.com/developer/docs
package fmp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://financialmodelingprep.com/stable"

// Client is the FMP API client. Create one with New and reuse it across calls.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client (30 s timeout).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// New creates a new FMP client with the given API key.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// get performs a GET to the FMP stable API path and decodes the JSON response.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("apikey", c.apiKey)

	u := baseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("fmp: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fmp: http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fmp: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errMsg struct {
			Error string `json:"Error Message"`
		}
		if json.Unmarshal(body, &errMsg) == nil && errMsg.Error != "" {
			return fmt.Errorf("fmp: api error (HTTP %d): %s", resp.StatusCode, errMsg.Error)
		}
		return fmt.Errorf("fmp: http %d: %s", resp.StatusCode, body)
	}

	// FMP returns plan/premium errors as a plain JSON string.
	if len(body) > 0 && body[0] == '"' {
		var msg string
		_ = json.Unmarshal(body, &msg)
		return fmt.Errorf("fmp: api error: %s", msg)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("fmp: decode response: %w (body: %.300s)", err, body)
	}
	return nil
}
