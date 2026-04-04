# OpenSpec 04: Phase 4 Production Operations

## Goal

Make the infra layer operationally safe and observable in scheduled production runs.

## Status

Partially started. Read-only readiness and dataset freshness APIs exist, but scheduled-run tracking and admin controls are not implemented yet.

## Planned Deliverables

- Scheduled run model for imports, backfills, and feature recomputes.
- Run-state tables with start time, finish time, status, and error summary.
- Freshness endpoints by market and dataset.
- Admin APIs for targeted reruns and dry-run inspection.
- Integration tests for schema initialization and critical API probes.

## Operational APIs

- Readiness and freshness.
- Dataset status by market.
- Backfill/recompute task status.
- Last successful update timestamps.

Current state:

- Implemented now: readiness plus dataset freshness/status inspection.
- Not implemented yet: run-state tables, task-status APIs, scheduler integration, rerun control, and dry-run admin surfaces.

## Constraints

- No destructive admin command should exist without dry-run support.
- API consumers must be able to distinguish `ready`, `degraded`, and `down` states.