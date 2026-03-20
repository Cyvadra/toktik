package dvol

import "time"

// DefaultBaseURL is the default Deribit API root.
const DefaultBaseURL = "https://www.deribit.com"

// AcceptedCurrencies are currencies currently accepted by
// public/get_volatility_index_data (as observed from live endpoint probes).
// Some accepted currencies may legitimately return zero rows for a given range.
var AcceptedCurrencies = []string{"BTC", "ETH"}

// DefaultCurrencies are the default sync targets with stable historical coverage.
var DefaultCurrencies = []string{"BTC", "ETH"}

// AcceptedResolutions are the documented-by-probe resolution values accepted by
// public/get_volatility_index_data.
var AcceptedResolutions = []string{"1", "60", "3600", "43200", "86400"}

// Bar is one OHLC row from Deribit volatility index data.
type Bar struct {
	Currency   string
	IndexName  string
	Resolution string
	Timestamp  time.Time
	Open       float64
	High       float64
	Low        float64
	Close      float64
}

type apiResponse struct {
	JSONRPC string     `json:"jsonrpc"`
	Result  *apiResult `json:"result,omitempty"`
	Error   *apiError  `json:"error,omitempty"`
	Testnet bool       `json:"testnet"`
	USIn    int64      `json:"usIn"`
	USOut   int64      `json:"usOut"`
	USDiff  int64      `json:"usDiff"`
}

type apiResult struct {
	Data         [][]float64 `json:"data"`
	Continuation *int64      `json:"continuation"`
}

type apiError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    *apiErrorData `json:"data,omitempty"`
}

type apiErrorData struct {
	Reason string `json:"reason,omitempty"`
	Param  string `json:"param,omitempty"`
}
