package dto

import "time"

// ForexBarRequest is the query parameters for the forex bars endpoint.
type ForexBarRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"required"`
	From     string `form:"from" binding:"required"`
	To       string `form:"to" binding:"required"`
	Limit    int    `form:"limit" binding:"omitempty"`
	Cursor   string `form:"cursor" binding:"omitempty"`
}

// ForexBarRow is a single OHLCV bar returned by the forex bars endpoint.
type ForexBarRow struct {
	Timestamp    time.Time `json:"timestamp"`
	Symbol       string    `json:"symbol"`
	Open         float32   `json:"open"`
	High         float32   `json:"high"`
	Low          float32   `json:"low"`
	Close        float32   `json:"close"`
	Volume       float64   `json:"volume"`
	Transactions uint64    `json:"transactions"`
}

// ForexBarResponse wraps paginated forex bars.
type ForexBarResponse struct {
	Data       []ForexBarRow `json:"data"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// ForexSymbolRequest is the query parameters for the forex symbols endpoint.
type ForexSymbolRequest struct {
	Search string `form:"search" binding:"omitempty"`
	Limit  int    `form:"limit" binding:"omitempty"`
	Cursor string `form:"cursor" binding:"omitempty"`
}

// ForexSymbolRow describes one forex symbol.
type ForexSymbolRow struct {
	Symbol string `json:"symbol"`
}

// ForexSymbolResponse wraps paginated forex symbols.
type ForexSymbolResponse struct {
	Data       []ForexSymbolRow `json:"data"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
