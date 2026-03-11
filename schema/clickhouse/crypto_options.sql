-- ============================================================
-- Crypto Options Schema for ClickHouse
-- ============================================================

-- Symbol metadata table
-- Stores unique option contract metadata extracted from symbol strings.
-- Uses ReplacingMergeTree to deduplicate on repeated imports.
CREATE TABLE IF NOT EXISTS crypto_options_symbol_meta
(
    symbol_id        UInt32,
    symbol           String,
    base_asset       LowCardinality(String),
    option_type      Enum8('call' = 1, 'put' = 2),
    strike_price     Float32,
    expiration       DateTime,
    underlying_index LowCardinality(String)
)
ENGINE = ReplacingMergeTree()
ORDER BY symbol_id
SETTINGS index_granularity = 8192;

-- 1-minute aggregated bar table
-- Pre-aggregated from tick data. Each row represents one symbol's
-- 1-minute bar with OHLC on mark/last prices, bid/ask snapshots,
-- implied volatility, greeks (from earliest tick), and open interest.
CREATE TABLE IF NOT EXISTS crypto_options_bar_1m
(
    -- Key columns
    timestamp              DateTime,
    symbol_id              UInt32,
    base_asset             LowCardinality(String),

    -- Mark price OHLC
    mark_open              Float32,
    mark_high              Float32,
    mark_low               Float32,
    mark_close             Float32,

    -- Last price OHLC
    last_open              Float32,
    last_high              Float32,
    last_low               Float32,
    last_close             Float32,

    -- Bid/Ask snapshot (open & close of the minute)
    bid_open               Float32,
    bid_close              Float32,
    ask_open               Float32,
    ask_close              Float32,

    -- Implied volatility
    mark_iv_open           Float32,
    mark_iv_close          Float32,
    bid_iv_open            Float32,
    ask_iv_open            Float32,

    -- Greeks (earliest tick in the minute window)
    delta                  Float32,
    gamma                  Float32,
    vega                   Float32,
    theta                  Float32,
    rho                    Float32,

    -- Underlying price
    underlying_price_open  Float32,
    underlying_price_close Float32,

    -- Open interest & activity
    open_interest          Float32,
    tick_count             UInt16
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (base_asset, symbol_id, timestamp)
SETTINGS index_granularity = 8192;
