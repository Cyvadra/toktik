package chquery

import "fmt"

// ----- US Stocks SQL -----

// USStockSymbolsBase is the base query for listing US stock symbols.
const USStockSymbolsBase = `SELECT symbol
FROM us_stocks_bar_1m
WHERE 1 = 1`

// USStockBarsSQL returns a query for US stock bars from a specific table,
// with an optional session condition suffix.
func USStockBarsSQL(tableName, sessionCondition string) string {
	return fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    open,
    high,
    low,
    close,
	toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')%s
ORDER BY timestamp
LIMIT {limit:UInt32}`, tableName, sessionCondition)
}

// USStocksUnderlyingCloseHistoryBase returns the base query for daily close prices
// used in volatility feature computation.
const USStocksUnderlyingCloseHistoryBase = `SELECT day, close
FROM (
	SELECT
		toDate(timestamp) AS day,
		toFloat64(close) AS close
	FROM us_stocks_bar_1d
	WHERE symbol = {symbol:String}
	`

// USOptionsIVHistoryBase returns the base query for US options implied volatility history.
const USOptionsIVHistoryBase = `SELECT day, iv
FROM (
	SELECT
		toDate(timestamp) AS day,
		avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0) AS iv
	FROM us_options_bar_1d
	WHERE underlying = {underlying:String}
	`
