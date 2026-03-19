# ThetaData Sync v2 — Design Document

> New implementation replacing the removed `pkg/thetadata` + `cmd/thetadata-sync`.
> Uses the standard Theta Data **REST v3** API directly (no MCP/SSE transport).

## Goals

1. Synchronise US equity options data from Theta Data into ClickHouse.
2. Use the **REST v3 JSON API** at `http://127.0.0.1:25503/v3` (plain HTTP GET).
3. Leverage **server-side Greeks** (`/option/history/greeks/eod`,
   `/option/history/greeks/first_order`) instead of computing locally.
4. Leverage **wildcard queries** (`expiration=*`, `strike=*`) to fetch full chains per
   root per date in a single request, dramatically reducing API call count.
5. Resumable: track progress per (root, date); skip already-completed work.
6. Concurrent workers with configurable rate limiting.

---

## Architecture Overview

```
cmd/thetadata-sync/main.go          CLI entry point
pkg/thetadata/
  client.go                          REST v3 HTTP client
  models.go                          Data types
  pipeline.go                        Orchestration (day-by-day per root)
  storage.go                         ClickHouse writer
  progress.go                        Resumable state tracker
```

---

## REST v3 Client (`client.go`)

Plain `net/http` client. All calls are `GET` with `format=json`. Retry up to 3 times
with exponential backoff on transient failures (5xx, timeout).

### Endpoints Used

| Method | REST Path | Purpose | Min Tier |
|--------|-----------|---------|----------|
| `ListSymbols` | `/option/list/symbols` | All tradable root symbols | free |
| `ListExpirations` | `/option/list/expirations?symbol=X` | Expirations for a root | free |
| `ListStrikes` | `/option/list/strikes?symbol=X&expiration=D` | Strikes for root+exp | free |
| `ListContracts` | `/option/list/contracts/quote?symbol=X&date=D` | Contracts quoted on date | value |
| `GetEOD` | `/option/history/eod?symbol=X&expiration=*&start_date=D&end_date=D` | Full-chain EOD in one call | free |
| `GetGreeksEOD` | `/option/history/greeks/eod?symbol=X&expiration=*&start_date=D&end_date=D` | Full-chain EOD Greeks | standard |
| `GetOHLC` | `/option/history/ohlc?symbol=X&expiration=E&start_date=D&end_date=D&interval=1m` | 1m OHLC per expiration batch | value |
| `GetQuotes` | `/option/history/quote?symbol=X&expiration=E&start_date=D&end_date=D&interval=1m` | 1m NBBO quotes per expiration batch | value |
| `GetOpenInterest` | `/option/history/open_interest?symbol=X&expiration=*&date=D` | Full-chain OI | value |

### Key Design Decisions

* **Wildcard first**: wherever the API supports `expiration=*` and `strike=*`, we use
  them to fetch an entire chain in one call rather than enumerating contracts.
* **Server-side Greeks**: the `/greeks/eod` endpoint returns IV, delta, gamma, vega,
  theta, rho, underlying_price — pre-calculated by Theta Data using proper rate curves
  (SOFR). No need for client-side Black-76.
* **JSON format**: all responses requested as `format=json` and decoded into typed Go
  structs.
* **Rate limiting**: token-bucket limiter shared across workers, configurable via
  `--rate-limit` (default 10 req/s total). The v3 local terminal has no hard rate
  limit but we throttle to avoid overwhelming it.

---

## Pipeline (`pipeline.go`)

### Day-by-Day Per-Root

For each `(root, date)` pair in the configured range:

1. **Skip** if already marked completed in progress tracker.
2. **Fetch EOD + Greeks**: single call per root per day using `expiration=*`.
   Returns all contracts with their OHLC, bid/ask, Greeks, and underlying price.
3. **Fetch Open Interest**: single call per root per day using `expiration=*`.
4. **Optionally fetch 1m bars**: if `--intraday` flag is set, fetch 1m OHLC +
   quotes per expiration (multi-day requests limited to 1 month, so we batch by
   expiration within the day).
5. **Write to ClickHouse**: batch insert into `equity_options_bar_1m`,
   `equity_options_symbol_meta`, `equity_spot_bar_1m`.
6. **Mark completed** in progress tracker.

### Worker Model

* `N` goroutine workers pull `(root, date)` tasks from a shared channel.
* Each worker has its own `http.Client` (connection pooling handled by Go).
* A single shared rate limiter (`golang.org/x/time/rate`) throttles total request rate.

### Error Handling

* Transient API errors → retry with backoff (within client).
* Persistent failures → mark `(root, date)` as failed in progress, log, continue to
  next task. Failed tasks can be retried on re-run.
* Context cancellation (SIGINT/SIGTERM) → save progress, exit cleanly.

---

## Storage (`storage.go`)

Re-uses the existing ClickHouse schema (`schema/clickhouse/equity_options.sql`):

| Table | Engine | Partition | Order |
|-------|--------|-----------|-------|
| `equity_options_symbol_meta` | ReplacingMergeTree | — | symbol_id |
| `equity_options_bar_1m` | MergeTree | toYYYYMM(timestamp) | (base_asset, symbol_id, timestamp) |
| `equity_spot_bar_1m` | MergeTree | toYYYYMM(timestamp) | (symbol, timestamp) |

### Column Mapping (EOD mode)

| API Field | CH Column | Notes |
|-----------|-----------|-------|
| close | mark_close, last_close | Same value for EOD |
| open/high/low/close | last_open/high/low/close | Trade OHLC |
| bid/ask | bid_close, ask_close | EOD snapshot |
| implied_vol | mark_iv_close | From greeks/eod |
| delta/gamma/vega/theta/rho | delta, gamma, vega, theta, rho | From greeks/eod |
| open_interest | open_interest | From open_interest endpoint |
| underlying_price | → equity_spot_bar_1m | Spot proxy |

---

## Progress Tracking (`progress.go`)

Same sharded-file approach as v1:

```
<progress-dir>/states/<YYYY-MM-DD>/<ROOT>.json
```

States: `started` → `completed` | `failed`.

On startup, load all state files. Skip `completed` pairs. Retry `failed`/`started`
(previously interrupted) pairs.

---

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--roots` | `AAPL,SPY` | Comma-separated roots (or `*` for all) |
| `--start-date` | `2019-01-01` | Start date inclusive |
| `--end-date` | `2026-02-28` | End date inclusive |
| `--base-url` | `http://127.0.0.1:25503` | Theta Terminal base URL |
| `--clickhouse-dsn` | `clickhouse://default:@localhost:9000/default` | ClickHouse DSN |
| `--workers` | 4 | Concurrent workers |
| `--rate-limit` | 10.0 | Max total requests/sec |
| `--progress-dir` | `.thetadata-progress` | State directory |
| `--schema` | auto-detect | ClickHouse DDL file |
| `--intraday` | false | Also fetch 1m OHLC + quotes (slow) |
| `--debug` | false | Verbose logging |

---

## Improvements over v1

| Area | v1 | v2 |
|------|----|----|
| Transport | Custom MCP/SSE JSON-RPC | Standard HTTP GET REST |
| Greeks | Client-side Black-76 | Server-side (Theta Data calculated) |
| API calls | Per-contract requests | Wildcard chain-level requests |
| Complexity | ~2000 LOC across 8 files | ~800 LOC across 5 files |
| Dependencies | SSE reader, custom RPC | Only `net/http`, `encoding/json` |
