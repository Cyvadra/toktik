-- ============================================================
-- Crypto Options Schema for ClickHouse
-- ============================================================

CREATE TABLE IF NOT EXISTS import_ledger
(
    importer_name LowCardinality(String),
    source_key    String,
    scope_key     String,
    import_id     String,
    source_hash   String DEFAULT '',
    status        Enum8('pending' = 1, 'success' = 2, 'failed' = 3),
    rows_inserted UInt64 DEFAULT 0,
    error_message String DEFAULT '',
    started_at    DateTime64(3, 'UTC'),
    completed_at  DateTime64(3, 'UTC') DEFAULT toDateTime64(0, 3, 'UTC'),
    version       UInt64
)
ENGINE = ReplacingMergeTree(version)
ORDER BY (importer_name, source_key, scope_key)
SETTINGS index_granularity = 8192;

-- Symbol metadata table
-- Stores unique option contract metadata extracted from symbol strings.
-- Uses ReplacingMergeTree to deduplicate on repeated imports.
CREATE TABLE IF NOT EXISTS crypto_options_symbol_meta
(
    symbol_id        UInt64,
    symbol           String,
    base_asset       LowCardinality(String),
    option_type      Enum8('call' = 1, 'put' = 2),
    strike_price     Float32,
    expiration       DateTime('UTC'),
    underlying_index LowCardinality(String)
)
ENGINE = ReplacingMergeTree()
ORDER BY symbol_id
SETTINGS index_granularity = 8192;

-- 1-minute aggregated option bar table
-- Pre-aggregated from tick data. Each row represents one option symbol's
-- 1-minute bar with OHLC on mark/last/bid/ask prices, implied volatility,
-- greeks (from earliest tick), and open interest.
CREATE TABLE IF NOT EXISTS crypto_options_bar_1m
(
    -- Key columns
    timestamp              DateTime('UTC'),
    symbol_id              UInt64,
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

    -- Bid price OHLC
    bid_open               Float32,
    bid_high               Float32,
    bid_low                Float32,
    bid_close              Float32,

    -- Ask price OHLC
    ask_open               Float32,
    ask_high               Float32,
    ask_low                Float32,
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

    -- Open interest & activity
    volume                 Float64 DEFAULT toFloat64(tick_count),
    open_interest          Float32,
    tick_count             UInt16
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (base_asset, symbol_id, timestamp)
SETTINGS index_granularity = 8192;

-- 1-minute standalone spot-like bar table for the option underlyings.
-- This keeps the underlying asset price series normalized and reusable
-- by non-options consumers.
CREATE TABLE IF NOT EXISTS crypto_spot_bar_1m
(
    timestamp    DateTime('UTC'),
    symbol       LowCardinality(String),
    price_source LowCardinality(String),
    open         Float32,
    high         Float32,
    low          Float32,
    close        Float32,
    volume       Float64 DEFAULT volume_base,
    tick_count   UInt32,
    volume_base  Float64 DEFAULT 0,
    volume_quote Float64 DEFAULT 0,
    bar_interval LowCardinality(String) DEFAULT '1m'
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (symbol, timestamp)
SETTINGS index_granularity = 8192;
