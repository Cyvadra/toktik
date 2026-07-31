-- ClickHouse 26.7+ workload scheduler for Toktik request classes.
-- API server initializes this schema automatically when clickhouse.priority.enabled=true.
-- This reference uses the default capacity values. On first startup, the API
-- server instead uses clickhouse.priority values from toktik.yaml.
CREATE RESOURCE IF NOT EXISTS toktik_query (QUERY);
CREATE RESOURCE IF NOT EXISTS toktik_master_cpu (MASTER THREAD);
CREATE RESOURCE IF NOT EXISTS toktik_worker_cpu (WORKER THREAD);

CREATE WORKLOAD IF NOT EXISTS toktik_all SETTINGS
    max_concurrent_queries = 64 FOR toktik_query,
    max_concurrent_threads = 64 FOR toktik_master_cpu,
    max_concurrent_threads = 64 FOR toktik_worker_cpu;

CREATE WORKLOAD IF NOT EXISTS default IN toktik_all SETTINGS
    priority = 0,
    weight = 3;

CREATE WORKLOAD IF NOT EXISTS toktik_interactive IN toktik_all SETTINGS
    priority = -1,
    weight = 8;

CREATE WORKLOAD IF NOT EXISTS toktik_background IN toktik_all SETTINGS
    priority = 1,
    weight = 1,
    max_concurrent_queries = 4 FOR toktik_query,
    max_concurrent_threads = 4 FOR toktik_master_cpu,
    max_concurrent_threads = 8 FOR toktik_worker_cpu;