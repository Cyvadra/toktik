-- ============================================================
-- US Market Schema for ClickHouse
-- Polygon.io minute-agg data for US options and stocks.
-- Session-aware: every 1m bar carries market_date, session
-- classification, and session_open for aligned aggregation.
-- ============================================================

-- -------------------------------------------------------
-- Trading session calendar (dimension table)
-- -------------------------------------------------------
-- Generated programmatically; handles DST, holidays,
-- and early-close days. One row per weekday.

CREATE TABLE IF NOT EXISTS us_equity_sessions
(
    market_date          Date,
    regular_open_utc     DateTime('UTC'),
    regular_close_utc    DateTime('UTC'),
    premarket_open_utc   DateTime('UTC'),
    postmarket_close_utc DateTime('UTC'),
    is_holiday           UInt8 DEFAULT 0,
    is_early_close       UInt8 DEFAULT 0
)
ENGINE = ReplacingMergeTree()
ORDER BY market_date
SETTINGS index_granularity = 8192;

-- -------------------------------------------------------
-- US Options: 1-minute bars from Polygon OPRA flatfiles
-- -------------------------------------------------------

CREATE TABLE IF NOT EXISTS us_options_bar_1m
(
    timestamp          DateTime('UTC'),
    symbol             LowCardinality(String),
    underlying         LowCardinality(String),
    option_type        Enum8('C' = 1, 'P' = 2),
    expiration         Date,
    strike             Float64,
    open               Float32,
    high               Float32,
    low                Float32,
    close              Float32,
    underlying_close   Float32,
    implied_volatility Float32,
    delta              Float32,
    gamma              Float32,
    vega               Float32,
    theta              Float32,
    rho                Float32,
    volume             UInt32,
    transactions       UInt32,
    market_date        Date,
    session_kind       Enum8('premarket' = 1, 'regular' = 2, 'postmarket' = 3, 'closed' = 4) DEFAULT 'closed',
    is_regular_session UInt8 DEFAULT 0,
    session_open       DateTime('UTC'),
    session_seq        UInt16 DEFAULT 0
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (underlying, symbol, timestamp)
SETTINGS index_granularity = 8192;

-- -------------------------------------------------------
-- US Stocks: 1-minute bars from Polygon SIP flatfiles
-- -------------------------------------------------------

CREATE TABLE IF NOT EXISTS us_stocks_bar_1m
(
    timestamp          DateTime('UTC'),
    symbol             LowCardinality(String),
    open               Float32,
    high               Float32,
    low                Float32,
    close              Float32,
    volume             UInt32,
    transactions       UInt32,
    market_date        Date,
    session_kind       Enum8('premarket' = 1, 'regular' = 2, 'postmarket' = 3, 'closed' = 4) DEFAULT 'closed',
    is_regular_session UInt8 DEFAULT 0,
    session_open       DateTime('UTC'),
    session_seq        UInt16 DEFAULT 0
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (symbol, timestamp)
SETTINGS index_granularity = 8192;
