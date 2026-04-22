package dto

import "time"

// ScreenUnderlyingRequest defines filters for the underlying screener.
type ScreenUnderlyingRequest struct {
	Market              string   `form:"market" binding:"required"`
	IVPercentileMin     *float64 `form:"iv_percentile_min" binding:"omitempty"`
	IVPercentileMax     *float64 `form:"iv_percentile_max" binding:"omitempty"`
	IVRankMin           *float64 `form:"iv_rank_min" binding:"omitempty"`
	IVRankMax           *float64 `form:"iv_rank_max" binding:"omitempty"`
	HV20Min             *float64 `form:"hv20_min" binding:"omitempty"`
	HV20Max             *float64 `form:"hv20_max" binding:"omitempty"`
	VolumeMin           *float64 `form:"volume_min" binding:"omitempty"`
	OpenInterestMin     *float64 `form:"open_interest_min" binding:"omitempty"`
	ActivityRatioMin    *float64 `form:"activity_ratio_min" binding:"omitempty"`
	TradabilityRatioMin *float64 `form:"tradability_ratio_min" binding:"omitempty"`
	SortBy              string   `form:"sort_by" binding:"omitempty"`
	Limit               int      `form:"limit" binding:"omitempty"`
	Cursor              string   `form:"cursor" binding:"omitempty"`
}

// ScreenedUnderlying is one result from the underlying screener.
type ScreenedUnderlying struct {
	Market           string    `json:"market"`
	Underlying       string    `json:"underlying"`
	AsOfDate         time.Time `json:"as_of_date"`
	HV10             *float64  `json:"hv10,omitempty"`
	HV20             *float64  `json:"hv20,omitempty"`
	HV30             *float64  `json:"hv30,omitempty"`
	CurrentIV        *float64  `json:"current_iv,omitempty"`
	IVPercentile     *float64  `json:"iv_percentile,omitempty"`
	IVRank           *float64  `json:"iv_rank,omitempty"`
	OpenInterest     *float64  `json:"open_interest,omitempty"`
	Volume           *int      `json:"volume,omitempty"`
	ActivityRatio    *float64  `json:"activity_ratio,omitempty"`
	TradabilityRatio *float64  `json:"tradability_ratio,omitempty"`
}

// ScreenUnderlyingResponse wraps paginated screened underlyings.
type ScreenUnderlyingResponse struct {
	Data       []ScreenedUnderlying `json:"data"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

// ScreenOptionRequest defines filters for the options screener.
type ScreenOptionRequest struct {
	Market            string   `form:"market" binding:"required"`
	Underlying        string   `form:"underlying" binding:"required"`
	OptionType        string   `form:"option_type" binding:"omitempty"`
	MinDTE            *int     `form:"min_dte" binding:"omitempty"`
	MaxDTE            *int     `form:"max_dte" binding:"omitempty"`
	DTEMin            *int     `form:"dte_min" binding:"omitempty"`
	DTEMax            *int     `form:"dte_max" binding:"omitempty"`
	DeltaMin          *float64 `form:"delta_min" binding:"omitempty"`
	DeltaMax          *float64 `form:"delta_max" binding:"omitempty"`
	IVMin             *float64 `form:"iv_min" binding:"omitempty"`
	IVMax             *float64 `form:"iv_max" binding:"omitempty"`
	PremiumMin        *float64 `form:"premium_min" binding:"omitempty"`
	PremiumMax        *float64 `form:"premium_max" binding:"omitempty"`
	VolumeMin         *float64 `form:"volume_min" binding:"omitempty"`
	OpenInterestMin   *float64 `form:"open_interest_min" binding:"omitempty"`
	RelativeSpreadMax *float64 `form:"relative_spread_max" binding:"omitempty"`
	SortBy            string   `form:"sort_by" binding:"omitempty"`
	Limit             int      `form:"limit" binding:"omitempty"`
	Cursor            string   `form:"cursor" binding:"omitempty"`
}

func (r *ScreenOptionRequest) NormalizeAliases() {
	if r.DTEMin == nil {
		r.DTEMin = r.MinDTE
	}
	if r.DTEMax == nil {
		r.DTEMax = r.MaxDTE
	}
	if r.MinDTE == nil {
		r.MinDTE = r.DTEMin
	}
	if r.MaxDTE == nil {
		r.MaxDTE = r.DTEMax
	}
}

// ScreenedOption is one result from the options screener.
type ScreenedOption struct {
	Symbol            string    `json:"symbol"`
	Underlying        string    `json:"underlying"`
	OptionType        string    `json:"option_type"`
	Expiration        time.Time `json:"expiration"`
	DaysToExpiry      int       `json:"days_to_expiry"`
	Strike            float64   `json:"strike"`
	Close             float64   `json:"close"`
	BidClose          float64   `json:"bid_close"`
	AskClose          float64   `json:"ask_close"`
	ImpliedVolatility float64   `json:"implied_volatility"`
	Delta             float64   `json:"delta"`
	Gamma             float64   `json:"gamma"`
	Vega              float64   `json:"vega"`
	Theta             float64   `json:"theta"`
	OpenInterest      float64   `json:"open_interest"`
	Volume            float64   `json:"volume"`
	RelativeSpread    *float64  `json:"relative_spread,omitempty"`
	UnderlyingClose   float64   `json:"underlying_close"`
}

// ScreenOptionResponse wraps paginated screened options.
type ScreenOptionResponse struct {
	Data       []ScreenedOption `json:"data"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
