package cryptooptions

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// KlineInterval defines a K-line aggregation interval with the ClickHouse
// time-bucketing expression used to round 1-minute timestamps.
type KlineInterval struct {
	Suffix   string // table name suffix, e.g. "5m", "1h", "1d"
	TimeFunc string // ClickHouse expression applied to `timestamp`
}

// KlineIntervals lists all pre-computed K-line intervals.
// Materialized views aggregate crypto_options_bar_1m on INSERT.
var KlineIntervals = []KlineInterval{
	{Suffix: "5m", TimeFunc: "toStartOfFiveMinutes(timestamp)"},
	{Suffix: "15m", TimeFunc: "toStartOfFifteenMinutes(timestamp)"},
	{Suffix: "30m", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 30 minute)"},
	{Suffix: "1h", TimeFunc: "toStartOfHour(timestamp)"},
	{Suffix: "4h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 4 hour)"},
	{Suffix: "1d", TimeFunc: "toStartOfDay(timestamp)"},
}

// InitKlineSchema creates AggregatingMergeTree tables, materialized views,
// and query views for every interval in KlineIntervals.
func InitKlineSchema(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		stmts := klineDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("kline schema [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[kline] initialized schema for %s interval", iv.Suffix)
	}
	return nil
}

// klineDDL returns the three DDL statements (agg table, materialized view,
// query view) for a single K-line interval.
func klineDDL(iv KlineInterval) []string {
	agg := "crypto_options_bar_" + iv.Suffix + "_agg"
	mv := "crypto_options_bar_" + iv.Suffix + "_mv"
	view := "crypto_options_bar_" + iv.Suffix

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts                           DateTime,
    symbol_id                    UInt32,
    base_asset                   LowCardinality(String),
    mark_open_state              AggregateFunction(argMin, Float32, DateTime),
    mark_high_state              AggregateFunction(max, Float32),
    mark_low_state               AggregateFunction(min, Float32),
    mark_close_state             AggregateFunction(argMax, Float32, DateTime),
    last_open_state              AggregateFunction(argMin, Float32, DateTime),
    last_high_state              AggregateFunction(max, Float32),
    last_low_state               AggregateFunction(min, Float32),
    last_close_state             AggregateFunction(argMax, Float32, DateTime),
    bid_open_state               AggregateFunction(argMin, Float32, DateTime),
    bid_close_state              AggregateFunction(argMax, Float32, DateTime),
    ask_open_state               AggregateFunction(argMin, Float32, DateTime),
    ask_close_state              AggregateFunction(argMax, Float32, DateTime),
    mark_iv_open_state           AggregateFunction(argMin, Float32, DateTime),
    mark_iv_close_state          AggregateFunction(argMax, Float32, DateTime),
    bid_iv_open_state            AggregateFunction(argMin, Float32, DateTime),
    ask_iv_open_state            AggregateFunction(argMin, Float32, DateTime),
    delta_state                  AggregateFunction(argMin, Float32, DateTime),
    gamma_state                  AggregateFunction(argMin, Float32, DateTime),
    vega_state                   AggregateFunction(argMin, Float32, DateTime),
    theta_state                  AggregateFunction(argMin, Float32, DateTime),
    rho_state                    AggregateFunction(argMin, Float32, DateTime),
    underlying_price_open_state  AggregateFunction(argMin, Float32, DateTime),
    underlying_price_close_state AggregateFunction(argMax, Float32, DateTime),
    open_interest_state          AggregateFunction(argMax, Float32, DateTime),
    tick_count_state             AggregateFunction(sum, UInt16)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (base_asset, symbol_id, ts)
SETTINGS index_granularity = 8192`, agg)

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s
TO %s
AS SELECT
    %s AS ts,
    symbol_id,
    base_asset,
    argMinState(mark_open, timestamp)              AS mark_open_state,
    maxState(mark_high)                            AS mark_high_state,
    minState(mark_low)                             AS mark_low_state,
    argMaxState(mark_close, timestamp)             AS mark_close_state,
    argMinState(last_open, timestamp)              AS last_open_state,
    maxState(last_high)                            AS last_high_state,
    minState(last_low)                             AS last_low_state,
    argMaxState(last_close, timestamp)             AS last_close_state,
    argMinState(bid_open, timestamp)               AS bid_open_state,
    argMaxState(bid_close, timestamp)              AS bid_close_state,
    argMinState(ask_open, timestamp)               AS ask_open_state,
    argMaxState(ask_close, timestamp)              AS ask_close_state,
    argMinState(mark_iv_open, timestamp)           AS mark_iv_open_state,
    argMaxState(mark_iv_close, timestamp)          AS mark_iv_close_state,
    argMinState(bid_iv_open, timestamp)            AS bid_iv_open_state,
    argMinState(ask_iv_open, timestamp)            AS ask_iv_open_state,
    argMinState(delta, timestamp)                  AS delta_state,
    argMinState(gamma, timestamp)                  AS gamma_state,
    argMinState(vega, timestamp)                   AS vega_state,
    argMinState(theta, timestamp)                  AS theta_state,
    argMinState(rho, timestamp)                    AS rho_state,
    argMinState(underlying_price_open, timestamp)  AS underlying_price_open_state,
    argMaxState(underlying_price_close, timestamp) AS underlying_price_close_state,
    argMaxState(open_interest, timestamp)           AS open_interest_state,
    sumState(tick_count)                           AS tick_count_state
FROM crypto_options_bar_1m
GROUP BY ts, symbol_id, base_asset`, mv, agg, iv.TimeFunc)

	createView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT
    ts AS timestamp,
    symbol_id,
    base_asset,
    argMinMerge(mark_open_state)              AS mark_open,
    maxMerge(mark_high_state)                 AS mark_high,
    minMerge(mark_low_state)                  AS mark_low,
    argMaxMerge(mark_close_state)             AS mark_close,
    argMinMerge(last_open_state)              AS last_open,
    maxMerge(last_high_state)                 AS last_high,
    minMerge(last_low_state)                  AS last_low,
    argMaxMerge(last_close_state)             AS last_close,
    argMinMerge(bid_open_state)               AS bid_open,
    argMaxMerge(bid_close_state)              AS bid_close,
    argMinMerge(ask_open_state)               AS ask_open,
    argMaxMerge(ask_close_state)              AS ask_close,
    argMinMerge(mark_iv_open_state)           AS mark_iv_open,
    argMaxMerge(mark_iv_close_state)          AS mark_iv_close,
    argMinMerge(bid_iv_open_state)            AS bid_iv_open,
    argMinMerge(ask_iv_open_state)            AS ask_iv_open,
    argMinMerge(delta_state)                  AS delta,
    argMinMerge(gamma_state)                  AS gamma,
    argMinMerge(vega_state)                   AS vega,
    argMinMerge(theta_state)                  AS theta,
    argMinMerge(rho_state)                    AS rho,
    argMinMerge(underlying_price_open_state)  AS underlying_price_open,
    argMaxMerge(underlying_price_close_state) AS underlying_price_close,
    argMaxMerge(open_interest_state)          AS open_interest,
    sumMerge(tick_count_state)                AS tick_count
FROM %s
GROUP BY ts, symbol_id, base_asset`, view, agg)

	return []string{createAgg, createMV, createView}
}

// validAdHocIntervals maps user-facing interval strings to ClickHouse INTERVAL
// expressions. Only values in this map are accepted by QueryTimeAggregationSQL,
// preventing SQL injection.
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
	"4h":  "4 hour",
	"6h":  "6 hour",
	"8h":  "8 hour",
	"12h": "12 hour",
	"1d":  "1 day",
	"1w":  "1 week",
}

// PrecomputedIntervals maps interval suffixes that have materialized views
// to their view name. Use these when the interval is available pre-computed.
var PrecomputedIntervals = map[string]string{
	"5m":  "crypto_options_bar_5m",
	"15m": "crypto_options_bar_15m",
	"30m": "crypto_options_bar_30m",
	"1h":  "crypto_options_bar_1h",
	"4h":  "crypto_options_bar_4h",
	"1d":  "crypto_options_bar_1d",
}

// QueryTimeAggregationSQL returns a SQL query that aggregates 1-minute bars
// into the requested interval on the fly. This is the fallback for ad-hoc
// intervals that lack pre-computed materialized views.
//
// The returned query expects ClickHouse named parameters:
//
//	{symbol_id:UInt32}, {from:DateTime}, {to:DateTime}
func QueryTimeAggregationSQL(interval string) (string, error) {
	chInterval, ok := validAdHocIntervals[interval]
	if !ok {
		return "", fmt.Errorf("unsupported interval: %q", interval)
	}

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
    argMax(bid_close, timestamp)              AS bid_close,
    argMin(ask_open, timestamp)               AS ask_open,
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
    argMin(underlying_price_open, timestamp)  AS underlying_price_open,
    argMax(underlying_price_close, timestamp) AS underlying_price_close,
    argMax(open_interest, timestamp)           AS open_interest,
    sum(tick_count)                            AS tick_count
FROM crypto_options_bar_1m
WHERE symbol_id = {symbol_id:UInt32}
  AND timestamp >= {from:DateTime}
  AND timestamp < {to:DateTime}
GROUP BY
    toStartOfInterval(timestamp, INTERVAL %s),
    symbol_id,
    base_asset
ORDER BY timestamp`, chInterval, chInterval), nil
}
