package cryptooptions

import "fmt"

// OptionBarColumns is the standard column list for option bar queries.
const OptionBarColumns = `timestamp, symbol_id, base_asset,
    mark_open, mark_high, mark_low, mark_close,
    last_open, last_high, last_low, last_close,
    bid_open, bid_high, bid_low, bid_close,
    ask_open, ask_high, ask_low, ask_close,
    mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
    delta, gamma, vega, theta, rho,
    open_interest, tick_count`

// SpotBarColumns is the standard column list for spot bar queries.
const SpotBarColumns = `timestamp, symbol, price_source, open, high, low, close, tick_count`

// BuildOptionBarSubquery returns a SQL subquery for option bars using
// ClickHouse named parameters: {symbol_id:UInt32}, {from:DateTime}, {to:DateTime}.
func BuildOptionBarSubquery(interval string) (string, error) {
	if interval == "1m" {
		return fmt.Sprintf(`SELECT
    %s
FROM crypto_options_bar_1m
WHERE symbol_id = {symbol_id:UInt32}
  AND timestamp >= {from:DateTime}
  AND timestamp < {to:DateTime}`, OptionBarColumns), nil
	}

	if viewName, ok := PrecomputedIntervals[interval]; ok {
		return fmt.Sprintf(`SELECT
    %s
FROM %s
WHERE symbol_id = {symbol_id:UInt32}
  AND timestamp >= {from:DateTime}
  AND timestamp < {to:DateTime}`, OptionBarColumns, viewName), nil
	}

	return QueryTimeAggregationSQL(interval)
}

// BuildSpotBarSubquery returns a SQL subquery for spot bars using
// ClickHouse named parameters: {symbol:String}, {from:DateTime}, {to:DateTime}.
func BuildSpotBarSubquery(interval string) (string, error) {
	if interval == "1m" {
		return fmt.Sprintf(`SELECT
    %s
FROM crypto_spot_bar_1m
WHERE symbol = {symbol:String}
  AND timestamp >= {from:DateTime}
  AND timestamp < {to:DateTime}`, SpotBarColumns), nil
	}

	if viewName, ok := SpotPrecomputedIntervals[interval]; ok {
		return fmt.Sprintf(`SELECT
    %s
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= {from:DateTime}
  AND timestamp < {to:DateTime}`, SpotBarColumns, viewName), nil
	}

	return QuerySpotAggregationSQL(interval)
}
