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
	"math"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"
)

const baseURL = "https://financialmodelingprep.com/stable"
const defaultHTTPTimeout = 2 * time.Minute

// Client is the FMP API client. Create one with New and reuse it across calls.
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	cacheDir   string
	cacheTTL   time.Duration
}

// Option configures the Client.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client (2 min timeout).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithCacheDir enables on-disk GET response caching for this client.
func WithCacheDir(dir string) Option {
	return func(c *Client) { c.cacheDir = normalizeCacheDir(dir) }
}

// New creates a new FMP client with the given API key.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		baseURL:  baseURL,
		cacheDir: defaultCacheDir(),
		cacheTTL: defaultCacheTTL,
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
	if body, ok := c.loadCachedBody(u); ok {
		if err := decodeResponseBody(body, out); err == nil {
			return nil
		}
		_ = c.deleteCachedBody(u)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("fmp: build request: %w", err)
	}

	const maxRetries = 3
	for attempt := 0; ; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if shouldRetryTransport(err) && attempt < maxRetries {
				if waitErr := waitRetryBackoff(ctx, retryDelayForAttempt(attempt, 500*time.Millisecond, 4*time.Second)); waitErr != nil {
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
				if waitErr := waitRetryBackoff(ctx, retryDelayForAttempt(attempt, 500*time.Millisecond, 4*time.Second)); waitErr != nil {
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
				RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After")),
			}
			if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
				if waitErr := waitRetryBackoff(ctx, retryDelayFor429(attempt, httpErr.RetryAfter)); waitErr != nil {
					return fmt.Errorf("fmp: http: %w", waitErr)
				}
				continue
			}
			if resp.StatusCode >= 500 && attempt < maxRetries {
				if waitErr := waitRetryBackoff(ctx, retryDelayForAttempt(attempt, 500*time.Millisecond, 4*time.Second)); waitErr != nil {
					return fmt.Errorf("fmp: http: %w", waitErr)
				}
				continue
			}
			return httpErr
		}

		if err := decodeResponseBody(body, out); err != nil {
			return err
		}
		c.storeCachedBody(u, body)
		return nil
	}
}

func decodeResponseBody(body []byte, out any) error {
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

func normalizeCacheDir(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
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

func retryDelayFor429(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return retryDelayForAttempt(attempt, 2*time.Second, 30*time.Second)
}

func retryDelayForAttempt(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	if baseDelay <= 0 {
		return 0
	}
	if maxDelay <= 0 || maxDelay < baseDelay {
		maxDelay = baseDelay
	}
	delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt)))
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func waitRetryBackoff(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
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

func parseRetryAfterHeader(value string) time.Duration {
	trimmed := stringsTrimSpaceMax(value, 128)
	if trimmed == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(trimmed)
	if err != nil {
		return 0
	}
	delay := time.Until(when)
	if delay <= 0 {
		return 0
	}
	return delay
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
