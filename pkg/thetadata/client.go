package thetadata

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

type contractEnvelope struct {
	Contract json.RawMessage   `json:"contract"`
	Data     []json.RawMessage `json:"data"`
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
		flattened, flattenErr := decodeContractEnvelope[T](elem)
		if flattenErr == nil {
			items = append(items, flattened...)
			continue
		}

		var item T
		if err := json.Unmarshal(elem, &item); err != nil {
			return nil, fmt.Errorf("decode item: %w (flatten: %v)", err, flattenErr)
		}
		normalizeOptionRightFields(&item)
		items = append(items, item)
	}
	return items, nil
}

func decodeContractEnvelope[T any](raw json.RawMessage) ([]T, error) {
	var envelope contractEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Contract) == 0 || len(envelope.Data) == 0 {
		return nil, fmt.Errorf("not contract envelope")
	}

	var contract map[string]any
	if err := json.Unmarshal(envelope.Contract, &contract); err != nil {
		return nil, fmt.Errorf("decode contract: %w", err)
	}

	items := make([]T, 0, len(envelope.Data))
	for _, datum := range envelope.Data {
		var payload map[string]any
		if err := json.Unmarshal(datum, &payload); err != nil {
			return nil, fmt.Errorf("decode data row: %w", err)
		}
		merged := make(map[string]any, len(contract)+len(payload))
		for key, value := range contract {
			merged[key] = value
		}
		for key, value := range payload {
			merged[key] = value
		}

		buf, err := json.Marshal(merged)
		if err != nil {
			return nil, fmt.Errorf("marshal merged row: %w", err)
		}

		var item T
		if err := json.Unmarshal(buf, &item); err != nil {
			return nil, fmt.Errorf("decode merged row: %w", err)
		}
		normalizeOptionRightFields(&item)
		items = append(items, item)
	}
	return items, nil
}

func normalizeOptionRightFields[T any](item *T) {
	switch row := any(item).(type) {
	case *EODRow:
		row.Right = normalizeOptionRight(row.Right)
	case *GreeksEODRow:
		row.Right = normalizeOptionRight(row.Right)
	case *OpenInterestRow:
		row.Right = normalizeOptionRight(row.Right)
	case *QuoteRow:
		row.Right = normalizeOptionRight(row.Right)
	case *OHLCRow:
		row.Right = normalizeOptionRight(row.Right)
	}
}

func normalizeOptionRight(right string) string {
	switch {
	case strings.EqualFold(right, "call"), strings.EqualFold(right, "c"):
		return "call"
	case strings.EqualFold(right, "put"), strings.EqualFold(right, "p"):
		return "put"
	default:
		return strings.ToLower(strings.TrimSpace(right))
	}
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

// GetQuotes fetches interval NBBO quotes for a root+expiration on a date range.
// Multi-day requests are limited to 1 month and must specify an expiration.
func (c *Client) GetQuotes(ctx context.Context, symbol, expiration, startDate, endDate, interval string) ([]QuoteRow, error) {
	q := url.Values{
		"symbol":     {symbol},
		"expiration": {expiration},
		"start_date": {startDate},
		"end_date":   {endDate},
		"interval":   {interval},
	}
	raw, err := c.getJSON(ctx, "/option/history/quote", q)
	if err != nil {
		return nil, err
	}
	return decodeResponse[QuoteRow](raw)
}

// GetQuotes1m fetches 1-minute NBBO quotes for a root+expiration on a date range.
func (c *Client) GetQuotes1m(ctx context.Context, symbol, expiration, startDate, endDate string) ([]QuoteRow, error) {
	return c.GetQuotes(ctx, symbol, expiration, startDate, endDate, "1m")
}

// GetOHLC fetches interval OHLC bars for a root+expiration on a date range.
func (c *Client) GetOHLC(ctx context.Context, symbol, expiration, startDate, endDate, interval string) ([]OHLCRow, error) {
	q := url.Values{
		"symbol":     {symbol},
		"expiration": {expiration},
		"start_date": {startDate},
		"end_date":   {endDate},
		"interval":   {interval},
	}
	raw, err := c.getJSON(ctx, "/option/history/ohlc", q)
	if err != nil {
		return nil, err
	}
	return decodeResponse[OHLCRow](raw)
}

// GetOHLC1m fetches 1-minute OHLC bars for a root+expiration on a date range.
func (c *Client) GetOHLC1m(ctx context.Context, symbol, expiration, startDate, endDate string) ([]OHLCRow, error) {
	return c.GetOHLC(ctx, symbol, expiration, startDate, endDate, "1m")
}
