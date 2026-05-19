package fmp

import (
	"context"
	"fmt"
	"net/url"
)

// EarningsCalendarEntry is one row returned by FMP's earnings-calendar
// endpoint. Incremental fundamentals discovery only needs symbol recency.
type EarningsCalendarEntry struct {
	Symbol      string `json:"symbol"`
	Date        string `json:"date"`
	LastUpdated string `json:"lastUpdated"`
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
