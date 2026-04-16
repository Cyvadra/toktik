package chquery

import "fmt"

// CryptoOptionsImpliedVolatilityExpr returns the reusable bar-level IV
// expression for crypto option queries.
func CryptoOptionsImpliedVolatilityExpr(alias string) string {
	if alias == "" {
		alias = "b"
	}
	return fmt.Sprintf(`if(
    isFinite(%[1]s.mark_iv_close) AND %[1]s.mark_iv_close > 0,
    %[1]s.mark_iv_close,
    if(isFinite(%[1]s.mark_iv_open), %[1]s.mark_iv_open, toFloat32(0))
)`, alias)
}

// ----- Crypto Options SQL -----

// CryptoOptionsSymbolQuery is the base query for crypto option symbol metadata.
const CryptoOptionsSymbolQuery = `SELECT symbol_id, symbol, base_asset, option_type, strike_price, expiration, underlying_index
FROM crypto_options_symbol_meta FINAL`

// CryptoOptionsBarsWithUnderlyingSQL returns a query that JOINs option bars
// with spot bars, given option and spot subqueries.
func CryptoOptionsBarsWithUnderlyingSQL(barSourceSQL, spotSourceSQL string) string {
	return fmt.Sprintf(`SELECT
    b.timestamp, b.symbol_id, b.base_asset,
    b.mark_open, b.mark_high, b.mark_low, b.mark_close,
    b.last_open, b.last_high, b.last_low, b.last_close,
    b.bid_open, b.bid_high, b.bid_low, b.bid_close,
    b.ask_open, b.ask_high, b.ask_low, b.ask_close,
    %s AS implied_volatility,
    b.mark_iv_open, b.mark_iv_close, b.bid_iv_open, b.ask_iv_open,
    b.delta, b.gamma, b.vega, b.theta, b.rho,
    ifNull(u.open, toFloat32(0))  AS underlying_price_open,
    ifNull(u.high, toFloat32(0))  AS underlying_price_high,
    ifNull(u.low, toFloat32(0))   AS underlying_price_low,
    ifNull(u.close, toFloat32(0)) AS underlying_price_close,
	b.volume,
    b.open_interest,
    toUInt16(b.tick_count) AS tick_count
FROM (%s) AS b
LEFT JOIN (%s) AS u
    ON u.timestamp = b.timestamp AND u.symbol = b.base_asset
ORDER BY b.timestamp
LIMIT {limit:UInt32}`, CryptoOptionsImpliedVolatilityExpr("b"), barSourceSQL, spotSourceSQL)
}

// CryptoOptionsGreeksSQL returns a query for greeks time series,
// given option and spot subqueries.
func CryptoOptionsGreeksSQL(barSourceSQL, spotSourceSQL string) string {
	return fmt.Sprintf(`SELECT
    b.timestamp, b.symbol_id,
    b.delta, b.gamma, b.vega, b.theta, b.rho,
    %s AS implied_volatility,
    b.mark_iv_open, b.mark_iv_close,
    ifNull(u.open, toFloat32(0))  AS underlying_price_open,
    ifNull(u.high, toFloat32(0))  AS underlying_price_high,
    ifNull(u.low, toFloat32(0))   AS underlying_price_low,
    ifNull(u.close, toFloat32(0)) AS underlying_price_close,
    b.open_interest
FROM (%s) AS b
LEFT JOIN (%s) AS u
    ON u.timestamp = b.timestamp AND u.symbol = b.base_asset
ORDER BY b.timestamp
LIMIT {limit:UInt32}`, CryptoOptionsImpliedVolatilityExpr("b"), barSourceSQL, spotSourceSQL)
}

// CryptoOptionsChainSQL returns a query for crypto option chain snapshots
// given a precomputed chain view name.
func CryptoOptionsChainSQL(chainView string) string {
	return fmt.Sprintf(`
SELECT
    c.timestamp,
    m.symbol_id,
    m.symbol,
    m.option_type,
    m.expiration,
    m.strike_price,
    c.mark_close,
    c.bid_close,
    c.ask_close,
    c.mark_iv,
    c.delta,
    c.gamma,
    c.vega,
    c.theta,
    c.rho,
	c.volume,
    c.open_interest,
    c.tick_count,
    c.underlying_close
FROM %s AS c
INNER JOIN crypto_options_symbol_meta FINAL AS m ON m.symbol_id = c.symbol_id
WHERE c.base_asset = {base_asset:String}
  AND c.timestamp >= {from:DateTime('UTC')}
  AND c.timestamp <= {to:DateTime('UTC')}
ORDER BY c.timestamp ASC, m.strike_price ASC
LIMIT {limit:UInt32}
`, chainView)
}

// CryptoOptionsSymbolCollisions returns a query for checking symbol ID collisions.
const CryptoOptionsSymbolCollisions = `SELECT symbol_id, symbol FROM crypto_options_symbol_meta FINAL WHERE symbol_id IN ({ids:Array(UInt64)})`

// CryptoOptionsInsertSymbolMeta is the INSERT statement for symbol metadata.
const CryptoOptionsInsertSymbolMeta = `INSERT INTO crypto_options_symbol_meta`

// CryptoOptionsInsertBar is the INSERT statement for option bars.
const CryptoOptionsInsertBar = `INSERT INTO crypto_options_bar_1m`

// CryptoSpotInsertBar is the INSERT statement for spot bars.
const CryptoSpotInsertBar = `INSERT INTO crypto_spot_bar_1m`
