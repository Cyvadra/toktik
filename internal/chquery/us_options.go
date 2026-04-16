package chquery

import "fmt"

// ----- US Options SQL -----

// USOptionSymbolsBase is the base query for listing US option symbols.
const USOptionSymbolsBase = `SELECT
    symbol,
    anyLast(underlying) AS underlying,
    CAST(anyLast(option_type), 'String') AS option_type,
    anyLast(expiration) AS expiration,
    anyLast(strike) AS strike
FROM us_options_bar_1m
WHERE underlying = {underlying:String}`

// USOptionBarsSQL returns a query for US option bars from a specific table,
// with an optional session condition suffix.
func USOptionBarsSQL(tableName, sessionCondition string) string {
	return fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    underlying,
    CAST(option_type, 'String') AS option_type,
    expiration,
    strike,
    open,
    high,
    low,
    close,
    underlying_close,
    implied_volatility,
    delta,
    gamma,
    vega,
    theta,
    rho,
	toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')%s
ORDER BY timestamp
LIMIT {limit:UInt32}`, tableName, sessionCondition)
}

// USOptionGreeksSQL returns a query for US option greeks from a specific table,
// with an optional session condition suffix.
func USOptionGreeksSQL(tableName, sessionCondition string) string {
	return fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    underlying,
    CAST(option_type, 'String') AS option_type,
    expiration,
    strike,
    underlying_close,
    implied_volatility,
    delta,
    gamma,
    vega,
    theta,
    rho,
	toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')%s
ORDER BY timestamp
LIMIT {limit:UInt32}`, tableName, sessionCondition)
}

// USOptionChainSQL returns a query for US option chain snapshots.
func USOptionChainSQL(viewName string) string {
	return fmt.Sprintf(`SELECT
    timestamp,
    underlying,
    symbols,
    arrayMap(x -> CAST(x, 'String'), option_types) AS option_types,
    expirations,
    strikes,
    close_prices,
    underlying_closes,
    implied_volatilities,
    deltas,
    gammas,
    vegas,
    thetas,
    rhos,
    volumes,
    transactions
FROM %s
WHERE underlying = {underlying:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT {limit:UInt32}`, viewName)
}

// USOptionsListUnderlyings lists distinct underlyings in US options data.
const USOptionsListUnderlyings = `SELECT underlying FROM us_options_bar_1m GROUP BY underlying ORDER BY underlying`

// LatestUSOptionsFeatureDate returns the latest regular-session market_date for US options.
const LatestUSOptionsFeatureDate = `SELECT ifNull(maxOrNull(market_date), toDate('1970-01-01'))
FROM us_options_bar_1m
WHERE underlying = {underlying:String}
  AND is_regular_session = 1`

// LatestUSStocksFeatureDate returns the latest regular-session market_date for US stocks.
const LatestUSStocksFeatureDate = `SELECT ifNull(maxOrNull(market_date), toDate('1970-01-01'))
FROM us_stocks_bar_1m
WHERE symbol = {underlying:String}
  AND is_regular_session = 1`
