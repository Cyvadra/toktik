# OpenSpec 00: Infra Platform Overview

## Objective

Build a reusable market-data infrastructure layer that supports:

- Multi-market ingestion and storage.
- Stable low-level query APIs.
- Derived feature computation needed by higher-level recommendation systems.
- Operational visibility into freshness, readiness, and backfill state.

## Current Baseline

The repository already provides:

- ClickHouse-backed storage for crypto options and US market data.
- Import and backfill CLIs.
- Precomputed K-line views and option-chain cache tables.
- A Gin API for legacy crypto-options routes plus unified market routes for crypto options, US stocks, and US options.
- Infra discovery/readiness APIs for markets and dataset freshness.
- Initial feature-store APIs for volatility, term structure, skew, liquidity snapshots/history, US market event-window snapshot/history, and merged daily feature panels.

The main gaps are:

- Market coverage is now exposed through unified low-level routes, but some capabilities are still asymmetric by market.
- Feature-store APIs now exist, but productionized recompute and broader feature coverage are still incomplete.
- Reference data and operational metadata are incomplete.
- Production run-state, scheduling, and rerun APIs are still missing.

## Target Principles

1. Keep market-specific table schemas, but expose market-agnostic API contracts.
2. Avoid breaking existing crypto-options endpoints during migration.
3. Prefer read-only infra APIs before business APIs.
4. Keep feature computation reproducible from stored data.
5. Expose readiness and freshness explicitly instead of hiding it in logs.

## Phase Map

1. Unified market API.
2. Feature store.
3. Reference data and quality metadata.
4. Production operations and scheduling.

## Progress Snapshot

- Phase 1: functional in code and test-covered for the current slice.
- Phase 2: active. Core volatility APIs, initial `us-options` surface APIs, cross-market liquidity APIs, US market event-window snapshot/history APIs, and a merged daily feature panel API are implemented.
- Phase 3: not started.
- Phase 4: only the read-only observability subset is implemented so far.

## Non-Goals

- No agent orchestration in this series.
- No recommendation ranking or strategy scoring APIs.
- No frontend work beyond API readiness.