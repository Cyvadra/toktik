package dto

import "time"

// USStockBarRequest is the query parameters for the US stock bars endpoint.
type USStockBarRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"required"`
	From     string `form:"from" binding:"required"`
	To       string `form:"to" binding:"required"`
	Limit    int    `form:"limit" binding:"omitempty"`
	Cursor   string `form:"cursor" binding:"omitempty"`
}

// USStockBarRow is a single OHLCV bar for a US stock.
type USStockBarRow struct {
	Timestamp    time.Time `json:"timestamp"`
	Symbol       string    `json:"symbol"`
	Open         float32   `json:"open"`
	High         float32   `json:"high"`
	Low          float32   `json:"low"`
	Close        float32   `json:"close"`
	Volume       uint64    `json:"volume"`
	Transactions uint64    `json:"transactions"`
}

// USStockBarResponse wraps paginated US stock bar results.
type USStockBarResponse struct {
	Data       []USStockBarRow `json:"data"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// USOptionBarRequest is the query parameters for the US option bars endpoint.
type USOptionBarRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"required"`
	From     string `form:"from" binding:"required"`
	To       string `form:"to" binding:"required"`
	Limit    int    `form:"limit" binding:"omitempty"`
	Cursor   string `form:"cursor" binding:"omitempty"`
}

// USOptionBarRow is a single bar for a US option contract.
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
	Volume            uint64    `json:"volume"`
	Transactions      uint64    `json:"transactions"`
}

// USOptionBarResponse wraps paginated US option bar results.
type USOptionBarResponse struct {
	Data       []USOptionBarRow `json:"data"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// USOptionChainRequest is the query parameters for the US option chain endpoint.
type USOptionChainRequest struct {
	Underlying string `form:"underlying" binding:"required"`
	Interval   string `form:"interval" binding:"required"`
	From       string `form:"from" binding:"required"`
	To         string `form:"to" binding:"required"`
	Limit      int    `form:"limit" binding:"omitempty"`
	Cursor     string `form:"cursor" binding:"omitempty"`
}

// USOptionChainRow is a single timestamped snapshot of the option chain for an underlying.
type USOptionChainRow struct {
	Timestamp          time.Time   `json:"timestamp"`
	Underlying         string      `json:"underlying"`
	Symbols            []string    `json:"symbols"`
	OptionTypes        []string    `json:"option_types"`
	Expirations        []time.Time `json:"expirations"`
	Strikes            []float64   `json:"strikes"`
	ClosePrices        []float32   `json:"close_prices"`
	UnderlyingCloses   []float32   `json:"underlying_closes"`
	ImpliedVolatilities []float32  `json:"implied_volatilities"`
	Deltas             []float32   `json:"deltas"`
	Gammas             []float32   `json:"gammas"`
	Vegas              []float32   `json:"vegas"`
	Thetas             []float32   `json:"thetas"`
	Rhos               []float32   `json:"rhos"`
	Volumes            []uint64    `json:"volumes"`
	Transactions       []uint64    `json:"transactions"`
}

// USOptionChainResponse wraps paginated US option chain results.
type USOptionChainResponse struct {
	Data       []USOptionChainRow `json:"data"`
	NextCursor string             `json:"next_cursor,omitempty"`
}
