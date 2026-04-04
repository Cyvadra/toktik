# OpenSpec 03: Phase 3 Reference Data

## Goal

Standardize external reference inputs required by downstream recommendation systems and risk controls.

## Status

Planned only. No reference-data ingestion or read APIs have been implemented yet in this series.

## Planned Domains

- Earnings calendar.
- FOMC and macro-event calendar.
- Risk-free rate history.
- Volatility index series such as VIX.
- Data freshness and quality annotations.

## Deliverables

- Reference tables with source and update metadata.
- Ingestion commands or jobs per source.
- Low-level read APIs for calendars, rates, and quality state.
- Quality flags that downstream services can consume directly.

## Constraints

- Every external source must record provenance.
- Missing or delayed data must remain queryable as degraded state, not silent failure.

## Entry Criteria

- Phase 2 contracts should be stable enough that feature consumers can combine market features with reference inputs without another API reshape.