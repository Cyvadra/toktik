package dto

import "time"

// ----- Catalog -----

// FundamentalFactorCatalogRequest filters the catalog by optional market.
type FundamentalFactorCatalogRequest struct {
	Market string `form:"market" binding:"omitempty"`
}

// FundamentalFactorCatalogEntry describes one symbol-bound fundamental factor.
type FundamentalFactorCatalogEntry struct {
	Market             string    `json:"market"`
	FactorCode         string    `json:"factor_code"`
	DisplayName        string    `json:"display_name"`
	Description        string    `json:"description,omitempty"`
	ValueType          string    `json:"value_type"`
	Unit               string    `json:"unit,omitempty"`
	PreferredFrequency string    `json:"preferred_frequency"`
	FillPolicy         string    `json:"fill_policy"`
	FillMaxDays        int       `json:"fill_max_days,omitempty"`
	PointInTime        bool      `json:"point_in_time"`
	Source             string    `json:"source,omitempty"`
	Active             bool      `json:"active"`
	SLAHours           int       `json:"sla_hours,omitempty"`
	Metadata           string    `json:"metadata,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// FundamentalFactorCatalogResponse is the catalog listing response.
type FundamentalFactorCatalogResponse struct {
	Data []FundamentalFactorCatalogEntry `json:"data"`
}

// ----- Series -----

// FundamentalSeriesRequest queries a single symbol/factor over a time range.
//
// `Mode` is one of:
//   - "event"  : raw observations only (sparse)
//   - "as_of"  : latest known-value per event_ts (no fill applied)
//   - "filled" : as_of plus forward-fill respecting the catalog policy
//
// `AsOf` is the point-in-time cutoff; defaults to To when omitted.
type FundamentalSeriesRequest struct {
	Market string `form:"market" binding:"required"`
	Symbol string `form:"symbol" binding:"required"`
	Factor string `form:"factor" binding:"required"`
	From   string `form:"from" binding:"required"`
	To     string `form:"to" binding:"required"`
	Mode   string `form:"mode" binding:"omitempty"`
	AsOf   string `form:"as_of" binding:"omitempty"`
}

// FundamentalSeriesPoint is one observation in a fundamental series.
type FundamentalSeriesPoint struct {
	EventTS  time.Time `json:"event_ts"`
	KnownAt  time.Time `json:"known_at"`
	Value    float64   `json:"value"`
	Source   string    `json:"source,omitempty"`
	Revision int       `json:"revision,omitempty"`
	Filled   bool      `json:"filled,omitempty"`
}

// FundamentalSeriesResponse returns a sparse or filled series for one factor.
type FundamentalSeriesResponse struct {
	Market     string                   `json:"market"`
	Symbol     string                   `json:"symbol"`
	Factor     string                   `json:"factor"`
	Mode       string                   `json:"mode"`
	AsOf       time.Time                `json:"as_of"`
	FillPolicy string                   `json:"fill_policy,omitempty"`
	Data       []FundamentalSeriesPoint `json:"data"`
}

// ----- Snapshot (single symbol, many factors) -----

// FundamentalSnapshotRequest queries the latest known value per factor for one symbol.
type FundamentalSnapshotRequest struct {
	Market  string   `form:"market" binding:"required"`
	Symbol  string   `form:"symbol" binding:"required"`
	Factors []string `form:"factor" binding:"omitempty"`
	AsOf    string   `form:"as_of" binding:"omitempty"`
}

// FundamentalSnapshotEntry is one factor's latest known value.
type FundamentalSnapshotEntry struct {
	Factor  string    `json:"factor"`
	EventTS time.Time `json:"event_ts"`
	KnownAt time.Time `json:"known_at"`
	Value   float64   `json:"value"`
	Source  string    `json:"source,omitempty"`
}

// FundamentalSnapshotResponse returns the latest values for a symbol.
type FundamentalSnapshotResponse struct {
	Market string                     `json:"market"`
	Symbol string                     `json:"symbol"`
	AsOf   time.Time                  `json:"as_of"`
	Data   []FundamentalSnapshotEntry `json:"data"`
}

// ----- Panel (many symbols, many factors) -----

// FundamentalPanelRequest queries the latest known values across symbols.
type FundamentalPanelRequest struct {
	Market  string   `form:"market" binding:"required"`
	Symbols []string `form:"symbol" binding:"required"`
	Factors []string `form:"factor" binding:"omitempty"`
	AsOf    string   `form:"as_of" binding:"omitempty"`
}

// FundamentalPanelRow is one (symbol, factor) cell.
type FundamentalPanelRow struct {
	Symbol  string    `json:"symbol"`
	Factor  string    `json:"factor"`
	EventTS time.Time `json:"event_ts"`
	KnownAt time.Time `json:"known_at"`
	Value   float64   `json:"value"`
}

// FundamentalPanelResponse returns a tall panel of latest values.
type FundamentalPanelResponse struct {
	Market string                `json:"market"`
	AsOf   time.Time             `json:"as_of"`
	Data   []FundamentalPanelRow `json:"data"`
}

// ----- Freshness -----

// FundamentalFreshnessRequest filters freshness reporting.
type FundamentalFreshnessRequest struct {
	Market string `form:"market" binding:"omitempty"`
	Factor string `form:"factor" binding:"omitempty"`
}

// FundamentalFreshnessEntry reports the latest known_at per (market, factor).
type FundamentalFreshnessEntry struct {
	Market      string    `json:"market"`
	Factor      string    `json:"factor"`
	LastKnownAt time.Time `json:"last_known_at"`
	StaleHours  *float64  `json:"stale_hours,omitempty"`
	SLAHours    int       `json:"sla_hours,omitempty"`
	Stale       *bool     `json:"stale,omitempty"`
}

// FundamentalFreshnessResponse lists freshness per factor.
type FundamentalFreshnessResponse struct {
	Data []FundamentalFreshnessEntry `json:"data"`
}
