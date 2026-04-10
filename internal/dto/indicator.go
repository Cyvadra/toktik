package dto

import "time"

// IndicatorSeriesRequest is the JSON body for the indicator-series endpoint.
type IndicatorSeriesRequest struct {
	Market     string                 `json:"market" binding:"required"`
	Symbol     string                 `json:"symbol" binding:"required"`
	Interval   string                 `json:"interval" binding:"required"`
	From       string                 `json:"from" binding:"required"`
	To         string                 `json:"to" binding:"required"`
	Session    string                 `json:"session,omitempty"`
	DSL        string                 `json:"dsl,omitempty"`
	Indicators []string               `json:"indicators,omitempty"`
	Params     map[string]interface{} `json:"params,omitempty"`
	Precision  *int                   `json:"precision,omitempty"`
}

// IndicatorSeriesResponse wraps the plotted time series generated from the request.
type IndicatorSeriesResponse struct {
	Market     string                `json:"market"`
	Symbol     string                `json:"symbol"`
	Interval   string                `json:"interval"`
	Timestamps []time.Time           `json:"timestamps"`
	Series     map[string][]*float64 `json:"series"`
}
