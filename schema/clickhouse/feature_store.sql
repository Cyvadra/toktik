CREATE TABLE IF NOT EXISTS feature_volatility_snapshot_daily
(
    market             LowCardinality(String),
    underlying         LowCardinality(String),
    lookback_days      UInt16,
    as_of_date         Date,
    price_observations UInt32,
    iv_observations    UInt32,
    hv10               Nullable(Float64),
    hv20               Nullable(Float64),
    hv30               Nullable(Float64),
    current_iv         Nullable(Float64),
    iv_percentile      Nullable(Float64),
    iv_rank            Nullable(Float64),
    updated_at         DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY (market, toYYYYMM(as_of_date))
ORDER BY (market, underlying, lookback_days, as_of_date)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS feature_term_structure_snapshot_daily
(
    market          LowCardinality(String),
    underlying      LowCardinality(String),
    as_of_date      Date,
    expiration      Date,
    days_to_expiry  UInt16,
    atm_iv          Nullable(Float64),
    call_iv         Nullable(Float64),
    put_iv          Nullable(Float64),
    contract_count  UInt32,
    updated_at      DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY (market, toYYYYMM(as_of_date))
ORDER BY (market, underlying, as_of_date, expiration)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS feature_skew_snapshot_daily
(
    market          LowCardinality(String),
    underlying      LowCardinality(String),
    as_of_date      Date,
    expiration      Date,
    days_to_expiry  UInt16,
    otm_call_iv     Nullable(Float64),
    otm_put_iv      Nullable(Float64),
    put_call_skew   Nullable(Float64),
    contract_count  UInt32,
    updated_at      DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY (market, toYYYYMM(as_of_date))
ORDER BY (market, underlying, as_of_date, expiration)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS feature_liquidity_snapshot_daily
(
    market                  LowCardinality(String),
    underlying              LowCardinality(String),
    as_of_date              Date,
    expiration              DateTime('UTC'),
    days_to_expiry          UInt16,
    avg_bid_close           Nullable(Float64),
    avg_ask_close           Nullable(Float64),
    avg_mark_close          Nullable(Float64),
    relative_spread         Nullable(Float64),
    open_interest           Nullable(Float64),
    tick_count              UInt64,
    volume                  UInt64 DEFAULT 0,
    transactions            UInt64 DEFAULT 0,
    contract_count          UInt32,
    active_contract_count   UInt32 DEFAULT 0,
    tradable_contract_count UInt32,
    activity_ratio          Nullable(Float64),
    tradability_ratio       Nullable(Float64),
    updated_at              DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY (market, toYYYYMM(as_of_date))
ORDER BY (market, underlying, as_of_date, expiration)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS feature_daily_panel_daily
(
    market                            LowCardinality(String),
    underlying                        LowCardinality(String),
    lookback_days                     UInt16,
    min_days_to_expiry                Int32,
    max_days_to_expiry                Int32,
    as_of_date                        Date,
    price_observations                UInt32,
    iv_observations                   UInt32,
    hv10                              Nullable(Float64),
    hv20                              Nullable(Float64),
    hv30                              Nullable(Float64),
    current_iv                        Nullable(Float64),
    iv_percentile                     Nullable(Float64),
    iv_rank                           Nullable(Float64),
    front_expiration                  DateTime('UTC') DEFAULT toDateTime(0, 'UTC'),
    front_days_to_expiry              Int32 DEFAULT -1,
    front_atm_iv                      Nullable(Float64),
    front_put_call_skew               Nullable(Float64),
    surface_contract_count            Int32 DEFAULT -1,
    liquidity_open_interest           Nullable(Float64),
    liquidity_relative_spread         Nullable(Float64),
    liquidity_tick_count              UInt64,
    liquidity_volume                  UInt64,
    liquidity_transactions            UInt64,
    liquidity_contract_count          UInt32,
    liquidity_active_contract_count   UInt32,
    liquidity_tradable_contract_count UInt32,
    liquidity_activity_ratio          Nullable(Float64),
    liquidity_tradability_ratio       Nullable(Float64),
    is_early_close                    UInt8,
    days_from_prev_holiday            Int32 DEFAULT -1,
    days_to_next_holiday              Int32 DEFAULT -1,
    updated_at                        DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY (market, toYYYYMM(as_of_date))
ORDER BY (market, underlying, lookback_days, min_days_to_expiry, max_days_to_expiry, as_of_date)
SETTINGS index_granularity = 8192;

ALTER TABLE feature_liquidity_snapshot_daily ADD COLUMN IF NOT EXISTS volume UInt64 DEFAULT 0;
ALTER TABLE feature_liquidity_snapshot_daily ADD COLUMN IF NOT EXISTS transactions UInt64 DEFAULT 0;
ALTER TABLE feature_liquidity_snapshot_daily ADD COLUMN IF NOT EXISTS active_contract_count UInt32 DEFAULT 0;
ALTER TABLE feature_liquidity_snapshot_daily ADD COLUMN IF NOT EXISTS activity_ratio Nullable(Float64);