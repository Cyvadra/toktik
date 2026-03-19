package thetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client wraps the Theta Data v3 REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a REST v3 client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL + "/v3",
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// getJSON performs a GET request and decodes the JSON response body.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("format", "json")

	u := c.baseURL + path + "?" + query.Encode()

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}

		return json.RawMessage(body), nil
	}
	return nil, fmt.Errorf("after 3 attempts: %w", lastErr)
}

// jsonResponse is the common wrapper for Theta Data v3 JSON responses.
type jsonResponse struct {
	Response []json.RawMessage `json:"response"`
}

// decodeResponse extracts the response array from a Theta Data JSON response.
func decodeResponse[T any](raw json.RawMessage) ([]T, error) {
	var wrapper jsonResponse
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		// Some endpoints return a flat array.
		var items []T
		if err2 := json.Unmarshal(raw, &items); err2 != nil {
			return nil, fmt.Errorf("decode response: %w (flat: %w)", err, err2)
		}
		return items, nil
	}
	items := make([]T, 0, len(wrapper.Response))
	for _, elem := range wrapper.Response {
		var item T
		if err := json.Unmarshal(elem, &item); err != nil {
			return nil, fmt.Errorf("decode item: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

// ListSymbols returns all option root symbols.
func (c *Client) ListSymbols(ctx context.Context) ([]string, error) {
	raw, err := c.getJSON(ctx, "/option/list/symbols", nil)
	if err != nil {
		return nil, err
	}

	type symbolRow struct {
		Symbol string `json:"symbol"`
	}
	rows, err := decodeResponse[symbolRow](raw)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Symbol
	}
	return out, nil
}

// ListExpirations returns all expirations for a root symbol.
func (c *Client) ListExpirations(ctx context.Context, symbol string) ([]string, error) {
	q := url.Values{"symbol": {symbol}}
	raw, err := c.getJSON(ctx, "/option/list/expirations", q)
	if err != nil {
		return nil, err
	}

	type expRow struct {
		Expiration string `json:"expiration"`
	}
	rows, err := decodeResponse[expRow](raw)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Expiration
	}
	return out, nil
}

// GetEOD fetches the full-chain EOD report for a root on a single date.
// Uses expiration=* and strike=* to get all contracts in one call.
func (c *Client) GetEOD(ctx context.Context, symbol, date string) ([]EODRow, error) {
	q := url.Values{
		"symbol":     {symbol},
		"expiration": {"*"},
		"start_date": {date},
		"end_date":   {date},
	}
	raw, err := c.getJSON(ctx, "/option/history/eod", q)
	if err != nil {
		return nil, err
	}
	return decodeResponse[EODRow](raw)
}

// GetGreeksEOD fetches the full-chain EOD Greeks for a root on a single date.
// Uses expiration=* to get all contracts. NOTE: expiration=* must be requested day-by-day.
func (c *Client) GetGreeksEOD(ctx context.Context, symbol, date string) ([]GreeksEODRow, error) {
	q := url.Values{
		"symbol":     {symbol},
		"expiration": {"*"},
		"start_date": {date},
		"end_date":   {date},
	}
	raw, err := c.getJSON(ctx, "/option/history/greeks/eod", q)
	if err != nil {
		return nil, err
	}
	return decodeResponse[GreeksEODRow](raw)
}

// GetOpenInterest fetches full-chain open interest for a root on a single date.
func (c *Client) GetOpenInterest(ctx context.Context, symbol, date string) ([]OpenInterestRow, error) {
	q := url.Values{
		"symbol":     {symbol},
		"expiration": {"*"},
		"date":       {date},
	}
	raw, err := c.getJSON(ctx, "/option/history/open_interest", q)
	if err != nil {
		return nil, err
	}
	return decodeResponse[OpenInterestRow](raw)
}

// GetQuotes1m fetches 1-minute NBBO quotes for a root+expiration on a date range.
// Multi-day requests are limited to 1 month and must specify an expiration.
func (c *Client) GetQuotes1m(ctx context.Context, symbol, expiration, startDate, endDate string) ([]QuoteRow, error) {
	q := url.Values{
		"symbol":     {symbol},
		"expiration": {expiration},
		"start_date": {startDate},
		"end_date":   {endDate},
		"interval":   {"1m"},
	}
	raw, err := c.getJSON(ctx, "/option/history/quote", q)
	if err != nil {
		return nil, err
	}
	return decodeResponse[QuoteRow](raw)
}

// GetOHLC1m fetches 1-minute OHLC bars for a root+expiration on a date range.
func (c *Client) GetOHLC1m(ctx context.Context, symbol, expiration, startDate, endDate string) ([]OHLCRow, error) {
	q := url.Values{
		"symbol":     {symbol},
		"expiration": {expiration},
		"start_date": {startDate},
		"end_date":   {endDate},
		"interval":   {"1m"},
	}
	raw, err := c.getJSON(ctx, "/option/history/ohlc", q)
	if err != nil {
		return nil, err
	}
	return decodeResponse[OHLCRow](raw)
}
