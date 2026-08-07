package deribit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL      = "https://www.deribit.com"
	defaultTimeout      = 30 * time.Second
	maxResponseBodySize = 16 << 20
)

// Config controls Deribit HTTP connectivity.
type Config struct {
	BaseURL  string
	ProxyURL string
	Timeout  time.Duration
}

// Client wraps the Deribit public HTTP API used for realtime option snapshots.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Deribit client without making a network request.
func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("invalid deribit base URL %q", baseURL)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL := strings.TrimSpace(cfg.ProxyURL); proxyURL != "" {
		parsedProxyURL, err := url.Parse(proxyURL)
		if err != nil || parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
			return nil, fmt.Errorf("invalid deribit proxy URL %q", proxyURL)
		}
		transport.Proxy = http.ProxyURL(parsedProxyURL)
	} else {
		transport.Proxy = http.ProxyFromEnvironment
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
	}, nil
}

// OptionChain returns the current bulk book summary for active options in a currency.
func (c *Client) OptionChain(ctx context.Context, currency string) ([]BookSummary, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return nil, fmt.Errorf("deribit option chain: currency is required")
	}

	endpoint := c.baseURL
	if !strings.HasSuffix(endpoint, "/api/v2") {
		endpoint += "/api/v2"
	}
	requestURL, err := url.Parse(endpoint + "/public/get_book_summary_by_currency")
	if err != nil {
		return nil, fmt.Errorf("build deribit option chain URL: %w", err)
	}
	query := requestURL.Query()
	query.Set("currency", currency)
	query.Set("kind", "option")
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build deribit option chain request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &RequestError{Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize+1))
	if err != nil {
		return nil, &RequestError{Err: err}
	}
	if len(body) > maxResponseBodySize {
		return nil, &ResponseError{Message: "response body exceeds size limit"}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	var envelope apiResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, &ResponseError{Message: "response is not valid JSON"}
	}
	if envelope.Error != nil {
		return nil, &RPCError{Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil, &ResponseError{Message: "missing result"}
	}

	var summaries []BookSummary
	if err := json.Unmarshal(envelope.Result, &summaries); err != nil {
		return nil, &ResponseError{Message: "result has an unexpected shape"}
	}
	if summaries == nil {
		summaries = make([]BookSummary, 0)
	}
	return summaries, nil
}
