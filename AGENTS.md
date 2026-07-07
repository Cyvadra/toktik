# Toktik 代理接手指南

本文件是給未來較小模型或新代理接手本倉庫時使用的入口。請以目前程式碼為準，不要把 `README.md`、既有 `docs/` 文章、`analysis/README.md` 或策略說明檔當成最新事實來源；那些文件可能保留歷史設計、研究筆記或一次性報告。若文件與程式碼衝突，一律重新讀程式碼。

## 快速判斷

Toktik 是 Go 單體倉庫，主要處理多市場行情資料、資料同步、ClickHouse 查詢、策略回測、DSL 策略、基本面/宏觀資料與一個 React/Vite 資料瀏覽器。

主要技術：

- Go module：`github.com/Cyvadra/toktik`，`go 1.26.1`
- HTTP API：Gin，入口在 `cmd/api-server`
- 資料庫：ClickHouse 是行情/特徵/基本面主庫；MySQL/Gorm 用於財經日曆；Redis 可選作 API 快取與分散式限流
- 前端：`web/data-browser`，React + Vite + lightweight-charts
- 外部資料：FMP、Polygon/Massive、Deribit、CBOE VIX、GuruFocus、Tiger OpenAPI

## 先讀這些

1. `go.mod`：依賴與 Go 版本。
2. `cmd/api-server/main.go`：API 啟動、依賴組裝、ClickHouse/MySQL/Redis 初始化。
3. `internal/api/router.go` 與 `internal/api/service.go`：HTTP route table 與 handler 需要的 service 介面。
4. `internal/service/*`：業務編排層。多數 API 行為從這裡開始追。
5. `internal/chquery/tables.go`、`schema/clickhouse/*.sql`：ClickHouse 表與 interval 對應。
6. `internal/backtest/*`、`pkg/strategies/catalog/*`、`pkg/dsl/*`：回測、策略註冊、DSL runtime。
7. `internal/syncpipeline/*`、`cmd/data-sync-pipeline/main.go`、`configs/data-sync-pipeline.yaml`：現行資料同步框架。
8. `web/data-browser/src/api.ts`、`web/data-browser/src/App.tsx`：資料瀏覽器如何呼叫 API。

更多整理見：

- `docs/agent-codebase-map.zh-TW.md`
- `docs/agent-development-guide.zh-TW.md`
- `docs/agent-operations.zh-TW.md`

## 架構規則

- `internal/api` 只做 HTTP 綁定、驗證、錯誤映射與 response 裝飾。不要把資料查詢或業務規則塞進 handler。
- `internal/service` 是應用層，負責組合 repo、cache、外部 client、回測與 DTO。
- `internal/chrepo` 是 ClickHouse 連線的薄包裝；大量 SQL 字串與 query helper 在 `internal/chquery`。
- 市場匯入與領域邏輯分散在 `internal/cryptooptions`、`internal/usmarket`、`internal/forexmarket`。
- 回測核心在 `internal/backtest`，策略介面是 `Strategy{Name, Init, OnBar}`，策略狀態不可跨 run 重用。
- 策略註冊靠 `pkg/strategies/catalog.Register`，`pkg/strategies/strategies.go` 以 blank import 啟用內建策略。
- DSL 策略橋接在 `pkg/dsl/bridge`，builtin 分散於 `pkg/dsl/runtime/builtins_*.go`。
- 資料同步統一走 `syncpipeline.Syncer`、`import_ledger` 與 runner；不要為新同步工作另造一套狀態/鎖機制。
- 前端只是一個資料瀏覽器工具，不是主要產品殼。API base 由 `VITE_API_BASE_URL` 或同源路徑決定，API key 存在 localStorage。

## 常用指令

```bash
go test ./...
go test ./internal/service ./internal/api ./internal/backtest
go build -trimpath -ldflags '-s -w' -o bin/api-server ./cmd/api-server
make build-api
make web-build
npm --prefix web/data-browser run typecheck
```

資料同步常用入口：

```bash
go run ./cmd/data-sync-pipeline list-jobs
go run ./cmd/data-sync-pipeline status
go run ./cmd/data-sync-pipeline run --dry-run
go run ./cmd/data-sync-pipeline integrity --format json
```

API 本機預設：

```bash
go run ./cmd/api-server
# 預設 listen addr: :9010
# health: /health
# readiness: /ready
# API prefix: /api/v1
```

## 設定來源

預設讀 `toktik.yaml`，也可用 `TOKTIK_CONFIG` 指定。`toktik.example.yaml` 是目前格式範例。環境變數會覆蓋 YAML，重要名稱在 `internal/config/runtime.go`：

- `CLICKHOUSE_DSN`
- `MYSQL_DSN` 或 `MYSQL_HOST`、`MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DATABASE`
- `LISTEN_ADDR`
- `CORS_ORIGINS`
- `RATE_LIMIT_RPS`
- `TOKTIK_SCHEMA_DIR`
- `FMP_API_KEY`
- `TOKTIK_FMP_CACHE_DIR`
- `TOKTIK_REDIS_ENABLED`、`TOKTIK_REDIS_ADDR`、`TOKTIK_REDIS_PASSWORD`
- `TOKTIK_AES_KEY`

API server 需要 ClickHouse 與 MySQL；Redis 可關閉。API key 儲存在 MySQL，使用 `go run ./cmd/api-keys create ...` 建立，呼叫 API 時放在 `X-API-Key` header。

## 修改功能時的路徑

- 新增 API endpoint：先在 `internal/api/service.go` 加介面方法，再在 `internal/api/router.go` 註冊 route，handler 放 `internal/api/handler_*.go`，邏輯放 `internal/service`，DTO 放 `internal/dto`。
- 新增 ClickHouse 查詢：優先在 `internal/chquery` 放 query builder 或常數，service 維持組裝與輸出轉換。
- 新增市場資料 interval：同步更新 `internal/chquery/tables.go` interval map、schema/materialized view、對應 service validation、資料瀏覽器預設 interval。
- 新增同步工作：實作 `internal/syncpipeline.Syncer`，在 `internal/syncpipeline/jobs` 放 job，於 `cmd/data-sync-pipeline/main.go` 的 build function 接線，並在 `configs/data-sync-pipeline.yaml` 補設定。
- 新增策略：在 `pkg/strategies/<name>` 實作 `backtest.Strategy` 並於 init 註冊，確認 `pkg/strategies/strategies.go` blank import。
- 新增 DSL builtin：改 `pkg/dsl/runtime`，再確認 bridge 是否需要註冊或 expose metadata。
- 新增前端資料視圖：先確認後端 API DTO，再改 `web/data-browser/src/types.ts`、`api.ts`、`App.tsx` 與 CSS。

## 驗證習慣

- 小改動至少跑目標 package 測試。
- API 或 service 改動跑 `go test ./internal/api ./internal/service`。
- 回測、DSL、策略改動跑 `go test ./internal/backtest ./pkg/dsl/... ./pkg/strategies/...`。
- 同步 pipeline 改動跑 `go test ./cmd/data-sync-pipeline ./internal/syncpipeline/...`。
- 跨層改動或不確定時跑 `go test ./...`。
- 前端改動跑 `npm --prefix web/data-browser run typecheck` 與 `npm --prefix web/data-browser run build`。

最後一次建立本文件時，`go test ./...` 已通過。

## 注意事項

- 不要提交真實 API key、DSN 密碼、Tiger private key 或 Polygon/FMP credentials。
- `internal/config.Runtime` 支援用 AES key 將部分 credential 在記憶體中封存；取值應透過 `FMPAPIKey()`、`PolygonAPIKey()`、`MySQLDSN()` 等 accessor。
- `api-server` 啟動時會嘗試 migration 財經日曆 MySQL table，也會初始化部分 ClickHouse schema/materialized view。
- `PortfolioBacktestService` 會在背景清理已完成 run；API run 狀態保存在記憶體，不是持久化工作隊列。
- `syncpipeline` 的 ledger lock 與 stale lock 清理很重要，勿繞過。
- ClickHouse 表名與 interval map 必須一致，否則 API/回測會查到不存在的 relation。
- 既有中文策略文件多為策略說明，不等同目前執行邏輯；策略行為以 `.go` 與 DSL source 為準。
