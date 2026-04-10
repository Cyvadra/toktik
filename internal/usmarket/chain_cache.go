package usmarket

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ChainPrecomputedIntervals maps US option chain intervals to cached view names.
var ChainPrecomputedIntervals = map[string]string{
	"5m":  "us_options_chain_5m",
	"15m": "us_options_chain_15m",
	"30m": "us_options_chain_30m",
	"1h":  "us_options_chain_1h",
	"2h":  "us_options_chain_2h",
	"4h":  "us_options_chain_4h",
	"1d":  "us_options_chain_1d",
}

// DefaultChainCacheIntervals is the set of US chain cache resolutions we maintain.
var DefaultChainCacheIntervals = []KlineInterval{
	{Suffix: "5m", Seconds: 300},
	{Suffix: "15m", Seconds: 900},
	{Suffix: "30m", Seconds: 1800},
	{Suffix: "1h", Seconds: 3600},
	{Suffix: "2h", Seconds: 7200},
	{Suffix: "4h", Seconds: 14400},
	{Suffix: "1d", Seconds: 0},
}

// InitOptionChainCacheSchema creates option-chain cache tables/materialized views
// for precomputed US option intervals. Cache is regular-session only.
func InitOptionChainCacheSchema(ctx context.Context, conn driver.Conn) error {
	for _, iv := range DefaultChainCacheIntervals {
		if err := migrateOptionChainAggregateIfNeeded(ctx, conn, iv.Suffix); err != nil {
			return fmt.Errorf("migrate us option chain cache [%s]: %w", iv.Suffix, err)
		}
		stmts := optionChainCacheDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("us option chain cache [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[us-options-chain] initialized %s interval", iv.Suffix)
	}
	return nil
}

func migrateOptionChainAggregateIfNeeded(ctx context.Context, conn driver.Conn, suffix string) error {
	agg := "us_options_chain_" + suffix + "_agg"
	rows, err := conn.Query(ctx, `SELECT name, type
FROM system.columns
WHERE database = currentDatabase()
  AND table = {table:String}
  AND name IN ('volume_state', 'transactions_state')`, clickhouse.Named("table", agg))
	if err != nil {
		return fmt.Errorf("query chain aggregate schema: %w", err)
	}
	defer rows.Close()

	types := make(map[string]string, 2)
	for rows.Next() {
		var name string
		var colType string
		if err := rows.Scan(&name, &colType); err != nil {
			return fmt.Errorf("scan chain aggregate schema: %w", err)
		}
		types[name] = colType
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate chain aggregate schema: %w", err)
	}

	if len(types) == 0 {
		return nil
	}
	if types["volume_state"] == "AggregateFunction(sum, Float64)" && types["transactions_state"] == "AggregateFunction(sum, UInt64)" {
		return nil
	}

	view := "us_options_chain_" + suffix
	mv := "us_options_chain_" + suffix + "_mv"
	if err := conn.Exec(ctx, "DROP VIEW IF EXISTS "+view); err != nil {
		return fmt.Errorf("drop incompatible chain view: %w", err)
	}
	if err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+mv); err != nil {
		return fmt.Errorf("drop incompatible chain mv: %w", err)
	}
	if err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+agg+" SETTINGS max_table_size_to_drop=0, max_partition_size_to_drop=0"); err != nil {
		return fmt.Errorf("drop incompatible chain agg: %w", err)
	}
	log.Printf("[us-options-chain] dropped incompatible %s schema to apply latest aggregate types", suffix)
	return nil
}

// RebuildOptionChainCaches repopulates all option chain cache aggregates from
// the current 1m base table.
func RebuildOptionChainCaches(ctx context.Context, conn driver.Conn) error {
	for _, iv := range DefaultChainCacheIntervals {
		agg := "us_options_chain_" + iv.Suffix + "_agg"
		if err := conn.Exec(ctx, `TRUNCATE TABLE `+agg); err != nil {
			return fmt.Errorf("truncate us option chain aggregate [%s]: %w", iv.Suffix, err)
		}
		if err := conn.Exec(ctx, optionChainRebuildSQL(iv)); err != nil {
			return fmt.Errorf("rebuild us option chain aggregate [%s]: %w", iv.Suffix, err)
		}
		log.Printf("[us-options-chain] rebuilt %s interval", iv.Suffix)
	}
	return nil
}

func optionChainCacheDDL(iv KlineInterval) []string {
	agg := "us_options_chain_" + iv.Suffix + "_agg"
	mv := "us_options_chain_" + iv.Suffix + "_mv"
	view := "us_options_chain_" + iv.Suffix
	timeFunc := "timestamp"
	if iv.Suffix != "1m" {
		timeFunc = klineTimeFunc(iv)
	}

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts DateTime('UTC'),
    underlying LowCardinality(String),
    symbol LowCardinality(String),
    option_type_state AggregateFunction(argMax, Enum8('C' = 1, 'P' = 2), DateTime('UTC')),
    expiration_state AggregateFunction(argMax, Date, DateTime('UTC')),
    strike_state AggregateFunction(argMax, Float64, DateTime('UTC')),
    close_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    underlying_close_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    implied_volatility_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    delta_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    gamma_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    vega_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    theta_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    rho_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    volume_state AggregateFunction(sum, Float64),
    transactions_state AggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (underlying, ts, symbol)
SETTINGS index_granularity = 8192`, agg)

	dropMV := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, mv)

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW %s
TO %s
AS SELECT
    ts,
    underlying,
    symbol,
        argMaxState(option_type, last_ts)          AS option_type_state,
        argMaxState(expiration, last_ts)           AS expiration_state,
        argMaxState(strike, last_ts)               AS strike_state,
        argMaxState(close, last_ts)                AS close_state,
        argMaxState(underlying_close, last_ts)     AS underlying_close_state,
        argMaxState(implied_volatility, last_ts)   AS implied_volatility_state,
        argMaxState(delta, last_ts)                AS delta_state,
        argMaxState(gamma, last_ts)                AS gamma_state,
        argMaxState(vega, last_ts)                 AS vega_state,
        argMaxState(theta, last_ts)                AS theta_state,
        argMaxState(rho, last_ts)                  AS rho_state,
    sumState(volume)                           AS volume_state,
    sumState(toUInt64(transactions))           AS transactions_state
FROM
(
    SELECT
        %s AS ts,
        symbol,
        underlying,
        option_type,
        expiration,
        strike,
        argMax(close, timestamp)              AS close,
        argMax(underlying_close, timestamp)   AS underlying_close,
        argMax(implied_volatility, timestamp) AS implied_volatility,
        argMax(delta, timestamp)              AS delta,
        argMax(gamma, timestamp)              AS gamma,
        argMax(vega, timestamp)               AS vega,
        argMax(theta, timestamp)              AS theta,
        argMax(rho, timestamp)                AS rho,
        sum(volume)                           AS volume,
        sum(toUInt64(transactions))           AS transactions,
        max(timestamp)                        AS last_ts
    FROM us_options_bar_1m
    WHERE is_regular_session = 1
    GROUP BY ts, symbol, underlying, option_type, expiration, strike
)
GROUP BY ts, underlying, symbol`, mv, agg, timeFunc)

	createView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT
    ts AS timestamp,
    underlying,
    arrayMap(x -> tupleElement(x, 1), contracts) AS symbols,
    arrayMap(x -> tupleElement(x, 2), contracts) AS option_types,
    arrayMap(x -> tupleElement(x, 3), contracts) AS expirations,
    arrayMap(x -> tupleElement(x, 4), contracts) AS strikes,
    arrayMap(x -> tupleElement(x, 5), contracts) AS close_prices,
    arrayMap(x -> tupleElement(x, 6), contracts) AS underlying_closes,
    arrayMap(x -> tupleElement(x, 7), contracts) AS implied_volatilities,
    arrayMap(x -> tupleElement(x, 8), contracts) AS deltas,
    arrayMap(x -> tupleElement(x, 9), contracts) AS gammas,
    arrayMap(x -> tupleElement(x, 10), contracts) AS vegas,
    arrayMap(x -> tupleElement(x, 11), contracts) AS thetas,
    arrayMap(x -> tupleElement(x, 12), contracts) AS rhos,
    arrayMap(x -> tupleElement(x, 13), contracts) AS volumes,
    arrayMap(x -> tupleElement(x, 14), contracts) AS transactions
FROM
(
    SELECT
        ts,
        underlying,
        arraySort(
            x -> tupleElement(x, 1),
            groupArray(tuple(
                symbol,
                option_type,
                expiration,
                strike,
                close,
                underlying_close,
                implied_volatility,
                delta,
                gamma,
                vega,
                theta,
                rho,
                volume,
                transactions
            ))
        ) AS contracts
    FROM
    (
        SELECT
            ts,
            underlying,
            symbol,
            argMaxMerge(option_type_state)        AS option_type,
            argMaxMerge(expiration_state)         AS expiration,
            argMaxMerge(strike_state)             AS strike,
            argMaxMerge(close_state)              AS close,
            argMaxMerge(underlying_close_state)   AS underlying_close,
            argMaxMerge(implied_volatility_state) AS implied_volatility,
            argMaxMerge(delta_state)              AS delta,
            argMaxMerge(gamma_state)              AS gamma,
            argMaxMerge(vega_state)               AS vega,
            argMaxMerge(theta_state)              AS theta,
            argMaxMerge(rho_state)                AS rho,
            sumMerge(volume_state)                AS volume,
            sumMerge(transactions_state)          AS transactions
        FROM %s
        GROUP BY ts, underlying, symbol
    )
    GROUP BY ts, underlying
)`, view, agg)

	return []string{createAgg, dropMV, createMV, createView}
}

func optionChainRebuildSQL(iv KlineInterval) string {
	agg := "us_options_chain_" + iv.Suffix + "_agg"
	timeFunc := "timestamp"
	if iv.Suffix != "1m" {
		timeFunc = klineTimeFunc(iv)
	}

	return fmt.Sprintf(`INSERT INTO %s
SELECT
    ts,
    underlying,
    symbol,
        argMaxState(option_type, last_ts)          AS option_type_state,
        argMaxState(expiration, last_ts)           AS expiration_state,
        argMaxState(strike, last_ts)               AS strike_state,
        argMaxState(close, last_ts)                AS close_state,
        argMaxState(underlying_close, last_ts)     AS underlying_close_state,
        argMaxState(implied_volatility, last_ts)   AS implied_volatility_state,
        argMaxState(delta, last_ts)                AS delta_state,
        argMaxState(gamma, last_ts)                AS gamma_state,
        argMaxState(vega, last_ts)                 AS vega_state,
        argMaxState(theta, last_ts)                AS theta_state,
        argMaxState(rho, last_ts)                  AS rho_state,
    sumState(volume)                           AS volume_state,
    sumState(toUInt64(transactions))           AS transactions_state
FROM
(
    SELECT
        %s AS ts,
        symbol,
        underlying,
        option_type,
        expiration,
        strike,
        argMax(close, timestamp)              AS close,
        argMax(underlying_close, timestamp)   AS underlying_close,
        argMax(implied_volatility, timestamp) AS implied_volatility,
        argMax(delta, timestamp)              AS delta,
        argMax(gamma, timestamp)              AS gamma,
        argMax(vega, timestamp)               AS vega,
        argMax(theta, timestamp)              AS theta,
        argMax(rho, timestamp)                AS rho,
        sum(volume)                           AS volume,
        sum(toUInt64(transactions))           AS transactions,
        max(timestamp)                        AS last_ts
    FROM us_options_bar_1m
    WHERE is_regular_session = 1
    GROUP BY ts, symbol, underlying, option_type, expiration, strike
)
GROUP BY ts, underlying, symbol`, agg, timeFunc)
}
