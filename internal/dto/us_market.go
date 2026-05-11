package dto

import "time"

// USStockBarRequest is the query parameters for the US stock bars endpoint.
type USStockBarRequest struct {
	Symbol   string   `form:"symbol" binding:"required"`
	Interval string   `form:"interval" binding:"required"`
	From     string   `form:"from" binding:"required"`
	To       string   `form:"to" binding:"required"`
	Session  string   `form:"session" binding:"omitempty"`
	Factors  []string `form:"factor" binding:"omitempty"`
	Limit    int      `form:"limit" binding:"omitempty"`
	Cursor   string   `form:"cursor" binding:"omitempty"`
}

// USStockBarFundamentalValue is one point-in-time factor aligned onto a bar.
type USStockBarFundamentalValue struct {
	EventTS time.Time `json:"event_ts"`
	KnownAt time.Time `json:"known_at"`
	Value   float64   `json:"value"`
	Source  string    `json:"source,omitempty"`
	Filled  bool      `json:"filled,omitempty"`
}

// USStockBarRow is a single OHLCV bar returned by the US stock bars endpoint.
type USStockBarRow struct {
	Timestamp    time.Time                             `json:"timestamp"`
	Symbol       string                                `json:"symbol"`
	Open         float32                               `json:"open"`
	High         float32                               `json:"high"`
	Low          float32                               `json:"low"`
	Close        float32                               `json:"close"`
	Volume       float64                               `json:"volume"`
	Transactions uint64                                `json:"transactions"`
	Fundamentals map[string]USStockBarFundamentalValue `json:"fundamentals,omitempty"`
}

// USStockCompanyProfile describes cached company-classification metadata.
type USStockCompanyProfile struct {
	Symbol   string `json:"symbol"`
	Sector   string `json:"sector,omitempty"`
	Industry string `json:"industry,omitempty"`
}

// USStockBarMeta wraps optional metadata returned alongside bars.
type USStockBarMeta struct {
	Profile *USStockCompanyProfile `json:"profile,omitempty"`
}

// USStockBarResponse wraps paginated US stock bars.
type USStockBarResponse struct {
	Data       []USStockBarRow `json:"data"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Meta       *USStockBarMeta `json:"meta,omitempty"`
}

// USStockSymbolRequest is the query parameters for the US stock symbols endpoint.
type USStockSymbolRequest struct {
	Search string `form:"search" binding:"omitempty"`
	Limit  int    `form:"limit" binding:"omitempty"`
	Cursor string `form:"cursor" binding:"omitempty"`
}

// USStockSymbolRow describes one US stock symbol.
type USStockSymbolRow struct {
	Symbol  string                 `json:"symbol"`
	Profile *USStockCompanyProfile `json:"profile,omitempty"`
}

// USStockSymbolResponse wraps paginated US stock symbols.
type USStockSymbolResponse struct {
	Data       []USStockSymbolRow `json:"data"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// USOptionBarRequest is the query parameters for the US option bars endpoint.
type USOptionBarRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"required"`
	From     string `form:"from" binding:"required"`
	To       string `form:"to" binding:"required"`
	Session  string `form:"session" binding:"omitempty"`
	Limit    int    `form:"limit" binding:"omitempty"`
	Cursor   string `form:"cursor" binding:"omitempty"`
}

// USOptionBarRow is a single OHLCV bar returned by the US option bars endpoint.
type USOptionBarRow struct {
	Timestamp         time.Time `json:"timestamp"`
	Symbol            string    `json:"symbol"`
	Underlying        string    `json:"underlying"`
	OptionType        string    `json:"option_type"`
	Expiration        time.Time `json:"expiration"`
	Strike            float64   `json:"strike"`
	Open              float32   `json:"open"`
	High              float32   `json:"high"`
	Low               float32   `json:"low"`
	Close             float32   `json:"close"`
	UnderlyingClose   float32   `json:"underlying_close"`
	ImpliedVolatility float32   `json:"implied_volatility"`
	Delta             float32   `json:"delta"`
	Gamma             float32   `json:"gamma"`
	Vega              float32   `json:"vega"`
	Theta             float32   `json:"theta"`
	Rho               float32   `json:"rho"`
	Volume            float64   `json:"volume"`
	Transactions      uint64    `json:"transactions"`
}

// USOptionBarResponse wraps paginated US option bars.
type USOptionBarResponse struct {
	Data       []USOptionBarRow `json:"data"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// USOptionSymbolRequest is the query parameters for the US option symbols endpoint.
type USOptionSymbolRequest struct {
	Root       string `form:"root" binding:"omitempty"`
	Underlying string `form:"underlying" binding:"omitempty"`
	Search     string `form:"search" binding:"omitempty"`
	Limit      int    `form:"limit" binding:"omitempty"`
	Cursor     string `form:"cursor" binding:"omitempty"`
}

// USOptionSymbolRow describes one US option contract.
type USOptionSymbolRow struct {
	Symbol     string    `json:"symbol"`
	Underlying string    `json:"underlying"`
	OptionType string    `json:"option_type"`
	Expiration time.Time `json:"expiration"`
	Strike     float64   `json:"strike"`
}

// USOptionSymbolResponse wraps paginated US option symbols.
type USOptionSymbolResponse struct {
	Data       []USOptionSymbolRow `json:"data"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// USOptionGreeksRequest is the query parameters for the US option greeks endpoint.
type USOptionGreeksRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"omitempty"`
	From     string `form:"from" binding:"required"`
	To       string `form:"to" binding:"required"`
	Session  string `form:"session" binding:"omitempty"`
	Limit    int    `form:"limit" binding:"omitempty"`
	Cursor   string `form:"cursor" binding:"omitempty"`
}

// USOptionGreeksRow is a single greeks snapshot for a US option contract.
type USOptionGreeksRow struct {
	Timestamp         time.Time `json:"timestamp"`
	Symbol            string    `json:"symbol"`
	Underlying        string    `json:"underlying"`
	OptionType        string    `json:"option_type"`
	Expiration        time.Time `json:"expiration"`
	Strike            float64   `json:"strike"`
	UnderlyingClose   float32   `json:"underlying_close"`
	ImpliedVolatility float32   `json:"implied_volatility"`
	Delta             float32   `json:"delta"`
	Gamma             float32   `json:"gamma"`
	Vega              float32   `json:"vega"`
	Theta             float32   `json:"theta"`
	Rho               float32   `json:"rho"`
	Volume            float64   `json:"volume"`
	Transactions      uint64    `json:"transactions"`
}

// USOptionGreeksResponse wraps paginated greeks rows.
type USOptionGreeksResponse struct {
	Data       []USOptionGreeksRow `json:"data"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// USOptionChainRequest is the query parameters for the US option chain endpoint.
type USOptionChainRequest struct {
	Underlying string `form:"underlying" binding:"required"`
	Expiration string `form:"expiration" binding:"omitempty"`
	From       string `form:"from" binding:"omitempty"`
	To         string `form:"to" binding:"omitempty"`
	Interval   string `form:"interval" binding:"omitempty"`
	Limit      int    `form:"limit" binding:"omitempty"`
	Cursor     string `form:"cursor" binding:"omitempty"`
}

// USOptionChainContract is one contract inside a chain snapshot.
type USOptionChainContract struct {
	Symbol            string    `json:"symbol"`
	OptionType        string    `json:"option_type"`
	Expiration        time.Time `json:"expiration"`
	Strike            float64   `json:"strike"`
	Close             float32   `json:"close"`
	UnderlyingClose   float32   `json:"underlying_close"`
	ImpliedVolatility float32   `json:"implied_volatility"`
	Delta             float32   `json:"delta"`
	Gamma             float32   `json:"gamma"`
	Vega              float32   `json:"vega"`
	Theta             float32   `json:"theta"`
	Rho               float32   `json:"rho"`
	Volume            float64   `json:"volume"`
	Transactions      uint64    `json:"transactions"`
}

// USOptionChainSnapshot is a chain snapshot for one timestamp.
type USOptionChainSnapshot struct {
	Timestamp  time.Time               `json:"timestamp"`
	Underlying string                  `json:"underlying"`
	Contracts  []USOptionChainContract `json:"contracts"`
}

// USOptionChainResponse wraps paginated chain snapshots.
type USOptionChainResponse struct {
	Data       []USOptionChainSnapshot `json:"data"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}
