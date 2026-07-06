package fmp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
)

// EarningsCalendarEntry is one row returned by FMP's earnings-calendar
// endpoint. Incremental fundamentals discovery only needs symbol recency.
type EarningsCalendarEntry struct {
	Symbol           string   `json:"symbol"`
	Date             string   `json:"date"`
	EPSActual        *float64 `json:"epsActual"`
	EPSEstimated     *float64 `json:"epsEstimated"`
	RevenueActual    *int64   `json:"revenueActual"`
	RevenueEstimated *int64   `json:"revenueEstimated"`
	LastUpdated      string   `json:"lastUpdated"`
}

func (e *EarningsCalendarEntry) UnmarshalJSON(data []byte) error {
	type wire struct {
		Symbol           string          `json:"symbol"`
		Date             string          `json:"date"`
		EPSActual        *float64        `json:"epsActual"`
		EPSEstimated     *float64        `json:"epsEstimated"`
		RevenueActual    json.RawMessage `json:"revenueActual"`
		RevenueEstimated json.RawMessage `json:"revenueEstimated"`
		LastUpdated      string          `json:"lastUpdated"`
	}
	var row wire
	if err := json.Unmarshal(data, &row); err != nil {
		return err
	}
	revenueActual, err := decodeFMPInt64Number(row.RevenueActual)
	if err != nil {
		return fmt.Errorf("revenueActual: %w", err)
	}
	revenueEstimated, err := decodeFMPInt64Number(row.RevenueEstimated)
	if err != nil {
		return fmt.Errorf("revenueEstimated: %w", err)
	}
	*e = EarningsCalendarEntry{
		Symbol:           row.Symbol,
		Date:             row.Date,
		EPSActual:        row.EPSActual,
		EPSEstimated:     row.EPSEstimated,
		RevenueActual:    revenueActual,
		RevenueEstimated: revenueEstimated,
		LastUpdated:      row.LastUpdated,
	}
	return nil
}

// SecFilingsFinancial is one row returned by FMP's sec-filings-financials
// endpoint. Incremental fundamentals discovery uses filing timestamps and the
// hasFinancials flag to decide whether a symbol likely needs a refresh.
type SecFilingsFinancial struct {
	Symbol        string `json:"symbol"`
	FilingDate    string `json:"filingDate"`
	AcceptedDate  string `json:"acceptedDate"`
	FormType      string `json:"formType"`
	HasFinancials bool   `json:"hasFinancials"`
}

// EarningsCalendar returns earnings events within the requested window.
func (c *Client) EarningsCalendar(ctx context.Context, from, to string) ([]EarningsCalendarEntry, error) {
	params := url.Values{}
	if from != "" {
		params.Set("from", from)
	}
	if to != "" {
		params.Set("to", to)
	}
	var out []EarningsCalendarEntry
	if err := c.get(ctx, "/earnings-calendar", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeFMPInt64Number(raw json.RawMessage) (*int64, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return nil, nil
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, err
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, fmt.Errorf("invalid number %v", number)
	}
	rounded := int64(math.Round(number))
	return &rounded, nil
}

// SecFilingsFinancials returns market-wide filing rows within the requested
// window, paginated by FMP. limit <= 0 means the FMP default.
func (c *Client) SecFilingsFinancials(ctx context.Context, from, to string, page, limit int) ([]SecFilingsFinancial, error) {
	params := url.Values{}
	if from != "" {
		params.Set("from", from)
	}
	if to != "" {
		params.Set("to", to)
	}
	if page > 0 {
		params.Set("page", fmt.Sprintf("%d", page))
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var out []SecFilingsFinancial
	if err := c.get(ctx, "/sec-filings-financials", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}
