# 代理開發指南

本文件描述在 Toktik 裡安全改程式的慣例。內容來自目前程式碼。

## 基本原則

- 先讀相關 package 的測試。這個倉庫測試覆蓋範圍廣，很多邊界條件藏在 `_test.go`。
- 改 API 不要只改 handler；DTO、service interface、service implementation、router、測試通常都需要一起看。
- 改資料表或 interval map 時，不要只改 SQL 或只改 Go 常數。`schema/clickhouse`、`internal/chquery/tables.go`、service validation、前端 interval 清單可能都有耦合。
- 改策略時不要重用 strategy instance。回測策略有狀態，batch run 依賴 factory 產生新 instance。
- 改同步 pipeline 時維持 `Syncer`、ledger、cursor、audit 的框架，不要在 job 裡自行實作一套 pending/success 狀態。
- 改 config credential 時使用 accessor，不要直接讀私有欄位或讓 plaintext 長時間留在 struct。

## Go 開發流程

常用：

```bash
go test ./...
go test ./internal/api ./internal/service
go test ./internal/backtest ./pkg/dsl/... ./pkg/strategies/...
go test ./cmd/data-sync-pipeline ./internal/syncpipeline/...
```

建置：

```bash
make build-api
make build-all
```

格式：

```bash
gofmt -w <files>
```

本倉庫沒有看到集中式 lint 指令；以 `gofmt` 與 `go test` 為最低標準。

## 前端開發流程

```bash
npm --prefix web/data-browser install
npm --prefix web/data-browser run typecheck
npm --prefix web/data-browser run build
npm --prefix web/data-browser run dev
```

前端改動時檢查：

- API 路徑是否在 `src/api.ts` 有對應方法。
- response 型別是否在 `src/types.ts` 更新。
- `VITE_API_BASE_URL` 與同源模式是否都可用。
- localStorage/sessionStorage cache key 是否需要 bump version。

## 新增 API Endpoint

建議順序：

1. 在 `internal/dto` 新增 request/response 型別與 validation error 使用方式。
2. 在 `internal/api/service.go` 的相應 interface 新增方法。
3. 在 `internal/service/<domain>.go` 實作方法。
4. 在 `internal/api/handler_<domain>.go` 新增 handler，只做 bind、呼叫 service、寫 response、錯誤映射。
5. 在 `internal/api/router.go` 註冊 route。
6. 補 handler/service 測試。
7. 若是 Swagger 文件需要更新，使用 `make refresh-api-docs`，但注意舊 docs 可能仍有歷史內容。

錯誤處理：

- 使用 `dto.NewValidationError` 回 400。
- 使用 `dto.NewNotFoundError` 回 404。
- context timeout/cancel 會被映射成 504。
- Polygon upstream HTTP error 會透過 `polygonErrorMessage` 映射。
- 其他錯誤會記 slog 並回 500。

## 新增資料查詢

建議：

- 表名、interval map 放 `internal/chquery/tables.go`。
- SQL 常數或 builder 放 `internal/chquery`，避免散落在 handler。
- service 負責參數正規化、預設值、DTO 組裝。
- 若 query 需要 ClickHouse named args，沿用現有 `clickhouse.Named` 風格。
- 輸出時間請注意 UTC、market session date、`market_date` 的語意差異。

## 新增資料同步 Job

`Syncer` 介面重點：

```go
type Syncer interface {
    Name() string
    SourceKeys(context.Context, driver.Conn) ([]string, error)
    ResolveCursor(context.Context, driver.Conn, string) (time.Time, bool, error)
    ColdStartFloor(string) time.Time
    Sync(context.Context, driver.Conn, SyncRequest) (SyncResult, error)
    AuditTargets(string) []AuditTarget
    MaxConcurrency() int
}
```

實作時要回答：

- source key 是 symbol、dataset 還是 singleton？
- cursor 依哪個表與時間欄位？
- cold start floor 是哪一天？
- 是否支援 dry run？
- insert 是否 replace？
- audit target 如何抓重複？
- 最大 source concurrency 是否會打爆 provider rate limit？

新增後要改：

- `internal/syncpipeline/jobs/jobs.go`
- `cmd/data-sync-pipeline/main.go`
- `configs/data-sync-pipeline.yaml`
- 測試與必要 snapshot helper

## 新增策略

Go 策略：

1. 建 `pkg/strategies/<strategy_name>/strategy.go`。
2. 實作 `backtest.Strategy`。
3. 在 `init()` 呼叫 `catalog.Register`，填 `Name`、`Aliases`、`Groups`、`Profile`、`Factory`。
4. 在 `pkg/strategies/strategies.go` 加 blank import。
5. 加單元測試，至少覆蓋 config parsing、Init、主要 OnBar 決策。

DSL 策略：

1. 確認 DSL source 可被 `pkg/dsl/parser` parse。
2. 透過 `pkg/dsl/catalog.RegisterDSLWithMetadata` 註冊。
3. 若需要 signals，理解 `bridge.Options.SignalSource` 與 metadata 的 timestamp/type/value columns。
4. 若需要額外欄位，檢查 `expose_fields` 與 `PreloadHook`。

## 新增 DSL Builtin

1. 在 `pkg/dsl/runtime/builtins_<domain>.go` 實作。
2. 在 `pkg/dsl/runtime` 加 interpreter 測試。
3. 若 bridge strategy 需要該 builtin，於 `pkg/dsl/bridge/bridge.go` 的 `Init` 註冊。
4. 若 builtin 會宣告外部資料依賴，更新 metadata extraction 與 data request 解析。

## Config 與 Secrets

設定讀取流程：

1. `DefaultRuntime()`
2. YAML unmarshal
3. environment overrides
4. normalize
5. validate
6. seal credentials

有 AES key 時，敏感值會被放到 `secrets.Manager`，原欄位清空。呼叫端應用：

- `runtimeCfg.FMPAPIKey()`
- `runtimeCfg.PolygonAPIKey()`
- `runtimeCfg.PolygonFlatFilesAccessKey()`
- `runtimeCfg.PolygonFlatFilesSecretKey()`
- `runtimeCfg.TigerPrivateKey()`
- `runtimeCfg.TigerToken()`
- `runtimeCfg.MySQLDSN()`

不要 log credential，也不要在錯誤訊息中包含完整 DSN 密碼。

## 測試選擇

- `internal/api`：router、middleware、handler 行為。
- `internal/service`：service 邏輯、DTO 組裝、cache 行為。
- `internal/backtest`：回測 replay、broker/order/result、indicator。
- `pkg/dsl/*`：lexer/parser/interpreter/bridge。
- `pkg/strategies/*`：策略自身行為。
- `internal/syncpipeline`：runner、ledger、audit。
- `cmd/data-sync-pipeline`：config parsing、job construction、CLI helper。
- `pkg/fmp`、`pkg/polygon`：client parsing、retry、pagination、error handling。

若測試需要外部服務，先找現有 test fake/stub；不要讓單元測試依賴真 API key。
