package chquery

import "fmt"

// ----- Crypto Spot SQL -----

// CryptoSpotSymbolsBase is the base query for listing crypto spot symbols.
const CryptoSpotSymbolsBase = `SELECT symbol FROM crypto_spot_bar_1m WHERE 1 = 1`

// CryptoSpotBarsSQL returns a query for crypto spot bars from a specific table.
func CryptoSpotBarsSQL(tableName string) string {
	return fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    open,
    high,
    low,
    close,
	volume,
    tick_count
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT {limit:UInt32}`, tableName)
}

// CryptoSpotUnderlyingCloseHistory returns a query for daily close prices
// used in volatility feature computation.
const CryptoSpotUnderlyingCloseHistoryBase = `SELECT day, close
FROM (
	SELECT
		toDate(timestamp) AS day,
		toFloat64(argMax(close, timestamp)) AS close
	FROM crypto_spot_bar_1m
	WHERE symbol = {symbol:String}`

// CryptoOptionsIVHistoryBase returns the base query for crypto options implied volatility history.
const CryptoOptionsIVHistoryBase = `SELECT day, iv
FROM (
	SELECT
		toDate(timestamp) AS day,
		avgIf(toFloat64(mark_iv_close), isFinite(mark_iv_close) AND mark_iv_close > 0) AS iv
	FROM crypto_options_bar_1m
	WHERE base_asset = {underlying:String}`

// CryptoOptionsListUnderlyings lists distinct base assets.
const CryptoOptionsListUnderlyings = `SELECT base_asset FROM crypto_options_bar_1m GROUP BY base_asset ORDER BY base_asset`

// LatestCryptoOptionsFeatureDate returns the latest date for crypto options features.
const LatestCryptoOptionsFeatureDate = `SELECT ifNull(toDate(maxOrNull(timestamp)), toDate('1970-01-01'))
FROM crypto_options_bar_1m
WHERE base_asset = {underlying:String}`
