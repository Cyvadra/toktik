-- ============================================================
-- Forex Market Schema for ClickHouse
-- FMP minute bars for forex and metal-linked FX crosses.
-- Structure intentionally mirrors us_stocks_bar_1m so shared query
-- patterns can be reused with a market-specific table prefix.
-- ============================================================

CREATE TABLE IF NOT EXISTS forex_bar_1m
(
    timestamp          DateTime('UTC'),
    symbol             LowCardinality(String),
    open               Float32,
    high               Float32,
    low                Float32,
    close              Float32,
    volume             Float64,
    transactions       UInt32,
    market_date        Date,
    session_kind       Enum8('premarket' = 1, 'regular' = 2, 'postmarket' = 3, 'closed' = 4) DEFAULT 'regular',
    is_regular_session UInt8 DEFAULT 1,
    session_open       DateTime('UTC'),
    session_seq        UInt16 DEFAULT 0
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (symbol, timestamp)
SETTINGS index_granularity = 8192;