package chquery

import (
	"fmt"
	"strings"
	"time"
)

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
func CryptoOptionsBarsWithUnderlyingSQL(barSourceSQL, spotSourceSQL string, limit int) string {
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
LIMIT %d`, CryptoOptionsImpliedVolatilityExpr("b"), barSourceSQL, spotSourceSQL, limit)
}

// CryptoOptionsGreeksSQL returns a query for greeks time series,
// given option and spot subqueries.
func CryptoOptionsGreeksSQL(barSourceSQL, spotSourceSQL string, limit int) string {
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
LIMIT %d`, CryptoOptionsImpliedVolatilityExpr("b"), barSourceSQL, spotSourceSQL, limit)
}

// CryptoOptionsChainSQL returns a query for crypto option chain snapshots.
// chainView is the precomputed chain view (arrays per row); spotTable is the
// matching spot kline table used to populate underlying_close.
func CryptoOptionsChainSQL(chainView, spotTable, baseAsset string, fromT, toT time.Time, limit int) string {
	return fmt.Sprintf(`
SELECT
    e.timestamp,
    m.symbol_id,
    m.symbol,
    m.option_type,
    m.expiration,
    m.strike_price,
    e.mark_price        AS mark_close,
    e.bid_price         AS bid_close,
    e.ask_price         AS ask_close,
    e.mark_iv,
    e.delta,
    e.gamma,
    e.vega,
    e.theta,
    e.rho,
    e.volume,
    e.open_interest,
    toUInt16(e.tick_count) AS tick_count,
    ifNull(s.close, toFloat32(0)) AS underlying_close
FROM (
    SELECT
        timestamp,
        base_asset,
        sid AS symbol_id,
        d   AS delta,
        g   AS gamma,
        v   AS vega,
        t   AS theta,
        r   AS rho,
        bp  AS bid_price,
        ap  AS ask_price,
        mp  AS mark_price,
        mi  AS mark_iv,
        vol AS volume,
        tc  AS tick_count,
        oi  AS open_interest
    FROM %s
    ARRAY JOIN
        symbol_ids     AS sid,
        deltas         AS d,
        gammas         AS g,
        vegas          AS v,
        thetas         AS t,
        rhos           AS r,
        bid_prices     AS bp,
        ask_prices     AS ap,
        mark_prices    AS mp,
        mark_ivs       AS mi,
        volumes        AS vol,
        tick_counts    AS tc,
        open_interests AS oi
    WHERE base_asset = %s
      AND timestamp >= toDateTime(%s, 'UTC')
      AND timestamp <= toDateTime(%s, 'UTC')
) AS e
INNER JOIN (SELECT * FROM crypto_options_symbol_meta FINAL) AS m ON m.symbol_id = e.symbol_id
LEFT JOIN %s AS s ON s.timestamp = e.timestamp AND s.symbol = e.base_asset
ORDER BY e.timestamp ASC, m.strike_price ASC
LIMIT %d
`, chainView, QuotedString(baseAsset), QuotedDateTime(fromT), QuotedDateTime(toT), spotTable, limit)
}

// CryptoOptionsChainTimestampsSQL returns daily chain snapshot timestamps.
func CryptoOptionsChainTimestampsSQL(chainView, baseAsset string, fromT, toT time.Time) string {
	return fmt.Sprintf(`SELECT timestamp
FROM %s
WHERE base_asset = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')
GROUP BY timestamp
ORDER BY timestamp ASC`, chainView, QuotedString(baseAsset), QuotedDateTime(fromT), QuotedDateTime(toT))
}

// CryptoOptionsChainPointsAtTimestampsSQL expands IV and OI points for selected snapshots.
func CryptoOptionsChainPointsAtTimestampsSQL(chainView, baseAsset string, timestamps []time.Time) string {
	quoted := make([]string, len(timestamps))
	for i, timestamp := range timestamps {
		quoted[i] = "toDateTime(" + QuotedDateTime(timestamp) + ", 'UTC')"
	}
	return fmt.Sprintf(`SELECT
    e.timestamp,
    m.expiration,
    m.option_type,
    m.strike_price,
    e.mark_iv,
    e.open_interest
FROM (
    SELECT
        timestamp,
        sid AS symbol_id,
        mi AS mark_iv,
        oi AS open_interest
    FROM %s
    ARRAY JOIN
        symbol_ids AS sid,
        mark_ivs AS mi,
        open_interests AS oi
    WHERE base_asset = %s
      AND timestamp IN (%s)
) AS e
INNER JOIN (SELECT * FROM crypto_options_symbol_meta FINAL) AS m ON m.symbol_id = e.symbol_id
ORDER BY e.timestamp ASC, m.expiration ASC, m.option_type ASC, m.strike_price ASC`, chainView, QuotedString(baseAsset), strings.Join(quoted, ", "))
}

// CryptoOptionsSymbolCollisions returns a query for checking symbol ID collisions.
const CryptoOptionsSymbolCollisions = `SELECT symbol_id, symbol FROM crypto_options_symbol_meta FINAL WHERE symbol_id IN ({ids:Array(UInt64)})`

// CryptoOptionsInsertSymbolMeta is the INSERT statement for symbol metadata.
const CryptoOptionsInsertSymbolMeta = `INSERT INTO crypto_options_symbol_meta`

// CryptoOptionsInsertBar is the INSERT statement for option bars.
const CryptoOptionsInsertBar = `INSERT INTO crypto_options_bar_1m`

// CryptoSpotInsertBar is the INSERT statement for spot bars.
const CryptoSpotInsertBar = `INSERT INTO crypto_spot_bar_1m`
