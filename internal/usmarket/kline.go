package usmarket

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// KlineInterval defines a K-line aggregation interval.
// Sub-hour intervals use natural time alignment (which aligns to 09:30 ET since
// that minute is on a 5m/15m/30m boundary in UTC).
// Hourly and multi-hour intervals use session_open offset bucketing so bars
// start at 09:30, 10:30, ... rather than UTC whole hours.
// Daily interval groups by market_date.
type KlineInterval struct {
	Suffix  string // table name suffix, e.g. "5m", "1h", "1d"
	Seconds int    // interval in seconds; 0 means daily
}

// KlineIntervals lists all pre-computed K-line intervals.
var KlineIntervals = []KlineInterval{
	{Suffix: "5m", Seconds: 300},
	{Suffix: "15m", Seconds: 900},
	{Suffix: "30m", Seconds: 1800},
	{Suffix: "1h", Seconds: 3600},
	{Suffix: "2h", Seconds: 7200},
	{Suffix: "4h", Seconds: 14400},
	{Suffix: "1d", Seconds: 0},
}

// klineTimeFunc returns the ClickHouse expression used to compute the bucket
// timestamp for a given interval.
func klineTimeFunc(iv KlineInterval) string {
	if iv.Seconds == 0 {
		// Daily: bucket = midnight UTC of the market_date
		return "toDateTime(market_date, 'UTC')"
	}
	if iv.Seconds < 3600 {
		// Sub-hour: natural time alignment (09:30 ET is on a 5/15/30m boundary in UTC)
		switch iv.Seconds {
		case 300:
			return "toStartOfFiveMinutes(timestamp)"
		case 900:
			return "toStartOfFifteenMinutes(timestamp)"
		default:
			return fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL %d second)", iv.Seconds)
		}
	}
	// Session-aligned: offset from session_open
	// bucket = session_open + floor((ts - session_open) / interval) * interval
	return fmt.Sprintf(
		"toDateTime(toUnixTimestamp(session_open) + intDiv(toUnixTimestamp(timestamp) - toUnixTimestamp(session_open), %d) * %d, 'UTC')",
		iv.Seconds, iv.Seconds,
	)
}

// InitOptionKlineSchema creates AggregatingMergeTree tables, materialized views,
// and query views for US option bars at every interval.
// All kline MVs filter to regular-session bars only.
func InitOptionKlineSchema(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		stmts := optionKlineDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("option kline [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[us-options-kline] initialized %s interval", iv.Suffix)
	}
	return nil
}

// InitStockKlineSchema creates AggregatingMergeTree tables, materialized views,
// and query views for US stock bars at every interval.
func InitStockKlineSchema(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		stmts := stockKlineDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("stock kline [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[us-stocks-kline] initialized %s interval", iv.Suffix)
	}
	return nil
}

func optionKlineDDL(iv KlineInterval) []string {
	base := "us_options_bar_1m"
	agg := "us_options_bar_" + iv.Suffix + "_agg"
	mv := "us_options_bar_" + iv.Suffix + "_mv"
	view := "us_options_bar_" + iv.Suffix
	timeFunc := klineTimeFunc(iv)

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts                 DateTime('UTC'),
    symbol             LowCardinality(String),
    underlying         LowCardinality(String),
    option_type        Enum8('C' = 1, 'P' = 2),
    expiration         Date,
    strike             Float64,
    open_state         AggregateFunction(argMin, Float32, DateTime('UTC')),
    high_state         AggregateFunction(max, Float32),
    low_state          AggregateFunction(min, Float32),
    close_state        AggregateFunction(argMax, Float32, DateTime('UTC')),
    underlying_close_state   AggregateFunction(argMax, Float32, DateTime('UTC')),
    implied_volatility_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    delta_state        AggregateFunction(argMax, Float32, DateTime('UTC')),
    gamma_state        AggregateFunction(argMax, Float32, DateTime('UTC')),
    vega_state         AggregateFunction(argMax, Float32, DateTime('UTC')),
    theta_state        AggregateFunction(argMax, Float32, DateTime('UTC')),
    rho_state          AggregateFunction(argMax, Float32, DateTime('UTC')),
    volume_state       AggregateFunction(sum, UInt32),
    transactions_state AggregateFunction(sum, UInt32)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (underlying, symbol, ts)
SETTINGS index_granularity = 8192`, agg)

	dropMV := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, mv)

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW %s
TO %s
AS SELECT
    %s AS ts,
    symbol,
    underlying,
    option_type,
    expiration,
    strike,
    argMinState(open, timestamp)       AS open_state,
    maxState(high)                     AS high_state,
    minState(low)                      AS low_state,
    argMaxState(close, timestamp)      AS close_state,
    argMaxState(underlying_close, timestamp)   AS underlying_close_state,
    argMaxState(implied_volatility, timestamp) AS implied_volatility_state,
    argMaxState(delta, timestamp)      AS delta_state,
    argMaxState(gamma, timestamp)      AS gamma_state,
    argMaxState(vega, timestamp)       AS vega_state,
    argMaxState(theta, timestamp)      AS theta_state,
    argMaxState(rho, timestamp)        AS rho_state,
    sumState(volume)                   AS volume_state,
    sumState(transactions)             AS transactions_state
FROM %s
WHERE is_regular_session = 1
GROUP BY ts, symbol, underlying, option_type, expiration, strike`, mv, agg, timeFunc, base)

	createView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT
    ts AS timestamp,
    symbol,
    underlying,
    option_type,
    expiration,
    strike,
    argMinMerge(open_state)       AS open,
    maxMerge(high_state)          AS high,
    minMerge(low_state)           AS low,
    argMaxMerge(close_state)      AS close,
    argMaxMerge(underlying_close_state)   AS underlying_close,
    argMaxMerge(implied_volatility_state) AS implied_volatility,
    argMaxMerge(delta_state)      AS delta,
    argMaxMerge(gamma_state)      AS gamma,
    argMaxMerge(vega_state)       AS vega,
    argMaxMerge(theta_state)      AS theta,
    argMaxMerge(rho_state)        AS rho,
    sumMerge(volume_state)        AS volume,
    sumMerge(transactions_state)  AS transactions
FROM %s
GROUP BY ts, symbol, underlying, option_type, expiration, strike`, view, agg)

	return []string{createAgg, dropMV, createMV, createView}
}

func stockKlineDDL(iv KlineInterval) []string {
	base := "us_stocks_bar_1m"
	agg := "us_stocks_bar_" + iv.Suffix + "_agg"
	mv := "us_stocks_bar_" + iv.Suffix + "_mv"
	view := "us_stocks_bar_" + iv.Suffix
	timeFunc := klineTimeFunc(iv)

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts                 DateTime('UTC'),
    symbol             LowCardinality(String),
    open_state         AggregateFunction(argMin, Float32, DateTime('UTC')),
    high_state         AggregateFunction(max, Float32),
    low_state          AggregateFunction(min, Float32),
    close_state        AggregateFunction(argMax, Float32, DateTime('UTC')),
    volume_state       AggregateFunction(sum, UInt32),
    transactions_state AggregateFunction(sum, UInt32)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (symbol, ts)
SETTINGS index_granularity = 8192`, agg)

	dropMV := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, mv)

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW %s
TO %s
AS SELECT
    %s AS ts,
    symbol,
    argMinState(open, timestamp)       AS open_state,
    maxState(high)                     AS high_state,
    minState(low)                      AS low_state,
    argMaxState(close, timestamp)      AS close_state,
    sumState(volume)                   AS volume_state,
    sumState(transactions)             AS transactions_state
FROM %s
WHERE is_regular_session = 1
GROUP BY ts, symbol`, mv, agg, timeFunc, base)

	createView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT
    ts AS timestamp,
    symbol,
    argMinMerge(open_state)       AS open,
    maxMerge(high_state)          AS high,
    minMerge(low_state)           AS low,
    argMaxMerge(close_state)      AS close,
    sumMerge(volume_state)        AS volume,
    sumMerge(transactions_state)  AS transactions
FROM %s
GROUP BY ts, symbol`, view, agg)

	return []string{createAgg, dropMV, createMV, createView}
}
