-- ============================================================
-- US Market Schema for ClickHouse
-- Polygon.io minute-agg data for US options and stocks.
-- ============================================================

-- -------------------------------------------------------
-- US Options: 1-minute bars from Polygon OPRA flatfiles
-- -------------------------------------------------------
-- Ticker format: O:<underlying><YYMMDD><C|P><strike*1000>
-- Example: O:AAPL230120C00130000

CREATE TABLE IF NOT EXISTS us_options_bar_1m
(
    timestamp    DateTime('UTC'),
    symbol       LowCardinality(String),   -- full Polygon ticker e.g. O:AAPL230120C00130000
    underlying   LowCardinality(String),   -- extracted underlying e.g. AAPL
    option_type  Enum8('C' = 1, 'P' = 2),
    expiration   Date,
    strike       Float64,                  -- dollar strike (divided by 1000)
    open         Float32,
    high         Float32,
    low          Float32,
    close        Float32,
    volume       UInt32,
    transactions UInt32
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
    timestamp    DateTime('UTC'),
    symbol       LowCardinality(String),
    open         Float32,
    high         Float32,
    low          Float32,
    close        Float32,
    volume       UInt32,
    transactions UInt32
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (symbol, timestamp)
SETTINGS index_granularity = 8192;
