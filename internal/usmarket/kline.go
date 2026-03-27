package usmarket

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// KlineInterval defines a K-line aggregation interval.
type KlineInterval struct {
	Suffix   string // table name suffix, e.g. "5m", "1h", "1d"
	TimeFunc string // ClickHouse expression applied to `timestamp`
}

// KlineIntervals lists all pre-computed K-line intervals.
var KlineIntervals = []KlineInterval{
	{Suffix: "5m", TimeFunc: "toStartOfFiveMinutes(timestamp)"},
	{Suffix: "15m", TimeFunc: "toStartOfFifteenMinutes(timestamp)"},
	{Suffix: "30m", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 30 minute)"},
	{Suffix: "1h", TimeFunc: "toStartOfHour(timestamp)"},
	{Suffix: "2h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 2 hour)"},
	{Suffix: "4h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 4 hour)"},
	{Suffix: "1d", TimeFunc: "toStartOfDay(timestamp)"},
}

// InitOptionKlineSchema creates AggregatingMergeTree tables, materialized views,
// and query views for US option bars at every interval.
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
    volume_state       AggregateFunction(sum, UInt32),
    transactions_state AggregateFunction(sum, UInt32)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (underlying, symbol, ts)
SETTINGS index_granularity = 8192`, agg)

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s
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
    sumState(volume)                   AS volume_state,
    sumState(transactions)             AS transactions_state
FROM %s
GROUP BY ts, symbol, underlying, option_type, expiration, strike`, mv, agg, iv.TimeFunc, base)

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
    sumMerge(volume_state)        AS volume,
    sumMerge(transactions_state)  AS transactions
FROM %s
GROUP BY ts, symbol, underlying, option_type, expiration, strike`, view, agg)

	return []string{createAgg, createMV, createView}
}

func stockKlineDDL(iv KlineInterval) []string {
	base := "us_stocks_bar_1m"
	agg := "us_stocks_bar_" + iv.Suffix + "_agg"
	mv := "us_stocks_bar_" + iv.Suffix + "_mv"
	view := "us_stocks_bar_" + iv.Suffix

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

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s
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
GROUP BY ts, symbol`, mv, agg, iv.TimeFunc, base)

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

	return []string{createAgg, createMV, createView}
}
