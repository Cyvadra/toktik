-- ============================================================
-- Universe membership facts for point-in-time symbol expansion.
-- Control-plane definitions and runs live in MySQL.
-- ============================================================

CREATE TABLE IF NOT EXISTS universe_membership
(
    universe_code LowCardinality(String),
    market        LowCardinality(String),
    symbol        LowCardinality(String),
    valid_from    Date,
    valid_to      Date DEFAULT toDate('2100-01-01'),
    score         Nullable(Float64),
    rank          Nullable(UInt32),
    source_run_id String DEFAULT '',
    metadata      String DEFAULT '',
    source        LowCardinality(String) DEFAULT '',
    version       UInt64 DEFAULT toUnixTimestamp64Milli(now64(3)),
    ingested_at   DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(version)
ORDER BY (market, universe_code, symbol, valid_from, valid_to, source_run_id)
SETTINGS index_granularity = 8192;

-- ============================================================
-- Materialized daily candidate pool for turnover_intersection_union.
-- ============================================================

CREATE TABLE IF NOT EXISTS turnover_intersection_pool_daily
(
    market                LowCardinality(String),
    lookback_days         UInt16,
    non_etf_only          UInt8,
    as_of_date            Date,
    underlying            LowCardinality(String),
    stock_turnover_usd    Float64 DEFAULT 0,
    option_turnover_usd   Float64 DEFAULT 0,
    combined_turnover_usd Float64 DEFAULT 0,
    rank                  UInt32,
    updated_at            DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY (market, toYYYYMM(as_of_date))
ORDER BY (market, lookback_days, non_etf_only, as_of_date, rank, underlying)
SETTINGS index_granularity = 8192;
