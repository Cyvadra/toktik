package chquery

import (
	"fmt"
	"time"
)

// validAdHocIntervals maps user-facing interval strings to ClickHouse INTERVAL
// expressions. Only values in this map are accepted, preventing SQL injection.
var validAdHocIntervals = map[string]string{
	"1m":  "1 minute",
	"2m":  "2 minute",
	"3m":  "3 minute",
	"5m":  "5 minute",
	"10m": "10 minute",
	"15m": "15 minute",
	"30m": "30 minute",
	"1h":  "1 hour",
	"2h":  "2 hour",
	"3h":  "3 hour",
	"4h":  "4 hour",
	"6h":  "6 hour",
	"8h":  "8 hour",
	"12h": "12 hour",
	"1d":  "1 day",
	"1w":  "1 week",
}

// BuildOptionBarSubquery returns a SQL subquery for option bars with literal values.
func BuildOptionBarSubquery(interval string, symbolID uint64, fromT, toT time.Time) (string, error) {
	symIDLit := UInt64Literal(symbolID)
	fromLit := QuotedDateTime(fromT)
	toLit := QuotedDateTime(toT)

	if interval == "1m" {
		return fmt.Sprintf(`SELECT
    %s
FROM crypto_options_bar_1m
WHERE symbol_id = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')`, OptionBarColumns, symIDLit, fromLit, toLit), nil
	}

	if viewName, ok := CryptoOptionsIntervals[interval]; ok {
		return fmt.Sprintf(`SELECT
    %s
FROM %s
WHERE symbol_id = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')`, OptionBarColumns, viewName, symIDLit, fromLit, toLit), nil
	}

	return QueryTimeAggregationSQL(interval, symbolID, fromT, toT)
}

// BuildSpotBarSubquery returns a SQL subquery for spot bars with literal values.
func BuildSpotBarSubquery(interval string, symbol string, fromT, toT time.Time) (string, error) {
	symLit := QuotedString(symbol)
	fromLit := QuotedDateTime(fromT)
	toLit := QuotedDateTime(toT)

	if interval == "1m" {
		return fmt.Sprintf(`SELECT
    %s
FROM crypto_spot_bar_1m
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')`, SpotBarColumns, symLit, fromLit, toLit), nil
	}

	if viewName, ok := CryptoSpotIntervals[interval]; ok {
		return fmt.Sprintf(`SELECT
    %s
FROM %s
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')`, SpotBarColumns, viewName, symLit, fromLit, toLit), nil
	}

	return QuerySpotAggregationSQL(interval, symbol, fromT, toT)
}

// QueryTimeAggregationSQL returns a SQL query that aggregates 1-minute option bars
// into the requested interval on the fly. This is the fallback for ad-hoc
// intervals that lack pre-computed materialized views.
func QueryTimeAggregationSQL(interval string, symbolID uint64, fromT, toT time.Time) (string, error) {
	chInterval, ok := validAdHocIntervals[interval]
	if !ok {
		return "", fmt.Errorf("unsupported interval: %q", interval)
	}

	symIDLit := UInt64Literal(symbolID)
	fromLit := QuotedDateTime(fromT)
	toLit := QuotedDateTime(toT)

	return fmt.Sprintf(`SELECT
    toStartOfInterval(timestamp, INTERVAL %s) AS timestamp,
    symbol_id,
    base_asset,
    argMin(mark_open, timestamp)              AS mark_open,
    max(mark_high)                            AS mark_high,
    min(mark_low)                             AS mark_low,
    argMax(mark_close, timestamp)             AS mark_close,
    argMin(last_open, timestamp)              AS last_open,
    max(last_high)                            AS last_high,
    min(last_low)                             AS last_low,
    argMax(last_close, timestamp)             AS last_close,
    argMin(bid_open, timestamp)               AS bid_open,
    max(bid_high)                             AS bid_high,
    min(bid_low)                              AS bid_low,
    argMax(bid_close, timestamp)              AS bid_close,
    argMin(ask_open, timestamp)               AS ask_open,
    max(ask_high)                             AS ask_high,
    min(ask_low)                              AS ask_low,
    argMax(ask_close, timestamp)              AS ask_close,
    argMin(mark_iv_open, timestamp)           AS mark_iv_open,
    argMax(mark_iv_close, timestamp)          AS mark_iv_close,
    argMin(bid_iv_open, timestamp)            AS bid_iv_open,
    argMin(ask_iv_open, timestamp)            AS ask_iv_open,
    argMin(delta, timestamp)                  AS delta,
    argMin(gamma, timestamp)                  AS gamma,
    argMin(vega, timestamp)                   AS vega,
    argMin(theta, timestamp)                  AS theta,
    argMin(rho, timestamp)                    AS rho,
    sum(volume)                               AS volume,
    argMax(open_interest, timestamp)          AS open_interest,
    sum(tick_count)                           AS tick_count
FROM crypto_options_bar_1m
WHERE symbol_id = %s
    AND timestamp >= toDateTime(%s, 'UTC')
    AND timestamp < toDateTime(%s, 'UTC')
GROUP BY
    toStartOfInterval(timestamp, INTERVAL %s),
    symbol_id,
    base_asset
ORDER BY timestamp`, chInterval, symIDLit, fromLit, toLit, chInterval), nil
}

// QuerySpotAggregationSQL returns a SQL query that aggregates 1-minute spot bars
// into the requested interval on the fly.
func QuerySpotAggregationSQL(interval string, symbol string, fromT, toT time.Time) (string, error) {
	chInterval, ok := validAdHocIntervals[interval]
	if !ok {
		return "", fmt.Errorf("unsupported interval: %q", interval)
	}

	symLit := QuotedString(symbol)
	fromLit := QuotedDateTime(fromT)
	toLit := QuotedDateTime(toT)

	return fmt.Sprintf(`SELECT
    toStartOfInterval(timestamp, INTERVAL %s) AS timestamp,
    symbol,
    any(price_source)                         AS price_source,
    argMin(open, timestamp)                   AS open,
    max(high)                                 AS high,
    min(low)                                  AS low,
    argMax(close, timestamp)                  AS close,
    sum(volume)                               AS volume,
    sum(tick_count)                           AS tick_count,
    sum(volume_base)                          AS volume_base,
    sum(volume_quote)                         AS volume_quote
FROM crypto_spot_bar_1m
WHERE symbol = %s
    AND timestamp >= toDateTime(%s, 'UTC')
    AND timestamp < toDateTime(%s, 'UTC')
GROUP BY
    toStartOfInterval(timestamp, INTERVAL %s),
    symbol
ORDER BY timestamp`, chInterval, symLit, fromLit, toLit, chInterval), nil
}
