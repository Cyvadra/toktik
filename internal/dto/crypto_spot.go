package dto

import "time"

// CryptoSpotBarRequest is the query parameters for the crypto spot bars endpoint.
type CryptoSpotBarRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"required"`
	From     string `form:"from" binding:"required"`
	To       string `form:"to" binding:"required"`
	Limit    int    `form:"limit" binding:"omitempty"`
	Cursor   string `form:"cursor" binding:"omitempty"`
}

// CryptoSpotBarRow is a single OHLCV bar for a crypto spot/underlying asset.
type CryptoSpotBarRow struct {
	Timestamp time.Time `json:"timestamp"`
	Symbol    string    `json:"symbol"`
	Open      float32   `json:"open"`
	High      float32   `json:"high"`
	Low       float32   `json:"low"`
	Close     float32   `json:"close"`
	Volume    float64   `json:"volume"`
	TickCount uint64    `json:"tick_count"`
}

// CryptoSpotBarResponse wraps paginated crypto spot bars.
type CryptoSpotBarResponse struct {
	Data       []CryptoSpotBarRow `json:"data"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// CryptoSpotSymbolRequest is the query parameters for the crypto spot symbols endpoint.
type CryptoSpotSymbolRequest struct {
	Search string `form:"search" binding:"omitempty"`
	Limit  int    `form:"limit" binding:"omitempty"`
	Cursor string `form:"cursor" binding:"omitempty"`
}

// CryptoSpotSymbolRow describes one crypto spot symbol.
type CryptoSpotSymbolRow struct {
	Symbol string `json:"symbol"`
}

// CryptoSpotSymbolResponse wraps paginated crypto spot symbols.
type CryptoSpotSymbolResponse struct {
	Data       []CryptoSpotSymbolRow `json:"data"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

// CryptoOptionChainRequest is the query parameters for the crypto option chain endpoint.
type CryptoOptionChainRequest struct {
	BaseAsset string `form:"base_asset" binding:"required"`
	From      string `form:"from" binding:"required"`
	To        string `form:"to" binding:"required"`
	Interval  string `form:"interval" binding:"omitempty"`
	Limit     int    `form:"limit" binding:"omitempty"`
	Cursor    string `form:"cursor" binding:"omitempty"`
}

// CryptoOptionChainContract is one contract inside a crypto option chain snapshot.
type CryptoOptionChainContract struct {
	SymbolID        uint64    `json:"symbol_id"`
	Symbol          string    `json:"symbol"`
	OptionType      string    `json:"option_type"`
	Expiration      time.Time `json:"expiration"`
	Strike          float32   `json:"strike"`
	MarkClose       float32   `json:"mark_close"`
	BidClose        float32   `json:"bid_close"`
	AskClose        float32   `json:"ask_close"`
	MarkIV          float32   `json:"mark_iv"`
	Delta           float32   `json:"delta"`
	Gamma           float32   `json:"gamma"`
	Vega            float32   `json:"vega"`
	Theta           float32   `json:"theta"`
	Rho             float32   `json:"rho"`
	Volume          float64   `json:"volume"`
	OpenInterest    float32   `json:"open_interest"`
	TickCount       uint16    `json:"tick_count"`
	UnderlyingClose float32   `json:"underlying_close"`
}

// CryptoOptionChainSnapshot is a chain snapshot for one timestamp.
type CryptoOptionChainSnapshot struct {
	Timestamp time.Time                   `json:"timestamp"`
	BaseAsset string                      `json:"base_asset"`
	Contracts []CryptoOptionChainContract `json:"contracts"`
}

// CryptoOptionChainResponse wraps paginated crypto chain snapshots.
type CryptoOptionChainResponse struct {
	Data       []CryptoOptionChainSnapshot `json:"data"`
	NextCursor string                      `json:"next_cursor,omitempty"`
}

// UnderlyingInfo describes one underlying asset.
type UnderlyingInfo struct {
	Symbol              string   `json:"symbol"`
	LastPrice           *float64 `json:"last_price,omitempty"`
	Change24hPercent    *float64 `json:"change_24h_percent,omitempty"`
	Volume24h           *float64 `json:"volume_24h,omitempty"`
	ActiveContractCount *int     `json:"active_contract_count,omitempty"`
}

// UnderlyingListRequest is the query for listing underlyings in a market.
type UnderlyingListRequest struct {
	Limit  int    `form:"limit" binding:"omitempty"`
	Cursor string `form:"cursor" binding:"omitempty"`
}

// UnderlyingListResponse wraps paginated underlying info.
type UnderlyingListResponse struct {
	Market     string           `json:"market"`
	Data       []UnderlyingInfo `json:"data"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
