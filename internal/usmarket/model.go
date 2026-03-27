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
	Volume            uint32
	Transactions      uint32
}

// StockBar1m is a 1-minute bar for a US equity stock from Polygon SIP data.
type StockBar1m struct {
	Timestamp    time.Time
	Symbol       string
	Open         float32
	High         float32
	Low          float32
	Close        float32
	Volume       uint32
	Transactions uint32
}
