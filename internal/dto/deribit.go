package dto

type DeribitOptionChainRequest struct {
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

type DeribitOptionContract struct {
	Ticker           string  `json:"ticker"`
	UnderlyingTicker string  `json:"underlyingTicker"`
	ContractType     string  `json:"contractType"`
	ExerciseStyle    string  `json:"exerciseStyle"`
	ExpirationDate   string  `json:"expirationDate"`
	StrikePrice      float64 `json:"strikePrice"`
	BaseCurrency     string  `json:"baseCurrency"`
	QuoteCurrency    string  `json:"quoteCurrency"`
}

type DeribitOptionDay struct {
	High          *float64 `json:"high"`
	Low           *float64 `json:"low"`
	ChangePercent *float64 `json:"changePercent"`
	Volume        *float64 `json:"volume"`
	VolumeUSD     *float64 `json:"volumeUSD"`
}

type DeribitUnderlyingAsset struct {
	Ticker string   `json:"ticker"`
	Price  *float64 `json:"price"`
}

type DeribitOptionChainContract struct {
	Contract          DeribitOptionContract  `json:"contract"`
	Day               DeribitOptionDay       `json:"day"`
	MarkPrice         *float64               `json:"markPrice"`
	LastPrice         *float64               `json:"lastPrice"`
	BidPrice          *float64               `json:"bidPrice"`
	AskPrice          *float64               `json:"askPrice"`
	MidPrice          *float64               `json:"midPrice"`
	ImpliedVolatility *float64               `json:"impliedVolatility"`
	OpenInterest      *float64               `json:"openInterest"`
	UnderlyingAsset   DeribitUnderlyingAsset `json:"underlyingAsset"`
	PremiumCurrency   string                 `json:"premiumCurrency"`
	Timestamp         int64                  `json:"timestamp"`
}

type DeribitOptionChainResponse struct {
	Data []DeribitOptionChainContract `json:"data"`
}
