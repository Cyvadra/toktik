# 代理操作手冊

本文件整理本地啟動、設定、資料同步與常見風險。

## 本機啟動 API

預設設定：

- 設定檔：`toktik.yaml`
- ClickHouse：`clickhouse://default:@localhost:9000/default`
- MySQL：`127.0.0.1:3306`，user/database 預設 `toktik`
- API listen：`:9010`
- Schema dir：`schema/clickhouse`
- Reports root：`reports/backtests`

範例：

```bash
cp toktik.example.yaml toktik.yaml
go run ./cmd/api-server
```

檢查：

```bash
curl http://localhost:9010/health
curl http://localhost:9010/ready
```

若設定了 `api.api_keys` 或 `API_KEYS`，請帶：

```bash
curl -H 'X-API-Key: <key>' http://localhost:9010/api/v1/infra/markets
```

## 本機啟動資料瀏覽器

```bash
npm --prefix web/data-browser install
npm --prefix web/data-browser run dev
```

若前端與 API 不同 origin，設定：

```bash
VITE_API_BASE_URL=http://localhost:9010 npm --prefix web/data-browser run dev
```

## Runtime Config

預設讀 `toktik.yaml`。使用其他路徑：

```bash
TOKTIK_CONFIG=/path/to/toktik.yaml go run ./cmd/api-server
```

常見環境變數：

- `CLICKHOUSE_DSN`
- `MYSQL_DSN`
- `MYSQL_PASSWORD`
- `LISTEN_ADDR`
- `API_KEYS`
- `CORS_ORIGINS`
- `FMP_API_KEY`
- `TOKTIK_FMP_CACHE_DIR`
- `TOKTIK_REDIS_ENABLED`
- `TOKTIK_REDIS_ADDR`
- `TOKTIK_AES_KEY`

`TOKTIK_AES_KEY` 是 hex encoded AES key，長度需對應 16、24 或 32 bytes。留空時 credentials 以 plaintext 保留在記憶體。

## 資料同步 Pipeline

入口：

```bash
go run ./cmd/data-sync-pipeline <subcommand>
```

常用：

```bash
go run ./cmd/data-sync-pipeline list-jobs
go run ./cmd/data-sync-pipeline status
go run ./cmd/data-sync-pipeline run --dry-run
go run ./cmd/data-sync-pipeline run --jobs polygon_us_flatfiles,fmp_us_stocks
go run ./cmd/data-sync-pipeline audit
go run ./cmd/data-sync-pipeline integrity --format json
```

設定檔預設：

```bash
configs/data-sync-pipeline.yaml
```

Runner 行為：

- 依 `depends_on` 拓樸排序。
- 每個 source 用 cursor 決定增量 window。
- 套 `overlap_days` 修補最近資料。
- 用 `import_ledger` 防重入與記錄 success/failed/skipped。
- 可跑 duplicate audit。
- stale lock TTL 預設 2h。

重要風險：

- `--force` 或 force unlock 類行為可能影響 pending ledger，使用前先查 status。
- 行情資料固定由 Polygon flat files 更新；FMP stock job 只保留給最新行情補全等明確指定用途。
- daily-only flatfile 模式會影響 greeks 與 feature store，config 註解已有提醒。
- 大型 feature/fundamental integrity check 會使用 ClickHouse memory/thread settings；不要隨意放大。

## Schema 初始化

多數 CLI 透過 `internal/cli.ConnectClickHouse` 連 ClickHouse 並可初始化 schema。API server 啟動會使用 crypto options base schema finder，同時要求 kline、spot kline、chain cache、option wall 初始化。

Schema 路徑候選由 `Runtime.SchemaPathCandidates(fileName)` 決定，預設從 `schema/clickhouse` 找。

若遇到 table not found：

1. 確認 `TOKTIK_SCHEMA_DIR` 或 `paths.schema_dir`。
2. 確認對應 schema 檔是否包含該表或 materialized view。
3. 確認啟動入口是否有要求初始化該 schema。
4. 確認 `internal/chquery/tables.go` 表名與 schema 一致。

## 外部資料來源

FMP：

- 設 `FMP_API_KEY` 或 `fmp.api_key`。
- cache dir 由 `TOKTIK_FMP_CACHE_DIR` 或 `fmp.cache_dir` 決定。
- client 在 `pkg/fmp`。

Polygon/Massive：

- REST base 預設 `https://api.massive.com`。
- Flat files base 預設 `https://files.massive.com/flatfiles`。
- API key/access key/secret key 透過 config accessor 讀。
- service 在 `internal/service/polygon.go`，client 在 `pkg/polygon`。

Tiger：

- config 欄位在 `internal/config.Runtime.Tiger`。
- wrapper 在 `pkg/tigerapi`。

## Backtest API 操作

Backtest route：

- `POST /api/v1/backtests/validate`
- `POST /api/v1/backtests/runs`
- `GET /api/v1/backtests/runs/:runID`
- `GET /api/v1/backtests/runs/:runID/events`
- `GET /api/v1/backtests/runs/:runID/report`
- `GET /api/v1/backtests/runs/:runID/reports/:reportID`

行為：

- `StartStrategyBacktest` 會回 accepted 與 run ID。
- 進度事件可用 SSE endpoint 訂閱。
- report path 會被限制在 `reportsRoot` 之下。
- run 狀態只在目前 process 記憶體內，server restart 後不保留。

## 驗證紀錄

建立這組代理文件時已執行：

```bash
go test ./...
```

結果：通過。
