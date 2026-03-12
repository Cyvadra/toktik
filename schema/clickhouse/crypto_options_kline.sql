-- ============================================================
-- Crypto Options K-Line Schema for ClickHouse
-- Generates OHLCV bars at 5m, 15m, 30m, 1h, 4h, 1d intervals
-- from the 1-minute source table crypto_options_bar_1m.
--
-- Each interval uses three objects:
--   1. *_agg  – AggregatingMergeTree table storing aggregate states
--   2. *_mv   – Materialized view triggered on INSERT to bar_1m
--   3. view   – Regular view presenting the same columns as bar_1m
--
-- NOTE: This file is a reference. The Go function
-- cryptooptions.InitKlineSchema() generates identical DDL
-- programmatically and is the primary mechanism.
-- ============================================================

-- ======================== 5m bars ========================

CREATE TABLE IF NOT EXISTS crypto_options_bar_5m_agg
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
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS crypto_options_bar_5m_mv
TO crypto_options_bar_5m_agg
AS SELECT
    toStartOfFiveMinutes(timestamp) AS ts,
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
GROUP BY ts, symbol_id, base_asset;

CREATE OR REPLACE VIEW crypto_options_bar_5m AS
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
FROM crypto_options_bar_5m_agg
GROUP BY ts, symbol_id, base_asset;

-- ======================== 15m bars ========================

CREATE TABLE IF NOT EXISTS crypto_options_bar_15m_agg
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
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS crypto_options_bar_15m_mv
TO crypto_options_bar_15m_agg
AS SELECT
    toStartOfFifteenMinutes(timestamp) AS ts,
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
GROUP BY ts, symbol_id, base_asset;

CREATE OR REPLACE VIEW crypto_options_bar_15m AS
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
FROM crypto_options_bar_15m_agg
GROUP BY ts, symbol_id, base_asset;

-- ======================== 30m bars ========================

CREATE TABLE IF NOT EXISTS crypto_options_bar_30m_agg
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
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS crypto_options_bar_30m_mv
TO crypto_options_bar_30m_agg
AS SELECT
    toStartOfInterval(timestamp, INTERVAL 30 minute) AS ts,
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
GROUP BY ts, symbol_id, base_asset;

CREATE OR REPLACE VIEW crypto_options_bar_30m AS
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
FROM crypto_options_bar_30m_agg
GROUP BY ts, symbol_id, base_asset;

-- ======================== 1h bars ========================

CREATE TABLE IF NOT EXISTS crypto_options_bar_1h_agg
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
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS crypto_options_bar_1h_mv
TO crypto_options_bar_1h_agg
AS SELECT
    toStartOfHour(timestamp) AS ts,
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
GROUP BY ts, symbol_id, base_asset;

CREATE OR REPLACE VIEW crypto_options_bar_1h AS
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
FROM crypto_options_bar_1h_agg
GROUP BY ts, symbol_id, base_asset;

-- ======================== 4h bars ========================

CREATE TABLE IF NOT EXISTS crypto_options_bar_4h_agg
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
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS crypto_options_bar_4h_mv
TO crypto_options_bar_4h_agg
AS SELECT
    toStartOfInterval(timestamp, INTERVAL 4 hour) AS ts,
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
GROUP BY ts, symbol_id, base_asset;

CREATE OR REPLACE VIEW crypto_options_bar_4h AS
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
FROM crypto_options_bar_4h_agg
GROUP BY ts, symbol_id, base_asset;

-- ======================== 1d bars ========================

CREATE TABLE IF NOT EXISTS crypto_options_bar_1d_agg
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
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS crypto_options_bar_1d_mv
TO crypto_options_bar_1d_agg
AS SELECT
    toStartOfDay(timestamp) AS ts,
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
GROUP BY ts, symbol_id, base_asset;

CREATE OR REPLACE VIEW crypto_options_bar_1d AS
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
FROM crypto_options_bar_1d_agg
GROUP BY ts, symbol_id, base_asset;
