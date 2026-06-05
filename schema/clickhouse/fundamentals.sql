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

-- FMP quarterly financial statements
-- Persist the source statement facts used to derive symbol-level PE/PB. These
-- tables are intentionally separate from `fundamental_observation`: statements
-- are source-domain facts, while PE/PB rows are derived point-in-time factors.

CREATE TABLE IF NOT EXISTS fmp_income_statement_quarterly
(
    symbol                         LowCardinality(String),
    date                           Date,
    fiscal_year                    LowCardinality(String),
    period                         LowCardinality(String),
    reported_currency              LowCardinality(String),
    cik                            String DEFAULT '',
    filing_date                    DateTime('UTC') DEFAULT toDateTime(0),
    accepted_date                  DateTime('UTC') DEFAULT toDateTime(0),
    revenue                        Float64 DEFAULT 0,
    cost_of_revenue                Float64 DEFAULT 0,
    gross_profit                   Float64 DEFAULT 0,
    operating_income               Float64 DEFAULT 0,
    income_before_tax              Float64 DEFAULT 0,
    income_tax_expense             Float64 DEFAULT 0,
    net_income                     Float64 DEFAULT 0,
    bottom_line_net_income         Float64 DEFAULT 0,
    eps                            Float64 DEFAULT 0,
    eps_diluted                    Float64 DEFAULT 0,
    weighted_average_shs_out       Float64 DEFAULT 0,
    weighted_average_shs_out_dil   Float64 DEFAULT 0,
    source                         LowCardinality(String) DEFAULT 'fmp',
    content_hash                   String,
    revision                       UInt32 DEFAULT 0,
    ingested_at                    DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(date)
ORDER BY (symbol, date, period, fiscal_year, accepted_date, source, revision)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS fmp_balance_sheet_quarterly
(
    symbol                         LowCardinality(String),
    date                           Date,
    fiscal_year                    LowCardinality(String),
    period                         LowCardinality(String),
    reported_currency              LowCardinality(String),
    cik                            String DEFAULT '',
    filing_date                    DateTime('UTC') DEFAULT toDateTime(0),
    accepted_date                  DateTime('UTC') DEFAULT toDateTime(0),
    cash_and_cash_equivalents      Float64 DEFAULT 0,
    total_current_assets           Float64 DEFAULT 0,
    total_assets                   Float64 DEFAULT 0,
    total_current_liabilities      Float64 DEFAULT 0,
    total_liabilities              Float64 DEFAULT 0,
    total_stockholders_equity      Float64 DEFAULT 0,
    total_equity                   Float64 DEFAULT 0,
    total_debt                     Float64 DEFAULT 0,
    net_debt                       Float64 DEFAULT 0,
    source                         LowCardinality(String) DEFAULT 'fmp',
    content_hash                   String,
    revision                       UInt32 DEFAULT 0,
    ingested_at                    DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(date)
ORDER BY (symbol, date, period, fiscal_year, accepted_date, source, revision)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS fmp_cash_flow_statement_quarterly
(
    symbol                                      LowCardinality(String),
    date                                        Date,
    fiscal_year                                 LowCardinality(String),
    period                                      LowCardinality(String),
    reported_currency                           LowCardinality(String),
    cik                                         String DEFAULT '',
    filing_date                                 DateTime('UTC') DEFAULT toDateTime(0),
    accepted_date                               DateTime('UTC') DEFAULT toDateTime(0),
    net_income                                  Float64 DEFAULT 0,
    depreciation_and_amortization               Float64 DEFAULT 0,
    stock_based_compensation                    Float64 DEFAULT 0,
    net_cash_provided_by_operating_activities   Float64 DEFAULT 0,
    capital_expenditure                         Float64 DEFAULT 0,
    free_cash_flow                              Float64 DEFAULT 0,
    source                                      LowCardinality(String) DEFAULT 'fmp',
    content_hash                                String,
    revision                                    UInt32 DEFAULT 0,
    ingested_at                                 DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(date)
ORDER BY (symbol, date, period, fiscal_year, accepted_date, source, revision)
SETTINGS index_granularity = 8192;

-- Macro fundamentals domain
-- Weakly symbol-bound or symbol-agnostic macro/fundamental series ingested from
-- external datasets such as Gurufocus Shiller/CAPE. These remain sparse and
-- point-in-time safe, while query-time expansion can align them onto high-
-- frequency reference bars (for example SPX/SPY minute bars).

CREATE TABLE IF NOT EXISTS macro_factor_catalog
(
    dataset             LowCardinality(String),
    factor_code         LowCardinality(String),
    display_name        String,
    description         String DEFAULT '',
    value_type          LowCardinality(String) DEFAULT 'float',
    unit                LowCardinality(String) DEFAULT '',
    preferred_frequency LowCardinality(String) DEFAULT 'monthly',
    fill_policy         LowCardinality(String) DEFAULT 'forward_fill',
    fill_max_days       UInt16 DEFAULT 0,
    point_in_time       UInt8 DEFAULT 1,
    source              LowCardinality(String) DEFAULT '',
    reference_market    LowCardinality(String) DEFAULT '',
    reference_symbol    LowCardinality(String) DEFAULT '',
    realtime_mode       LowCardinality(String) DEFAULT 'forward_fill',  -- forward_fill | price_scaled
    active              UInt8 DEFAULT 1,
    sla_hours           UInt32 DEFAULT 0,
    metadata            String DEFAULT '',
    updated_at          DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (dataset, factor_code)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS macro_observation
(
    dataset          LowCardinality(String),
    factor_code      LowCardinality(String),
    event_ts         DateTime('UTC'),
    known_at         DateTime('UTC'),
    period_start     DateTime('UTC') DEFAULT toDateTime(0),
    period_end       DateTime('UTC') DEFAULT toDateTime(0),
    source           LowCardinality(String) DEFAULT '',
    value            Float64,
    reference_market LowCardinality(String) DEFAULT '',
    reference_symbol LowCardinality(String) DEFAULT '',
    anchor_value     Float64 DEFAULT 0,
    revision         UInt32 DEFAULT 0,
    ingested_at      DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY (dataset, toYYYYMM(known_at))
ORDER BY (dataset, factor_code, known_at, event_ts, source, revision)
SETTINGS index_granularity = 8192;
