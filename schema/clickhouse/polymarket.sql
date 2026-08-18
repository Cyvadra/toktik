CREATE TABLE IF NOT EXISTS polymarket_condition
(
    condition_id       FixedString(66),
    event_id           String,
    market_id          String,
    slug               String,
    underlying         LowCardinality(String),
    contract_interval  LowCardinality(String),
    window_start       DateTime64(3, 'UTC'),
    window_end         DateTime64(3, 'UTC'),
    market_start       Nullable(DateTime64(3, 'UTC')),
    market_end         Nullable(DateTime64(3, 'UTC')),
    closed             UInt8,
    resolved_outcome   Nullable(String),
    metadata_version   UInt64,
    updated_at         DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(metadata_version)
ORDER BY condition_id;

CREATE TABLE IF NOT EXISTS polymarket_outcome
(
    asset_id          String,
    condition_id      FixedString(66),
    outcome_index     UInt8,
    outcome_name      LowCardinality(String),
    metadata_version  UInt64,
    updated_at        DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(metadata_version)
ORDER BY (asset_id, condition_id);

CREATE TABLE IF NOT EXISTS polymarket_raw_file_catalog
(
    source_file          String,
    source_hash          String,
    selection_hash       String DEFAULT '',
    file_size            UInt64,
    row_count            UInt64,
    target_row_count     UInt64,
    first_received_at    Nullable(DateTime64(3, 'UTC')),
    last_received_at     Nullable(DateTime64(3, 'UTC')),
    schema_version       UInt16,
    import_status        Enum8('pending' = 1, 'success' = 2, 'failed' = 3),
    error_message        String DEFAULT '',
    updated_at           DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY source_file;

ALTER TABLE polymarket_raw_file_catalog
    ADD COLUMN IF NOT EXISTS selection_hash String DEFAULT '' AFTER source_hash;

CREATE TABLE IF NOT EXISTS polymarket_l2_event
(
    condition_id      FixedString(66),
    asset_id          String,
    event_type        Enum8('book' = 1, 'price_change' = 2, 'last_trade_price' = 3, 'tick_size_change' = 4),
    timestamp          DateTime64(3, 'UTC'),
    timestamp_received DateTime64(3, 'UTC'),
    source_file       LowCardinality(String),
    source_row_number UInt64,
    event_id          FixedString(32),
    side              Nullable(Enum8('BUY' = 1, 'SELL' = 2)),
    price_e4          Nullable(Int64),
    size_e6           Nullable(Int64),
    best_bid_e4       Nullable(Int64),
    best_ask_e4       Nullable(Int64),
    fee_rate_bps      Nullable(UInt16),
    transaction_hash  Nullable(String),
    old_tick_size_e4  Nullable(Int64),
    new_tick_size_e4  Nullable(Int64),
    bids_json         Nullable(String),
    asks_json         Nullable(String),
    ingested_at       DateTime64(3, 'UTC') DEFAULT now64(3),
    INDEX idx_condition condition_id TYPE bloom_filter GRANULARITY 4
)
ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp_received)
ORDER BY (asset_id, timestamp_received, timestamp, source_file, source_row_number);