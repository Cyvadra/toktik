# OpenSpec 01: Phase 1 Unified Market API

## Goal

Introduce a stable infra API skeleton that can host multiple market domains while preserving current crypto-options compatibility.

## Status

Implemented for the current phase slice.

## Deliverables

- Shared infra endpoints for health, readiness, and market catalog.
- Market-aware API namespace under `/api/v1/markets/{market}`.
- Backward-compatible aliases for existing `/api/v1/crypto-options/*` routes.
- Service contracts that separate infra metadata from market query handlers.

## Phase 1 Slice

The initial implementation slice in this repository includes:

- `GET /health`
- `GET /ready`
- `GET /api/v1/infra/markets`
- `GET /api/v1/infra/datasets`
	- Supports `market` and `status` query filters for low-level infra inspection.
	- Returns per-market and overall dataset summary aggregates.
- `GET /api/v1/markets/crypto-options/bars`
- `GET /api/v1/markets/crypto-options/symbols`
- `GET /api/v1/markets/crypto-options/greeks`
- `POST /api/v1/markets/crypto-options/backtest`
- `GET /api/v1/markets/us-stocks/bars`
- `GET /api/v1/markets/us-stocks/symbols`
- `GET /api/v1/markets/us-options/bars`
- `GET /api/v1/markets/us-options/symbols`
- `GET /api/v1/markets/us-options/greeks`
- `GET /api/v1/markets/us-options/chain`

The legacy `/api/v1/crypto-options/*` routes remain active during migration.

## Remaining Out of Scope for Phase 1

- Feature endpoints moved into Phase 2 and are now implemented under `/api/v1/features/*`.
- Job-state and freshness history APIs.

## Success Criteria

- Existing crypto-options API tests still pass.
- New infra endpoints are covered by tests.
- API server can advertise supported and planned markets.
- Infra dataset inspection exposes at least relation name, row count, latest timestamp, and freshness state.

## Follow-Up

Once the routing skeleton is stable, Phase 2 can add derived feature resources without forcing another router redesign.