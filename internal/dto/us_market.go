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
	Symbol               string   `json:"symbol"`
	Ticker               string   `json:"ticker,omitempty"`
	Name                 string   `json:"name,omitempty"`
	Country              string   `json:"country,omitempty"`
	Currency             string   `json:"currency,omitempty"`
	Exchange             string   `json:"exchange,omitempty"`
	ExchangeFullName     string   `json:"exchange_full_name,omitempty"`
	Sector               string   `json:"sector,omitempty"`
	Industry             string   `json:"industry,omitempty"`
	IPO                  string   `json:"ipo,omitempty"`
	MarketCapitalization *float64 `json:"market_capitalization,omitempty"`
	ShareOutstanding     *float64 `json:"share_outstanding,omitempty"`
	WebURL               string   `json:"weburl,omitempty"`
	Logo                 string   `json:"logo,omitempty"`
	Source               string   `json:"source,omitempty"`
	IsETF                bool     `json:"is_etf,omitempty"`
	IsFund               bool     `json:"is_fund,omitempty"`
}

type USStockProfileRequest struct {
	Symbols []string `json:"symbols" binding:"required"`
}

type USStockProfileResponse struct {
	Data []USStockCompanyProfile `json:"data"`
}

type USStockFundamentalMetricsRequest struct {
	Symbols []string `json:"symbols" binding:"required"`
	AsOf    string   `json:"as_of,omitempty" binding:"omitempty"`
}

type USStockFundamentalMetricsRow struct {
	Symbol                    string   `json:"symbol"`
	PeTtm                     *float64 `json:"peTtm,omitempty"`
	ForwardPe                 *float64 `json:"forwardPe,omitempty"`
	PB                        *float64 `json:"pb,omitempty"`
	BookValuePerShare         *float64 `json:"bookValuePerShare,omitempty"`
	EpsTtm                    *float64 `json:"epsTtm,omitempty"`
	EpsGrowthTtmYoy           *float64 `json:"epsGrowthTtmYoy,omitempty"`
	EpsGrowthQuarterlyYoy     *float64 `json:"epsGrowthQuarterlyYoy,omitempty"`
	RevenueGrowthTtmYoy       *float64 `json:"revenueGrowthTtmYoy,omitempty"`
	RevenueGrowthQuarterlyYoy *float64 `json:"revenueGrowthQuarterlyYoy,omitempty"`
	AsOf                      string   `json:"asOf,omitempty"`
	Period                    string   `json:"period,omitempty"`
	Source                    string   `json:"source,omitempty"`
}

type USStockFundamentalMetricsResponse struct {
	Data []USStockFundamentalMetricsRow `json:"data"`
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

// USStockSplitRequest is the query parameters for the US stock splits endpoint.
type USStockSplitRequest struct {
	Symbols      []string `form:"symbol" binding:"omitempty"`
	SymbolsAlias []string `form:"symbols" binding:"omitempty" json:"-"`
}

// USStockSplitRow is one split-adjustment event from us_stock_splits.
type USStockSplitRow struct {
	Symbol      string    `json:"symbol"`
	SplitDate   time.Time `json:"split_date"`
	Numerator   float64   `json:"numerator"`
	Denominator float64   `json:"denominator"`
	SplitType   string    `json:"split_type,omitempty"`
	Source      string    `json:"source,omitempty"`
	SourceHash  string    `json:"source_hash,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// USStockSplitResponse wraps split-adjustment rows.
type USStockSplitResponse struct {
	Data []USStockSplitRow `json:"data"`
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

// USOptionWallRequest requests realtime option-wall slices for expirations in a DTE range.
// The relevant date dimension here is expiration, not the snapshot day bucket used for caching.
type USOptionWallRequest struct {
	Symbol string `form:"symbol" binding:"required"`
	MinDTE int    `form:"min_dte" binding:"omitempty"`
	MaxDTE int    `form:"max_dte" binding:"omitempty"`
}

// USOptionWallStrikeRow is one strike bucket inside an expiration wall.
type USOptionWallStrikeRow struct {
	Strike            float64  `json:"strike"`
	TotalOpenInterest float64  `json:"total_open_interest"`
	CallOpenInterest  float64  `json:"call_open_interest"`
	PutOpenInterest   float64  `json:"put_open_interest"`
	CallContractCount int      `json:"call_contract_count"`
	PutContractCount  int      `json:"put_contract_count"`
	AverageBid        *float64 `json:"average_bid,omitempty"`
	AverageAsk        *float64 `json:"average_ask,omitempty"`
	AverageMidpoint   *float64 `json:"average_midpoint,omitempty"`
	AverageSpread     *float64 `json:"average_spread,omitempty"`
}

// USOptionWall is the derived wall for one expiration and one snapshot-day cache bucket.
type USOptionWall struct {
	Symbol       string                  `json:"symbol"`
	Expiration   time.Time               `json:"expiration"`
	SnapshotDay  time.Time               `json:"snapshot_day"`
	DaysToExpiry int                     `json:"days_to_expiry"`
	Strikes      []USOptionWallStrikeRow `json:"strikes"`
}

// USOptionWallResponse wraps all expiration walls in the requested DTE range.
type USOptionWallResponse struct {
	Symbol      string         `json:"symbol"`
	SnapshotDay time.Time      `json:"snapshot_day"`
	Data        []USOptionWall `json:"data"`
}
