package thetadata

import "time"

// SyncConfig holds all configuration for the sync pipeline.
type SyncConfig struct {
	Roots       []string
	StartDate   time.Time
	EndDate     time.Time
	Mode        string
	BaseURL     string
	CHDSN       string
	Workers     int
	RateLimit   float64
	ProgressDir string
	SchemaFile  string
	Debug       bool
}

// Contract identifies an option contract.
type Contract struct {
	Symbol     string  `json:"symbol"`
	Expiration string  `json:"expiration"` // YYYY-MM-DD
	Strike     float64 `json:"strike"`
	Right      string  `json:"right"` // "call" or "put"
}

// EODRow represents a single row from the /option/history/eod endpoint.
type EODRow struct {
	Symbol     string  `json:"symbol"`
	Expiration string  `json:"expiration"`
	Strike     float64 `json:"strike"`
	Right      string  `json:"right"`
	Created    string  `json:"created"`
	LastTrade  string  `json:"last_trade"`
	Open       float64 `json:"open"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Close      float64 `json:"close"`
	Volume     int     `json:"volume"`
	Count      int     `json:"count"`
	BidSize    int     `json:"bid_size"`
	Bid        float64 `json:"bid"`
	AskSize    int     `json:"ask_size"`
	Ask        float64 `json:"ask"`
}

// GreeksEODRow represents a single row from /option/history/greeks/eod.
type GreeksEODRow struct {
	Symbol          string  `json:"symbol"`
	Expiration      string  `json:"expiration"`
	Strike          float64 `json:"strike"`
	Right           string  `json:"right"`
	Timestamp       string  `json:"timestamp"`
	Open            float64 `json:"open"`
	High            float64 `json:"high"`
	Low             float64 `json:"low"`
	Close           float64 `json:"close"`
	Volume          int     `json:"volume"`
	Count           int     `json:"count"`
	Bid             float64 `json:"bid"`
	Ask             float64 `json:"ask"`
	Delta           float64 `json:"delta"`
	Theta           float64 `json:"theta"`
	Vega            float64 `json:"vega"`
	Rho             float64 `json:"rho"`
	Gamma           float64 `json:"gamma"`
	ImpliedVol      float64 `json:"implied_vol"`
	UnderlyingPrice float64 `json:"underlying_price"`
}

// OpenInterestRow from /option/history/open_interest.
type OpenInterestRow struct {
	Symbol     string  `json:"symbol"`
	Expiration string  `json:"expiration"`
	Strike     float64 `json:"strike"`
	Right      string  `json:"right"`
	Timestamp  string  `json:"timestamp"`
	OI         int     `json:"open_interest"`
}

// QuoteRow from /option/history/quote.
type QuoteRow struct {
	Symbol     string  `json:"symbol"`
	Expiration string  `json:"expiration"`
	Strike     float64 `json:"strike"`
	Right      string  `json:"right"`
	Timestamp  string  `json:"timestamp"`
	BidSize    int     `json:"bid_size"`
	Bid        float64 `json:"bid"`
	AskSize    int     `json:"ask_size"`
	Ask        float64 `json:"ask"`
}

// OHLCRow from /option/history/ohlc.
type OHLCRow struct {
	Symbol     string  `json:"symbol"`
	Expiration string  `json:"expiration"`
	Strike     float64 `json:"strike"`
	Right      string  `json:"right"`
	Timestamp  string  `json:"timestamp"`
	Open       float64 `json:"open"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Close      float64 `json:"close"`
	Volume     int     `json:"volume"`
	Count      int     `json:"count"`
}

// SyncState persists the status of a (root, date) processing unit.
type SyncState struct {
	Root        string `json:"root"`
	Date        string `json:"date"`
	Status      string `json:"status"` // "started", "completed", "failed"
	Attempt     int    `json:"attempt"`
	Bars        int    `json:"bars"`
	Error       string `json:"error,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// DateTask is a unit of work for the pipeline worker pool.
type DateTask struct {
	Root string
	Date time.Time
}
