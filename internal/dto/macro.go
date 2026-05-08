package dto

import "time"

type MacroFactorCatalogRequest struct {
	Dataset string `form:"dataset" binding:"omitempty"`
}

type MacroFactorCatalogEntry struct {
	Dataset            string    `json:"dataset"`
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
	ReferenceMarket    string    `json:"reference_market,omitempty"`
	ReferenceSymbol    string    `json:"reference_symbol,omitempty"`
	RealtimeMode       string    `json:"realtime_mode,omitempty"`
	Active             bool      `json:"active"`
	SLAHours           int       `json:"sla_hours,omitempty"`
	Metadata           string    `json:"metadata,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type MacroFactorCatalogResponse struct {
	Data []MacroFactorCatalogEntry `json:"data"`
}

type MacroSeriesRequest struct {
	Dataset         string   `form:"dataset" binding:"required"`
	Factors         []string `form:"factor" binding:"required"`
	From            string   `form:"from" binding:"required"`
	To              string   `form:"to" binding:"required"`
	AsOf            string   `form:"as_of" binding:"omitempty"`
	Interval        string   `form:"interval" binding:"omitempty"`
	ReferenceMarket string   `form:"reference_market" binding:"omitempty"`
	ReferenceSymbol string   `form:"reference_symbol" binding:"omitempty"`
	Limit           int      `form:"limit" binding:"omitempty"`
}

type MacroSeriesPoint struct {
	Factor          string    `json:"factor"`
	Timestamp       time.Time `json:"timestamp"`
	EventTS         time.Time `json:"event_ts"`
	KnownAt         time.Time `json:"known_at"`
	Value           float64   `json:"value"`
	Source          string    `json:"source,omitempty"`
	Filled          bool      `json:"filled,omitempty"`
	Realtime        bool      `json:"realtime,omitempty"`
	ReferenceMarket string    `json:"reference_market,omitempty"`
	ReferenceSymbol string    `json:"reference_symbol,omitempty"`
}

type MacroSeriesResponse struct {
	Dataset         string             `json:"dataset"`
	Interval        string             `json:"interval"`
	AsOf            time.Time          `json:"as_of"`
	ReferenceMarket string             `json:"reference_market,omitempty"`
	ReferenceSymbol string             `json:"reference_symbol,omitempty"`
	Data            []MacroSeriesPoint `json:"data"`
}
