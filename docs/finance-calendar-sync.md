# Finance Calendar Sync

This document describes how Toktik syncs finance calendar data, how the observed stock watchlist is constructed, and how to operate the standalone and pipeline-based sync flows.

## Scope

Finance calendar data is pulled from FMP stable endpoints and stored in MySQL through GORM.

Covered event groups:

- macro economic calendar
- earnings calendar
- dividends calendar
- IPO calendar
- splits calendar
- financial report date references

The API layer reads from MySQL. FMP is only used as the upstream sync source.

## Storage and Runtime Requirements

Finance calendar storage is separate from ClickHouse and requires MySQL runtime configuration in `toktik.yaml` or corresponding env overrides.

Example:

```yaml
mysql:
  dsn: ""
  host: "127.0.0.1:3306"
  user: "toktik"
  password: "..."
  database: "toktik"
```

Relevant env overrides:

- `MYSQL_DSN`
- `MYSQL_HOST`
- `MYSQL_USER`
- `MYSQL_PASSWORD`
- `MYSQL_DATABASE`

`api-server` is fail-fast on MySQL startup and runs `AutoMigrate` for the finance calendar table before serving requests.

## API-Triggered Sync

The API exposes two endpoints:

- `GET /api/v1/calendar/economic`
- `POST /api/v1/calendar/stocks`

Behavior:

- each request checks a finance calendar sync marker cache
- if the marker is fresh, Toktik skips the upstream FMP sync
- the response still queries MySQL directly, so newly synced pipeline data remains visible immediately

Current default windows:

- economic calendar: today minus `7d` through today plus `30d`
- stock calendar: today minus `30d` through today plus `90d`

## Cache Policy

Two caches are involved.

### Turnover Watchlist Cache

The observed stock pool is derived from the turnover intersection screener.

- TTL: `24h`
- requests with `limit <= 60` use the same cache bucket
- this means a `Top 30` request can reuse an existing `Top 60` cache entry

### Finance Calendar Sync Marker Cache

- TTL: `12h`
- stores a lightweight marker keyed by sync scope, not the API response payload
- used by API-triggered sync, standalone CLI sync, and pipeline sync

Practical meaning:

- multiple runs inside `12h` avoid repeated FMP sync calls for the same scope
- reads still come from MySQL, not from cached API blobs

## Observed Stock Watchlist Definition

The observed stock universe is constructed in-process and does not rely on a static symbol file.

Rules:

- source: `GET /api/v1/screener/us-underlyings/turnover-intersection` equivalent service logic
- filter: non-ETF only
- lookbacks: `20`, `60`, `120` trading days
- per-lookback limit: `Top 60`
- final list: merge and de-duplicate symbols across all three lists in first-seen order

This same resolver is reused by:

- the standalone CLI sync
- the `fmp_observed_stock_calendar` pipeline job

## Standalone CLI

The repository includes a dedicated manual sync command:

```bash
go run ./cmd/us-market-calendar-sync --target economic
go run ./cmd/us-market-calendar-sync --target watchlist
go run ./cmd/us-market-calendar-sync --target all
```

Flags:

- `--target all|economic|watchlist`
- `--clickhouse-dsn ...` optional override for watchlist resolution

What each target does:

- `economic`: syncs the macro calendar window only
- `watchlist`: resolves the observed stock pool, then syncs stock-related calendar events for that pool
- `all`: runs `economic` and then `watchlist`

The CLI uses the same `12h` sync marker cache as the API and pipeline jobs.

## data-sync-pipeline Integration

Two jobs are now available in `configs/data-sync-pipeline.yaml`.

### `fmp_economic_calendar`

Purpose:

- sync macro economic events from FMP into MySQL-backed finance calendar storage

Default behavior:

- single logical source key
- no ClickHouse cursor tracking
- uses the finance calendar service window and `12h` sync marker cache
- cache initialization is required; pipeline construction now fails fast instead of silently falling back to an in-memory cache

### `fmp_observed_stock_calendar`

Purpose:

- resolve the observed US stock pool from turnover intersection results
- sync stock-related finance calendar events for that pool into MySQL

Dependencies:

- default config depends on `fmp_us_fundamentals`

Reason for dependency:

- the watchlist builder uses the non-ETF turnover screener path
- the non-ETF filter relies on cached/batched company profile classification and the turnover universe used by the screener stack

Manual pipeline examples:

```bash
go run ./cmd/data-sync-pipeline list-jobs --config configs/data-sync-pipeline.yaml
go run ./cmd/data-sync-pipeline run --config configs/data-sync-pipeline.yaml --jobs fmp_economic_calendar
go run ./cmd/data-sync-pipeline run --config configs/data-sync-pipeline.yaml --jobs fmp_observed_stock_calendar
go run ./cmd/data-sync-pipeline run --config configs/data-sync-pipeline.yaml --jobs fmp_economic_calendar,fmp_observed_stock_calendar
```

If `--jobs` selects a downstream job without its configured dependency set, the pipeline now prints a warning and keeps the legacy behavior of running only the selected jobs.

## Operational Notes

- MySQL availability is required for all finance calendar entry points.
- ClickHouse is still required for the watchlist-based stock calendar sync because the observed pool comes from the turnover screener.
- When a pipeline run or manual sync reports `rows=0`, that can mean either no upstream deltas or a warm `12h` sync marker cache.
- runner `--from/--to` is currently informational for the finance calendar jobs; the effective sync scope is still controlled by the finance calendar service marker/cache logic.
- Finance calendar writes are upserts keyed by event identity, so repeated syncs are intended to be idempotent rather than append-only.