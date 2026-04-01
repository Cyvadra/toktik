package cryptooptions

import "time"

// TickRow represents a single raw tick from the OPTIONS.csv file.
// All numeric fields use float32 per project requirements (max precision).
// Zero value indicates missing data in the source CSV.
type TickRow struct {
	Exchange        string
	Symbol          string
	Timestamp       time.Time // parsed from microsecond epoch
	LocalTimestamp  time.Time
	OptionType      string // "call" or "put"
	StrikePrice     float32
	Expiration      time.Time // parsed from microsecond epoch
	OpenInterest    float32
	LastPrice       float32
	BidPrice        float32
	BidAmount       float32
	BidIV           float32
	AskPrice        float32
	AskAmount       float32
	AskIV           float32
	MarkPrice       float32
	MarkIV          float32
	UnderlyingIndex string
	UnderlyingPrice float32
	Delta           float32
	Gamma           float32
	Vega            float32
	Theta           float32
	Rho             float32
}

// Bar1m is a 1-minute aggregated bar for a single crypto option symbol.
// This struct is used as the Parquet row schema and maps directly to
// the crypto_options_bar_1m ClickHouse table.
type Bar1m struct {
	// Key / identity columns (written to Parquet for self-contained files)
	Timestamp  time.Time `parquet:"timestamp,timestamp(millisecond)"`
	SymbolID   uint32    `parquet:"symbol_id"`
	Symbol     string    `parquet:"symbol"`
	BaseAsset  string    `parquet:"base_asset"`
	OptionType string    `parquet:"option_type"`

	// Metadata carried in Parquet for symbol_meta extraction
	StrikePrice     float32   `parquet:"strike_price"`
	Expiration      time.Time `parquet:"expiration,timestamp(millisecond)"`
	UnderlyingIndex string    `parquet:"underlying_index"`

	// Mark price OHLC
	MarkOpen  float32 `parquet:"mark_open"`
	MarkHigh  float32 `parquet:"mark_high"`
	MarkLow   float32 `parquet:"mark_low"`
	MarkClose float32 `parquet:"mark_close"`

	// Last price OHLC
	LastOpen  float32 `parquet:"last_open"`
	LastHigh  float32 `parquet:"last_high"`
	LastLow   float32 `parquet:"last_low"`
	LastClose float32 `parquet:"last_close"`

	// Bid/Ask OHLC
	BidOpen  float32 `parquet:"bid_open"`
	BidHigh  float32 `parquet:"bid_high"`
	BidLow   float32 `parquet:"bid_low"`
	BidClose float32 `parquet:"bid_close"`
	AskOpen  float32 `parquet:"ask_open"`
	AskHigh  float32 `parquet:"ask_high"`
	AskLow   float32 `parquet:"ask_low"`
	AskClose float32 `parquet:"ask_close"`

	// Implied volatility
	MarkIVOpen  float32 `parquet:"mark_iv_open"`
	MarkIVClose float32 `parquet:"mark_iv_close"`
	BidIVOpen   float32 `parquet:"bid_iv_open"`
	AskIVOpen   float32 `parquet:"ask_iv_open"`

	// Greeks (from the earliest tick in the minute)
	Delta float32 `parquet:"delta"`
	Gamma float32 `parquet:"gamma"`
	Vega  float32 `parquet:"vega"`
	Theta float32 `parquet:"theta"`
	Rho   float32 `parquet:"rho"`

	// Open interest and activity
	OpenInterest float32 `parquet:"open_interest"`
	TickCount    uint16  `parquet:"tick_count"`
}

// SpotBar1m is a 1-minute OHLC bar for the underlying crypto asset price.
// It stores the underlying as a standalone market series rather than
// duplicating it on every option contract row.
type SpotBar1m struct {
	Timestamp   time.Time `parquet:"timestamp,timestamp(millisecond)"`
	Symbol      string    `parquet:"symbol"`
	PriceSource string    `parquet:"price_source"`
	Open        float32   `parquet:"open"`
	High        float32   `parquet:"high"`
	Low         float32   `parquet:"low"`
	Close       float32   `parquet:"close"`
	TickCount   uint32    `parquet:"tick_count"`
	VolumeBase  float64   `parquet:"volume_base"`
	VolumeQuote float64   `parquet:"volume_quote"`
	BarInterval string    `parquet:"bar_interval"`
}

// SymbolMeta holds parsed option contract metadata extracted from
// a symbol string. Maps to crypto_options_symbol_meta in ClickHouse.
type SymbolMeta struct {
	SymbolID        uint32
	Symbol          string
	BaseAsset       string
	OptionType      string // "call" or "put"
	StrikePrice     float32
	Expiration      time.Time
	UnderlyingIndex string
}
