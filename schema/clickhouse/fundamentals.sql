-- Fundamentals domain
-- Symbol-bound numeric fundamental factors (PE, PB, dividend yield, market cap,
-- valuation ratios, etc.) for us-stocks and crypto-spot. Storage is tall and
-- sparse: one row per (market, symbol, factor_code, event_ts, known_at, source).
-- All point-in-time backtestable queries filter on `known_at <= requested_time`.
--
-- Companion catalog table (`fundamental_factor_catalog`) declares each factor's
-- metadata (display name, unit, preferred frequency, default fill policy, etc.)
-- and is the control plane for ingest validation and query defaults.

CREATE TABLE IF NOT EXISTS fundamental_factor_catalog
(
    market              LowCardinality(String),
    factor_code         LowCardinality(String),
    display_name        String,
    description         String DEFAULT '',
    value_type          LowCardinality(String) DEFAULT 'float',          -- float | ratio | percent | currency | count
    unit                LowCardinality(String) DEFAULT '',
    preferred_frequency LowCardinality(String) DEFAULT 'daily',          -- intraday | daily | weekly | monthly | quarterly | event
    fill_policy         LowCardinality(String) DEFAULT 'forward_fill',   -- event_only | forward_fill | limited_forward_fill
    fill_max_days       UInt16 DEFAULT 0,                                -- 0 = unlimited (only used when fill_policy = limited_forward_fill)
    point_in_time       UInt8 DEFAULT 1,                                 -- 1 = require known_at, 0 = event_ts is also availability
    source              LowCardinality(String) DEFAULT '',
    active              UInt8 DEFAULT 1,
    sla_hours           UInt32 DEFAULT 0,                                -- expected freshness in hours; 0 = unspecified
    metadata            String DEFAULT '',                               -- optional JSON blob
    updated_at          DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (market, factor_code)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS fundamental_observation
(
    market        LowCardinality(String),
    symbol        LowCardinality(String),
    factor_code   LowCardinality(String),
    event_ts      DateTime('UTC'),                       -- when the underlying event/value applies (e.g., quarter end, EOD)
    known_at      DateTime('UTC'),                       -- when the value first became known to us (point-in-time)
    period_start  DateTime('UTC') DEFAULT toDateTime(0), -- optional period semantics (e.g., quarter start)
    period_end    DateTime('UTC') DEFAULT toDateTime(0),
    source        LowCardinality(String) DEFAULT '',
    value         Float64,
    revision      UInt32 DEFAULT 0,                      -- monotonic per (symbol,factor,event_ts,source); higher wins on ties
    ingested_at   DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY (market, toYYYYMM(known_at))
ORDER BY (market, symbol, factor_code, known_at, event_ts, source, revision)
SETTINGS index_granularity = 8192;
