# ThetaData Sync — Original Design Summary

> This document captures the design intent of the now-removed `cmd/thetadata-sync` and
> `pkg/thetadata` modules so the knowledge is preserved for future reference.

## Purpose

Synchronise US equity options data from the **Theta Data** terminal (local REST / MCP
server on `127.0.0.1:25503`) into **ClickHouse** for downstream back-testing and
analytics.

The tool is a CLI binary (`thetadata-sync`) that:

1. Discovers the option contract universe for one or more root symbols (e.g. AAPL, SPY).
2. Downloads 1-minute OHLC and NBBO quote bars plus EOD Greeks and open interest.
3. Computes Greeks locally via a Black-76 model when server-side Greeks are unavailable.
4. Stores everything into three ClickHouse tables (`equity_options_bar_1m`,
   `equity_options_symbol_meta`, `equity_spot_bar_1m`).
5. Tracks progress per (root, date) so a run can be interrupted and resumed.

---

## Transport Layer

### MCP Client (`mcp.go`)

* SSE-based **Model Context Protocol** (JSON-RPC 2.0 over Server-Sent Events).
* Lifecycle: `GET /mcp/sse` → receive session path → POST JSON-RPC requests → read
  SSE responses keyed by request ID.
* One mutex per client instance serialises RPC calls; the pipeline creates a dedicated
  MCP client per worker.

### REST Client (`client.go`)

* Thin wrapper over `MCPClient` with per-worker rate limiting (token bucket).
* Exposes high-level methods:
  - **List** — `ListRoots`, `ListExpirations`, `ListStrikes`, `ListDates`
  - **History** — `GetQuotes1m`, `GetOHLC1m`, `GetQuotes1mRange`, `GetOHLC1mRange`,
    `GetStockEOD`, `GetGreeksEOD`, `GetOpenInterest`
* REST fallback: some calls use direct `GET /v3/…` with JSON format when MCP is not
  needed (list endpoints).
* Automatic retry with exponential backoff (4 attempts).

---

## Pipeline (`pipeline.go`)

### Three-Phase Weekly-Batch Architecture

| Phase | Name | What happens |
|-------|------|-------------|
| 1 | **Contract Enumeration** | For each root, list expirations → list strikes → filter strikes ±50 % of spot → build complete contract universe. |
| 2 | **OHLC Discovery** | Chunk dates into weekly batches. Download 1 m OHLC for all contracts. Keep only contracts with volume ≥ `MinVolume`. Cache results in-memory (`sync.Map`). |
| 3 | **Quote Download + Greeks** | For the filtered contracts, download 1 m NBBO quotes. Merge with OHLC cache. Compute Greeks; batch-insert into ClickHouse. |

### Worker Model

* Configurable concurrency (`--workers`, default 4).
* Each worker opens its own MCP client with independent rate limiter.
* Work is distributed via a channel; errors are accumulated but do not stop siblings.

### Greeks Computation (`greeks.go`)

* **Black-76** forward model (not Black-Scholes).
* **Forward estimation** via put-call parity linear regression:
  `C(K)−P(K) = DF·(F−K)` → solve for F and DF.
* **Implied vol** via bisection search [0.001, 10.0], tolerance 1e-8, 100 iterations.
* **Greeks** computed analytically: Delta, Gamma, Vega (per 1 % vol), Theta (per
  calendar day), Rho (per 1 % rate).

---

## Storage (`storage.go`)

### ClickHouse Tables

| Table | Purpose |
|-------|---------|
| `equity_options_symbol_meta` | Contract metadata (symbol_id, strike, expiration, type) — `ReplacingMergeTree` |
| `equity_options_bar_1m` | 1-minute bars: mark/last/bid/ask OHLC, IV, Greeks, OI — `MergeTree` partitioned by month |
| `equity_spot_bar_1m` | Underlying price proxy (forward price used as spot) — `MergeTree` partitioned by month |

Batch inserts in 50 000-row chunks via ClickHouse native batching.

---

## Progress Tracking (`progress.go`)

* State sharded on disk: `<dir>/states/<date>/<ROOT>.json`.
* States: `inflight → completed | failed`.
* Incremental: attempts counter, intermediate "downloaded" checkpoint, completed with
  stats (expected/stored bars).
* Resume: on startup, any `completed` (root, date) is skipped; `inflight` triggers
  delete-and-retry.

---

## Root Pre-Filtering (`root_filter.go`)

* Optional `--prefilter-roots` mode scores roots by:
  - Number of **recent expirations** (within `--root-recent-lookback-days`).
  - **Strike density** across a sample of nearest expirations.
  - Score = `RecentExpirations × 100 000 + SampledStrikes`.
* Sorts descending; optionally caps to `--root-top-n`.

---

## Data Models (`models.go`)

| Struct | Fields |
|--------|--------|
| `Contract` | Root, Expiration, Strike, Right |
| `QuoteBar` | Timestamp, Bid, BidSize, Ask, AskSize |
| `OHLCBar` | Timestamp, O/H/L/C, Volume, Count |
| `GreeksEOD` | Date, UnderlyingPrice, IV, Delta/Gamma/Vega/Theta/Rho, Close, Bid, Ask, Volume, OI |
| `GreeksResult` | IV, Delta, Gamma, Vega, Theta, Rho |
| `ForwardInfo` | Forward, DiscountFactor, Rate |
| `SyncConfig` | All CLI flags as typed fields |

---

## Configuration (CLI Flags)

| Flag | Default | Description |
|------|---------|-------------|
| `--roots` | `AAPL,SPY` | Comma-separated roots (or `*` / `all`) |
| `--all-roots` | false | Discover all available roots |
| `--start-date` / `--end-date` | `2019-01-01` / `2026-02-28` | Date range |
| `--mcp-url` | `http://127.0.0.1:25503` | Theta Terminal address |
| `--clickhouse-dsn` | `clickhouse://default:@localhost:9000/default` | ClickHouse connection |
| `--workers` | 4 | Parallel download workers |
| `--batch-days` | 5 | Trading days per weekly batch |
| `--min-volume` | 1 | Minimum daily volume to include contract |
| `--rate-limit` | 5.0 | Max requests/sec/worker |
| `--progress-dir` | `.thetadata-progress` | Disk progress state |
| `--prefilter-roots` | false | Enable root scoring |
| `--schema` | auto-detected | ClickHouse DDL file |

---

## Known Limitations

1. **MCP-only transport**: used a custom SSE/JSON-RPC transport layer instead of the
   standard REST v3 API. This added complexity and a hard dependency on a specific
   server mode.
2. **Client-side Greeks**: computed Greeks locally using Black-76. The Theta Data API
   already provides server-side Greeks (EOD and intraday) with more accurate models
   (SOFR rates, dividends, real TTE).
3. **Per-contract requests**: each contract × date required separate API calls, leading
   to extremely high request counts. The v3 REST API supports wildcard expirations
   (`expiration=*`) and wildcard strikes (`strike=*`) to fetch entire chains in one
   request.
4. **No stock/index data endpoints**: used MCP tool `stock_history_eod`; the standard
   REST API also provides `/v3/stock/history/eod`.
