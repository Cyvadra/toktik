# OpenSpec 02: Phase 2 Feature Store

## Goal

Turn derived market features into explicit infra resources instead of embedding them inside strategy code or ad hoc queries.

## Status

In progress. The first API slice is implemented and the current snapshot set now has precompute/backfill support.

## Planned Features

- Historical volatility windows: HV10, HV20, HV30.
- IV rank and IV percentile.
- Put-call skew.
- Term structure snapshots.
- Liquidity metrics: spread, OI, volume, tradability flags.
- Event-window proximity flags.

## Deliverables

- Feature snapshot tables.
- Feature history tables for backfills and audits.
- Read APIs by market, underlying, contract, and date.
- Recompute jobs for targeted refreshes.

## First Increment

- `GET /api/v1/features/volatility-snapshot`
- `GET /api/v1/features/volatility-history`
- `GET /api/v1/features/term-structure-snapshot`
- `GET /api/v1/features/skew-snapshot`
- `GET /api/v1/features/liquidity-snapshot`
- `GET /api/v1/features/liquidity-history`
- `GET /api/v1/features/event-window-snapshot`
- `GET /api/v1/features/event-window-history`
- `GET /api/v1/features/daily-feature-panel`
- Required query params: `market`, `underlying`
- History adds required `from` and `to`
- Optional query params: `lookback_days` with default `252`
- Initial markets: `crypto-options`, `us-options`
- Current output: `HV10`, `HV20`, `HV30`, `current_iv`, `iv_percentile`, `iv_rank`
- Current extension: `us-options` term structure and put-call skew snapshots.
- Current extension: `crypto-options` and `us-options` liquidity features.
- Current extension: US market event-window flags from the session calendar, available as snapshot and history.
- Current extension: merged daily feature panel rows that align volatility, liquidity, surface, and event-window signals by date.
- Implementation mode: API prefers `feature_volatility_snapshot_daily` when present and falls back to query-time computation from existing bar tables.
- Backfill entrypoint: `cmd/feature-store-backfill`
- Daily panel materialization keys: `market`, `underlying`, `lookback_days`, `min_days_to_expiry`, `max_days_to_expiry`, `as_of_date`
- Incremental refresh support: `--incremental-days N` refreshes only the recent daily window when explicit `--from/--to` are omitted.
- Backfill summary output reports markets processed, underlyings written/skipped, rows written, replacement count, failure count, and elapsed time.
- Failure details are emitted per failed market/underlying scope when the backfill continues past an error.
- Current implementation now also writes and reads precomputed `us-options` term-structure and skew snapshots.
- Current implementation now also writes and reads precomputed daily feature panel rows for `crypto-options` and `us-options`.

## Implemented Scope

- Volatility snapshot/history APIs for `crypto-options` and `us-options`.
- Hybrid volatility reads: prefer precomputed `feature_volatility_snapshot_daily`, otherwise compute from raw bars.
- `us-options` term-structure snapshot API with precomputed-read preference and raw fallback.
- `us-options` skew snapshot API with precomputed-read preference and raw fallback.
- `crypto-options` and `us-options` liquidity snapshot/history APIs backed by the feature-store liquidity table.
- `us-options` and `us-stocks` event-window snapshot/history APIs based on the session calendar.
- Daily feature panel API that merges volatility history, liquidity history, front surface state, and event-window flags into one date-aligned response, with precomputed-read preference and backfill support.
- Feature-store schema tables for volatility, term structure, and skew daily snapshots.
- Feature-store schema table for liquidity daily snapshots.
- Feature-store schema table for merged daily panels.
- Backfill writes for volatility, term structure, and skew snapshot tables.
- Backfill writes for crypto-options and us-options liquidity snapshots.
- Backfill writes for crypto-options and us-options daily feature panels.
- Feature-store dataset visibility under `/api/v1/infra/datasets` and capability advertisement under `/api/v1/infra/markets`.

## Remaining Work In This Phase

- Cross-market liquidity normalization and richer US option microstructure fields beyond the current volume/transactions activity coverage.
- Clear methodology hardening for skew definitions beyond the current simple put-call IV spread.

## Dependencies

- Phase 1 routing and market catalog.
- Stable identifiers for underlyings and contracts.
- Basic readiness reporting.