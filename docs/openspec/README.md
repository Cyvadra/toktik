# Toktik Infra OpenSpec Series

This directory tracks the staged infrastructure work needed to move Toktik from a market-specific data platform into a reusable market-data infra layer for the AI options product.

Scope rules for this series:

- Focus on infra only: ingestion, storage, feature store, reference data, operational APIs, and platform observability.
- Exclude agent orchestration, recommendation logic, and user-facing business APIs.
- Prefer additive changes that preserve existing crypto-options workflows while introducing market-agnostic contracts.

Document index:

- `00-infra-platform-overview.md`: target architecture, non-goals, and cross-phase constraints.
- `01-phase-1-unified-market-api.md`: unified market-data API surface and routing skeleton.
- `02-phase-2-feature-store.md`: derived feature computation and read APIs.
- `03-phase-3-reference-data.md`: event calendar, rates, volatility indices, and quality metadata.
- `04-phase-4-production-ops.md`: scheduling, run-state tracking, and operational controls.

Current status:

- Phase 1: in place in code. Unified market routes, legacy crypto-options compatibility, infra market catalog, and dataset freshness inspection are available.
- Phase 2: partially in place in code. Volatility snapshot/history, `us-options` term-structure/skew, cross-market liquidity snapshot/history, US market event-window snapshot/history, and a merged daily feature panel API are available. The current snapshot set supports precomputed writes and precomputed-read preference where applicable.
- Phase 3: planned only.
- Phase 4: partially started at the read-only observability layer through readiness and dataset freshness APIs, but run-state, scheduling, and rerun controls are still planned work.

Execution policy:

- Phase 1 starts immediately in code.
- Later phases should not begin full implementation until the previous phase has stable contracts and tests.