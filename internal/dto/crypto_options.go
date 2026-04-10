package dto

import (
	"encoding/json"
	"fmt"
	"time"
)

// BarRequest is the query parameters for the bars endpoint.
type BarRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"required"`
	From     string `form:"from" binding:"required"`    // RFC3339 or "2006-01-02"
	To       string `form:"to" binding:"required"`      // RFC3339 or "2006-01-02"
	Limit    int    `form:"limit" binding:"omitempty"`  // max rows, default 1000
	Cursor   string `form:"cursor" binding:"omitempty"` // opaque cursor for pagination
}

// BarRow is a single OHLCV bar returned by the API.
type BarRow struct {
	Timestamp            time.Time `json:"timestamp"`
	SymbolID             uint64    `json:"symbol_id"`
	BaseAsset            string    `json:"base_asset"`
	MarkOpen             float32   `json:"mark_open"`
	MarkHigh             float32   `json:"mark_high"`
	MarkLow              float32   `json:"mark_low"`
	MarkClose            float32   `json:"mark_close"`
	LastOpen             float32   `json:"last_open"`
	LastHigh             float32   `json:"last_high"`
	LastLow              float32   `json:"last_low"`
	LastClose            float32   `json:"last_close"`
	BidOpen              float32   `json:"bid_open"`
	BidHigh              float32   `json:"bid_high"`
	BidLow               float32   `json:"bid_low"`
	BidClose             float32   `json:"bid_close"`
	AskOpen              float32   `json:"ask_open"`
	AskHigh              float32   `json:"ask_high"`
	AskLow               float32   `json:"ask_low"`
	AskClose             float32   `json:"ask_close"`
	MarkIVOpen           float32   `json:"mark_iv_open"`
	MarkIVClose          float32   `json:"mark_iv_close"`
	BidIVOpen            float32   `json:"bid_iv_open"`
	AskIVOpen            float32   `json:"ask_iv_open"`
	Delta                float32   `json:"delta"`
	Gamma                float32   `json:"gamma"`
	Vega                 float32   `json:"vega"`
	Theta                float32   `json:"theta"`
	Rho                  float32   `json:"rho"`
	UnderlyingPriceOpen  float32   `json:"underlying_price_open"`
	UnderlyingPriceHigh  float32   `json:"underlying_price_high"`
	UnderlyingPriceLow   float32   `json:"underlying_price_low"`
	UnderlyingPriceClose float32   `json:"underlying_price_close"`
	Volume               float64   `json:"volume"`
	OpenInterest         float32   `json:"open_interest"`
	TickCount            uint16    `json:"tick_count"`
}

// BarResponse wraps paginated bar results.
type BarResponse struct {
	Data       []BarRow `json:"data"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// SymbolRequest is the query parameters for the symbols endpoint.
type SymbolRequest struct {
	Search    string `form:"search" binding:"omitempty"`     // substring match
	BaseAsset string `form:"base_asset" binding:"omitempty"` // filter by base asset
	Limit     int    `form:"limit" binding:"omitempty"`      // max rows, default 100
	Cursor    string `form:"cursor" binding:"omitempty"`     // opaque cursor
}

// SymbolRow is a single option symbol returned by the API.
type SymbolRow struct {
	SymbolID        uint64    `json:"symbol_id"`
	Symbol          string    `json:"symbol"`
	BaseAsset       string    `json:"base_asset"`
	OptionType      string    `json:"option_type"`
	StrikePrice     float32   `json:"strike_price"`
	Expiration      time.Time `json:"expiration"`
	UnderlyingIndex string    `json:"underlying_index"`
}

// SymbolResponse wraps paginated symbol results.
type SymbolResponse struct {
	Data       []SymbolRow `json:"data"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// GreeksRequest is the query parameters for the greeks endpoint.
type GreeksRequest struct {
	Symbol   string `form:"symbol" binding:"required"`
	Interval string `form:"interval" binding:"omitempty"` // default "1m"
	From     string `form:"from" binding:"required"`
	To       string `form:"to" binding:"required"`
	Limit    int    `form:"limit" binding:"omitempty"`
	Cursor   string `form:"cursor" binding:"omitempty"`
}

// GreeksRow is a single greeks snapshot returned by the API.
type GreeksRow struct {
	Timestamp            time.Time `json:"timestamp"`
	SymbolID             uint64    `json:"symbol_id"`
	Delta                float32   `json:"delta"`
	Gamma                float32   `json:"gamma"`
	Vega                 float32   `json:"vega"`
	Theta                float32   `json:"theta"`
	Rho                  float32   `json:"rho"`
	MarkIVOpen           float32   `json:"mark_iv_open"`
	MarkIVClose          float32   `json:"mark_iv_close"`
	UnderlyingPriceOpen  float32   `json:"underlying_price_open"`
	UnderlyingPriceHigh  float32   `json:"underlying_price_high"`
	UnderlyingPriceLow   float32   `json:"underlying_price_low"`
	UnderlyingPriceClose float32   `json:"underlying_price_close"`
	OpenInterest         float32   `json:"open_interest"`
}

// GreeksResponse wraps paginated greeks results.
type GreeksResponse struct {
	Data       []GreeksRow `json:"data"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// BacktestRequest is the JSON body for the backtest endpoint.
type BacktestRequest struct {
	Symbol          string          `json:"symbol" binding:"required"`
	Interval        string          `json:"interval" binding:"required"`
	From            string          `json:"from" binding:"required"`
	To              string          `json:"to" binding:"required"`
	Capital         *float64        `json:"capital,omitempty"`
	Strategy        string          `json:"strategy,omitempty"`
	CommissionModel string          `json:"commission_model,omitempty"`
	CommissionValue *float64        `json:"commission_value,omitempty"`
	SlippagePct     *float64        `json:"slippage_pct,omitempty"`
	FillMode        string          `json:"fill_mode,omitempty"`
	ValuationMode   string          `json:"valuation_mode,omitempty"`
	TriggerMode     string          `json:"trigger_mode,omitempty"`
	Params          json.RawMessage `json:"params,omitempty"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ValidationError indicates a client-provided input is invalid (HTTP 400).
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// NewValidationError creates a ValidationError with a formatted message.
func NewValidationError(format string, a ...interface{}) *ValidationError {
	return &ValidationError{Message: fmt.Sprintf(format, a...)}
}

// ParseTimeRange parses from/to strings in RFC3339 or date-only format.
func ParseTimeRange(from, to string) (time.Time, time.Time, error) {
	fromT, err := parseFlexibleTime(from)
	if err != nil {
		return time.Time{}, time.Time{}, NewValidationError("invalid 'from': %v", err)
	}
	toT, err := parseFlexibleTime(to)
	if err != nil {
		return time.Time{}, time.Time{}, NewValidationError("invalid 'to': %v", err)
	}
	if !fromT.Before(toT) {
		return time.Time{}, time.Time{}, NewValidationError("'from' must be before 'to'")
	}
	return fromT, toT, nil
}

func parseFlexibleTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD, got %q", s)
}
