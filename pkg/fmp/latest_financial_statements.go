package fmp

import (
	"context"
	"fmt"
	"net/url"
)

// LatestFinancialStatement is one row returned by FMP's
// latest-financial-statements endpoint. The endpoint returns a wide statement
// payload; the incremental fundamentals sync only needs identity and filing
// recency fields for discovery.
type LatestFinancialStatement struct {
	Symbol       string `json:"symbol"`
	Date         string `json:"date"`
	FilingDate   string `json:"filingDate"`
	AcceptedDate string `json:"acceptedDate"`
	FiscalYear   string `json:"fiscalYear"`
	Period       string `json:"period"`
}

// LatestFinancialStatements returns the newest market-wide financial statement
// rows, paginated by FMP. limit <= 0 means the FMP default.
func (c *Client) LatestFinancialStatements(ctx context.Context, page, limit int) ([]LatestFinancialStatement, error) {
	params := url.Values{}
	if page > 0 {
		params.Set("page", fmt.Sprintf("%d", page))
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var out []LatestFinancialStatement
	if err := c.get(ctx, "/latest-financial-statements", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}
