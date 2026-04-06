# toktik

Quantitative backtesting and market data platform for crypto options trading, built in Go.

## Features

- **Market data pipeline** — Convert zstd-compressed CSV tick data → Parquet → ClickHouse OHLCV bars with pre-computed materialized views (5m, 15m, 30m, 1h, 2h, 3h, 4h, 6h, 8h, 12h, 1d)
- **Event-driven backtesting engine** — Pine Script-style strategy interface with multi-asset, multi-leg options support, vectorized indicator computation, and realistic broker simulation (slippage, commissions, TWAP)
- **Unified infra API** — Query market data through shared `/api/v1/markets/{market}` routes while preserving legacy `/api/v1/crypto-options/*` compatibility
- **Infra observability** — Inspect readiness, market catalog, dataset row counts, latest timestamps, freshness, and dataset summary aggregates
- **Feature-store APIs** — Read volatility snapshots/history, `us-options` term-structure/skew, cross-market liquidity history, and merged daily feature panels from explicit infra endpoints, with precomputed-read preference where available
- **Event-window APIs** — Read early-close and holiday-proximity flags as both latest snapshots and daily history from the US market-session calendar
- **Options spread strategies** — Built-in bull put / bear call spread strategies with MA deviation signals
- **Strategy reuse helpers** — Shared `pkg/strategies/optutil` mixins and helpers for options strategy development
- **US market flatfile import** — Import Polygon US options and stocks minute CSV flatfiles into ClickHouse with precomputed K-line views
- **Feature-store backfill CLI** — Precompute supported daily feature snapshots and merged daily panels with incremental refresh, replacement, and failure reporting
- **HTML reports** — Self-contained backtest reports with equity curves, drawdown charts, and trade markers

## Infra Progress

The low-level infra roadmap is tracked in `docs/openspec/`.

- Phase 1: unified market API and infra dataset/market inspection are implemented.
- Phase 2: feature-store APIs are in progress; volatility, liquidity, event-window history, daily panel, and `us-options` surface features are available.
- Phase 3: reference-data APIs are planned.
- Phase 4: production scheduling/run-state APIs are planned, with readiness and freshness inspection already in place.

## Strategy Development

When adding a new options strategy, start from the shared helpers in `pkg/strategies/optutil` before writing local boilerplate.
See `docs/strategy-reuse.md` for the current reuse patterns and reference strategies.

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

```bash
bin/api-server \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default" \
  --addr ":8080"
```

The server auto-detects `schema/clickhouse/crypto_options.sql` relative to the working directory. Override with `--schema path/to/file.sql`.

Environment variable fallbacks:
- `CLICKHOUSE_DSN` — ClickHouse connection string
- `LISTEN_ADDR` — Server listen address

### 3. Query the API

**Check infra readiness:**
```bash
curl "http://localhost:8080/ready"
```

**List available low-level markets:**
```bash
curl "http://localhost:8080/api/v1/infra/markets"
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

**Inspect dataset freshness and summary:**
```bash
curl "http://localhost:8080/api/v1/infra/datasets?market=feature-store"
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
curl "http://localhost:8080/api/v1/crypto-options/symbols?base_asset=BTC&limit=10"
```

**List US option symbols from the unified market namespace:**
```bash
curl "http://localhost:8080/api/v1/markets/us-options/symbols?underlying=SPY&limit=10"
```

**Get OHLCV bars:**
```bash
curl "http://localhost:8080/api/v1/crypto-options/bars?symbol=BTC-28MAR25-100000-C&interval=1h&from=2025-01-01&to=2025-03-01&limit=500"
```

**Get US stock bars from the unified market namespace:**
```bash
curl "http://localhost:8080/api/v1/markets/us-stocks/bars?symbol=AAPL&interval=1h&from=2025-01-01&to=2025-03-01&limit=500"
```

**Get greeks time series:**
```bash
curl "http://localhost:8080/api/v1/crypto-options/greeks?symbol=BTC-28MAR25-100000-C&from=2025-01-01&to=2025-03-01"
```

**Get a volatility feature snapshot:**
```bash
curl "http://localhost:8080/api/v1/features/volatility-snapshot?market=us-options&underlying=SPY"
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
curl "http://localhost:8080/api/v1/features/volatility-history?market=crypto-options&underlying=BTC&from=2025-01-01&to=2025-03-01"
```

**Get a term-structure snapshot:**
```bash
curl "http://localhost:8080/api/v1/features/term-structure-snapshot?market=us-options&underlying=SPY&min_days_to_expiry=7&max_days_to_expiry=90"
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
curl "http://localhost:8080/api/v1/features/skew-snapshot?market=us-options&underlying=SPY&min_days_to_expiry=7&max_days_to_expiry=90"
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
curl "http://localhost:8080/api/v1/features/liquidity-snapshot?market=crypto-options&underlying=BTC&min_days_to_expiry=7&max_days_to_expiry=60"
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
curl "http://localhost:8080/api/v1/features/liquidity-history?market=us-options&underlying=AAPL&from=2026-03-01&to=2026-04-01&min_days_to_expiry=7&max_days_to_expiry=60"
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
curl "http://localhost:8080/api/v1/features/event-window-snapshot?market=us-options&underlying=AAPL"
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
curl "http://localhost:8080/api/v1/features/event-window-history?market=us-options&underlying=AAPL&from=2026-03-01&to=2026-04-01"
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

**Get a merged daily feature panel:**
```bash
curl "http://localhost:8080/api/v1/features/daily-feature-panel?market=us-options&underlying=AAPL&from=2026-03-01&to=2026-04-01&min_days_to_expiry=7&max_days_to_expiry=60"
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
curl -X POST http://localhost:8080/api/v1/crypto-options/backtest \
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
bin/crypto-options-missing-days \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --base-asset BTC
```

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

### 7.5. Backfill Missing US Option Greeks from ThetaData

When imported US option `1m` rows are missing Greeks because the underlying was not covered by the stock flatfiles, use the ThetaData daily-Greeks backfill command:

```bash
go run ./cmd/us-market-greeks-backfill \
  --start-date 2023-01-03 \
  --end-date 2025-12-31 \
  --symbols "SPX,SPXW,XSP,VIX,VIXW,RUT,RUTW,NDX,NDXP,DJX,OEX,MRUT,NANOS" \
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
- ThetaData EOD chain requests are made day by day with `expiration=*`, then matched back onto ClickHouse rows by `expiration + strike + option_type`.
- Known index-option alias families such as `SPX/SPXW`, `RUT/RUTW`, `VIX/VIXW`, and `NDX/NDXP` are tried automatically.
- If ThetaData returns `No data found` for a product/day, that task is logged as `SKIPPED` and the batch continues.
- Backfilled rows use the confirmed daily ThetaData Greeks as authoritative values for all affected `1m` rows of the matched contract on that market date.

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
- Liquidity snapshots are written for both `crypto-options` and `us-options`, with `us-options` currently exposing activity-style metrics from volume and transaction coverage.
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
  api-server/             REST API server
  backtest-portfolio/     Crypto/US spot and option-contract portfolio backtester
  backtest-example/       Simple strategy examples
  crypto-options-convert/ CSV.zst → Parquet converter
  crypto-options-import/  Parquet → ClickHouse importer
  crypto-options-missing-days/  Data gap scanner
  feature-store-backfill/ Feature-store volatility snapshot backfill
  us-market-import/       Polygon US market flatfile importer
internal/
  api/                    Gin HTTP handlers & router
  backtest/               Core engine, broker, indicators, options
  cryptooptions/          ClickHouse models, queries, CSV/Parquet I/O
  datafeed/               DataFeed implementations (ClickHouse-backed)
  dto/                    API request/response types
  optimization/           Parameter space & grid/random search
  report/                 HTML report generation
  service/                Business logic layer
  strategies/             Strategy implementations & builder
  usmarket/               US market CSV parsing and ClickHouse import
schema/
  clickhouse/             DDL for ClickHouse tables
```

## Testing

```bash
go test ./...
```

## License

Private — all rights reserved.
