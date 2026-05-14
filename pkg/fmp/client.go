// Package fmp provides a Go client for the Financial Modeling Prep stable API.
// All methods are generated from live endpoint probing; re-run generate.py to refresh.
// Docs: https://site.financialmodelingprep.com/developer/docs
package fmp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://financialmodelingprep.com/stable"

// Client is the FMP API client. Create one with New and reuse it across calls.
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
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
		baseURL: baseURL,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithBaseURL overrides the API base URL. Primarily used for tests.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = u
		}
	}
}

// get performs a GET to the FMP stable API path and decodes the JSON response.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("apikey", c.apiKey)

	u := c.baseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("fmp: build request: %w", err)
	}

	const maxRetries = 3
	for attempt := 0; ; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if shouldRetryTransport(err) && attempt < maxRetries {
				if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
					return fmt.Errorf("fmp: http: %w", waitErr)
				}
				continue
			}
			return fmt.Errorf("fmp: http: %w", err)
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			if attempt < maxRetries {
				if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
					return fmt.Errorf("fmp: read body: %w", waitErr)
				}
				continue
			}
			return fmt.Errorf("fmp: read body: %w", readErr)
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &HTTPStatusError{
				URL:        u,
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Body:       stringsTrimSpaceMax(string(body), 300),
			}
			if resp.StatusCode >= 500 && attempt < maxRetries {
				if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
					return fmt.Errorf("fmp: http: %w", waitErr)
				}
				continue
			}
			return httpErr
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
}

func shouldRetryTransport(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return true
}

func waitRetryBackoff(ctx context.Context, attempt int) error {
	delay := 500 * time.Millisecond
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= 4*time.Second {
			delay = 4 * time.Second
			break
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func stringsTrimSpaceMax(value string, max int) string {
	trimmed := value
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\n' || trimmed[0] == '\r' || trimmed[0] == '\t') {
		trimmed = trimmed[1:]
	}
	for len(trimmed) > 0 {
		last := trimmed[len(trimmed)-1]
		if last != ' ' && last != '\n' && last != '\r' && last != '\t' {
			break
		}
		trimmed = trimmed[:len(trimmed)-1]
	}
	if max > 0 && len(trimmed) > max {
		return trimmed[:max]
	}
	return trimmed
}
