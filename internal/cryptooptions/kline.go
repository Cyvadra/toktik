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
	{Suffix: "2h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 2 hour)"},
	{Suffix: "3h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 3 hour)"},
	{Suffix: "4h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 4 hour)"},
	{Suffix: "6h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 6 hour)"},
	{Suffix: "8h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 8 hour)"},
	{Suffix: "12h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 12 hour)"},
	{Suffix: "1d", TimeFunc: "toStartOfDay(timestamp)"},
}

// InitKlineSchema creates AggregatingMergeTree tables, materialized views,
// and query views for option bars at every interval in KlineIntervals.
func InitKlineSchema(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		stmts := optionKlineDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("kline schema [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[kline] initialized schema for %s interval", iv.Suffix)
	}
	return nil
}

// InitSpotKlineSchema creates AggregatingMergeTree tables, materialized views,
// and query views for spot bars at every interval in KlineIntervals.
func InitSpotKlineSchema(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		stmts := spotKlineDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("spot kline schema [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[spot-kline] initialized schema for %s interval", iv.Suffix)
	}
	return nil
}

// optionKlineDDL returns the three DDL statements (agg table, materialized view,
// query view) for a single option K-line interval using crypto_options prefix.
func optionKlineDDL(iv KlineInterval) []string {
	return optionKlineDDLWithPrefix("crypto_options", iv)
}

// optionKlineDDLWithPrefix returns option K-line DDL for the given table prefix.
func optionKlineDDLWithPrefix(prefix string, iv KlineInterval) []string {
	base := prefix + "_bar_1m"
	agg := prefix + "_bar_" + iv.Suffix + "_agg"
	mv := prefix + "_bar_" + iv.Suffix + "_mv"
	view := prefix + "_bar_" + iv.Suffix

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts                           DateTime('UTC'),
    symbol_id                    UInt64,
    base_asset                   LowCardinality(String),
    mark_open_state              AggregateFunction(argMin, Float32, DateTime('UTC')),
    mark_high_state              AggregateFunction(max, Float32),
    mark_low_state               AggregateFunction(min, Float32),
    mark_close_state             AggregateFunction(argMax, Float32, DateTime('UTC')),
    last_open_state              AggregateFunction(argMin, Float32, DateTime('UTC')),
    last_high_state              AggregateFunction(max, Float32),
    last_low_state               AggregateFunction(min, Float32),
    last_close_state             AggregateFunction(argMax, Float32, DateTime('UTC')),
    bid_open_state               AggregateFunction(argMin, Float32, DateTime('UTC')),
    bid_high_state               AggregateFunction(max, Float32),
    bid_low_state                AggregateFunction(min, Float32),
    bid_close_state              AggregateFunction(argMax, Float32, DateTime('UTC')),
    ask_open_state               AggregateFunction(argMin, Float32, DateTime('UTC')),
    ask_high_state               AggregateFunction(max, Float32),
    ask_low_state                AggregateFunction(min, Float32),
    ask_close_state              AggregateFunction(argMax, Float32, DateTime('UTC')),
    mark_iv_open_state           AggregateFunction(argMin, Float32, DateTime('UTC')),
    mark_iv_close_state          AggregateFunction(argMax, Float32, DateTime('UTC')),
    bid_iv_open_state            AggregateFunction(argMin, Float32, DateTime('UTC')),
    ask_iv_open_state            AggregateFunction(argMin, Float32, DateTime('UTC')),
    delta_state                  AggregateFunction(argMin, Float32, DateTime('UTC')),
    gamma_state                  AggregateFunction(argMin, Float32, DateTime('UTC')),
    vega_state                   AggregateFunction(argMin, Float32, DateTime('UTC')),
    theta_state                  AggregateFunction(argMin, Float32, DateTime('UTC')),
    rho_state                    AggregateFunction(argMin, Float32, DateTime('UTC')),
    volume_state                 AggregateFunction(sum, Float64),
    open_interest_state          AggregateFunction(argMax, Float32, DateTime('UTC')),
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
    maxState(bid_high)                             AS bid_high_state,
    minState(bid_low)                              AS bid_low_state,
    argMaxState(bid_close, timestamp)              AS bid_close_state,
    argMinState(ask_open, timestamp)               AS ask_open_state,
    maxState(ask_high)                             AS ask_high_state,
    minState(ask_low)                              AS ask_low_state,
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
    sumState(volume)                               AS volume_state,
    argMaxState(open_interest, timestamp)          AS open_interest_state,
    sumState(tick_count)                           AS tick_count_state
FROM %s
GROUP BY ts, symbol_id, base_asset`, mv, agg, iv.TimeFunc, base)

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
    maxMerge(bid_high_state)                  AS bid_high,
    minMerge(bid_low_state)                   AS bid_low,
    argMaxMerge(bid_close_state)              AS bid_close,
    argMinMerge(ask_open_state)               AS ask_open,
    maxMerge(ask_high_state)                  AS ask_high,
    minMerge(ask_low_state)                   AS ask_low,
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
    sumMerge(volume_state)                    AS volume,
    argMaxMerge(open_interest_state)          AS open_interest,
    sumMerge(tick_count_state)                AS tick_count
FROM %s
GROUP BY ts, symbol_id, base_asset`, view, agg)

	return []string{createAgg, createMV, createView}
}

// spotKlineDDL returns the three DDL statements for a single spot K-line interval using crypto_spot prefix.
func spotKlineDDL(iv KlineInterval) []string {
	return spotKlineDDLWithPrefix("crypto_spot", iv)
}

// spotKlineDDLWithPrefix returns spot K-line DDL for the given table prefix.
func spotKlineDDLWithPrefix(prefix string, iv KlineInterval) []string {
	base := prefix + "_bar_1m"
	agg := prefix + "_bar_" + iv.Suffix + "_agg"
	mv := prefix + "_bar_" + iv.Suffix + "_mv"
	view := prefix + "_bar_" + iv.Suffix

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts                    DateTime('UTC'),
    symbol                LowCardinality(String),
    price_source          LowCardinality(String),
    open_state            AggregateFunction(argMin, Float32, DateTime('UTC')),
    high_state            AggregateFunction(max, Float32),
    low_state             AggregateFunction(min, Float32),
    close_state           AggregateFunction(argMax, Float32, DateTime('UTC')),
    volume_state          AggregateFunction(sum, Float64),
    tick_count_state      AggregateFunction(sum, UInt32),
    volume_base_state     AggregateFunction(sum, Float64),
    volume_quote_state    AggregateFunction(sum, Float64)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (symbol, ts)
SETTINGS index_granularity = 8192`, agg)

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s
TO %s
AS SELECT
    %s AS ts,
    symbol,
    any(price_source)                            AS price_source,
    argMinState(open, timestamp)                 AS open_state,
    maxState(high)                               AS high_state,
    minState(low)                                AS low_state,
    argMaxState(close, timestamp)                AS close_state,
    sumState(volume)                            AS volume_state,
    sumState(tick_count)                         AS tick_count_state,
    sumState(volume_base)                        AS volume_base_state,
    sumState(volume_quote)                       AS volume_quote_state
FROM %s
GROUP BY ts, symbol`, mv, agg, iv.TimeFunc, base)

	createView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT
    ts AS timestamp,
    symbol,
    any(price_source)                 AS price_source,
    argMinMerge(open_state)           AS open,
    maxMerge(high_state)              AS high,
    minMerge(low_state)               AS low,
    argMaxMerge(close_state)          AS close,
    sumMerge(volume_state)            AS volume,
    sumMerge(tick_count_state)        AS tick_count,
    sumMerge(volume_base_state)       AS volume_base,
    sumMerge(volume_quote_state)      AS volume_quote
FROM %s
GROUP BY ts, symbol`, view, agg)

	return []string{createAgg, createMV, createView}
}

// InitKlineSchemaForPrefix creates kline aggregation tables and views
// for both option and spot bars using the specified table prefixes.
func InitKlineSchemaForPrefix(ctx context.Context, conn driver.Conn, optionsPrefix, spotPrefix string) error {
	for _, iv := range KlineIntervals {
		stmts := optionKlineDDLWithPrefix(optionsPrefix, iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("kline schema [%s/%s]: %w", optionsPrefix, iv.Suffix, err)
			}
		}
		log.Printf("[kline] initialized %s schema for %s interval", optionsPrefix, iv.Suffix)

		stmts = spotKlineDDLWithPrefix(spotPrefix, iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("spot kline schema [%s/%s]: %w", spotPrefix, iv.Suffix, err)
			}
		}
		log.Printf("[kline] initialized %s schema for %s interval", spotPrefix, iv.Suffix)
	}
	return nil
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
	"3h":  "3 hour",
	"4h":  "4 hour",
	"6h":  "6 hour",
	"8h":  "8 hour",
	"12h": "12 hour",
	"1d":  "1 day",
	"1w":  "1 week",
}

// PrecomputedIntervals maps option interval suffixes that have materialized
// views to their view name. Use these when the interval is available pre-computed.
var PrecomputedIntervals = map[string]string{
	"5m":  "crypto_options_bar_5m",
	"15m": "crypto_options_bar_15m",
	"30m": "crypto_options_bar_30m",
	"1h":  "crypto_options_bar_1h",
	"2h":  "crypto_options_bar_2h",
	"3h":  "crypto_options_bar_3h",
	"4h":  "crypto_options_bar_4h",
	"6h":  "crypto_options_bar_6h",
	"8h":  "crypto_options_bar_8h",
	"12h": "crypto_options_bar_12h",
	"1d":  "crypto_options_bar_1d",
}

// SpotPrecomputedIntervals maps spot interval suffixes that have materialized
// views to their view name.
var SpotPrecomputedIntervals = map[string]string{
	"5m":  "crypto_spot_bar_5m",
	"15m": "crypto_spot_bar_15m",
	"30m": "crypto_spot_bar_30m",
	"1h":  "crypto_spot_bar_1h",
	"2h":  "crypto_spot_bar_2h",
	"3h":  "crypto_spot_bar_3h",
	"4h":  "crypto_spot_bar_4h",
	"6h":  "crypto_spot_bar_6h",
	"8h":  "crypto_spot_bar_8h",
	"12h": "crypto_spot_bar_12h",
	"1d":  "crypto_spot_bar_1d",
}

// QueryTimeAggregationSQL returns a SQL query that aggregates 1-minute option bars
// into the requested interval on the fly. This is the fallback for ad-hoc
// intervals that lack pre-computed materialized views.
//
// The returned query expects ClickHouse named parameters:
//
//	{symbol_id:UInt64}, {from:String}, {to:String}
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
WHERE symbol_id = {symbol_id:UInt64}
    AND timestamp >= toDateTime({from:String}, 'UTC')
    AND timestamp < toDateTime({to:String}, 'UTC')
GROUP BY
    toStartOfInterval(timestamp, INTERVAL %s),
    symbol_id,
    base_asset
ORDER BY timestamp`, chInterval, chInterval), nil
}

// QuerySpotAggregationSQL returns a SQL query that aggregates 1-minute spot bars
// into the requested interval on the fly.
func QuerySpotAggregationSQL(interval string) (string, error) {
	chInterval, ok := validAdHocIntervals[interval]
	if !ok {
		return "", fmt.Errorf("unsupported interval: %q", interval)
	}

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
WHERE symbol = {symbol:String}
    AND timestamp >= toDateTime({from:String}, 'UTC')
    AND timestamp < toDateTime({to:String}, 'UTC')
GROUP BY
    toStartOfInterval(timestamp, INTERVAL %s),
    symbol
ORDER BY timestamp`, chInterval, chInterval), nil
}
