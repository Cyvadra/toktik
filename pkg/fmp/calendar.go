package fmp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// EarningsCalendarEvent is one row from /stable/earnings-calendar.
type EarningsCalendarEvent = EarningsCalendarEntry

// DividendsCalendarEvent is one row from /stable/dividends-calendar.
type DividendsCalendarEvent = DividendEvent

// IPOCalendarEvent is one row from /stable/ipos-calendar.
type IPOCalendarEvent struct {
	Symbol     string   `json:"symbol"`
	Date       string   `json:"date"`
	DAA        string   `json:"daa"`
	Company    string   `json:"company"`
	Exchange   string   `json:"exchange"`
	Actions    string   `json:"actions"`
	Shares     *float64 `json:"shares"`
	PriceRange string   `json:"priceRange"`
	MarketCap  *float64 `json:"marketCap"`
}

// SplitsCalendarEvent is one row from /stable/splits-calendar.
type SplitsCalendarEvent struct {
	Symbol      string  `json:"symbol"`
	Date        string  `json:"date"`
	Numerator   float64 `json:"numerator"`
	Denominator float64 `json:"denominator"`
	SplitType   string  `json:"splitType"`
}

// StockSplit is one row from /stable/splits for a symbol.
type StockSplit struct {
	Symbol      string  `json:"symbol"`
	Date        string  `json:"date"`
	Numerator   float64 `json:"numerator"`
	Denominator float64 `json:"denominator"`
	SplitType   string  `json:"splitType"`
}

// FinancialReportDate is one row from /stable/financial-reports-dates.
type FinancialReportDate struct {
	Symbol     string `json:"symbol"`
	FiscalYear int    `json:"fiscalYear"`
	Period     string `json:"period"`
	LinkJSON   string `json:"linkJson"`
	LinkXLSX   string `json:"linkXlsx"`
}

func (c *Client) DividendsCalendar(ctx context.Context, from, to string) ([]DividendsCalendarEvent, error) {
	params := dateRangeParams(from, to)
	var out []DividendsCalendarEvent
	if err := c.get(ctx, "/dividends-calendar", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) IPOsCalendar(ctx context.Context, from, to string) ([]IPOCalendarEvent, error) {
	params := dateRangeParams(from, to)
	var out []IPOCalendarEvent
	if err := c.get(ctx, "/ipos-calendar", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SplitsCalendar(ctx context.Context, from, to string) ([]SplitsCalendarEvent, error) {
	params := dateRangeParams(from, to)
	var out []SplitsCalendarEvent
	if err := c.get(ctx, "/splits-calendar", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Splits(ctx context.Context, symbol string) ([]StockSplit, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	var out []StockSplit
	if err := c.get(ctx, "/splits", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) FinancialReportDates(ctx context.Context, symbol string) ([]FinancialReportDate, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	var out []FinancialReportDate
	if err := c.get(ctx, "/financial-reports-dates", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) FinancialReportJSON(ctx context.Context, symbol string, year int, period string) (json.RawMessage, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if year > 0 {
		params.Set("year", strconv.Itoa(year))
	}
	if period != "" {
		params.Set("period", period)
	}
	var out json.RawMessage
	if err := c.get(ctx, "/financial-reports-json", params, &out); err != nil {
		return nil, fmt.Errorf("financial reports json: %w", err)
	}
	return out, nil
}

func dateRangeParams(from, to string) url.Values {
	params := url.Values{}
	if from != "" {
		params.Set("from", from)
	}
	if to != "" {
		params.Set("to", to)
	}
	return params
}
