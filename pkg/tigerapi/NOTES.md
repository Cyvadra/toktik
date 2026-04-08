# Tiger API Notes

This file captures live behavior observed from Tiger option market-data endpoints while testing `pkg/tigerapi` on 2026-04-08.

## Environment

- Commands were run from the repository root after `source ./env.sh`.
- The wrapper under test is `pkg/tigerapi` plus the CLI at `cmd/tools/tigerapi`.
- Main live test underlying: `AAPL`.

## Working Endpoints

- `market-state` works.
- `stock-kline` works.
- `option-expirations` works.
- `option-chain` works in the current environment.
- `option-quote` works in the current environment.
- `option-kline` works in the current environment.

## Quote Snapshot Behavior

- `option-quote` is a snapshot endpoint, not a historical series endpoint.
- The returned `timestamp` is millisecond precision.
- Example live value observed: `1775583302328` => `2026-04-07T17:35:02Z`.

## Option Kline Periods

The following periods were observed to return data:

- `1min`
- `5min`
- `15min`
- `30min`
- `60min`
- `day`

Additional notes:

- `1s` and `1sec` do not behave like usable second-level intraday history. They returned data in default queries, but the timestamps/behavior looked inconsistent with true second bars, and explicit time-window queries came back empty.
- `min` failed.

## Day-Level History Depth

Tested contract:

- `AAPL 260417C00200000`

Observed day-series response:

- count: `162`
- first bar: `2025-08-14`
- last bar: `2026-04-07`

Implication:

- Day-level history reached about 236 calendar days back from 2026-04-08.
- Day-level history did include bars on both `2026-03-09` and `2026-01-08`.

## Intraday History Findings

### Explicit 30-day / 90-day backtest windows

For `AAPL 260417C00200000`, the following explicit time-window queries returned empty:

- 30 days back: `1min`, `5min`, `30min`, `1h`
- 90 days back: `1min`, `5min`, `30min`, `1h`

This means old intraday history was not retrievable by explicit historical window query even when day-level history existed.

### Default no-range responses

Default `OptionKlines(period)` responses without explicit `begin_time/end_time` returned:

- `1min`: `300` bars, from `2026-04-07T15:00:00Z` to `2026-04-07T19:59:00Z`
- `5min`: `300` bars, from `2026-04-01T14:30:00Z` to `2026-04-07T19:55:00Z`
- `30min`: `300` bars, from `2026-03-04T20:30:00Z` to `2026-04-07T19:30:00Z`
- `1h`: `300` bars, from `2026-02-04T15:30:00Z` to `2026-04-07T19:30:00Z`

Important distinction:

- These default responses make it look like `30min` and `1h` can reach 30+ days back.
- But explicit raw historical window queries 30 days back still returned empty.
- So the default response window and the true historical range-query behavior are not equivalent.

## 300-Bar Limit Hypothesis

Evidence suggests the default intraday response is likely capped, but the raw range behavior is not a simple fixed-limit implementation.

Observed evidence:

- Default no-range calls for `1min`, `5min`, `30min`, `1h` all returned `300` bars.
- Small explicit window queries returned the expected smaller counts:
  - `1min` 1-hour window => `60` bars
  - `5min` 1-hour window => `12` bars
  - `30min` 4-hour window => `8` bars
  - `1h` 4-hour window => `5` bars
- Larger explicit window queries did not simply return `300` bars from older periods. Instead, they collapsed to recent data or returned empty.

Best current interpretation:

- Default intraday responses likely have a `300`-bar ceiling.
- Explicit `begin_time/end_time` queries for option intraday history do not behave like a normal historical range endpoint.
- Tiger may have additional retention or query restrictions on option intraday history beyond a simple bar-count limit.

## Retest On More Active Near-Month Contracts

To rule out the possibility that the missing history was specific to a less active contract, the same historical checks were repeated on more active `AAPL 2026-04-17` calls.

Active candidates observed:

- `AAPL 260417C00220000`, volume `4510`, open interest `4056`
- `AAPL 260417C00190000`, volume `50`, open interest `132`
- `AAPL 260417C00200000`, volume `46`, open interest `538`

Result:

- All three contracts still returned `false` / empty for 30-day and 90-day explicit history checks on:
  - `1min`
  - `5min`
  - `30min`
  - `1h`

This strongly suggests the limitation is endpoint-level behavior, not just a single illiquid-contract artifact.

## Practical Takeaways

- Use `option-quote` for current snapshot data only.
- Use `option-kline` day bars if you need older option history.
- Do not assume Tiger option intraday history supports reliable arbitrary backfill with `begin_time/end_time`.
- Do not assume that the apparent default lookback window from `OptionKlines(period)` can be reproduced via explicit old date-window queries.

## Repro Commands

Examples used during validation:

```bash
source ./env.sh
go run ./cmd/tools/tigerapi -- option-quote --identifiers 'AAPL 260417C00200000'
go run ./cmd/tools/tigerapi -- option-kline --identifier 'AAPL 260417C00200000' --period day
go run ./cmd/tools/tigerapi -- option-kline --identifier 'AAPL 260417C00200000' --period 1min
go run ./cmd/tools/tigerapi -- option-chain --symbol AAPL --expiry 2026-04-17
```

For older intraday windows, tests were executed through temporary single-process Go probes that called `ExecuteRawResponseVersioned("option_kline", ..., "2.0")` with explicit `begin_time` and `end_time`.