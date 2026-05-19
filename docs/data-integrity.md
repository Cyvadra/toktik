# Data integrity checks

`data-sync-pipeline integrity` validates core derived market-data outputs after sync jobs run. By default it only reports findings. It mutates ClickHouse only when `--repair` is provided.

## Checks

The command currently checks:

- US options aggregate coverage: `us_options_bar_1m` versus `us_options_bar_{5m,15m,30m,1h,2h,4h,1d}` by `(market_date, underlying)`.
- US stocks aggregate coverage: `us_stocks_bar_1m` versus `us_stocks_bar_{5m,15m,30m,1h,2h,4h,1d}` by `(market_date, symbol)`.
- US options chain cache coverage: `us_options_bar_1m` versus `us_options_chain_{5m,15m,30m,1h,2h,4h,1d}` by `(market_date, underlying)`.
- PE/PB coverage and freshness in `fundamental_observation`.
- IV/HV feature quality in `feature_volatility_snapshot_daily` and `feature_daily_panel_daily`.

## Repair scope

The first repairable scope is limited to data that can be rebuilt entirely from local ClickHouse base tables:

- `us_options_bar_*_agg`
- `us_options_chain_*_agg`
- `us_stocks_bar_*_agg`

Repairs use the existing full-table rebuild functions. A repair run can therefore be expensive: it truncates the aggregate backing tables and repopulates them from current `1m` base data.

The command reports fundamentals and feature-store issues but does not automatically call external fundamental providers or feature backfills. Those repairs should stay explicit because they may be slow, call external APIs, or rewrite broad feature-store ranges.

## Examples

Report all checks for the recent default window:

```bash
go run ./cmd/data-sync-pipeline integrity --config configs/data-sync-pipeline.yaml
```

Report the known August 2025 options aggregate gap:

```bash
go run ./cmd/data-sync-pipeline integrity \
  --config configs/data-sync-pipeline.yaml \
  --from 2025-08-01 \
  --to 2025-09-02 \
  --targets us-options-aggregates,features \
  --underlyings PLTR,NFLX,LITE
```

Preview aggregate repairs without mutating data:

```bash
go run ./cmd/data-sync-pipeline integrity \
  --config configs/data-sync-pipeline.yaml \
  --from 2025-08-01 \
  --to 2025-09-02 \
  --targets us-options-aggregates,chain-cache \
  --repair \
  --dry-run
```

Run a real aggregate repair:

```bash
go run ./cmd/data-sync-pipeline integrity \
  --config configs/data-sync-pipeline.yaml \
  --from 2025-08-01 \
  --to 2025-09-02 \
  --targets us-options-aggregates,chain-cache \
  --repair
```

JSON output is available for schedulers or CI:

```bash
go run ./cmd/data-sync-pipeline integrity --format json
```
