# toktik

Quantitative backtesting and market data platform for crypto options trading, built in Go.

## Features

- **Market data pipeline** — Convert zstd-compressed CSV tick data → Parquet → ClickHouse OHLCV bars with pre-computed materialized views (5m, 15m, 30m, 1h, 2h, 3h, 4h, 6h, 8h, 12h, 1d)
- **Event-driven backtesting engine** — Pine Script-style strategy interface with multi-asset, multi-leg options support, vectorized indicator computation, and realistic broker simulation (slippage, commissions, TWAP)
- **REST API** — Query historical bars, symbols, greeks, and run backtests with cursor-based pagination
- **Options spread strategies** — Built-in bull put / bear call spread strategies with MA deviation signals
- **US market flatfile import** — Import Polygon US options and stocks minute CSV flatfiles into ClickHouse with precomputed K-line views
- **HTML reports** — Self-contained backtest reports with equity curves, drawdown charts, and trade markers

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
make build-backtest-btc-portfolio
make build-us-market-import

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

**List symbols:**
```bash
curl "http://localhost:8080/api/v1/crypto-options/symbols?base_asset=BTC&limit=10"
```

**Get OHLCV bars:**
```bash
curl "http://localhost:8080/api/v1/crypto-options/bars?symbol=BTC-28MAR25-100000-C&interval=1h&from=2025-01-01&to=2025-03-01&limit=500"
```

**Get greeks time series:**
```bash
curl "http://localhost:8080/api/v1/crypto-options/greeks?symbol=BTC-28MAR25-100000-C&from=2025-01-01&to=2025-03-01"
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

**BTC portfolio backtest strategy:**
```bash
bin/backtest-btc-portfolio \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
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
- Options-led strategies, or strategies whose spot leg is only a signal-sized sidecar, use `BTC`.
- Multi-strategy runs emit one overview HTML plus one detail page per strategy so mixed denomination runs remain readable.

**BTC forum-style short put strategy:**
```bash
bin/backtest-btc-portfolio \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
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
bin/crypto-options-kline-backfill \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --intervals "1m,5m,15m,30m,1h,2h,3h,4h,6h,8h,12h,1d" \
  --base-asset BTC \
  --from 2025-01-01 \
  --to 2025-03-01
```

Notes:
- `1m` is the base table and will be skipped during backfill.
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
  backtest-btc-portfolio/ BTC spot/options portfolio backtester
  backtest-example/       Simple strategy examples
  crypto-options-convert/ CSV.zst → Parquet converter
  crypto-options-import/  Parquet → ClickHouse importer
  crypto-options-missing-days/  Data gap scanner
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
