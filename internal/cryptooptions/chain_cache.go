package cryptooptions

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ChainPrecomputedIntervals maps option chain intervals to precomputed chain
// cache view names.
var ChainPrecomputedIntervals = map[string]string{
	"1m":  "crypto_options_chain_1m",
	"5m":  "crypto_options_chain_5m",
	"15m": "crypto_options_chain_15m",
	"30m": "crypto_options_chain_30m",
	"1h":  "crypto_options_chain_1h",
	"2h":  "crypto_options_chain_2h",
	"3h":  "crypto_options_chain_3h",
	"4h":  "crypto_options_chain_4h",
	"6h":  "crypto_options_chain_6h",
	"8h":  "crypto_options_chain_8h",
	"12h": "crypto_options_chain_12h",
	"1d":  "crypto_options_chain_1d",
}

// InitChainCacheSchema creates option-chain cache tables/materialized views
// for 1m + all precomputed K-line intervals.
func InitChainCacheSchema(ctx context.Context, conn driver.Conn) error {
	intervals := make([]KlineInterval, 0, len(KlineIntervals)+1)
	intervals = append(intervals, KlineInterval{Suffix: "1m", TimeFunc: "timestamp"})
	intervals = append(intervals, KlineIntervals...)

	for _, iv := range intervals {
		stmts := chainCacheDDL(iv)
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("chain cache schema [%s]: %w", iv.Suffix, err)
			}
		}
		log.Printf("[chain-cache] initialized schema for %s interval", iv.Suffix)
	}
	return nil
}

func chainCacheDDL(iv KlineInterval) []string {
	agg := fmt.Sprintf("crypto_options_chain_%s_agg", iv.Suffix)
	mv := fmt.Sprintf("crypto_options_chain_%s_mv", iv.Suffix)
	view := fmt.Sprintf("crypto_options_chain_%s", iv.Suffix)

	createAgg := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    ts DateTime('UTC'),
    base_asset LowCardinality(String),
    symbol_id UInt32,
    delta_state AggregateFunction(argMin, Float32, DateTime('UTC')),
    gamma_state AggregateFunction(argMin, Float32, DateTime('UTC')),
    vega_state AggregateFunction(argMin, Float32, DateTime('UTC')),
    theta_state AggregateFunction(argMin, Float32, DateTime('UTC')),
    rho_state AggregateFunction(argMin, Float32, DateTime('UTC')),
    bid_close_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    ask_close_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    mark_close_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    mark_iv_close_state AggregateFunction(argMax, Float32, DateTime('UTC')),
    tick_count_state AggregateFunction(sum, UInt64),
    open_interest_state AggregateFunction(argMax, Float32, DateTime('UTC'))
)
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (base_asset, ts, symbol_id)
SETTINGS index_granularity = 8192`, agg)

	createMV := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s
TO %s
AS
SELECT
    ts,
    base_asset,
    symbol_id,
    argMinState(delta, first_ts)          AS delta_state,
    argMinState(gamma, first_ts)          AS gamma_state,
    argMinState(vega, first_ts)           AS vega_state,
    argMinState(theta, first_ts)          AS theta_state,
    argMinState(rho, first_ts)            AS rho_state,
    argMaxState(bid_close, last_ts)       AS bid_close_state,
    argMaxState(ask_close, last_ts)       AS ask_close_state,
    argMaxState(mark_close, last_ts)      AS mark_close_state,
    argMaxState(mark_iv_close, last_ts)   AS mark_iv_close_state,
    sumState(toUInt64(tick_count))        AS tick_count_state,
    argMaxState(open_interest, last_ts)   AS open_interest_state
FROM
(
    SELECT
        %s AS ts,
        symbol_id,
        base_asset,
        min(timestamp)                    AS first_ts,
        max(timestamp)                    AS last_ts,
        argMin(delta, timestamp)         AS delta,
        argMin(gamma, timestamp)         AS gamma,
        argMin(vega, timestamp)          AS vega,
        argMin(theta, timestamp)         AS theta,
        argMin(rho, timestamp)           AS rho,
        argMax(bid_close, timestamp)     AS bid_close,
        argMax(ask_close, timestamp)     AS ask_close,
        argMax(mark_close, timestamp)    AS mark_close,
        argMax(mark_iv_close, timestamp) AS mark_iv_close,
        sum(toUInt64(tick_count))        AS tick_count,
        argMax(open_interest, timestamp) AS open_interest
    FROM crypto_options_bar_1m
    GROUP BY ts, symbol_id, base_asset
)
GROUP BY ts, base_asset, symbol_id`, mv, agg, iv.TimeFunc)

	createView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT
    ts AS timestamp,
    base_asset,
    arrayMap(x -> tupleElement(x, 1), contracts) AS symbol_ids,
    arrayMap(x -> tupleElement(x, 2), contracts) AS deltas,
    arrayMap(x -> tupleElement(x, 3), contracts) AS gammas,
    arrayMap(x -> tupleElement(x, 4), contracts) AS vegas,
    arrayMap(x -> tupleElement(x, 5), contracts) AS thetas,
    arrayMap(x -> tupleElement(x, 6), contracts) AS rhos,
    arrayMap(x -> tupleElement(x, 7), contracts) AS bid_prices,
    arrayMap(x -> tupleElement(x, 8), contracts) AS ask_prices,
    arrayMap(x -> tupleElement(x, 9), contracts) AS mark_prices,
    arrayMap(x -> tupleElement(x, 10), contracts) AS mark_ivs,
    arrayMap(x -> tupleElement(x, 11), contracts) AS volumes,
    arrayMap(x -> tupleElement(x, 12), contracts) AS open_interests
FROM
(
    SELECT
        ts,
        base_asset,
        arraySort(
            x -> tupleElement(x, 1),
            groupArray(tuple(
                symbol_id,
                delta,
                gamma,
                vega,
                theta,
                rho,
                bid_close,
                ask_close,
                mark_close,
                mark_iv_close,
                tick_count,
                open_interest
            ))
        ) AS contracts
    FROM
    (
        SELECT
            ts,
            base_asset,
            symbol_id,
            argMinMerge(delta_state)         AS delta,
            argMinMerge(gamma_state)         AS gamma,
            argMinMerge(vega_state)          AS vega,
            argMinMerge(theta_state)         AS theta,
            argMinMerge(rho_state)           AS rho,
            argMaxMerge(bid_close_state)     AS bid_close,
            argMaxMerge(ask_close_state)     AS ask_close,
            argMaxMerge(mark_close_state)    AS mark_close,
            argMaxMerge(mark_iv_close_state) AS mark_iv_close,
            sumMerge(tick_count_state)       AS tick_count,
            argMaxMerge(open_interest_state) AS open_interest
        FROM %s
        GROUP BY ts, base_asset, symbol_id
    )
    GROUP BY ts, base_asset
)`, view, agg)

	return []string{createAgg, createMV, createView}
}
