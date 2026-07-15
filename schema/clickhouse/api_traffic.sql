CREATE TABLE IF NOT EXISTS api_traffic_minute
(
    minute_ts          DateTime('UTC'),
    method             LowCardinality(String),
    route              LowCardinality(String),
    status_class       UInt16,
    request_count      UInt64,
    ingress_bytes      UInt64,
    egress_bytes       UInt64,
    peak_ingress_bytes UInt64,
    peak_egress_bytes  UInt64,
    peak_total_bytes   UInt64,
    updated_at         DateTime('UTC') DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY toYYYYMM(minute_ts)
ORDER BY (minute_ts, route, method, status_class)
TTL minute_ts + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;