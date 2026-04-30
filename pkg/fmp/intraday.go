package fmp

import (
	"context"
	"net/url"
)

// IntradayInterval is a valid candle interval for IntradayPrices.
// Available values: 1min, 5min, 15min, 30min, 1hour, 4hour.
type IntradayInterval string

const (
	Interval1Min  IntradayInterval = "1min"
	Interval5Min  IntradayInterval = "5min"
	Interval15Min IntradayInterval = "15min"
	Interval30Min IntradayInterval = "30min"
	Interval1Hour IntradayInterval = "1hour"
	Interval4Hour IntradayInterval = "4hour"
)

// IntradayBar is one OHLCV candle returned by the FMP historical-chart endpoint.
// The Date field is formatted "2006-01-02 15:04:05" in ET for US stocks.
type IntradayBar struct {
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"` // float64: crypto volumes can be fractional
}

// IntradayPrices fetches intraday OHLCV bars for any symbol (US stock, crypto, or forex).
//
// symbol   – e.g. "AAPL", "BTCUSD", "EURUSD"
// interval – one of the Interval* constants
// from, to – date bounds in "YYYY-MM-DD" format; both optional
//
// Results are sorted newest-first (matching FMP's default ordering).
func (c *Client) IntradayPrices(ctx context.Context, symbol string, interval IntradayInterval, from, to string) ([]IntradayBar, error) {
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	if from != "" {
		params.Set("from", from)
	}
	if to != "" {
		params.Set("to", to)
	}
	var out []IntradayBar
	if err := c.get(ctx, "/historical-chart/"+string(interval), params, &out); err != nil {
		return nil, err
	}
	return out, nil
}
