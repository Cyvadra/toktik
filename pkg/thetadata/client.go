package thetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Client provides high-level methods for querying the Theta Data API
// through the MCP transport.
type Client struct {
	mcp       *MCPClient
	rateLimit <-chan time.Time // rate limiter ticker
}

// NewClient creates a new Theta Data API client with the given MCP transport
// and request rate limit (requests per second). Pass 0 for no limit.
func NewClient(mcp *MCPClient, reqPerSec float64) *Client {
	c := &Client{mcp: mcp}
	if reqPerSec > 0 {
		interval := time.Duration(float64(time.Second) / reqPerSec)
		ticker := time.NewTicker(interval)
		c.rateLimit = ticker.C
	}
	return c
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

// ListExpirations returns all available option expirations for a root symbol.
func (c *Client) ListExpirations(ctx context.Context, root string) ([]time.Time, error) {
	raw, err := c.callTool(ctx, "option_list_expirations", map[string]any{
		"symbol": root,
	})
	if err != nil {
		return nil, fmt.Errorf("list expirations for %s: %w", root, err)
	}

	// Response format: {"response": [{"expiration": "2025-01-17"}, ...]}
	// or: {"response": ["2025-01-17", ...]}
	var resp thetaResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse expirations response: %w", err)
	}

	// Try array of objects with "expiration" field
	var expirations []time.Time
	var items []map[string]any
	if err := json.Unmarshal(resp.Response, &items); err == nil {
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
	if err := json.Unmarshal(resp.Response, &dateStrs); err == nil {
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
	raw, err := c.callTool(ctx, "option_list_strikes", map[string]any{
		"symbol":     root,
		"expiration": exp.Format("2006-01-02"),
	})
	if err != nil {
		return nil, fmt.Errorf("list strikes for %s exp %s: %w", root, exp.Format("2006-01-02"), err)
	}

	var resp thetaResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse strikes response: %w", err)
	}

	// Try array of numbers
	var strikes []float64
	if err := json.Unmarshal(resp.Response, &strikes); err != nil {
		// Try array of objects with "strike" field
		var items []map[string]any
		if err := json.Unmarshal(resp.Response, &items); err != nil {
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
	raw, err := c.callTool(ctx, "option_list_dates", map[string]any{
		"symbol":     contract.Root,
		"expiration": contract.Expiration.Format("2006-01-02"),
		"strike":     contract.Strike,
		"right":      contract.Right,
	})
	if err != nil {
		return nil, fmt.Errorf("list dates for %s: %w", contract.Symbol(), err)
	}

	var resp thetaResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse dates response: %w", err)
	}

	var dates []time.Time
	var items []map[string]any
	if err := json.Unmarshal(resp.Response, &items); err == nil {
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
		if err := json.Unmarshal(resp.Response, &dateStrs); err == nil {
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
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
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

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func parseQuoteBars(raw string, refDate time.Time) ([]QuoteBar, error) {
	var resp thetaResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
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
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
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
			Volume:    getInt(rec, "volume"),
			Count:     getInt(rec, "count"),
		}
		bars = append(bars, bar)
	}

	return bars, nil
}

func parseGreeksEOD(raw string) ([]GreeksEOD, error) {
	var resp thetaResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
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
