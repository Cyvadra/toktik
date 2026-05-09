# toktik

Unified multi-market quantitative trading platform for crypto and US equity options, built in Go. Provides a full data pipeline (tick → Parquet → ClickHouse OHLCV), an event-driven backtesting engine, a feature store, a symbol-bound fundamentals domain, and a REST API.

## Features

- **Market data pipeline** — Convert zstd-compressed CSV tick data → Parquet → ClickHouse OHLCV bars with pre-computed materialized views (5m, 15m, 30m, 1h, 2h, 3h, 4h, 6h, 8h, 12h, 1d)
- **Event-driven backtesting engine** — Pine Script-style strategy interface with multi-asset, multi-leg options support, vectorized indicator computation, and realistic broker simulation (slippage, commissions, TWAP)
- **Unified infra API** — Query market data through shared `/api/v1/markets/{market}` routes while preserving legacy `/api/v1/crypto-options/*` compatibility
- **Infra observability** — Inspect readiness, market catalog, dataset row counts, latest timestamps, freshness, and dataset summary aggregates
- **Feature-store APIs** — Read volatility snapshots/history, `us-options` term-structure/skew, cross-market liquidity history, and merged daily feature panels from explicit infra endpoints, with precomputed-read preference where available
- **Fundamentals APIs** — Read symbol-bound point-in-time factor catalog, sparse/as-of/filled series, latest snapshots, multi-symbol panels, and freshness for low-frequency factors such as PE/PB
- **Event-window APIs** — Read early-close and holiday-proximity flags as both latest snapshots and daily history from the US market-session calendar
- **Options spread strategies** — Built-in bull put / bear call spread strategies with MA deviation signals
- **Strategy reuse helpers** — Shared `pkg/strategies/optutil` mixins and helpers for options strategy development
- **US market flatfile import** — Import Polygon US options and stocks minute CSV flatfiles into ClickHouse with precomputed K-line views
- **Feature-store backfill CLI** — Precompute supported daily feature snapshots and merged daily panels with incremental refresh, replacement, and failure reporting
- **HTML reports** — Self-contained backtest reports with equity curves, drawdown charts, and trade markers

## Architecture

### High-Level Overview

```
┌───────────────────────────────────────────────────────────┐
│                     CLI Binaries (cmd/)                    │
│  api-server ┃ backtest-portfolio ┃ importers ┃ backfill   │
└──────┬──────────────┬──────────────────┬──────────────────┘
       │              │                  │
       ▼              ▼                  ▼
┌─────────────┐ ┌───────────┐  ┌────────────────────┐
│  api/       │ │ service/  │  │ cryptooptions/     │
│  (Gin HTTP  │ │ (Business │  │ usmarket/          │
│   router)   │ │  logic)   │  │ (Domain importers) │
└──────┬──────┘ └─────┬─────┘  └─────────┬──────────┘
       │              │                   │
       ▼              ▼                   ▼
┌─────────────┐ ┌───────────┐  ┌──────────────────┐
│  dto/       │ │ datafeed/ │  │ schema/clickhouse │
│  (Request / │ │ (DataFeed │  │ (DDL: tables,     │
│   Response) │ │  adapters)│  │  mat. views)      │
└─────────────┘ └─────┬─────┘  └────────┬─────────┘
                      │                  │
                      ▼                  ▼
               ┌──────────────────────────────┐
               │        ClickHouse            │
               │  crypto_options_bar_1m       │
               │  us_options_bar_1m           │
               │  us_stocks_bar_1m            │
               │  feature_*_snapshot_daily    │
               │  fundamental_*               │
               │  + materialized K-line views │
               └──────────────────────────────┘
```

### Core Packages

| Package | Layer | Purpose |
|---------|-------|---------|
| `internal/backtest` | Engine | Event-driven backtester: Strategy interface, Engine orchestrator, Broker (fills/commissions/slippage), columnar DataSet, Indicator DAG, OptionsChain, SpreadGroup lifecycle |
| `internal/api` | HTTP | Gin router and handlers for `/api/v1/*` (bars, symbols, greeks, backtest, infra, features, fundamentals) |
| `internal/service` | Business | DTO-driven service layer backed by ClickHouse queries — zero direct DB access from handlers |
| `internal/datafeed` | Adapter | DataFeed implementations that load ClickHouse data into columnar DataSets for the backtest engine, including symbol-bound fundamentals factor bridges |
| `internal/cryptooptions` | Domain | Crypto options: CSV/Parquet parsing, tick→1m aggregation, symbol hashing, chain cache, ClickHouse queries |
| `internal/usmarket` | Domain | US market: Polygon CSV import, session-aware K-line aggregation (DST + holidays), Black-Scholes greeks |
| `internal/dto` | Schema | API request/response types with validation and time parsing |
| `internal/optimization` | Engine | Grid search, parameter space definition, walk-forward optimization |
| `internal/report` | Output | Self-contained HTML report generation (equity curves, drawdown charts, trade markers) |
| `internal/cli` | Util | CLI bootstrap: env fallback, DSN resolution, ClickHouse connection helpers |
| `internal/validation` | Util | Numeric sanity checks (NaN, Inf) |
| `pkg/feeds` | Plugin | External data source interface (`Feed`) + registry; pluggable sources (DVOL) |
| `pkg/strategies` | Plugin | Strategy catalog, config parsing, and individual strategy implementations |
| `pkg/strategies/optutil` | Mixin | Shared options helpers: PricingMixin, GroupMixin, PendingRefCounter, contract resolution |

### Backtest Engine Internals

```
Strategy.Init()          Register indicators, add securities
        │
        ▼
 Indicator DAG           Topological sort → parallel vectorized compute (single pass)
        │
        ▼
 Bar-by-Bar Replay       Engine feeds each bar to Strategy.OnBar()
        │                  ├─ ctx.Ind("sma20")    → read pre-computed indicator
        │                  ├─ ctx.Buy(ref, qty)   → submit order to Broker
        │                  └─ ctx.OptionsChain()  → filter contracts at bar time
        ▼
 Broker Execution        Next-bar open fill (no lookahead), slippage, commission
        │
        ▼
 Result                  Trades, equity curve, stats, spread groups → HTML/JSON
```

Key design decisions:
- **Preflight indicator DAG** — all indicators computed once before replay, not per-bar
- **Columnar DataSet** — `[]float64` per field for cache-friendly access
- **Next-bar execution** — orders execute at next bar's open to prevent lookahead bias
- **Multi-symbol alignment** — binary search maps timestamps across intervals/symbols
- **SpreadGroup decay** — rolling positions reduce notional via DecayFactor per roll

### Database Schema

| Table / View | Market | Content |
|--------------|--------|---------|
| `crypto_options_bar_1m` | Crypto | 1m option bars (mark/last/bid/ask OHLC, IV, greeks, OI) — partitioned by month |
| `crypto_spot_bar_1m` | Crypto | 1m underlying spot bars |
| `us_options_bar_1m` | US | 1m option bars with session metadata (market_date, session_kind, session_seq) |
| `us_stocks_bar_1m` | US | 1m stock bars with session metadata |
| `us_equity_sessions` | US | Session calendar (regular/pre/post times, DST, holidays, early closes) |
| `feature_volatility_snapshot_daily` | Both | Precomputed HV (10/20/30d), current IV, IV percentile/rank |
| `feature_term_structure_snapshot_daily` | US | ATM IV by expiration |
| `feature_skew_snapshot_daily` | US | OTM call/put IV skew |
| `feature_liquidity_snapshot_daily` | Both | Bid/ask spreads, relative spread, OI, tradability/activity ratio |
| `feature_daily_panel_daily` | Both | Merged daily feature panel (all above combined) |
| `fundamental_factor_catalog` | US stocks, Crypto spot | Factor control plane: metadata, preferred frequency, fill policy, SLA, source |
| `fundamental_observation` | US stocks, Crypto spot | Tall sparse symbol-bound observations with `event_ts`, `known_at`, revision, and point-in-time query semantics |
| Materialized K-line views | Both | 5m, 15m, 30m, 1h, 2h, 3h, 4h, 6h, 8h, 12h, 1d auto-aggregated from 1m |

### API Route Map

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/ready` | GET | Health / readiness check |
| `/api/v1/backtests/runs` | POST | Start an async strategy backtest run |
| `/api/v1/backtests/runs/{run_id}` | GET | Query async strategy backtest status/result |
| `/api/v1/backtests/runs/{run_id}/events` | GET | Subscribe to async strategy backtest progress via SSE |
| `/api/v1/infra/markets` | GET | List available markets and capabilities |
| `/api/v1/infra/datasets` | GET | Dataset row counts, freshness, summary aggregates |
| `/api/v1/crypto-options/bars` | GET | OHLCV bars (interval routing: 1m→base, 5m+→precomputed views) |
| `/api/v1/crypto-options/symbols` | GET | Contract metadata search |
| `/api/v1/crypto-options/greeks` | GET | Time-series greeks |
| `/api/v1/crypto-options/backtest` | POST | Run backtest from JSON |
| `/api/v1/markets/{market}/bars` | GET | Unified bars (us-stocks, us-options, etc.) |
| `/api/v1/markets/{market}/symbols` | GET | Unified symbol search |
| `/api/v1/features/volatility-snapshot` | GET | Latest HV/IV/percentile for a symbol |
| `/api/v1/features/volatility-history` | GET | Historical IV time-series |
| `/api/v1/features/term-structure-snapshot` | GET | ATM IV by expiration |
| `/api/v1/features/skew-snapshot` | GET | OTM call/put skew |
| `/api/v1/features/liquidity-snapshot` | GET | Bid/ask spreads, OI by expiration |
| `/api/v1/features/liquidity-history` | GET | Historical liquidity series |
| `/api/v1/features/event-window-snapshot` | GET | Early-close / holiday flags |
| `/api/v1/features/event-window-history` | GET | Historical market events |
| `/api/v1/features/daily-feature-panel` | GET | Merged daily features |
| `/api/v1/fundamentals/factors` | GET | List symbol-bound fundamental factors and metadata |
| `/api/v1/fundamentals/series` | GET | Query sparse, as-of, or filled point-in-time series for one symbol/factor |
| `/api/v1/fundamentals/snapshot` | GET | Latest known values for one symbol across many factors |
| `/api/v1/fundamentals/panel` | GET | Latest known values across many symbols/factors |
| `/api/v1/fundamentals/freshness` | GET | Latest `known_at` and SLA-based freshness per factor |

All list endpoints support cursor-based pagination (`cursor` / `next_cursor`). Supported intervals: `1m` through `1w`.

### Strategy Architecture

Strategies implement the `backtest.Strategy` interface (`Init` + `OnBar`) and are registered in `pkg/strategies/catalog`. Options strategies embed reusable mixins from `pkg/strategies/optutil`:

Toktik DSL documentation: [docs/dsl.md](docs/dsl.md)

- **PricingMixin** — entry/exit/valuation price modes for spread legs
- **GroupMixin** — position group lifecycle tracking
- **PendingRefCounter** — scheduled spread open resolution

Built-in strategies: `golden-cross`, `delta-filter`, `bull-put-spread`, `bear-call-spread`, `forum-short-put`, `lvol-scalper`, `ema-atr-spot`, `turtle-trend-simp`, `buy-flash-low`, `covered-call`, `retracement-ratio-protective-spread-long`, `retracement-ratio-protective-spread-short`.

## Infra Progress

- Phase 1: unified market API and infra dataset/market inspection — **implemented**.
- Phase 2: feature-store and fundamentals APIs — **in progress**; volatility, liquidity, event-window, daily panel, `us-options` surface features, and symbol-bound fundamentals endpoints are available.
- Phase 3: reference-data APIs — planned.
- Phase 4: production scheduling/run-state APIs — planned (readiness and freshness inspection already in place).

## Prerequisites

- **Go 1.26+**
- **ClickHouse** — running instance (default: `localhost:9000`)

## Build

```bash
# Build all binaries
make build-all

# Build individual tools
make build-api
make build-convert
make build-import
make build-kline-backfill
make build-kline-migrate-utc
make build-backtest-portfolio
make build-us-market-import
make build-feature-store-backfill

# Cross-compile
make build-win-arm
make build-darwin-amd64

# Clean
make clean
```

Binaries are output to `bin/`.

## Quick Start

### 1. Ingest Data

Convert raw tick CSV files (zstd-compressed) into Parquet:

```bash
bin/crypto-options-convert \
  --input-dir /path/to/csv-zst-files \
  --output-dir /path/to/parquet-output \
  --workers 4
```

Import Parquet files into ClickHouse:

```bash
bin/crypto-options-import \
  --input-dir /path/to/parquet-output \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default" \
  --batch-size 50000 \
  --workers 2
```

The importer automatically initializes the ClickHouse schema (tables + materialized views) and performs deduplication sampling to skip already-imported files.
Default generated windows are: `1m`, `5m`, `15m`, `30m`, `1h`, `2h`, `3h`, `4h`, `6h`, `8h`, `12h`, `1d`.

Import spot bars from a Julia-exported minute JSON plus a 1-hour CSV volume file:

```bash
go run ./cmd/crypto-spot-import-julia \
  --json-file btc2023_2025.json \
  --csv-file BTCUSDT_1h.csv \
  --symbol BTC \
  --json-time-offset=-8h \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default"
```

Notes:
- `crypto-spot-import-julia` uses JSON minute `OHLC` as the price source.
- Hourly volume comes from the CSV file and is redistributed to each minute using normalized JSON minute volume weights within the same hour.
- `--json-time-offset` is required when the JSON timestamps and CSV timestamps are expressed in different time bases. For the checked-in `btc2023_2025.json` plus `BTCUSDT_1h.csv` pair, use `--json-time-offset=-8h`.
- The imported `1m` spot bars feed the higher spot windows through ClickHouse aggregation tables and views.

### 2. Start the API Server

Shared runtime defaults now live in `toktik.yaml`:

```yaml
clickhouse:
  dsn: "clickhouse://default:@localhost:9000/default"

api_server:
  listen_addr: ":9010"

paths:
  schema_dir: "schema/clickhouse"
```

Flags still override the YAML values when you need a one-off run:

```bash
bin/api-server \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default" \
  --addr ":9010"
```

The server auto-detects `crypto_options.sql` from `paths.schema_dir`. Override with `--schema path/to/file.sql`.

Environment variable fallbacks:
- `CLICKHOUSE_DSN` — ClickHouse connection string
- `LISTEN_ADDR` — Server listen address
- `TOKTIK_SCHEMA_DIR` — Base directory used for schema autodiscovery

### 3. Query the API

**Check infra readiness:**
```bash
curl "http://localhost:9010/ready"
```

**List available low-level markets:**
```bash
curl "http://localhost:9010/api/v1/infra/markets"
```

Sample response:
```json
{
  "markets": [
    {
      "name": "crypto-options",
      "status": "available",
      "capabilities": ["bars", "symbols", "greeks", "backtest", "chain-cache"]
    },
    {
      "name": "feature-store",
      "status": "available",
      "capabilities": ["volatility-snapshots", "volatility-history", "term-structure-snapshots", "skew-snapshots", "liquidity-snapshots", "liquidity-history", "event-window-snapshots", "event-window-history", "daily-feature-panel", "backfill", "freshness-monitoring"]
    }
  ]
}
```

## Tiger API CLI

The repository includes a Tiger OpenAPI testing CLI at `cmd/tools/tigerapi` for ad-hoc US stock and option market-data checks.

Supported commands:
- `market-state`
- `stock-quote`
- `stock-kline`
- `stock-timeline`
- `stock-trade-tick`
- `stock-depth`
- `option-expirations`
- `option-chain`
- `option-quote`
- `option-kline`
- `raw`

Setup:

```yaml
tiger:
  tiger_id: "..."
  private_key: "..."
  account: "..."
  license: "..."
  environment: "PROD"
  token: ""
  token_file: ""
```

The `cmd/tools/tigerapi` CLI now reads Tiger settings from `toktik.yaml`. The old `tiger-env.sh` script remains only as a compatibility shim that exports `TOKTIK_CONFIG`.

Optional auth inputs for Tiger option endpoints, either in YAML or as env overrides:
- `TIGEROPEN_TOKEN` — sends the token as the HTTP `Authorization` header.
- `TIGEROPEN_TOKEN_FILE` — optional path to a `token=...` properties file.
- If `TIGEROPEN_TOKEN` is unset and `TIGEROPEN_TOKEN_FILE` is unset, `pkg/tigerapi` will also try the SDK default file name `tiger_openapi_token.properties` in the current working directory.

Examples:

```bash
go run ./cmd/tools/tigerapi -- market-state --market US
go run ./cmd/tools/tigerapi -- stock-kline --symbol AAPL --period day
go run ./cmd/tools/tigerapi -- option-expirations --symbol AAPL
go run ./cmd/tools/tigerapi -- raw --method option_expiration --biz-content '{"symbols":["AAPL"]}'
```

Current live validation status with the checked-in runtime config:
- `market-state` works.
- `stock-kline` works.
- `option-expirations` works.
- `stock-quote` currently fails with Tiger permission denied for US real-time quotes on the current account/device.
- `option-chain`, `option-quote`, and `option-kline` may require additional Tiger token/device provisioning. In live testing, `option_chain` returned `device_id cannot be empty` from the upstream API. The runtime config includes `tiger.device_id` so the value is no longer stranded in a shell script, but the current SDK wrapper still has no explicit `device_id` request option.

## Massive REST Client

The repository includes a Massive (Polygon) REST wrapper in `pkg/polygon` for stock and option market data plus history.

The repository also includes a CLI at `cmd/tools/polygon` for ad-hoc Massive REST queries.

Setup:

```yaml
polygon:
  api_key: "..."
  base_url: "https://api.massive.com"
  flat_files_base_url: "https://files.massive.com/flatfiles"
  flat_files_tool: "mc" # or "rclone"
  flat_files_cache_dir: "tmp/polygon"
  flat_files_access_key: "..."
  flat_files_secret_key: "..."
  timeout_seconds: 60
  trace: false
  pagination: true
```

The `cmd/tools/polygon` CLI now reads Polygon settings from `toktik.yaml`. The old `polygon-env.sh` script remains only as a compatibility shim that exports `TOKTIK_CONFIG`.

Implemented package methods cover:
- Stock snapshot, quotes, trades, and aggregate history
- Option contract lookup, chain snapshot, quotes, trades, and aggregate history
- Stock minute aggregate flatfile download from `us_stocks_sip/minute_aggs_v1/<year>/<month>/<date>.csv.gz`
- Option minute aggregate flatfile download from `us_options_opra/minute_aggs_v1/<year>/<month>/<date>.csv.gz`

Supported CLI commands:
- `stock-minute-flatfile`
- `stock-snapshot`
- `stock-aggregates`
- `stock-quotes`
- `stock-trades`
- `option-minute-flatfile`
- `option-contract`
- `option-chain`
- `option-aggregates`
- `option-quotes`
- `option-trades`

Examples:

```bash
go run ./cmd/tools/polygon -- stock-snapshot --symbol AAPL
go run ./cmd/tools/polygon -- stock-minute-flatfile --date 2026-04-07
go run ./cmd/tools/polygon -- stock-aggregates --ticker AAPL --multiplier 1 --timespan minute --from 2025-11-03 --to 2025-11-28
go run ./cmd/tools/polygon -- option-chain --underlying SPY --expiration-date 2025-12-19 --contract-type call
go run ./cmd/tools/polygon -- option-minute-flatfile --date 2026-04-07
go run ./cmd/tools/polygon -- option-trades --ticker O:SPY251219C00650000 --limit 10
```

Flatfile download example from Go:

```go
client, err := polygon.NewFromEnv()
if err != nil {
  log.Fatal(err)
}

stockPath, err := client.DownloadStockMinuteAggregates(time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false)
if err != nil {
  log.Fatal(err)
}

optionPath, err := client.DownloadOptionMinuteAggregates(time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false)
if err != nil {
  log.Fatal(err)
}

fmt.Println(stockPath)
fmt.Println(optionPath)
```

**Inspect dataset freshness and summary:**
```bash
curl "http://localhost:9010/api/v1/infra/datasets?market=feature-store"
```

Sample response:
```json
{
  "summary": {
    "total": 5,
    "ready": 5,
    "stale": 0,
    "missing": 0,
    "empty": 0,
    "markets": [
      {
        "market": "feature-store",
        "total": 5,
        "ready": 5,
        "stale": 0,
        "missing": 0,
        "empty": 0
      }
    ]
  },
  "datasets": [
    {
      "name": "feature-volatility-snapshots",
      "market": "feature-store",
      "relation": "feature_volatility_snapshot_daily",
      "status": "ready",
      "freshness": "fresh"
    },
    {
      "name": "feature-daily-panels",
      "market": "feature-store",
      "relation": "feature_daily_panel_daily",
      "status": "ready",
      "freshness": "fresh"
    }
  ]
}
```

**List symbols:**
```bash
curl "http://localhost:9010/api/v1/crypto-options/symbols?base_asset=BTC&limit=10"
```

**List US option symbols from the unified market namespace:**
```bash
curl "http://localhost:9010/api/v1/markets/us-options/symbols?underlying=SPY&limit=10"
```

**Get OHLCV bars:**
```bash
curl "http://localhost:9010/api/v1/crypto-options/bars?symbol=BTC-28MAR25-100000-C&interval=1h&from=2025-01-01&to=2025-03-01&limit=500"
```

**Get US stock bars from the unified market namespace:**
```bash
curl "http://localhost:9010/api/v1/markets/us-stocks/bars?symbol=AAPL&interval=1h&from=2025-01-01&to=2025-03-01&limit=500"
```

**Get greeks time series:**
```bash
curl "http://localhost:9010/api/v1/crypto-options/greeks?symbol=BTC-28MAR25-100000-C&from=2025-01-01&to=2025-03-01"
```

**Get a volatility feature snapshot:**
```bash
curl "http://localhost:9010/api/v1/features/volatility-snapshot?market=us-options&underlying=SPY"
```

Sample response:
```json
{
  "market": "us-options",
  "underlying": "SPY",
  "lookback_days": 252,
  "price_as_of": "2026-04-02T00:00:00Z",
  "iv_as_of": "2026-04-02T00:00:00Z",
  "price_observations": 252,
  "iv_observations": 252,
  "hv10": 0.1842,
  "hv20": 0.2015,
  "hv30": 0.2168,
  "current_iv": 0.2331,
  "iv_percentile": 64.7,
  "iv_rank": 58.4
}
```

**Get volatility feature history:**
```bash
curl "http://localhost:9010/api/v1/features/volatility-history?market=crypto-options&underlying=BTC&from=2025-01-01&to=2025-03-01"
```

**Get a term-structure snapshot:**
```bash
curl "http://localhost:9010/api/v1/features/term-structure-snapshot?market=us-options&underlying=SPY&min_days_to_expiry=7&max_days_to_expiry=90"
```

Sample response:
```json
{
  "market": "us-options",
  "underlying": "SPY",
  "as_of": "2026-04-02T00:00:00Z",
  "data": [
    {
      "expiration": "2026-04-17T00:00:00Z",
      "days_to_expiry": 15,
      "atm_iv": 0.221,
      "call_iv": 0.214,
      "put_iv": 0.229,
      "contract_count": 42
    }
  ]
}
```

**Get a skew snapshot:**
```bash
curl "http://localhost:9010/api/v1/features/skew-snapshot?market=us-options&underlying=SPY&min_days_to_expiry=7&max_days_to_expiry=90"
```

Sample response:
```json
{
  "market": "us-options",
  "underlying": "SPY",
  "as_of": "2026-04-02T00:00:00Z",
  "data": [
    {
      "expiration": "2026-04-17T00:00:00Z",
      "days_to_expiry": 15,
      "otm_call_iv": 0.198,
      "otm_put_iv": 0.274,
      "put_call_skew": 0.076,
      "contract_count": 42
    }
  ]
}
```

**Get a crypto-options liquidity snapshot:**
```bash
curl "http://localhost:9010/api/v1/features/liquidity-snapshot?market=crypto-options&underlying=BTC&min_days_to_expiry=7&max_days_to_expiry=60"
```

Sample response:
```json
{
  "market": "crypto-options",
  "underlying": "BTC",
  "as_of": "2026-04-02T00:00:00Z",
  "data": [
    {
      "expiration": "2026-04-26T08:00:00Z",
      "days_to_expiry": 24,
      "avg_bid_close": 12.5,
      "avg_ask_close": 13.0,
      "avg_mark_close": 12.75,
      "relative_spread": 0.0392,
      "open_interest": 1520,
      "tick_count": 77,
      "contract_count": 12,
      "tradable_contract_count": 9,
      "tradability_ratio": 0.75
    }
  ]
}
```

**Get a liquidity history window:**
```bash
curl "http://localhost:9010/api/v1/features/liquidity-history?market=us-options&underlying=AAPL&from=2026-03-01&to=2026-04-01&min_days_to_expiry=7&max_days_to_expiry=60"
```

Sample response:
```json
{
  "market": "us-options",
  "underlying": "AAPL",
  "data": [
    {
      "as_of_date": "2026-03-31T00:00:00Z",
      "expiration": "2026-04-17T00:00:00Z",
      "days_to_expiry": 17,
      "avg_mark_close": 4.21,
      "volume": 12840,
      "transactions": 922,
      "contract_count": 36,
      "active_contract_count": 23,
      "activity_ratio": 0.6389
    }
  ]
}
```

**Get an event-window snapshot:**
```bash
curl "http://localhost:9010/api/v1/features/event-window-snapshot?market=us-options&underlying=AAPL"
```

Sample response:
```json
{
  "market": "us-options",
  "underlying": "AAPL",
  "as_of_date": "2026-04-02T00:00:00Z",
  "is_early_close": false,
  "previous_holiday_date": "2026-02-16T00:00:00Z",
  "next_holiday_date": "2026-05-25T00:00:00Z",
  "days_from_prev_holiday": 45,
  "days_to_next_holiday": 53
}
```

**Get event-window history:**
```bash
curl "http://localhost:9010/api/v1/features/event-window-history?market=us-options&underlying=AAPL&from=2026-03-01&to=2026-04-01"
```

Sample response:
```json
{
  "market": "us-options",
  "underlying": "AAPL",
  "data": [
    {
      "date": "2026-03-31T00:00:00Z",
      "market": "us-options",
      "underlying": "AAPL",
      "as_of_date": "2026-03-31T00:00:00Z",
      "is_early_close": false,
      "previous_holiday_date": "2026-02-16T00:00:00Z",
      "next_holiday_date": "2026-05-25T00:00:00Z",
      "days_from_prev_holiday": 43,
      "days_to_next_holiday": 55
    }
  ]
}
```

### 4. Run Strategy Backtests via API

The strategy backtest API is asynchronous. Submit a run first, then poll the run status, subscribe to its SSE event stream, or open the reserved HTML report URL.

Supported request fields mirror the `backtest-portfolio` CLI where practical, including:
- `market`: `crypto` or `us`
- `instrument`: `auto`, `spot`, `contract`, or `mixed`
- `asset`, `interval`, `from`, `to`, `capital`, `strategy`
- spread pricing fields such as `spread_entry_price_mode`, `spread_exit_price_mode`, `spread_valuation_price_mode`
- strategy tuning fields such as `direction`, `position_size`, `target_expiry_days`, `short_delta_min`, and `long_delta_max`
- `signal_source`: per-request external signal source override for strategies that support it

Completed runs return HTML report file paths only, not inline HTML content.

The initial `POST /api/v1/backtests/runs` response also includes `report_url`, which points to a stable browser-ready endpoint. Before the run finishes, that URL returns `202` with the current run status. After completion, the same URL returns the generated HTML report directly. For multi-strategy runs, the API also exposes `/api/v1/backtests/runs/{run_id}/reports/overview` and `/api/v1/backtests/runs/{run_id}/reports/{n}` for the overview page and per-strategy detail pages.

**Start an async backtest run:**
```bash
curl -X POST http://localhost:9010/api/v1/backtests/runs \
  -H 'Content-Type: application/json' \
  -d '{
    "market": "crypto",
    "instrument": "contract",
    "asset": "BTC",
    "from": "2023-01-01",
    "to": "2025-12-31",
    "strategy": "retracement-ratio-protective-spread-long",
    "interval": "2h",
    "capital": 100,
    "signal_source": "12h",
    "spread_entry_price_mode": "mark_close",
    "spread_exit_price_mode": "mark_close",
    "spread_valuation_price_mode": "mark_close",
    "direction": "long_only"
  }'
```

Sample response:
```json
{
  "run_id": "c40505f1a16f02f33380b4ccbe4f74db",
  "status": "queued",
  "created_at": "2026-04-07T09:45:08.974090366Z",
  "status_url": "/api/v1/backtests/runs/c40505f1a16f02f33380b4ccbe4f74db",
  "events_url": "/api/v1/backtests/runs/c40505f1a16f02f33380b4ccbe4f74db/events",
  "report_url": "/api/v1/backtests/runs/c40505f1a16f02f33380b4ccbe4f74db/report"
}
```

Open the `report_url` in a browser once the run reaches `completed`; the endpoint will serve the generated HTML directly.

This request is equivalent to the CLI command:
```bash
SIGNAL_LEVEL=12h go run ./cmd/backtest-portfolio/main.go \
  --asset BTC \
  --from 2023-01-01 \
  --to 2025-12-31 \
  --strategy retracement-ratio-protective-spread-long \
  --interval 2h \
  --capital 100 \
  --spread-entry-price-mode mark_close \
  --spread-exit-price-mode mark_close \
  --spread-valuation-price-mode mark_close \
  --direction long_only \
  --clear-previous-data
```

**Run the weighted US wheel put-writer DSL example:**

The repository now includes a runnable multi-symbol options DSL example plus a matching API payload:

- `docs/examples/wheel-portfolio-us-sell-put.dsl`
- `docs/examples/wheel-portfolio-us-sell-put.run.json`

This example targets `QQQ 20% + GLD 10% + MSFT 15% + AAPL 10% + TSLA 30% + TQQQ 15%` and uses the current multi-symbol options runtime to rotate short puts per symbol. It models the put-writing leg of the wheel. Full assignment plus covered-call rotation is not modeled yet in the DSL/runtime, so the example intentionally stops at rolling cash-secured puts.

Use the payload directly:

```bash
curl -X POST http://localhost:9010/api/v1/backtests/runs \
  -H 'Content-Type: application/json' \
  -d @docs/examples/wheel-portfolio-us-sell-put.run.json
```

Important detail: the example uses request-level `symbols` + `weights` so the service can preload every required option chain and inject `portfolio.items()` / `portfolio.weights()` into the DSL runtime.

策略运行方式：
- `SIGNAL_LEVEL` 控制读取哪组信号文件，支持 `12h` 和 `1d`。
- `retracement-ratio-protective-spread-long` 固定读取 `12h_long.csv` / `1d_long.csv`。
- `retracement-ratio-protective-spread-short` 固定读取 `12h_short.csv` / `1d_short.csv`。
- 策略会在信号出现时先关闭所有已存在的 order group，再按初始逻辑重新开仓。
- 30% 盈利时会部分平掉卖方腿，并在同一个 order group 中重建买方腿，方便回测结果在 HTML 中按组展示。
- 50% 盈利时会全部平仓，然后进入第二阶段趋势追击逻辑。

如果你只想先跑多头版本，推荐先用下面这组参数：

```bash
SIGNAL_LEVEL=12h go run ./cmd/backtest-portfolio/main.go \
  --asset BTC \
  --from 2023-01-01 \
  --to 2025-12-31 \
  --strategy retracement-ratio-protective-spread-long \
  --interval 2h \
  --capital 100 \
  --spread-entry-price-mode mark_close \
  --spread-exit-price-mode mark_close \
  --spread-valuation-price-mode mark_close \
  --direction long_only \
  --clear-previous-data
```

When `--clear-previous-data` is set, `backtest-portfolio` removes existing `.csv` and `.json` files under `reports/backtests/` before writing fresh outputs.

**Poll run status:**
```bash
curl "http://localhost:9010/api/v1/backtests/runs/c40505f1a16f02f33380b4ccbe4f74db"
```

Sample terminal result excerpt:
```json
{
  "run_id": "c40505f1a16f02f33380b4ccbe4f74db",
  "status": "completed",
  "progress": {
    "phase": "replay",
    "current": 216,
    "total": 216,
    "percent": 100,
    "completed": true
  },
  "result": {
    "summaries": [
      {
        "strategy_name": "RetracementRatioProtectiveSpreadLong",
        "html_path": "reports/backtests/api/c40505f1a16f02f33380b4ccbe4f74db/retracement-ratio-protective-spread-long_btc_2h_20230101_20251231.html"
      }
    ]
  }
}
```

**Subscribe to SSE progress updates:**
```bash
curl -N "http://localhost:9010/api/v1/backtests/runs/c40505f1a16f02f33380b4ccbe4f74db/events"
```

SSE emits at most 1 message per second and uses event names such as `status`, `progress`, `completed`, and `failed`.

Example SSE payload:
```text
event: completed
data: {"run_id":"c40505f1a16f02f33380b4ccbe4f74db","status":"completed",...}
```

Notes:
- If a strategy supports request-level `signal_source`, the request value takes precedence.
- If `signal_source` is omitted, compatible strategies may still fall back to their historical environment variables.
- Multi-strategy runs return one `html_path` per strategy plus `overview_html_path` for the combined overview page.

**Get a merged daily feature panel:**
```bash
curl "http://localhost:9010/api/v1/features/daily-feature-panel?market=us-options&underlying=AAPL&from=2026-03-01&to=2026-04-01&min_days_to_expiry=7&max_days_to_expiry=60"
```

Sample response:
```json
{
  "market": "us-options",
  "underlying": "AAPL",
  "lookback_days": 252,
  "data": [
    {
      "date": "2026-03-31T00:00:00Z",
      "price_observations": 252,
      "iv_observations": 252,
      "hv20": 0.2143,
      "current_iv": 0.2384,
      "iv_percentile": 66.1,
      "front_expiration": "2026-04-17T00:00:00Z",
      "front_days_to_expiry": 17,
      "front_atm_iv": 0.2291,
      "front_put_call_skew": 0.0714,
      "surface_contract_count": 36,
      "liquidity_volume": 12840,
      "liquidity_transactions": 922,
      "liquidity_contract_count": 36,
      "liquidity_active_contract_count": 23,
      "liquidity_activity_ratio": 0.6389,
      "is_early_close": false,
      "days_from_prev_holiday": 43,
      "days_to_next_holiday": 55
    }
  ]
}
```

**Run a backtest:**
```bash
curl -X POST http://localhost:9010/api/v1/crypto-options/backtest \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "BTC-28MAR25-100000-C",
    "interval": "1h",
    "from": "2025-01-01",
    "to": "2025-03-01",
    "capital": 1.0,
    "strategy": "golden-cross",
    "params": {"fast_period": 10, "slow_period": 50}
  }'
```

All list endpoints support cursor-based pagination via the `cursor` query parameter. The response includes `next_cursor` when more data is available.

Supported intervals: `1m`, `2m`, `3m`, `5m`, `10m`, `15m`, `30m`, `1h`, `2h`, `4h`, `6h`, `8h`, `12h`, `1d`, `1w`.

### 4. Run Backtests from CLI

**Simple golden-cross strategy:**
```bash
bin/backtest-example \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --symbol "BTC-28MAR25-100000-C" \
  --interval 1h \
  --from 2025-01-01 \
  --to 2025-03-01 \
  --strategy golden-cross \
  --capital 1.0
```

**Crypto option-contract portfolio backtest:**
```bash
bin/backtest-portfolio \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --market crypto \
  --instrument contract \
  --asset BTC \
  --interval 1h \
  --from 2025-01-01 \
  --to 2025-03-01 \
  --strategy bull-put-spread \
  --capital 1.0 \
  --position-size 1 \
  --max-hold-hours 48 \
  --target-expiry-days 15 \
  --short-delta-min 0.4 --short-delta-max 0.5 \
  --long-delta-min 0.1 --long-delta-max 0.15 \
  --html-output report.html
```

`--capital` is interpreted per strategy profile:
- Regular-only strategies use `USD`.
- Crypto option-contract strategies, or strategies whose spot leg is only a signal-sized sidecar, use the underlying asset unit such as `BTC`.
- US option-contract strategies use `USD`.
- Multi-strategy runs emit one overview HTML plus one detail page per strategy so mixed denomination runs remain readable.

`--instrument` controls which strategy class is allowed:
- `auto` infers from the resolved strategy set.
- `spot` only allows regular stock/spot strategies.
- `contract` only allows option-contract strategies.
- `mixed` allows both in one run.

**US stock spot backtest:**
```bash
bin/backtest-portfolio \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --market us \
  --instrument spot \
  --asset AAPL \
  --interval 1h \
  --from 2025-01-01 \
  --to 2025-03-01 \
  --strategy golden-cross \
  --capital 100000 \
  --html-output report.html
```

**Crypto forum-style short put strategy:**
```bash
bin/backtest-portfolio \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --market crypto \
  --instrument contract \
  --asset BTC \
  --interval 1h \
  --from 2025-01-01 \
  --to 2025-03-01 \
  --strategy forum-short-put \
  --capital 1.0 \
  --html-output report.html
```

Available strategies: `golden-cross`, `delta-filter`, `bull-put-spread` (alias `ma-deviation-bull`), `bear-call-spread` (alias `ma-deviation-bear`), `forum-short-put` (alias `ma-deviation-forum`), `both`.

### 5. Find Missing Data

Scan ClickHouse for date gaps:

```bash
bin/market-missing-days \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --market crypto-options \
  --asset BTC \
  --from 2025-01-01 \
  --to 2025-03-01
```

Supported datasets: `crypto-options`, `us-stocks`, `us-options`.

### 6. Backfill Missing K-line Window Tables

Manually generate precomputed K-line windows from 1-minute base tables:

```bash
bin/options-kline-backfill \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --market crypto \
  --intervals "1d" \
  --base-asset BTC \
  --from 2025-01-01 \
  --to 2025-03-01
```

Notes:
- Command renamed to `options-kline-backfill`, supporting `--market crypto|us`.
- Crypto chain cache is generated for the same precomputed windows as crypto option K-lines (`5m,15m,30m,1h,2h,3h,4h,6h,8h,12h,1d`). A `1m` chain request still falls back to raw bar snapshots.
- To build matching crypto chain caches for backtests, include the target interval in `--intervals`, for example `--intervals "1h,2h,1d"`.
- Without `--replace`, intervals with existing rows in the selected scope are skipped to avoid duplicate aggregation states.
- Add `--replace` to rebuild selected intervals in-range.
- After re-importing spot `1m` bars, use `--replace` if you need to overwrite existing higher spot windows from the new base data.

### 6.5. Migrate K-line Tables to Explicit UTC

If your ClickHouse K-line tables were created before the UTC alignment fix, migrate them with:

```bash
bin/crypto-options-kline-migrate-utc \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --intervals "1m,5m,15m,30m,1h,2h,3h,4h,6h,8h,12h,1d" \
  --base-asset BTC \
  --from 2023-01-01 \
  --to 2025-12-31
```

Notes:
- The tool drops and recreates all precomputed crypto K-line aggregate tables, materialized views, and query views.
- It alters `crypto_options_bar_1m.timestamp` and `crypto_spot_bar_1m.timestamp` to `DateTime('UTC')`.
- By default it backfills the selected higher intervals from the 1-minute base tables after recreating the schema.
- Use `--skip-backfill` if you only want the schema migration and will repopulate aggregates later.
- Use `--dry-run` to inspect the migration plan before executing it.

### 7. Import US Market Flatfiles

```bash
make build-us-market-import

bin/us-market-import \
  --options-dir /path/to/polygon/options \
  --stocks-dir /path/to/polygon/stocks \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --workers 2 \
  --batch-size 100000
```

Or run directly without building:

```bash
go run ./cmd/us-market-import \
  --options-dir /path/to/polygon/options \
  --stocks-dir /path/to/polygon/stocks \
  --clickhouse-dsn "clickhouse://localhost:9000/default"
```

Notes:
- At least one of `--options-dir` or `--stocks-dir` is required.
- Input files must be named by trading date, for example `2025-01-02.csv` or `2025-01-02.csv.gz`.
- The schema file is auto-detected from `schema/clickhouse/us_market.sql`; override with `--schema path/to/file.sql` if needed.
- Existing dates are skipped by default; pass `--skip-existing=false` to force re-import.

### 7.5. Recalculate Missing US Option Greeks Locally

When imported US option `1m` rows are missing Greeks, use the local backfill command to recompute them from the corresponding stock minute bars already stored in ClickHouse:

```bash
go run ./cmd/us-market-greeks-backfill \
  --start-date 2023-01-03 \
  --end-date 2025-12-31 \
  --symbols "SPY,QQQ,IWM,DIA,AAPL,TSLA" \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default" \
  --workers 4 \
  --batch-size 50000
```

Use `--dry-run` first to inspect matching without writing:

```bash
go run ./cmd/us-market-greeks-backfill \
  --date 2025-12-23 \
  --symbols "DJX" \
  --dry-run
```

Notes:
- The command only scans rows that are still missing one or more of `underlying_close`, `implied_volatility`, `delta`, `gamma`, `vega`, `theta`, or `rho`.
- Each missing row is recomputed from the latest available underlying close at or before that option-bar timestamp on the same market date.
- The command does not call any external greek vendor; if the required underlying minute bars do not exist locally, the task remains unresolved and is reported in the summary.
- For index options such as `SPX` or `NDX`, local backfill only works if you also ingest a matching local underlying minute series into `us_stocks_bar_1m`.
- `cmd/us-market-flatfiles-sync` now runs this recalculation automatically for the downloaded date range after import; pass `--skip-greeks-backfill` only if you explicitly want to suppress it.

### 8. Backfill Feature-Store Snapshots

```bash
make build-feature-store-backfill

bin/feature-store-backfill \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default" \
  --markets "crypto-options,us-options" \
  --min-days-to-expiry 0 \
  --max-days-to-expiry 365 \
  --incremental-days 7
```

To rebuild an explicit historical range:

```bash
bin/feature-store-backfill \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default" \
  --markets "us-options" \
  --underlyings "SPY,QQQ" \
  --from 2025-01-01 \
  --to 2025-03-31 \
  --replace
```

Notes:
- This command writes all currently supported feature-store snapshot tables.
- Volatility snapshots are written for `crypto-options` and `us-options`.
- Term-structure and skew snapshots are written for `us-options`.
- Liquidity snapshots are written for both `crypto-options` and `us-options`; when US daily bars include bid/ask closes, quote-based tradability metrics are materialized alongside activity metrics.
- Daily feature panels are written for both `crypto-options` and `us-options` using the requested `lookback_days` and DTE bounds as materialization keys.
- Summary output includes counts for written, skipped, empty, replaced, and failed scopes.
- When `ContinueOnError` is active, per-scope failure lines are printed to stderr before the final summary.

## Writing Custom Strategies

Implement the `backtest.Strategy` interface:

```go
type MyStrategy struct{}

func (s *MyStrategy) Name() string { return "MyStrategy" }

func (s *MyStrategy) Init(ctx *backtest.SetupContext) error {
    // Register indicators on the primary security
    ctx.Register("sma20", backtest.SMA("close", 20))
    ctx.Register("rsi14", backtest.RSI("close", 14))

    // Optionally add cross-symbol data
    // btcSpot := ctx.AddSecurity("crypto-options", "BTC-SPOT", "1h")
    // ctx.RegisterOn(btcSpot, "sma50", backtest.SMA("close", 50))

    return nil
}

func (s *MyStrategy) OnBar(ctx *backtest.BarContext) {
    ref := ctx.PrimaryRef()
    sma := ctx.Ind("sma20")
    price := ctx.Close()

    if price > sma && ctx.Position(ref) == 0 {
        qty := ctx.Equity() * 0.5 / price
        ctx.Buy(ref, qty)
    }
    if price < sma && ctx.Position(ref) > 0 {
        ctx.ClosePosition(ref)
    }
}
```

Built-in indicators: `SMA`, `EMA`, `RSI`, `MACD`, `Crossover`, `Crossunder`, `Highest`, `Lowest`, `Custom`.

Order types: `Buy`, `Sell`, `BuyTWAP`, `ClosePosition`, plus direct `Broker` access for limit/stop orders.

## Project Structure

```
cmd/
  api-server/                  REST API server (Gin, ClickHouse)
  backtest-portfolio/          Multi-strategy portfolio backtester (crypto + US, spot + options)
  backtest-example/            Minimal strategy demo runner
  crypto-options-convert/      CSV.zst tick files → Parquet converter
  crypto-options-import/       Parquet → ClickHouse importer (auto-DDL + dedup)
  market-missing-days/          Data gap scanner
  crypto-options-kline-migrate-utc/ K-line timezone migration utility
  crypto-spot-import-julia/    Julia-exported JSON+CSV spot → ClickHouse
  crypto-spot-import-15m/      Binance 15m spot CSV → ClickHouse
  deribit/dvol/                DVOL index tooling
  feature-store-backfill/      Precompute volatility/liquidity/panel snapshots
  options-kline-backfill/      Backfill higher K-line windows from 1m base
  us-market-import/            Polygon OPRA/SIP flatfile importer (session-aware)
  us-market-greeks-backfill/   Local missing-greeks recalculation for US options
internal/
  api/           Gin HTTP handlers & router
  backtest/      Core engine: Strategy, Engine, Broker, Indicator DAG, DataSet, OptionsChain, SpreadGroup
  cli/           CLI bootstrap (env fallback, DSN, schema init)
  cryptooptions/ Crypto options domain: CSV/Parquet I/O, aggregation, chain cache, queries
  csvutil/       Generic CSV parsing utilities
  datafeed/      DataFeed adapters (ClickHouse → columnar DataSet)
  dto/           API request/response types + validation
  optimization/  Grid search, walk-forward optimization
  report/        Self-contained HTML report generation
  service/       Business logic layer (DTO-driven, no direct DB from handlers)
  usmarket/      US market domain: Polygon import, session calendar, Black-Scholes greeks
  validation/    Numeric sanity checks (NaN, Inf)
pkg/
  feeds/         External data source interface + registry (Feed, Registry)
  strategies/    Strategy catalog + per-strategy implementations
    catalog/     Registry, config parsing, direction/capital modes
    optutil/     Shared options mixins (PricingMixin, GroupMixin, PendingRefCounter)
    helpers/     Common cross-strategy utilities
schema/
  clickhouse/    DDL scripts (crypto_options.sql, us_market.sql, feature_store.sql)
data/
  crypto-15m/    Sample 15m spot CSVs (Binance pairs)
docs/            Design docs, roadmap, strategy PRDs
reports/
  backtests/     Generated HTML backtest reports
```

## Development Notes

### Adding a New Strategy

1. Create a directory under `pkg/strategies/` (e.g., `pkg/strategies/my_strat/`).
2. Implement `backtest.Strategy` (`Name`, `Init`, `OnBar`).
3. For options strategies, embed `optutil.PricingMixin` and `optutil.GroupMixin` to avoid boilerplate.
4. Register the strategy in `pkg/strategies/catalog/` with a Config struct.
5. See `docs/strategy-reuse.md` for current reuse patterns and reference.

### Adding a New Market

Each market needs:
- A ClickHouse schema under `schema/clickhouse/` (1m base table + materialized K-line views).
- A domain package under `internal/` (parser, aggregator, queries).
- A `DataFeed` adapter in `internal/datafeed/`.
- A service in `internal/service/` and routes in `internal/api/`.

### Key Conventions

- **DTO boundary** — all validation and time parsing happens in the DTO layer; services receive clean typed structs.
- **Interval routing** — 1m queries hit the base table; 5m+ queries hit precomputed materialized views; ad-hoc intervals are computed at query time.
- **Session-aware aggregation (US)** — every US bar carries `market_date`, `session_kind`, `session_seq` to prevent cross-session blending.
- **Cursor pagination** — time-series endpoints use RFC 3339 timestamps as cursors.
- **No lookahead** — the backtest engine enforces next-bar execution; indicators are preflight-computed.

### Testing

```bash
go test ./...
```

Key test files:
- `cmd/backtest-portfolio/main_test.go` — portfolio backtester integration tests
- `cmd/crypto-spot-import-*/main_test.go` — spot import tests
- `cmd/feature-store-backfill/main_test.go` — feature store tests
- `internal/usmarket/session_test.go` — session calendar / DST / holiday tests

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLICKHOUSE_DSN` | `clickhouse://default:@localhost:9000/default` | ClickHouse connection string |
| `LISTEN_ADDR` | `:9010` | API server listen address |

## License

Private — all rights reserved.
