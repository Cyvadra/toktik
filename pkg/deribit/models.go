package deribit

import "encoding/json"

// BookSummary is one option contract returned by Deribit's bulk book summary endpoint.
type BookSummary struct {
	InstrumentName         string   `json:"instrument_name"`
	BaseCurrency           string   `json:"base_currency"`
	QuoteCurrency          string   `json:"quote_currency"`
	UnderlyingIndex        string   `json:"underlying_index"`
	CreationTimestamp      int64    `json:"creation_timestamp"`
	BidPrice               *float64 `json:"bid_price"`
	AskPrice               *float64 `json:"ask_price"`
	MidPrice               *float64 `json:"mid_price"`
	MarkPrice              *float64 `json:"mark_price"`
	LastPrice              *float64 `json:"last"`
	MarkIV                 *float64 `json:"mark_iv"`
	OpenInterest           *float64 `json:"open_interest"`
	Volume                 *float64 `json:"volume"`
	VolumeUSD              *float64 `json:"volume_usd"`
	High                   *float64 `json:"high"`
	Low                    *float64 `json:"low"`
	PriceChange            *float64 `json:"price_change"`
	UnderlyingPrice        *float64 `json:"underlying_price"`
	EstimatedDeliveryPrice *float64 `json:"estimated_delivery_price"`
}

type apiResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *apiError       `json:"error,omitempty"`
}

type apiError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}
