package chquery

import "time"

// ----- Standard column lists -----

// OptionBarColumns is the standard column list for crypto option bar queries.
const OptionBarColumns = `timestamp, symbol_id, base_asset,
    mark_open, mark_high, mark_low, mark_close,
    last_open, last_high, last_low, last_close,
    bid_open, bid_high, bid_low, bid_close,
    ask_open, ask_high, ask_low, ask_close,
    mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
    delta, gamma, vega, theta, rho,
  volume, open_interest, tick_count`

// SpotBarColumns is the standard column list for crypto spot bar queries.
const SpotBarColumns = `timestamp, symbol, price_source, open, high, low, close, volume, tick_count, volume_base, volume_quote`

// TimeParam formats a timestamp for ClickHouse string-bound query parameters.
func TimeParam(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// DateParam formats a timestamp as a date string for ClickHouse.
func DateParam(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}
