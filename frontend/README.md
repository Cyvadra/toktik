# DSL Backtest Frontend

用于个人实验的只读 DSL 策略回测界面。它枚举 `pkg/dsl/scripts/strategies/*.toktik`，通过服务端代理调用现有 Toktik API，并在完成后嵌入 API 生成的 HTML 报告。API key 不会发送到浏览器。

## 启动

先启动依赖 ClickHouse、MySQL 的 API server：

```bash
go run ./cmd/api-server
```

将 API key 存放在仓库根目录的 `toktik-api-key`（该文件已被 `.gitignore` 忽略），然后启动前端：

```bash
make frontend-dev
```

也可以通过 `TOKTIK_API_KEY` 或 `-api-key` 临时覆盖文件中的值。

打开 <http://127.0.0.1:9020>。Bootstrap 和 HTMX 使用固定版本的 jsDelivr 资源，因此首次加载页面需要网络连接。

也可以构建单一二进制：

```bash
make build-frontend
./bin/dsl-backtest-frontend
```

## 配置

| Flag | Environment | Default |
| --- | --- | --- |
| `-listen` | `TOKTIK_FRONTEND_LISTEN` | `127.0.0.1:9020` |
| `-api-base-url` | `TOKTIK_FRONTEND_API_BASE_URL` | `http://127.0.0.1:9010` |
| `-api-key` | `TOKTIK_API_KEY` | root `toktik-api-key` |
| `-strategy-dir` | `TOKTIK_FRONTEND_STRATEGY_DIR` | `pkg/dsl/scripts/strategies` |

运行记录只存在于 frontend 进程内。进程重启后，旧报告仍在 API 的报告目录中，但不会出现在此界面。