package dto

import polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"

type PolygonStockSnapshotRequest struct {
	Symbol string `form:"symbol" binding:"required"`
}

type PolygonAggregateRequest struct {
	Ticker     string `form:"ticker" binding:"required"`
	Multiplier int    `form:"multiplier"`
	Timespan   string `form:"timespan" binding:"required"`
	From       string `form:"from" binding:"required"`
	To         string `form:"to" binding:"required"`
	Adjusted   *bool  `form:"adjusted"`
	Sort       string `form:"sort"`
	Limit      int    `form:"limit"`
}

type PolygonQuoteRequest struct {
	Timestamp    string `form:"timestamp"`
	TimestampGte string `form:"timestamp_gte"`
	TimestampGt  string `form:"timestamp_gt"`
	TimestampLte string `form:"timestamp_lte"`
	TimestampLt  string `form:"timestamp_lt"`
	Order        string `form:"order"`
	Sort         string `form:"sort"`
	Limit        int    `form:"limit"`
}

type PolygonTradeRequest struct {
	Timestamp    string `form:"timestamp"`
	TimestampGte string `form:"timestamp_gte"`
	TimestampGt  string `form:"timestamp_gt"`
	TimestampLte string `form:"timestamp_lte"`
	TimestampLt  string `form:"timestamp_lt"`
	Order        string `form:"order"`
	Sort         string `form:"sort"`
	Limit        int    `form:"limit"`
}

type PolygonStockQuotesRequest struct {
	Symbol string `form:"symbol" binding:"required"`
	PolygonQuoteRequest
}

type PolygonStockTradesRequest struct {
	Symbol string `form:"symbol" binding:"required"`
	PolygonTradeRequest
}

type PolygonOptionContractRequest struct {
	Ticker string `form:"ticker" binding:"required"`
}

type PolygonOptionChainRequest struct {
	Underlying        string   `form:"underlying" binding:"required"`
	ExpirationDate    string   `form:"expiration_date"`
	ExpirationDateGte string   `form:"expiration_date_gte"`
	ExpirationDateGt  string   `form:"expiration_date_gt"`
	ExpirationDateLte string   `form:"expiration_date_lte"`
	ExpirationDateLt  string   `form:"expiration_date_lt"`
	ContractType      string   `form:"contract_type"`
	StrikePrice       *float64 `form:"strike_price"`
	StrikePriceGte    *float64 `form:"strike_price_gte"`
	StrikePriceGt     *float64 `form:"strike_price_gt"`
	StrikePriceLte    *float64 `form:"strike_price_lte"`
	StrikePriceLt     *float64 `form:"strike_price_lt"`
	Order             string   `form:"order"`
	Sort              string   `form:"sort"`
	Limit             int      `form:"limit"`
}

type PolygonOptionQuotesRequest struct {
	Ticker string `form:"ticker" binding:"required"`
	PolygonQuoteRequest
}

type PolygonOptionTradesRequest struct {
	Ticker string `form:"ticker" binding:"required"`
	PolygonTradeRequest
}

type PolygonStockSnapshotResponse struct {
	Data *polygonpkg.StockSnapshot `json:"data"`
}

type PolygonAggregateResponse struct {
	Data []polygonpkg.AggregateBar `json:"data"`
}

type PolygonQuoteResponse struct {
	Data []polygonpkg.Quote `json:"data"`
}

type PolygonTradeResponse struct {
	Data []polygonpkg.Trade `json:"data"`
}

type PolygonOptionContractResponse struct {
	Data *polygonpkg.OptionContract `json:"data"`
}

type PolygonOptionChainResponse struct {
	Data []polygonpkg.OptionChainContract `json:"data"`
}

func (r PolygonAggregateRequest) ToPolygon() polygonpkg.AggregateRequest {
	return polygonpkg.AggregateRequest{
		Ticker:     r.Ticker,
		Multiplier: r.Multiplier,
		Timespan:   r.Timespan,
		From:       r.From,
		To:         r.To,
		Adjusted:   r.Adjusted,
		Sort:       r.Sort,
		Limit:      r.Limit,
	}
}

func (r PolygonQuoteRequest) ToPolygon() polygonpkg.QuoteRequest {
	return polygonpkg.QuoteRequest{
		Timestamp:    r.Timestamp,
		TimestampGte: r.TimestampGte,
		TimestampGt:  r.TimestampGt,
		TimestampLte: r.TimestampLte,
		TimestampLt:  r.TimestampLt,
		Order:        r.Order,
		Sort:         r.Sort,
		Limit:        r.Limit,
	}
}

func (r PolygonTradeRequest) ToPolygon() polygonpkg.TradeRequest {
	return polygonpkg.TradeRequest{
		Timestamp:    r.Timestamp,
		TimestampGte: r.TimestampGte,
		TimestampGt:  r.TimestampGt,
		TimestampLte: r.TimestampLte,
		TimestampLt:  r.TimestampLt,
		Order:        r.Order,
		Sort:         r.Sort,
		Limit:        r.Limit,
	}
}

func (r PolygonOptionChainRequest) ToPolygon() polygonpkg.OptionChainRequest {
	return polygonpkg.OptionChainRequest{
		Underlying:        r.Underlying,
		ExpirationDate:    r.ExpirationDate,
		ExpirationDateGte: r.ExpirationDateGte,
		ExpirationDateGt:  r.ExpirationDateGt,
		ExpirationDateLte: r.ExpirationDateLte,
		ExpirationDateLt:  r.ExpirationDateLt,
		ContractType:      r.ContractType,
		StrikePrice:       r.StrikePrice,
		StrikePriceGte:    r.StrikePriceGte,
		StrikePriceGt:     r.StrikePriceGt,
		StrikePriceLte:    r.StrikePriceLte,
		StrikePriceLt:     r.StrikePriceLt,
		Order:             r.Order,
		Sort:              r.Sort,
		Limit:             r.Limit,
	}
}
