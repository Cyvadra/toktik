package dto

import "time"

// FactorInfo describes one registered factor feed.
type FactorInfo struct {
	Name          string   `json:"name" example:"dvol"`
	Symbols       []string `json:"symbols" example:"BTC,ETH"`
	SourceWindows []string `json:"source_windows" example:"1h,12h,1d"`
	Fields        []string `json:"fields,omitempty"`
}

// FactorCatalogResponse wraps the list of available factor feeds.
type FactorCatalogResponse struct {
	Data []FactorInfo `json:"data"`
}

// FactorBarRequest defines query parameters for factor time-series data.
type FactorBarRequest struct {
	Name   string `form:"name" binding:"required"`
	Symbol string `form:"symbol" binding:"required"`
	Window string `form:"window" binding:"required"`
	From   string `form:"from" binding:"required"`
	To     string `form:"to" binding:"required"`
	Limit  int    `form:"limit" binding:"omitempty"`
	Cursor string `form:"cursor" binding:"omitempty"`
}

// FactorBarRow is a single OHLC bar for a factor feed.
type FactorBarRow struct {
	Timestamp time.Time `json:"timestamp"`
	Symbol    string    `json:"symbol"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
}

// FactorBarResponse wraps paginated factor bars.
type FactorBarResponse struct {
	Data       []FactorBarRow `json:"data"`
	NextCursor string         `json:"next_cursor,omitempty"`
}
