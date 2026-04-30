package fmp

import "context"

// CryptoQuote returns the real-time quote for a crypto pair symbol
// (e.g. "BTCUSD", "ETHUSD", "SOLUSD").
// It is a thin convenience wrapper around the shared Quote endpoint.
func (c *Client) CryptoQuote(ctx context.Context, symbol string) (*Quote, error) {
	rows, err := c.Quote(ctx, symbol)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// CryptoHistoricalPrices returns daily EOD OHLCV bars for a crypto pair.
// Delegates to HistoricalPrices.
func (c *Client) CryptoHistoricalPrices(ctx context.Context, symbol, from, to string) ([]EODPrice, error) {
	return c.HistoricalPrices(ctx, symbol, from, to)
}

// CryptoIntradayPrices returns intraday bars for a crypto pair.
// Delegates to IntradayPrices; valid intervals are Interval1Min … Interval4Hour.
func (c *Client) CryptoIntradayPrices(ctx context.Context, symbol string, interval IntradayInterval, from, to string) ([]IntradayBar, error) {
	return c.IntradayPrices(ctx, symbol, interval, from, to)
}
