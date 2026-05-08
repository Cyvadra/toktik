package chquery

import "fmt"

// ForexSymbolsBase is the base query for listing forex symbols.
const ForexSymbolsBase = `SELECT symbol
FROM forex_bar_1m
WHERE 1 = 1`

// ForexBarsSQL returns a query for forex bars from a specific table.
func ForexBarsSQL(tableName string) string {
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
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT {limit:UInt32}`, tableName)
}
