package dto

import "time"

// FeatureVolatilitySnapshotRequest defines the query for the first feature-store API.
type FeatureVolatilitySnapshotRequest struct {
	Market       string `form:"market" binding:"required"`
	Underlying   string `form:"underlying" binding:"required"`
	LookbackDays int    `form:"lookback_days" binding:"omitempty"`
}

// FeatureVolatilityHistoryRequest defines the query for volatility feature history.
type FeatureVolatilityHistoryRequest struct {
	Market       string `form:"market" binding:"required"`
	Underlying   string `form:"underlying" binding:"required"`
	From         string `form:"from" binding:"required"`
	To           string `form:"to" binding:"required"`
	LookbackDays int    `form:"lookback_days" binding:"omitempty"`
}

// FeatureVolatilityHistoryRow is one daily derived-volatility record.
type FeatureVolatilityHistoryRow struct {
	Date              time.Time `json:"date"`
	PriceObservations int       `json:"price_observations"`
	IVObservations    int       `json:"iv_observations"`
	HV10              *float64  `json:"hv10,omitempty"`
	HV20              *float64  `json:"hv20,omitempty"`
	HV30              *float64  `json:"hv30,omitempty"`
	CurrentIV         *float64  `json:"current_iv,omitempty"`
	IVPercentile      *float64  `json:"iv_percentile,omitempty"`
	IVRank            *float64  `json:"iv_rank,omitempty"`
}

// FeatureVolatilityHistoryResponse returns a range of daily volatility metrics.
type FeatureVolatilityHistoryResponse struct {
	Market       string                        `json:"market"`
	Underlying   string                        `json:"underlying"`
	LookbackDays int                           `json:"lookback_days"`
	Data         []FeatureVolatilityHistoryRow `json:"data"`
}

// FeatureSurfaceSnapshotRequest defines the query for skew/term-structure snapshots.
type FeatureSurfaceSnapshotRequest struct {
	Market          string `form:"market" binding:"required"`
	Underlying      string `form:"underlying" binding:"required"`
	MinDaysToExpiry int    `form:"min_days_to_expiry" binding:"omitempty"`
	MaxDaysToExpiry int    `form:"max_days_to_expiry" binding:"omitempty"`
}

// FeatureUnderlyingSnapshotRequest defines a market + underlying point-in-time query.
type FeatureUnderlyingSnapshotRequest struct {
	Market     string `form:"market" binding:"required"`
	Underlying string `form:"underlying" binding:"required"`
}

// FeatureUnderlyingHistoryRequest defines a market + underlying range query.
type FeatureUnderlyingHistoryRequest struct {
	Market     string `form:"market" binding:"required"`
	Underlying string `form:"underlying" binding:"required"`
	From       string `form:"from" binding:"required"`
	To         string `form:"to" binding:"required"`
}

// FeatureLiquidityHistoryRequest defines the query for liquidity feature history.
type FeatureLiquidityHistoryRequest struct {
	Market          string `form:"market" binding:"required"`
	Underlying      string `form:"underlying" binding:"required"`
	From            string `form:"from" binding:"required"`
	To              string `form:"to" binding:"required"`
	MinDaysToExpiry int    `form:"min_days_to_expiry" binding:"omitempty"`
	MaxDaysToExpiry int    `form:"max_days_to_expiry" binding:"omitempty"`
}

// FeatureLiquiditySnapshotRow describes one expiry bucket of option liquidity.
type FeatureLiquiditySnapshotRow struct {
	Expiration            time.Time `json:"expiration"`
	DaysToExpiry          int       `json:"days_to_expiry"`
	AvgBidClose           *float64  `json:"avg_bid_close,omitempty"`
	AvgAskClose           *float64  `json:"avg_ask_close,omitempty"`
	AvgMarkClose          *float64  `json:"avg_mark_close,omitempty"`
	RelativeSpread        *float64  `json:"relative_spread,omitempty"`
	OpenInterest          *float64  `json:"open_interest,omitempty"`
	TickCount             int       `json:"tick_count"`
	Volume                int       `json:"volume"`
	Transactions          int       `json:"transactions"`
	ContractCount         int       `json:"contract_count"`
	ActiveContractCount   int       `json:"active_contract_count"`
	TradableContractCount int       `json:"tradable_contract_count"`
	ActivityRatio         *float64  `json:"activity_ratio,omitempty"`
	TradabilityRatio      *float64  `json:"tradability_ratio,omitempty"`
}

// FeatureLiquiditySnapshotResponse returns the latest liquidity snapshot for one underlying.
type FeatureLiquiditySnapshotResponse struct {
	Market     string                        `json:"market"`
	Underlying string                        `json:"underlying"`
	AsOf       *time.Time                    `json:"as_of,omitempty"`
	Data       []FeatureLiquiditySnapshotRow `json:"data"`
}

// FeatureLiquidityHistoryRow describes one daily liquidity feature row.
type FeatureLiquidityHistoryRow struct {
	AsOfDate time.Time `json:"as_of_date"`
	FeatureLiquiditySnapshotRow
}

// FeatureLiquidityHistoryResponse returns a range of liquidity feature rows.
type FeatureLiquidityHistoryResponse struct {
	Market     string                       `json:"market"`
	Underlying string                       `json:"underlying"`
	Data       []FeatureLiquidityHistoryRow `json:"data"`
}

// FeatureEventWindowSnapshotResponse returns market-session proximity flags around the latest trading date.
type FeatureEventWindowSnapshotResponse struct {
	Market              string     `json:"market"`
	Underlying          string     `json:"underlying"`
	AsOfDate            *time.Time `json:"as_of_date,omitempty"`
	IsEarlyClose        bool       `json:"is_early_close"`
	PreviousHolidayDate *time.Time `json:"previous_holiday_date,omitempty"`
	NextHolidayDate     *time.Time `json:"next_holiday_date,omitempty"`
	DaysFromPrevHoliday *int       `json:"days_from_prev_holiday,omitempty"`
	DaysToNextHoliday   *int       `json:"days_to_next_holiday,omitempty"`
}

// FeatureEventWindowHistoryRow describes one daily event-window record.
type FeatureEventWindowHistoryRow struct {
	Date time.Time `json:"date"`
	FeatureEventWindowSnapshotResponse
}

// FeatureEventWindowHistoryResponse returns a range of event-window rows.
type FeatureEventWindowHistoryResponse struct {
	Market     string                         `json:"market"`
	Underlying string                         `json:"underlying"`
	Data       []FeatureEventWindowHistoryRow `json:"data"`
}

// FeatureDailyPanelRequest defines a range query for a merged feature panel.
type FeatureDailyPanelRequest struct {
	Market          string `form:"market" binding:"required"`
	Underlying      string `form:"underlying" binding:"required"`
	From            string `form:"from" binding:"required"`
	To              string `form:"to" binding:"required"`
	LookbackDays    int    `form:"lookback_days" binding:"omitempty"`
	MinDaysToExpiry int    `form:"min_days_to_expiry" binding:"omitempty"`
	MaxDaysToExpiry int    `form:"max_days_to_expiry" binding:"omitempty"`
}

// FeatureDailyPanelRow is one date-aligned merged feature record.
type FeatureDailyPanelRow struct {
	Date                       time.Time  `json:"date"`
	PriceObservations          int        `json:"price_observations"`
	IVObservations             int        `json:"iv_observations"`
	HV10                       *float64   `json:"hv10,omitempty"`
	HV20                       *float64   `json:"hv20,omitempty"`
	HV30                       *float64   `json:"hv30,omitempty"`
	CurrentIV                  *float64   `json:"current_iv,omitempty"`
	IVPercentile               *float64   `json:"iv_percentile,omitempty"`
	IVRank                     *float64   `json:"iv_rank,omitempty"`
	FrontExpiration            *time.Time `json:"front_expiration,omitempty"`
	FrontDaysToExpiry          *int       `json:"front_days_to_expiry,omitempty"`
	FrontATMIV                 *float64   `json:"front_atm_iv,omitempty"`
	FrontPutCallSkew           *float64   `json:"front_put_call_skew,omitempty"`
	SurfaceContractCount       *int       `json:"surface_contract_count,omitempty"`
	LiquidityOpenInterest      *float64   `json:"liquidity_open_interest,omitempty"`
	LiquidityRelativeSpread    *float64   `json:"liquidity_relative_spread,omitempty"`
	LiquidityTickCount         int        `json:"liquidity_tick_count"`
	LiquidityVolume            int        `json:"liquidity_volume"`
	LiquidityTransactions      int        `json:"liquidity_transactions"`
	LiquidityContractCount     int        `json:"liquidity_contract_count"`
	LiquidityActiveContracts   int        `json:"liquidity_active_contract_count"`
	LiquidityTradableContracts int        `json:"liquidity_tradable_contract_count"`
	LiquidityActivityRatio     *float64   `json:"liquidity_activity_ratio,omitempty"`
	LiquidityTradabilityRatio  *float64   `json:"liquidity_tradability_ratio,omitempty"`
	IsEarlyClose               bool       `json:"is_early_close"`
	DaysFromPrevHoliday        *int       `json:"days_from_prev_holiday,omitempty"`
	DaysToNextHoliday          *int       `json:"days_to_next_holiday,omitempty"`
}

// FeatureDailyPanelResponse returns a range of merged feature records.
type FeatureDailyPanelResponse struct {
	Market       string                 `json:"market"`
	Underlying   string                 `json:"underlying"`
	LookbackDays int                    `json:"lookback_days"`
	Data         []FeatureDailyPanelRow `json:"data"`
}

// FeatureTermStructureSnapshotRow describes one expiry point on the IV term structure.
type FeatureTermStructureSnapshotRow struct {
	Expiration    time.Time `json:"expiration"`
	DaysToExpiry  int       `json:"days_to_expiry"`
	ATMIV         *float64  `json:"atm_iv,omitempty"`
	CallIV        *float64  `json:"call_iv,omitempty"`
	PutIV         *float64  `json:"put_iv,omitempty"`
	ContractCount int       `json:"contract_count"`
}

// FeatureTermStructureSnapshotResponse returns the current IV term structure snapshot.
type FeatureTermStructureSnapshotResponse struct {
	Market     string                            `json:"market"`
	Underlying string                            `json:"underlying"`
	AsOf       *time.Time                        `json:"as_of,omitempty"`
	Data       []FeatureTermStructureSnapshotRow `json:"data"`
}

// FeatureSkewSnapshotRow describes one expiry point for put-call skew.
type FeatureSkewSnapshotRow struct {
	Expiration    time.Time `json:"expiration"`
	DaysToExpiry  int       `json:"days_to_expiry"`
	OTMCallIV     *float64  `json:"otm_call_iv,omitempty"`
	OTMPutIV      *float64  `json:"otm_put_iv,omitempty"`
	PutCallSkew   *float64  `json:"put_call_skew,omitempty"`
	ContractCount int       `json:"contract_count"`
}

// FeatureSkewSnapshotResponse returns the current put-call skew snapshot.
type FeatureSkewSnapshotResponse struct {
	Market     string                   `json:"market"`
	Underlying string                   `json:"underlying"`
	AsOf       *time.Time               `json:"as_of,omitempty"`
	Data       []FeatureSkewSnapshotRow `json:"data"`
}

// FeatureVolatilitySnapshotResponse returns current HV and IV regime metrics.
type FeatureVolatilitySnapshotResponse struct {
	Market            string     `json:"market"`
	Underlying        string     `json:"underlying"`
	LookbackDays      int        `json:"lookback_days"`
	PriceAsOf         *time.Time `json:"price_as_of,omitempty"`
	IVAsOf            *time.Time `json:"iv_as_of,omitempty"`
	PriceObservations int        `json:"price_observations"`
	IVObservations    int        `json:"iv_observations"`
	HV10              *float64   `json:"hv10,omitempty"`
	HV20              *float64   `json:"hv20,omitempty"`
	HV30              *float64   `json:"hv30,omitempty"`
	CurrentIV         *float64   `json:"current_iv,omitempty"`
	IVPercentile      *float64   `json:"iv_percentile,omitempty"`
	IVRank            *float64   `json:"iv_rank,omitempty"`
}

// FeatureTermStructureHistoryRequest defines a range query for IV term structure history.
type FeatureTermStructureHistoryRequest struct {
	Market          string `form:"market" binding:"required"`
	Underlying      string `form:"underlying" binding:"required"`
	From            string `form:"from" binding:"required"`
	To              string `form:"to" binding:"required"`
	MinDaysToExpiry int    `form:"min_days_to_expiry" binding:"omitempty"`
	MaxDaysToExpiry int    `form:"max_days_to_expiry" binding:"omitempty"`
}

// FeatureTermStructureHistoryRow is one daily term structure record.
type FeatureTermStructureHistoryRow struct {
	AsOfDate time.Time `json:"as_of_date"`
	FeatureTermStructureSnapshotRow
}

// FeatureTermStructureHistoryResponse returns a range of term structure rows.
type FeatureTermStructureHistoryResponse struct {
	Market     string                           `json:"market"`
	Underlying string                           `json:"underlying"`
	Data       []FeatureTermStructureHistoryRow `json:"data"`
}

// FeatureSkewHistoryRequest defines a range query for put-call skew history.
type FeatureSkewHistoryRequest struct {
	Market          string `form:"market" binding:"required"`
	Underlying      string `form:"underlying" binding:"required"`
	From            string `form:"from" binding:"required"`
	To              string `form:"to" binding:"required"`
	MinDaysToExpiry int    `form:"min_days_to_expiry" binding:"omitempty"`
	MaxDaysToExpiry int    `form:"max_days_to_expiry" binding:"omitempty"`
}

// FeatureSkewHistoryRow is one daily skew record.
type FeatureSkewHistoryRow struct {
	AsOfDate time.Time `json:"as_of_date"`
	FeatureSkewSnapshotRow
}

// FeatureSkewHistoryResponse returns a range of skew rows.
type FeatureSkewHistoryResponse struct {
	Market     string                  `json:"market"`
	Underlying string                  `json:"underlying"`
	Data       []FeatureSkewHistoryRow `json:"data"`
}
