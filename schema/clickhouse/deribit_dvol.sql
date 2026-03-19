CREATE TABLE IF NOT EXISTS deribit_dvol_bar
(
    currency String,
    index_name String,
    resolution String,
    timestamp DateTime64(3, 'UTC'),

    open Float32,
    high Float32,
    low Float32,
    close Float32,

    ingested_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY (resolution, toYYYYMM(timestamp))
ORDER BY (currency, resolution, timestamp)
SETTINGS index_granularity = 8192;
