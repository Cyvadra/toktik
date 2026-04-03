package usmarket

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ChainPrecomputedIntervals maps US option chain intervals to cached view names.
var ChainPrecomputedIntervals = map[string]string{
	"1m":  "us_options_chain_1m",
	"5m":  "us_options_chain_5m",
	"15m": "us_options_chain_15m",
	"30m": "us_options_chain_30m",
	"1h":  "us_options_chain_1h",
	"2h":  "us_options_chain_2h",
	"4h":  "us_options_chain_4h",
	"1d":  "us_options_chain_1d",
}

// InitOptionChainCacheSchema creates option-chain cache tables/materialized views
// for 1m + all precomputed US option intervals. Cache is regular-session only.
func InitOptionChainCacheSchema(ctx context.Context, conn driver.Conn) error {
	intervals := make([]KlineInterval, 0, len(KlineIntervals)+1)
	intervals = append(intervals, KlineInterval{Suffix: "1m", Seconds: 60})
	intervals = append(intervals, KlineIntervals...)

	for _, iv := range intervals {
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

// RebuildOptionChainCaches repopulates all option chain cache aggregates from
// the current 1m base table.
func RebuildOptionChainCaches(ctx context.Context, conn driver.Conn) error {
	intervals := make([]KlineInterval, 0, len(KlineIntervals)+1)
	intervals = append(intervals, KlineInterval{Suffix: "1m", Seconds: 60})
	intervals = append(intervals, KlineIntervals...)

	for _, iv := range intervals {
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
    volume_state AggregateFunction(sum, UInt32),
    transactions_state AggregateFunction(sum, UInt32)
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
    argMaxState(option_type, timestamp)        AS option_type_state,
    argMaxState(expiration, timestamp)         AS expiration_state,
    argMaxState(strike, timestamp)             AS strike_state,
    argMaxState(close, timestamp)              AS close_state,
    argMaxState(underlying_close, timestamp)   AS underlying_close_state,
    argMaxState(implied_volatility, timestamp) AS implied_volatility_state,
    argMaxState(delta, timestamp)              AS delta_state,
    argMaxState(gamma, timestamp)              AS gamma_state,
    argMaxState(vega, timestamp)               AS vega_state,
    argMaxState(theta, timestamp)              AS theta_state,
    argMaxState(rho, timestamp)                AS rho_state,
    sumState(volume)                           AS volume_state,
    sumState(transactions)                     AS transactions_state
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
        sum(transactions)                     AS transactions,
        max(timestamp)                        AS timestamp
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
    argMaxState(option_type, timestamp)        AS option_type_state,
    argMaxState(expiration, timestamp)         AS expiration_state,
    argMaxState(strike, timestamp)             AS strike_state,
    argMaxState(close, timestamp)              AS close_state,
    argMaxState(underlying_close, timestamp)   AS underlying_close_state,
    argMaxState(implied_volatility, timestamp) AS implied_volatility_state,
    argMaxState(delta, timestamp)              AS delta_state,
    argMaxState(gamma, timestamp)              AS gamma_state,
    argMaxState(vega, timestamp)               AS vega_state,
    argMaxState(theta, timestamp)              AS theta_state,
    argMaxState(rho, timestamp)                AS rho_state,
    sumState(volume)                           AS volume_state,
    sumState(transactions)                     AS transactions_state
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
        sum(transactions)                     AS transactions,
        max(timestamp)                        AS timestamp
    FROM us_options_bar_1m
    WHERE is_regular_session = 1
    GROUP BY ts, symbol, underlying, option_type, expiration, strike
)
GROUP BY ts, underlying, symbol`, agg, timeFunc)
}
