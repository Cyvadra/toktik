package forexmarket

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// KlineInterval defines a precomputed forex K-line aggregation interval.
type KlineInterval struct {
	Suffix  string
	Seconds int // 0 means daily
}

// KlineIntervals lists all pre-computed forex K-line intervals.
var KlineIntervals = []KlineInterval{
	{Suffix: "5m", Seconds: 300},
	{Suffix: "15m", Seconds: 900},
	{Suffix: "30m", Seconds: 1800},
	{Suffix: "1h", Seconds: 3600},
	{Suffix: "2h", Seconds: 7200},
	{Suffix: "4h", Seconds: 14400},
	{Suffix: "1d", Seconds: 0},
}

// PrecomputedIntervals maps supported forex intervals to their materialized views.
var PrecomputedIntervals = map[string]string{
	"5m":  "forex_bar_5m",
	"15m": "forex_bar_15m",
	"30m": "forex_bar_30m",
	"1h":  "forex_bar_1h",
	"2h":  "forex_bar_2h",
	"4h":  "forex_bar_4h",
	"1d":  "forex_bar_1d",
}

func klineTimeFunc(iv KlineInterval) string {
	if iv.Seconds == 0 {
		return "toDateTime(market_date, 'UTC')"
	}
	if iv.Seconds < 3600 {
		switch iv.Seconds {
		case 300:
			return "toStartOfFiveMinutes(timestamp)"
		case 900:
			return "toStartOfFifteenMinutes(timestamp)"
		default:
			return fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL %d second)", iv.Seconds)
		}
	}
	return fmt.Sprintf(
		"toDateTime(toUnixTimestamp(session_open) + intDiv(toUnixTimestamp(timestamp) - toUnixTimestamp(session_open), %d) * %d, 'UTC')",
		iv.Seconds, iv.Seconds,
	)
}

// InitKlineSchema creates AggregatingMergeTree tables, materialized views, and query views for forex bars.
func InitKlineSchema(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		stmts := klineDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("forex kline [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[forex-kline] initialized %s interval", iv.Suffix)
	}
	return nil
}

// QueryAggregationSQL returns a SQL query that aggregates 1-minute forex bars at query time.
func QueryAggregationSQL(interval string) (string, error) {
	var selected *KlineInterval
	for _, iv := range KlineIntervals {
		if iv.Suffix == interval {
			selected = &iv
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("unsupported forex interval %q (supported: 5m,15m,30m,1h,2h,4h,1d)", interval)
	}
	timeFunc := klineTimeFunc(*selected)
	return fmt.Sprintf(`SELECT
    %s AS timestamp,
    symbol,
    argMin(open, timestamp)  AS open,
    max(high)                AS high,
    min(low)                 AS low,
    argMax(close, timestamp) AS close,
    sum(volume)              AS volume,
    sum(transactions)        AS transactions
FROM forex_bar_1m
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
GROUP BY timestamp, symbol
ORDER BY timestamp`, timeFunc), nil
}

func klineDDL(iv KlineInterval) []string {
	base := "forex_bar_1m"
	agg := "forex_bar_" + iv.Suffix + "_agg"
	mv := "forex_bar_" + iv.Suffix + "_mv"
	view := "forex_bar_" + iv.Suffix
	timeFunc := klineTimeFunc(iv)

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts                 DateTime('UTC'),
    symbol             LowCardinality(String),
    open_state         AggregateFunction(argMin, Float32, DateTime('UTC')),
    high_state         AggregateFunction(max, Float32),
    low_state          AggregateFunction(min, Float32),
    close_state        AggregateFunction(argMax, Float32, DateTime('UTC')),
    volume_state       AggregateFunction(sum, Float64),
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
