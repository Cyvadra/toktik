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