# Toktik Platform - Roadmap

This document captures the current implementation status and future plans for the toktik data platform.

---

## Phase 2: K-Line Generation

Status: Implemented

Generate OHLCV bars at multiple time windows from 1-minute data stored in ClickHouse:

- **Intervals**: 5m, 15m, 30m, 1h, 4h, 1d
- **Approach**: ClickHouse materialized views and query-time aggregation from `crypto_options_bar_1m`
- **Schema**: Precomputed interval views are exposed as `crypto_options_bar_{interval}` with the same column structure as `crypto_options_bar_1m`
- **Implementation**: Each precomputed interval uses an `AggregatingMergeTree` state table, a materialized view fed from `crypto_options_bar_1m`, and a query view for read access
- **Fallback**: Query-time aggregation is supported for ad-hoc intervals that are not precomputed

---

## Phase 3: Market Data API (Gin)

Status: Implemented

Expose market data through a RESTful web API powered by Gin:

- **Endpoints**:
  - `GET /api/v1/crypto-options/bars` - query bars by symbol, time range, interval
  - `GET /api/v1/crypto-options/symbols` - list/search option symbols with metadata
  - `GET /api/v1/crypto-options/greeks` - greeks time series for a symbol
- **DTO layer**: All business services call the DTO layer, never the database directly
  - `internal/dto/crypto_options.go` - request/response DTOs
  - `internal/service/crypto_options.go` - business logic using DTOs
- **Pagination**: cursor-based for time-series data
- **Interval routing**: `1m` reads from the base table, precomputed intervals read from K-line views, ad-hoc intervals use query-time aggregation
- **Cache**: Consider Redis or in-memory cache for hot symbol metadata

---

## Phase 4: Backtest Engine

Integrate with backtesting strategies that run efficiently on the platform:

- **Data feed**: Stream 1m/5m bars from ClickHouse to the backtest engine
- **Strategy interface**: Define a Go interface for strategies to implement
- **Execution model**: Event-driven with bar-by-bar replay
- **Performance tracking**: PnL, Sharpe, drawdown, Greeks exposure over time
- **Output**: Results stored in ClickHouse for historical comparison

---

## Phase 5: Market Data View (UI)

User-facing UI for viewing market data and backtest performance:

- **TradingView integration**: Lightweight Charts library for candlestick rendering
- **TailwindCSS**: For layout and styling
- **Features**:
  - Symbol search and selection
  - Multi-interval candlestick charts (1m to 1d)
  - Greeks surface visualization (delta/gamma vs strike/expiry)
  - Backtest performance overlay on price charts
  - Open interest heatmap across strikes and expirations

---

## Phase 6: Multi-Market Expansion

The platform is designed to support various markets beyond crypto options:

| Market                     | Table prefix           | Notes                                |
|----------------------------|------------------------|--------------------------------------|
| Crypto Options (current)   | `crypto_options_`      | Deribit data, implemented in Phase 1 |
| US Stock Options           | `us_stock_options_`    | OPRA feed, different symbol format   |
| HK Stock Options           | `hk_stock_options_`    | HKEX format                          |
| ETF Options                | `etf_options_`         | Similar to stock options             |
| Crypto Perpetual Contracts | `crypto_perps_`        | Funding rate, different OHLCV model  |
| FOREX                      | `forex_`               | Spot and forward rates               |
| Polymarket Events          | `polymarket_`          | Binary outcome markets               |

**Key design principle**: Each market type gets its own set of tables, models, and parsers. Shared infrastructure (API routing, backtest engine, UI components) is market-agnostic and dispatches to market-specific implementations.

---

## Architecture Notes

### Current Status

```
Phase 1: Implemented
Phase 2: Implemented
Phase 3: Implemented
Phase 4+: Planned
```

### DTO Pattern

```
[API Handler] -> [Service Layer] -> [DTO Layer] -> [ClickHouse]
```

- Handlers parse HTTP requests into request DTOs
- Service layer contains business logic, works only with DTOs
- DTO layer translates between service models and database queries
- No direct database access from handlers or services

### Naming Conventions

- **Tables**: `{market_type}_{entity}` (e.g., `crypto_options_bar_1m`)
- **Go packages**: `internal/{markettype}/` (e.g., `internal/cryptooptions/`)
- **API routes**: `/api/v1/{market-type}/` (e.g., `/api/v1/crypto-options/`)
- **CLI tools**: `{market-type}-{verb}` (e.g., `crypto-options-convert`)
