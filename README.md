# toktik

Quantitative backtesting and market data platform for crypto options trading, built in Go.

## Features

- **Market data pipeline** — Convert zstd-compressed CSV tick data → Parquet → ClickHouse OHLCV bars with pre-computed materialized views (5m, 15m, 30m, 1h, 4h, 1d)
- **Event-driven backtesting engine** — Pine Script-style strategy interface with multi-asset, multi-leg options support, vectorized indicator computation, and realistic broker simulation (slippage, commissions, TWAP)
- **REST API** — Query historical bars, symbols, greeks, and run backtests with cursor-based pagination
- **Options spread strategies** — Built-in bull put / bear call spread strategies with MA deviation signals
- **US stock options sync** — Download and store US equity options data from Theta Data API
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
make build-backtest-btc-options

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

**BTC options spread strategy:**
```bash
bin/backtest-btc-options \
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

Available strategies: `golden-cross`, `delta-filter`, `bull-put-spread` (alias `ma-deviation-bull`), `bear-call-spread` (alias `ma-deviation-bear`), `both`.

### 5. Find Missing Data

Scan ClickHouse for date gaps:

```bash
bin/crypto-options-missing-days \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --base-asset BTC
```

### 6. Sync US Stock Options (Theta Data)

```bash
bin/thetadata-sync \
  --roots "AAPL,SPY" \
  --start-date 2024-01-01 \
  --end-date 2025-01-01 \
  --mcp-url "http://127.0.0.1:25503" \
  --clickhouse-dsn "clickhouse://localhost:9000/default" \
  --workers 4 \
  --rate-limit 5.0
```

Use `--all-roots` to discover and sync all available option roots. Add `--prefilter-roots` to score roots by activity and keep only the most active ones.

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
  backtest-btc-options/   BTC options spread backtester
  backtest-example/       Simple strategy examples
  crypto-options-convert/ CSV.zst → Parquet converter
  crypto-options-import/  Parquet → ClickHouse importer
  crypto-options-missing-days/  Data gap scanner
  thetadata-sync/         Theta Data API sync tool
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
pkg/
  thetadata/              Theta Data API client & pipeline
schema/
  clickhouse/             DDL for ClickHouse tables
```

## Testing

```bash
go test ./...
```

## License

Private — all rights reserved.
