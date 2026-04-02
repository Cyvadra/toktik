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
    argMinState(delta, timestamp)         AS delta_state,
    argMinState(gamma, timestamp)         AS gamma_state,
    argMinState(vega, timestamp)          AS vega_state,
    argMinState(theta, timestamp)         AS theta_state,
    argMinState(rho, timestamp)           AS rho_state,
    argMaxState(bid_close, timestamp)     AS bid_close_state,
    argMaxState(ask_close, timestamp)     AS ask_close_state,
    argMaxState(mark_close, timestamp)    AS mark_close_state,
    argMaxState(mark_iv_close, timestamp) AS mark_iv_close_state,
    sumState(toUInt64(tick_count))        AS tick_count_state,
    argMaxState(open_interest, timestamp) AS open_interest_state
FROM
(
    SELECT
        %s AS ts,
        symbol_id,
        base_asset,
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
WITH arraySort(
    x -> tupleElement(x, 1),
    groupArray(tuple(
        symbol_id,
        argMinMerge(delta_state),
        argMinMerge(gamma_state),
        argMinMerge(vega_state),
        argMinMerge(theta_state),
        argMinMerge(rho_state),
        argMaxMerge(bid_close_state),
        argMaxMerge(ask_close_state),
        argMaxMerge(mark_close_state),
        argMaxMerge(mark_iv_close_state),
        sumMerge(tick_count_state),
        argMaxMerge(open_interest_state)
    ))
) AS contracts
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
FROM %s
GROUP BY ts, base_asset`, view, agg)

	return []string{createAgg, createMV, createView}
}
