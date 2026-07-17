# 代理用程式碼地圖

本文件從目前程式碼整理，目的不是產品介紹，而是幫接手者快速定位修改點。舊文件可能過期，請以程式碼為準。

## 頂層結構

- `cmd/`：所有可執行入口。主要有 API server、資料同步 pipeline、回測 CLI、各市場匯入/補資料工具。
- `internal/api/`：Gin router、middleware、handler、service 介面。
- `internal/service/`：API 背後的應用服務，負責呼叫 ClickHouse repo、cache、外部 client、回測引擎。
- `internal/chrepo/`：ClickHouse repo 薄包裝，持有 `driver.Conn`。
- `internal/chquery/`：SQL 常數、表名、interval 對應、query builder。
- `internal/backtest/`：回測引擎、資料準備、replay、broker、order、indicator、result。
- `internal/datafeed/`：把 ClickHouse 市場資料接成 backtest `DataFeed`/chain provider。
- `internal/cryptooptions/`、`internal/usmarket/`、`internal/forexmarket/`：市場資料匯入、同步、schema 初始化與領域處理。
- `internal/syncpipeline/`：同步 runner、ledger lock、cursor、audit。
- `internal/syncpipeline/jobs/`：具體同步 job 實作。
- `internal/dataintegrity/`：資料完整性檢查與可修復項。
- `internal/config/`：runtime config、環境變數覆蓋、credential accessor。
- `internal/cache/`：memory/Redis cache store。
- `internal/calendarrepo/`：MySQL/Gorm 的財經日曆 repository。
- `internal/dto/`：API request/response 型別與 service error 型別。
- `pkg/fmp/`、`pkg/polygon/`、`pkg/tigerapi/`：外部服務 client。
- `pkg/feeds/`：factor feed registry/store；DVOL 已改由 macro 查詢與 `pkg/feeds/dvol` Deribit client 寫入 macro。
- `pkg/dsl/`：Toktik DSL lexer/parser/runtime/bridge/catalog。
- `pkg/strategies/`：Go 策略與策略 catalog。
- `schema/clickhouse/`：ClickHouse DDL。
- `web/data-browser/`：React/Vite 資料瀏覽器。

## API Server

入口：`cmd/api-server/main.go`

啟動流程：

1. `config.LoadRuntime()` 讀 `toktik.yaml` 或 `TOKTIK_CONFIG`，再套環境變數。
2. 連 MySQL，建立 `calendarrepo.Repo` 並 `AutoMigrate` 財經日曆表。
3. 連 ClickHouse，透過 `internal/cli.ConnectClickHouse` 初始化指定 schema。
4. 建立 `feeds.Store`、memory/Redis cache、`chrepo.Repo`。
5. 建立 `service.*Service`，組成 `api.Deps`。
6. `api.NewRouterFromDeps` 建 Gin engine。
7. 啟動 USTurnoverIntersection cache refresher。

Route table 在 `internal/api/router.go`，主要群組：

- `/api/v1/backtests`
- `/api/v1/infra`
- `/api/v1/browser`
- `/api/v1/features`
- `/api/v1/indicators`
- `/api/v1/markets/{crypto-options,crypto-spot,forex,us-stocks,us-options}`
- `/api/v1/screener`
- `/api/v1/universes`
- `/api/v1/factors`
- `/api/v1/fundamentals`
- `/api/v1/macro`
- `/api/v1/calendar`
- `/api/v1/polygon`
- `/api/v1/strategies`

Middleware 在 `internal/api/middleware.go`，包含 recovery、request logging、security headers、CORS、request timeout、API key auth、rate limit。SSE 與 HTML report endpoint 由 `isStreamingPath` 排除 request timeout。

## Service 層

`internal/api/service.go` 定義 handler 需要的介面，`internal/service` 提供實作。這是保持 handler 可測試的主要邊界。

重要 service：

- `CryptoOptionsService`：crypto options bars/symbols/greeks/chain/backtest。
- `CryptoSpotService`：crypto spot bars/symbols。
- `ForexService`：forex bars/symbols。
- `USStocksService`：US stock bars/symbols/splits，會整合 fundamentals 與 company profile provider。
- `USOptionsService`：US options bars/symbols/greeks/chain/wall，可接 Polygon client 與 cache。
- `DataBrowserService`：受控資料庫檢視、schema、preview、coverage、field profile、valid count、values。
- `FeatureService`：volatility、term structure、skew、liquidity、event window、daily panel。
- `IndicatorService`：DSL driven indicator series 與 presets。
- `PortfolioBacktestService`：非同步 portfolio backtest、SSE、HTML report、option chain provider cache。
- `ScreenerService`：underlyings/options screening 與 US turnover intersection cache。
- `FundamentalsService`、`MacroService`、`FinanceCalendarService`：基本面、宏觀與財經日曆。
- `PolygonService`：Polygon/Massive REST proxy，含 cache。

## ClickHouse 與表

主要表名與 interval map 在 `internal/chquery/tables.go`。新增 interval 或新表時先更新這裡，再確認 schema。

目前 DDL 重點：

- `schema/clickhouse/crypto_options.sql`
  - `import_ledger`
  - `crypto_options_symbol_meta`
  - `crypto_options_bar_1m`
  - `crypto_spot_bar_1m`
- `schema/clickhouse/us_market.sql`
  - `import_ledger`
  - `us_equity_sessions`
  - `us_stock_splits`
  - `us_options_bar_1m`
  - `us_stocks_bar_1m`
  - `us_options_bar_1d_direct`
  - `us_stocks_bar_1d_direct`
- `schema/clickhouse/forex_market.sql`
  - `forex_bar_1m`
- `schema/clickhouse/feature_store.sql`
  - `feature_volatility_snapshot_daily`
  - `feature_term_structure_snapshot_daily`
  - `feature_skew_snapshot_daily`
  - `feature_liquidity_snapshot_daily`
  - `feature_daily_panel_daily`
- `schema/clickhouse/fundamentals.sql`
  - `fundamental_factor_catalog`
  - `fundamental_observation`
  - `macro_factor_catalog`
  - `macro_observation`
- `schema/clickhouse/deribit_dvol.sql`
  - `deribit_dvol_bar`

注意：`import_ledger` 在多個 schema 檔中存在。改 DDL 時要確認實際初始化路徑與重複建立語句相容。

## 回測與策略

回測核心入口：

- `internal/backtest/engine.go`
- `internal/backtest/strategy.go`
- `internal/backtest/datafeed.go`
- `internal/backtest/replayer.go`
- `internal/backtest/broker.go`
- `internal/backtest/order*.go`
- `internal/backtest/result*.go`

`backtest.Strategy` 介面：

```go
type Strategy interface {
    Name() string
    Init(ctx *SetupContext) error
    OnBar(ctx *BarContext)
}
```

策略可選擇實作：

- `StrategyPreloader`
- `ReportColumnProvider`
- `ReportSeriesProvider`

策略註冊：

- registry 在 `pkg/strategies/catalog/registry.go`
- 策略 config/profile 在 `pkg/strategies/catalog/config.go`、`profile.go`
- 內建策略由 `pkg/strategies/strategies.go` blank import 啟用
- API server 還 blank import `pkg/dsl/catalog` 以啟用 DSL strategy loading

`PortfolioBacktestService` 的重點：

- run 狀態是記憶體 map，有 TTL eviction，不是持久化 queue。
- SSE 訂閱由 service 管理，handler 只轉發事件。
- option chain provider 有 TTL 與 max size cache，避免同一 window 重複載入。
- 多策略/多 summary 會產生 HTML report，handler 會限制 report path 必須在 `reportsRoot` 內。

## DSL

DSL 結構：

- `pkg/dsl/lexer`、`parser`、`ast`、`token`
- `pkg/dsl/runtime`：interpreter 與 builtin 模組
- `pkg/dsl/bridge`：把 DSL program 轉成 `backtest.Strategy`
- `pkg/dsl/catalog`：把 DSL source 註冊進策略 catalog

bridge 會在 `Init` 註冊 TA/math/string/strategy/input/request/options/alpha/signal/event/order/config/portfolio/ref builtins。若新增 builtin，確認 bridge 是否需要註冊。

DSL 支援 `request.security`、factor request、option chain request、signal source preload、expose fields、report series 等功能。改 metadata 時要看 `pkg/dsl/bridge/bridge.go` 的 `extractMetadata` 相關邏輯。

## 資料同步

主要入口：`cmd/data-sync-pipeline/main.go`

子命令：

- `run`
- `status`
- `audit`
- `integrity`
- `list-jobs`

框架：

- `internal/syncpipeline.Syncer` 是 job 必須實作的介面。
- Runner 會 topological sort jobs、處理 depends_on、計算 cursor window、套 overlap、跑 source concurrency、寫 `import_ledger`。
- ledger lock 預設 TTL 是 2h，stale lock 可清理。
- audit target 用於重複資料檢查。

目前 config 的 job 名稱包含：

- `cboe_vix_macro`
- `deribit_dvol_macro`
- `guru_macro`
- `fmp_sp500_macro`
- `fmp_nasdaq100_macro`
- `fmp_crypto_spot`
- `fmp_forex`
- `fmp_us_stocks`
- `fmp_us_stock_splits`
- `fmp_us_stock_profiles`
- `fmp_us_fundamentals`
- `fmp_etf_fundamentals`
- `fmp_economic_calendar`
- `fmp_observed_stock_calendar`
- `fmp_stock_earnings_calendar_backfill`
- `polygon_us_flatfiles`
- `polygon_us_greeks`
- `feature_store_backfill`

新增 job 時需要同步：

1. 在 `internal/syncpipeline/jobs` 實作 syncer。
2. 在 `cmd/data-sync-pipeline/main.go` 的 build function 接線。
3. 更新 `configs/data-sync-pipeline.yaml`。
4. 若需要 pre/post snapshot 或 option coverage warning，更新相關 helper。
5. 加測試到 `cmd/data-sync-pipeline` 或 `internal/syncpipeline/jobs`。

## 前端資料瀏覽器

路徑：`web/data-browser`

技術：

- React 19
- Vite
- TypeScript
- lightweight-charts
- lucide-react

主要檔案：

- `src/api.ts`：fetch wrapper、API key、session/local cache、endpoint map。
- `src/types.ts`：後端 response 型別。
- `src/App.tsx`：主要 UI 與 charts。
- `src/styles.css`：樣式。

API base：

- `VITE_API_BASE_URL` 存在時使用它。
- 否則使用同源路徑。

若後端 DTO 變更，前端型別與讀取邏輯要一起更新。
