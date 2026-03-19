package thetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client provides high-level methods for querying the Theta Data API
// through the MCP transport.
type Client struct {
	mcp       *MCPClient
	rateLimit <-chan time.Time // rate limiter ticker
	ticker    *time.Ticker     // kept for cleanup
}

// NewClient creates a new Theta Data API client with the given MCP transport
// and request rate limit (requests per second). Pass 0 for no limit.
func NewClient(mcp *MCPClient, reqPerSec float64) *Client {
	c := &Client{mcp: mcp}
	if reqPerSec > 0 {
		interval := time.Duration(float64(time.Second) / reqPerSec)
		ticker := time.NewTicker(interval)
		c.ticker = ticker
		c.rateLimit = ticker.C
	}
	return c
}

// Close releases resources held by the client.
func (c *Client) Close() {
	if c.ticker != nil {
		c.ticker.Stop()
	}
}

func (c *Client) throttle() {
	if c.rateLimit != nil {
		<-c.rateLimit
	}
}

// callTool is a helper that throttles and calls the MCP tool.
func (c *Client) callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	c.throttle()
	return c.mcp.CallTool(ctx, name, args)
}

// thetaResponse is the generic wrapper for Theta Data API responses.
type thetaResponse struct {
	Response json.RawMessage `json:"response"`
}

type thetaContractResponse struct {
	Contract json.RawMessage `json:"contract"`
	Data     json.RawMessage `json:"data"`
}

func unmarshalThetaJSON(raw string, target any) error {
	trimmed := strings.TrimSpace(raw)
	if err := json.Unmarshal([]byte(trimmed), target); err == nil {
		return nil
	}

	firstErr := json.Unmarshal([]byte(trimmed), target)

	for _, candidate := range []string{unwrapJSONString(trimmed)} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == trimmed {
			continue
		}
		if err := json.Unmarshal([]byte(candidate), target); err == nil {
			return nil
		}
		inner := unwrapJSONString(candidate)
		if inner != "" && inner != candidate {
			if err := json.Unmarshal([]byte(inner), target); err == nil {
				return nil
			}
		}
	}

	return firstErr
}

func unwrapJSONString(raw string) string {
	var unwrapped string
	if err := json.Unmarshal([]byte(raw), &unwrapped); err != nil {
		return ""
	}
	return unwrapped
}

func (c *Client) getREST(ctx context.Context, path string) (string, error) {
	c.throttle()

	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.mcp.baseURL+path, nil)
		if err != nil {
			return "", fmt.Errorf("create REST request: %w", err)
		}

		resp, err := c.mcp.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("REST request: %w", err)
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return "", fmt.Errorf("read REST response: %w", readErr)
			}

			if resp.StatusCode == http.StatusOK {
				var payload thetaResponse
				if err := json.Unmarshal(body, &payload); err != nil {
					return "", fmt.Errorf("decode REST response: %w", err)
				}
				return string(payload.Response), nil
			}

			bodyText := strings.TrimSpace(string(body))
			if len(bodyText) > 300 {
				bodyText = bodyText[:300]
			}
			lastErr = fmt.Errorf("REST request returned status %d: %s", resp.StatusCode, bodyText)

			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < http.StatusInternalServerError {
				return "", lastErr
			}
		}

		if attempt == maxAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(attempt*attempt) * 250 * time.Millisecond):
		}
	}

	return "", lastErr
}

func (c *Client) getRESTJSON(ctx context.Context, path string, query url.Values) (string, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("format", "json")
	return c.getREST(ctx, path+"?"+query.Encode())
}

// ListRoots returns all available option root symbols in Theta Data.
func (c *Client) ListRoots(ctx context.Context) ([]string, error) {
	raw, err := c.getREST(ctx, "/v3/option/list/symbols?format=json")
	if err != nil {
		return nil, fmt.Errorf("list option roots via REST: %w", err)
	}

	var roots []string
	if err := json.Unmarshal([]byte(raw), &roots); err == nil {
		for i := range roots {
			roots[i] = strings.TrimSpace(roots[i])
		}
		roots = compactStrings(roots)
		sort.Strings(roots)
		return roots, nil
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("parse option roots data: %w", err)
	}

	for _, item := range items {
		for _, key := range []string{"symbol", "root"} {
			if value, ok := item[key]; ok {
				roots = append(roots, strings.TrimSpace(fmt.Sprint(value)))
				break
			}
		}
	}

	roots = compactStrings(roots)
	sort.Strings(roots)
	return roots, nil
}

// ListExpirations returns all available option expirations for a root symbol.
func (c *Client) ListExpirations(ctx context.Context, root string) ([]time.Time, error) {
	raw, err := c.getRESTJSON(ctx, "/v3/option/list/expirations", url.Values{
		"symbol": {root},
	})
	if err != nil {
		return nil, fmt.Errorf("list expirations for %s via REST: %w", root, err)
	}

	// Try array of objects with "expiration" field
	var expirations []time.Time
	var items []map[string]any
	if err := unmarshalThetaJSON(raw, &items); err == nil {
		for _, item := range items {
			if exp, ok := item["expiration"]; ok {
				if t, err := parseDate(fmt.Sprint(exp)); err == nil {
					expirations = append(expirations, t)
				}
			} else if exp, ok := item["date"]; ok {
				if t, err := parseDate(fmt.Sprint(exp)); err == nil {
					expirations = append(expirations, t)
				}
			}
		}
		if len(expirations) > 0 {
			sort.Slice(expirations, func(i, j int) bool { return expirations[i].Before(expirations[j]) })
			return expirations, nil
		}
	}

	// Try flat array of strings
	var dateStrs []string
	if err := unmarshalThetaJSON(raw, &dateStrs); err == nil {
		for _, s := range dateStrs {
			if t, err := parseDate(s); err == nil {
				expirations = append(expirations, t)
			}
		}
	}

	sort.Slice(expirations, func(i, j int) bool { return expirations[i].Before(expirations[j]) })
	return expirations, nil
}

// ListStrikes returns all available strikes for a root symbol and expiration.
func (c *Client) ListStrikes(ctx context.Context, root string, exp time.Time) ([]float64, error) {
	raw, err := c.getRESTJSON(ctx, "/v3/option/list/strikes", url.Values{
		"symbol":     {root},
		"expiration": {exp.Format("2006-01-02")},
	})
	if err != nil {
		return nil, fmt.Errorf("list strikes for %s exp %s via REST: %w", root, exp.Format("2006-01-02"), err)
	}

	// Try array of numbers
	var strikes []float64
	if err := unmarshalThetaJSON(raw, &strikes); err != nil {
		// Try array of objects with "strike" field
		var items []map[string]any
		if err := unmarshalThetaJSON(raw, &items); err != nil {
			return nil, fmt.Errorf("parse strikes data: %w", err)
		}
		for _, item := range items {
			if s, ok := item["strike"]; ok {
				if v, ok := s.(float64); ok {
					strikes = append(strikes, v)
				}
			}
		}
	}

	sort.Float64s(strikes)
	return strikes, nil
}

// ListDates returns all trading dates with data for a specific contract.
func (c *Client) ListDates(ctx context.Context, contract Contract) ([]time.Time, error) {
	raw, err := c.getRESTJSON(ctx, "/v3/option/list/dates/quote", url.Values{
		"symbol":     {contract.Root},
		"expiration": {contract.Expiration.Format("2006-01-02")},
		"strike":     {strconv.FormatFloat(contract.Strike, 'f', -1, 64)},
		"right":      {contract.Right},
	})
	if err != nil {
		return nil, fmt.Errorf("list dates for %s via REST: %w", contract.Symbol(), err)
	}

	var dates []time.Time
	var items []map[string]any
	if err := unmarshalThetaJSON(raw, &items); err == nil {
		for _, item := range items {
			for _, key := range []string{"date", "timestamp"} {
				if d, ok := item[key]; ok {
					if t, err := parseDate(fmt.Sprint(d)); err == nil {
						dates = append(dates, t)
					}
				}
			}
		}
	} else {
		var dateStrs []string
		if err := unmarshalThetaJSON(raw, &dateStrs); err == nil {
			for _, s := range dateStrs {
				if t, err := parseDate(s); err == nil {
					dates = append(dates, t)
				}
			}
		}
	}

	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	return dates, nil
}

// GetQuotes1m returns 1-minute NBBO quote bars for a contract on a specific date.
func (c *Client) GetQuotes1m(ctx context.Context, contract Contract, date time.Time) ([]QuoteBar, error) {
	raw, err := c.callTool(ctx, "option_history_quote", map[string]any{
		"symbol":     contract.Root,
		"expiration": contract.Expiration.Format("2006-01-02"),
		"strike":     contract.Strike,
		"right":      contract.Right,
		"date":       date.Format("2006-01-02"),
		"start_time": "09:30",
		"end_time":   "16:00",
		"interval":   "1m",
	})
	if err != nil {
		return nil, fmt.Errorf("get quotes 1m for %s on %s: %w",
			contract.Symbol(), date.Format("2006-01-02"), err)
	}

	return parseQuoteBars(raw, date)
}

// GetOHLC1m returns 1-minute trade-based OHLC bars for a contract on a specific date.
func (c *Client) GetOHLC1m(ctx context.Context, contract Contract, date time.Time) ([]OHLCBar, error) {
	raw, err := c.callTool(ctx, "option_history_ohlc", map[string]any{
		"symbol":     contract.Root,
		"expiration": contract.Expiration.Format("2006-01-02"),
		"strike":     contract.Strike,
		"right":      contract.Right,
		"date":       date.Format("2006-01-02"),
		"start_time": "09:30",
		"end_time":   "16:00",
		"interval":   "1m",
	})
	if err != nil {
		return nil, fmt.Errorf("get ohlc 1m for %s on %s: %w",
			contract.Symbol(), date.Format("2006-01-02"), err)
	}

	return parseOHLCBars(raw, date)
}

// GetQuotes1mRange returns 1-minute NBBO quote bars for a contract over a date range.
// One API call returns bars for all trading days in [startDate, endDate].
func (c *Client) GetQuotes1mRange(ctx context.Context, contract Contract, startDate, endDate time.Time) ([]QuoteBar, error) {
	raw, err := c.callTool(ctx, "option_history_quote", map[string]any{
		"symbol":     contract.Root,
		"expiration": contract.Expiration.Format("2006-01-02"),
		"strike":     contract.Strike,
		"right":      contract.Right,
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
		"start_time": "09:30",
		"end_time":   "16:00",
		"interval":   "1m",
	})
	if err != nil {
		return nil, fmt.Errorf("get quotes 1m range for %s (%s to %s): %w",
			contract.Symbol(), startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), err)
	}

	return parseQuoteBars(raw, startDate)
}

// GetOHLC1mRange returns 1-minute trade-based OHLC bars for a contract over a date range.
// One API call returns bars for all trading days in [startDate, endDate].
func (c *Client) GetOHLC1mRange(ctx context.Context, contract Contract, startDate, endDate time.Time) ([]OHLCBar, error) {
	raw, err := c.callTool(ctx, "option_history_ohlc", map[string]any{
		"symbol":     contract.Root,
		"expiration": contract.Expiration.Format("2006-01-02"),
		"strike":     contract.Strike,
		"right":      contract.Right,
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
		"start_time": "09:30",
		"end_time":   "16:00",
		"interval":   "1m",
	})
	if err != nil {
		return nil, fmt.Errorf("get ohlc 1m range for %s (%s to %s): %w",
			contract.Symbol(), startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), err)
	}

	return parseOHLCBars(raw, startDate)
}

// GetStockEOD returns end-of-day OHLC data for a stock symbol over a date range.
func (c *Client) GetStockEOD(ctx context.Context, symbol string, startDate, endDate time.Time) ([]OHLCBar, error) {
	raw, err := c.callTool(ctx, "stock_history_eod", map[string]any{
		"symbol":     symbol,
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
	})
	if err != nil {
		return nil, fmt.Errorf("get stock eod for %s: %w", symbol, err)
	}

	return parseOHLCBars(raw, startDate)
}

// GetGreeksEOD returns end-of-day Greeks for a contract over a date range.
// This is a bulk call — one request covers many dates.
func (c *Client) GetGreeksEOD(ctx context.Context, contract Contract, startDate, endDate time.Time) ([]GreeksEOD, error) {
	raw, err := c.callTool(ctx, "option_history_greeks_eod", map[string]any{
		"symbol":     contract.Root,
		"expiration": contract.Expiration.Format("2006-01-02"),
		"strike":     contract.Strike,
		"right":      contract.Right,
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
	})
	if err != nil {
		return nil, fmt.Errorf("get greeks eod for %s: %w", contract.Symbol(), err)
	}

	return parseGreeksEOD(raw)
}

// GetOpenInterest returns open interest for a contract on a specific date.
func (c *Client) GetOpenInterest(ctx context.Context, contract Contract, date time.Time) (float64, error) {
	raw, err := c.callTool(ctx, "option_history_open_interest", map[string]any{
		"symbol":     contract.Root,
		"expiration": contract.Expiration.Format("2006-01-02"),
		"strike":     contract.Strike,
		"right":      contract.Right,
		"date":       date.Format("2006-01-02"),
	})
	if err != nil {
		return 0, fmt.Errorf("get open interest for %s on %s: %w",
			contract.Symbol(), date.Format("2006-01-02"), err)
	}

	var resp thetaResponse
	if err := unmarshalThetaJSON(raw, &resp); err != nil {
		return 0, err
	}

	var items []thetaContractResponse
	if err := json.Unmarshal(resp.Response, &items); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	var records []map[string]any
	if err := json.Unmarshal(items[0].Data, &records); err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}

	if oi, ok := records[0]["open_interest"]; ok {
		if v, ok := oi.(float64); ok {
			return v, nil
		}
	}
	return 0, nil
}

// --- Response parsing helpers ---

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05Z",
		"20060102",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q", s)
}

func parseTimestamp(s string, refDate time.Time) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"15:04:05",
		"15:04",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			if t.Year() == 0 {
				// Time-only format, combine with reference date
				return time.Date(refDate.Year(), refDate.Month(), refDate.Day(),
					t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
			}
			return t.UTC()
		}
	}
	return refDate
}

func getFloat(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func getInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

func getFirstInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		if value := getInt(m, key); value != 0 {
			return value
		}
	}
	return 0
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func parseQuoteBars(raw string, refDate time.Time) ([]QuoteBar, error) {
	var resp thetaResponse
	if err := unmarshalThetaJSON(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse quote response: %w", err)
	}

	var items []thetaContractResponse
	if err := json.Unmarshal(resp.Response, &items); err != nil {
		return nil, fmt.Errorf("parse quote items: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}

	var records []map[string]any
	if err := json.Unmarshal(items[0].Data, &records); err != nil {
		return nil, fmt.Errorf("parse quote records: %w", err)
	}

	bars := make([]QuoteBar, 0, len(records))
	for _, rec := range records {
		ts := refDate
		if tsStr := getString(rec, "timestamp"); tsStr != "" {
			ts = parseTimestamp(tsStr, refDate)
		} else if tsStr := getString(rec, "datetime"); tsStr != "" {
			ts = parseTimestamp(tsStr, refDate)
		}

		bar := QuoteBar{
			Timestamp: ts,
			Bid:       getFloat(rec, "bid"),
			BidSize:   getInt(rec, "bid_size"),
			Ask:       getFloat(rec, "ask"),
			AskSize:   getInt(rec, "ask_size"),
		}
		bars = append(bars, bar)
	}

	return bars, nil
}

func parseOHLCBars(raw string, refDate time.Time) ([]OHLCBar, error) {
	var resp thetaResponse
	if err := unmarshalThetaJSON(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse ohlc response: %w", err)
	}

	var items []thetaContractResponse
	if err := json.Unmarshal(resp.Response, &items); err != nil {
		return nil, fmt.Errorf("parse ohlc items: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}

	var records []map[string]any
	if err := json.Unmarshal(items[0].Data, &records); err != nil {
		return nil, fmt.Errorf("parse ohlc records: %w", err)
	}

	bars := make([]OHLCBar, 0, len(records))
	for _, rec := range records {
		ts := refDate
		if tsStr := getString(rec, "timestamp"); tsStr != "" {
			ts = parseTimestamp(tsStr, refDate)
		} else if tsStr := getString(rec, "datetime"); tsStr != "" {
			ts = parseTimestamp(tsStr, refDate)
		}

		bar := OHLCBar{
			Timestamp: ts,
			Open:      getFloat(rec, "open"),
			High:      getFloat(rec, "high"),
			Low:       getFloat(rec, "low"),
			Close:     getFloat(rec, "close"),
			Volume:    getFirstInt(rec, "volume", "size", "total_volume"),
			Count:     getFirstInt(rec, "count", "trade_count", "ticks"),
		}
		bars = append(bars, bar)
	}

	return bars, nil
}

func parseGreeksEOD(raw string) ([]GreeksEOD, error) {
	var resp thetaResponse
	if err := unmarshalThetaJSON(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse greeks eod response: %w", err)
	}

	var items []thetaContractResponse
	if err := json.Unmarshal(resp.Response, &items); err != nil {
		return nil, fmt.Errorf("parse greeks eod items: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}

	var records []map[string]any
	if err := json.Unmarshal(items[0].Data, &records); err != nil {
		return nil, fmt.Errorf("parse greeks eod records: %w", err)
	}

	results := make([]GreeksEOD, 0, len(records))
	for _, rec := range records {
		tsStr := getString(rec, "timestamp")
		if tsStr == "" {
			tsStr = getString(rec, "date")
		}
		date, _ := parseDate(tsStr)

		g := GreeksEOD{
			Date:            date,
			UnderlyingPrice: getFloat(rec, "underlying_price"),
			ImpliedVol:      getFloat(rec, "implied_vol"),
			Delta:           getFloat(rec, "delta"),
			Gamma:           getFloat(rec, "gamma"),
			Vega:            getFloat(rec, "vega"),
			Theta:           getFloat(rec, "theta"),
			Rho:             getFloat(rec, "rho"),
			Close:           getFloat(rec, "close"),
			Bid:             getFloat(rec, "bid"),
			Ask:             getFloat(rec, "ask"),
			Volume:          getInt(rec, "volume"),
			OpenInterest:    getInt(rec, "open_interest"),
		}
		results = append(results, g)
	}

	return results, nil
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
