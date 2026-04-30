package fmp

import (
	"context"
	"fmt"
	"net/url"
)

// EarningsEvent is one row from the FMP earnings calendar / history endpoint.
// epsActual and revenueActual are null for upcoming events; use *float64 / *int64.
type EarningsEvent struct {
	Symbol           string   `json:"symbol"`
	Date             string   `json:"date"`
	EPSActual        *float64 `json:"epsActual"`
	EPSEstimated     *float64 `json:"epsEstimated"`
	RevenueActual    *int64   `json:"revenueActual"`
	RevenueEstimated *int64   `json:"revenueEstimated"`
	LastUpdated      string   `json:"lastUpdated"`
}

// Earnings returns earnings calendar entries for a symbol, newest first.
// limit <= 0 means the FMP default (typically 10).
func (c *Client) Earnings(ctx context.Context, symbol string, limit int) ([]EarningsEvent, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var out []EarningsEvent
	if err := c.get(ctx, "/earnings", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DividendEvent is one row from the FMP dividends endpoint.
type DividendEvent struct {
	Symbol          string  `json:"symbol"`
	Date            string  `json:"date"`
	RecordDate      string  `json:"recordDate"`
	PaymentDate     string  `json:"paymentDate"`
	DeclarationDate string  `json:"declarationDate"`
	AdjDividend     float64 `json:"adjDividend"`
	Dividend        float64 `json:"dividend"`
	Yield           float64 `json:"yield"`
	Frequency       string  `json:"frequency"`
}

// Dividends returns dividend history for a symbol, newest first.
// limit <= 0 means the FMP default.
func (c *Client) Dividends(ctx context.Context, symbol string, limit int) ([]DividendEvent, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var out []DividendEvent
	if err := c.get(ctx, "/dividends", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}
