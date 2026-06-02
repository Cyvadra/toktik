package usmarket

import "time"

// OptionBar1m is a 1-minute bar for a US equity option from Polygon OPRA data.
type OptionBar1m struct {
	Timestamp         time.Time
	Symbol            string // full Polygon ticker, e.g. O:AAPL230120C00130000
	Underlying        string // extracted underlying, e.g. AAPL
	OptionType        string // "C" or "P"
	Expiration        time.Time
	Strike            float64 // dollar strike
	Open              float32
	High              float32
	Low               float32
	Close             float32
	UnderlyingClose   float32
	ImpliedVolatility float32
	Delta             float32
	Gamma             float32
	Vega              float32
	Theta             float32
	Rho               float32
	Volume            float64
	Transactions      uint32
	// Session metadata
	MarketDate       time.Time // trading date in America/New_York
	SessionKind      string    // premarket | regular | postmarket | closed
	IsRegularSession uint8
	SessionOpen      time.Time // regular session open (UTC) for this market_date
	SessionSeq       uint16    // minute index within regular session (0-based)
}

// StockBar1m is a 1-minute bar for a US equity stock from Polygon SIP data.
type StockBar1m struct {
	Timestamp    time.Time
	Symbol       string
	Open         float32
	High         float32
	Low          float32
	Close        float32
	Volume       float64
	Transactions uint32
	// Session metadata
	MarketDate       time.Time
	SessionKind      string
	IsRegularSession uint8
	SessionOpen      time.Time
	SessionSeq       uint16
}

// StockBar1d is a daily bar for a US equity stock from Polygon SIP day aggregates.
// Unlike 1m bars, daily bars have no session classification — each bar represents
// the full trading day. Timestamp is midnight UTC of the market_date.
type StockBar1d struct {
	Timestamp    time.Time
	Symbol       string
	Open         float32
	High         float32
	Low          float32
	Close        float32
	Volume       float64
	Transactions uint32
	MarketDate   time.Time
}

// OptionBar1d is a daily bar for a US equity option from Polygon OPRA day aggregates.
// Daily option bars have no session metadata. Greeks are zero initially; they can be
// enriched later via the same enrichment pipeline used for 1m bars.
type OptionBar1d struct {
	Timestamp         time.Time
	Symbol            string
	Underlying        string
	OptionType        string
	Expiration        time.Time
	Strike            float64
	Open              float32
	High              float32
	Low               float32
	Close             float32
	UnderlyingClose   float32
	ImpliedVolatility float32
	Delta             float32
	Gamma             float32
	Vega              float32
	Theta             float32
	Rho               float32
	Volume            float64
	Transactions      uint32
	MarketDate        time.Time
}

// StockSplit is a US stock split event used to front-adjust stock prices.
type StockSplit struct {
	Symbol      string
	SplitDate   time.Time
	Numerator   float64
	Denominator float64
	SplitType   string
	Source      string
	SourceHash  string
	UpdatedAt   time.Time
}
