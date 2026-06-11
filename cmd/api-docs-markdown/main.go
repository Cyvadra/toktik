package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

type swaggerDoc struct {
	Info        swaggerInfo                     `json:"info"`
	BasePath    string                          `json:"basePath"`
	Paths       map[string]map[string]operation `json:"paths"`
	Definitions map[string]schemaRef            `json:"definitions"`
}

type swaggerInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type operation struct {
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Tags        []string            `json:"tags"`
	Consumes    []string            `json:"consumes"`
	Produces    []string            `json:"produces"`
	Deprecated  bool                `json:"deprecated"`
	Parameters  []parameter         `json:"parameters"`
	Responses   map[string]response `json:"responses"`
}

type parameter struct {
	Name        string     `json:"name"`
	In          string     `json:"in"`
	Description string     `json:"description"`
	Required    bool       `json:"required"`
	Type        string     `json:"type"`
	Schema      *schemaRef `json:"schema"`
	Items       *schemaRef `json:"items"`
}

type response struct {
	Description string     `json:"description"`
	Schema      *schemaRef `json:"schema"`
}

type schemaRef struct {
	Ref                  string               `json:"$ref"`
	Type                 string               `json:"type"`
	Format               string               `json:"format"`
	Description          string               `json:"description"`
	Items                *schemaRef           `json:"items"`
	AdditionalProperties json.RawMessage      `json:"additionalProperties"`
	Properties           map[string]schemaRef `json:"properties"`
	Required             []string             `json:"required"`
}

type endpointSpec struct {
	Method string
	Path   string
	Label  string
}

type sectionSpec struct {
	Title     string
	Endpoints []endpointSpec
}

var marketSections = []sectionSpec{
	{
		Title: "Technical Indicators",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/indicators/presets", Label: "Indicator preset catalog"},
			{Method: "POST", Path: "/indicators/series", Label: "DSL indicator series query"},
			{Method: "GET", Path: "/factors", Label: "Factor catalog"},
			{Method: "GET", Path: "/factors/bars", Label: "Factor time series"},
		},
	},
	{
		Title: "Fundamentals",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/fundamentals/factors", Label: "Fundamental factor catalog"},
			{Method: "GET", Path: "/fundamentals/series", Label: "Fundamental factor series"},
			{Method: "GET", Path: "/fundamentals/snapshot", Label: "Fundamental snapshot"},
			{Method: "GET", Path: "/fundamentals/panel", Label: "Fundamental panel"},
			{Method: "GET", Path: "/fundamentals/freshness", Label: "Fundamental freshness"},
		},
	},
	{
		Title: "Macro",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/macro/factors", Label: "Macro factor catalog"},
			{Method: "GET", Path: "/macro/series", Label: "Macro factor series"},
		},
	},
	{
		Title: "Feature Store Analytics",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/features/volatility-snapshot", Label: "Volatility snapshot"},
			{Method: "GET", Path: "/features/volatility-history", Label: "Volatility history"},
			{Method: "GET", Path: "/features/term-structure-snapshot", Label: "Term-structure snapshot"},
			{Method: "GET", Path: "/features/term-structure-history", Label: "Term-structure history"},
			{Method: "GET", Path: "/features/skew-snapshot", Label: "Skew snapshot"},
			{Method: "GET", Path: "/features/skew-history", Label: "Skew history"},
			{Method: "GET", Path: "/features/liquidity-snapshot", Label: "Liquidity snapshot"},
			{Method: "GET", Path: "/features/liquidity-history", Label: "Liquidity history"},
			{Method: "GET", Path: "/features/event-window-snapshot", Label: "Event-window snapshot"},
			{Method: "GET", Path: "/features/event-window-history", Label: "Event-window history"},
			{Method: "GET", Path: "/features/daily-feature-panel", Label: "Daily feature panel"},
		},
	},
	{
		Title: "US Stocks Market Data",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/markets/us-stocks/bars", Label: "US stock bars"},
			{Method: "GET", Path: "/markets/us-stocks/symbols", Label: "US stock symbols"},
			{Method: "POST", Path: "/markets/us-stocks/profiles", Label: "US stock company profiles"},
			{Method: "POST", Path: "/markets/us-stocks/fundamentals", Label: "US stock fundamental metrics"},
			{Method: "GET", Path: "/markets/us-stocks/splits", Label: "US stock split events"},
		},
	},
	{
		Title: "US Options Market Data",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/markets/us-options/bars", Label: "US option bars"},
			{Method: "GET", Path: "/markets/us-options/symbols", Label: "US option symbols"},
			{Method: "GET", Path: "/markets/us-options/greeks", Label: "US option greeks"},
			{Method: "GET", Path: "/markets/us-options/chain", Label: "US option chain"},
			{Method: "GET", Path: "/markets/us-options/wall", Label: "US option wall"},
		},
	},
	{
		Title: "Screeners",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/screener/underlyings", Label: "Underlying screener"},
			{Method: "GET", Path: "/screener/us-underlyings/turnover-intersection", Label: "US turnover intersection screener"},
			{Method: "GET", Path: "/screener/options", Label: "Option screener"},
		},
	},
	{
		Title: "Calendar",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/calendar/economic", Label: "Economic calendar"},
			{Method: "POST", Path: "/calendar/stocks", Label: "Stock calendar"},
		},
	},
}

var backtestSections = []sectionSpec{
	{
		Title: "回測流程 API",
		Endpoints: []endpointSpec{
			{Method: "POST", Path: "/backtests/validate", Label: "驗證回測請求"},
			{Method: "POST", Path: "/backtests/runs", Label: "發起非同步回測"},
			{Method: "GET", Path: "/backtests/runs/{runID}", Label: "查詢回測狀態與結果"},
			{Method: "GET", Path: "/backtests/runs/{runID}/events", Label: "串流回測進度事件"},
			{Method: "GET", Path: "/backtests/runs/{runID}/report", Label: "取得主要 HTML 報告"},
			{Method: "GET", Path: "/backtests/runs/{runID}/reports/{reportID}", Label: "取得指定 HTML 報告"},
		},
	},
	{
		Title: "策略目錄 API",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/strategies", Label: "列出已註冊策略"},
		},
	},
}

func main() {
	input := flag.String("input", "docs/swagger.json", "Path to Swagger JSON")
	output := flag.String("output", "docs/db-market-indicator-api.md", "Path to output Markdown")
	title := flag.String("title", "Database Market Data & Indicator API", "Markdown title")
	scope := flag.String("scope", "market", "Document scope: market or backtests")
	flag.Parse()

	doc, err := loadSwagger(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load swagger: %v\n", err)
		os.Exit(1)
	}

	config, err := renderConfig(*scope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scope: %v\n", err)
		os.Exit(1)
	}

	content, err := renderMarkdown(doc, *input, *title, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render markdown: %v\n", err)
		os.Exit(1)
	}

	content = strings.TrimRight(content, "\n") + "\n"
	if err := os.WriteFile(*output, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write markdown: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "wrote %s\n", *output)
}

type renderSpec struct {
	Sections    []sectionSpec
	Scope       string
	Kind        string
	SchemaIntro string
}

func renderConfig(scope string) (renderSpec, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "market", "markets", "db-market-indicator":
		return renderSpec{
			Sections:    marketSections,
			Kind:        "market",
			Scope:       "This document exports the database-backed market data, technical indicator, feature-store analytics, symbol-bound fundamentals, and screener APIs. It intentionally excludes external proxy endpoints such as Polygon, and also excludes backtest and other non-query operational endpoints.",
			SchemaIntro: "This section expands every request/response schema referenced by the endpoints above. Nested DTOs are included so clients can inspect the complete JSON shape without opening Swagger.",
		}, nil
	case "backtest", "backtests", "dsl":
		return renderSpec{
			Sections:    backtestSections,
			Kind:        "backtests",
			Scope:       "本文檔匯出策略回測與 DSL 工作流 API。內容包含請求驗證、非同步回測建立、狀態輪詢、SSE 進度串流、HTML 報告取得、策略目錄查詢，以及讀取完成回測結果所需的 response schema。API 摘要與欄位描述來自 Swagger 註釋；教學、範例與使用建議由本生成器模板輸出。",
			SchemaIntro: "本節展開上方 API 使用到的 request/response schema。巢狀 DTO 也會被納入，方便客戶不打開 Swagger 也能檢查完整 JSON 結構。",
		}, nil
	default:
		return renderSpec{}, fmt.Errorf("unknown scope %q", scope)
	}
}

func loadSwagger(path string) (*swaggerDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc swaggerDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func renderMarkdown(doc *swaggerDoc, inputPath string, title string, config renderSpec) (string, error) {
	var builder strings.Builder

	builder.WriteString("<!-- Code generated by cmd/api-docs-markdown; DO NOT EDIT. -->\n\n")
	builder.WriteString("> Note: This Markdown is generated by `go run ./cmd/api-docs-markdown`. Do not edit this file directly. Update the Swagger source or the generator, then rerun the command.\n\n")
	builder.WriteString("# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	builder.WriteString("- Source Swagger: `")
	builder.WriteString(filepath.ToSlash(inputPath))
	builder.WriteString("`\n")
	builder.WriteString("- API title: `")
	builder.WriteString(doc.Info.Title)
	builder.WriteString("`\n")
	builder.WriteString("- API version: `")
	builder.WriteString(doc.Info.Version)
	builder.WriteString("`\n")
	builder.WriteString("- Generated at: `")
	builder.WriteString(time.Now().UTC().Format(time.RFC3339))
	builder.WriteString("`\n\n")
	builder.WriteString("## Scope\n\n")
	builder.WriteString(config.Scope)
	builder.WriteString("\n\n")
	builder.WriteString("## Contents\n\n")
	if config.Kind == "backtests" {
		builder.WriteString("- [使用流程總覽](#使用流程總覽)\n")
		builder.WriteString("- [DSL 快速教學](#dsl-快速教學)\n")
		builder.WriteString("- [DSL 語法速查](#dsl-語法速查)\n")
		builder.WriteString("- [DSL 內建模組速查](#dsl-內建模組速查)\n")
		builder.WriteString("- [DSL 函數參考](#dsl-函數參考)\n")
		builder.WriteString("- [提交與查詢範例](#提交與查詢範例)\n")
	}
	for _, section := range config.Sections {
		builder.WriteString("- [")
		builder.WriteString(section.Title)
		builder.WriteString("](#")
		builder.WriteString(slug(section.Title))
		builder.WriteString(")\n")
	}
	referencedSchemas, err := collectReferencedSchemas(doc, config.Sections)
	if err != nil {
		return "", err
	}
	if len(referencedSchemas) > 0 {
		builder.WriteString("- [Schemas](#schemas)\n")
	}
	builder.WriteString("\n")
	if config.Kind == "backtests" {
		writeBacktestTutorial(&builder)
	}

	for _, section := range config.Sections {
		builder.WriteString("## ")
		builder.WriteString(section.Title)
		builder.WriteString("\n\n")
		for _, endpoint := range section.Endpoints {
			op, err := findOperation(doc, endpoint.Method, endpoint.Path)
			if err != nil {
				return "", err
			}
			writeEndpoint(&builder, doc, endpoint, op)
			if endpoint.Path == "/indicators/series" {
				writeIndicatorExamples(&builder)
			}
			if endpoint.Path == "/backtests/runs" {
				writeBacktestExamples(&builder)
			}
		}
	}

	writeSchemas(&builder, doc, referencedSchemas, config.SchemaIntro)

	return builder.String(), nil
}

func collectReferencedSchemas(doc *swaggerDoc, sections []sectionSpec) ([]string, error) {
	seen := map[string]bool{}
	var ordered []string
	for _, section := range sections {
		for _, endpoint := range section.Endpoints {
			op, err := findOperation(doc, endpoint.Method, endpoint.Path)
			if err != nil {
				return nil, err
			}
			for _, param := range op.Parameters {
				addSchemaRefs(doc, param.Schema, seen, &ordered)
				addSchemaRefs(doc, param.Items, seen, &ordered)
			}
			for _, resp := range op.Responses {
				addSchemaRefs(doc, resp.Schema, seen, &ordered)
			}
		}
	}
	sort.Strings(ordered)
	return ordered, nil
}

func writeBacktestExamples(builder *strings.Builder) {
	builder.WriteString("#### curl 範例：發起並輪詢內建 DSL 策略\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("accepted=$(curl -sS -X POST \"http://127.0.0.1:9010/api/v1/backtests/runs\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us\",\n")
	builder.WriteString("    \"asset\": \"SPY\",\n")
	builder.WriteString("    \"interval\": \"1d\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-12-31\",\n")
	builder.WriteString("    \"capital\": 100000,\n")
	builder.WriteString("    \"strategy\": \"golden-cross-dsl\"\n")
	builder.WriteString("  }')\n")
	builder.WriteString("\n")
	builder.WriteString("run_id=$(printf '%s' \"$accepted\" | jq -r '.run_id')\n")
	builder.WriteString("curl -sS \"http://127.0.0.1:9010/api/v1/backtests/runs/${run_id}\" | jq\n")
	builder.WriteString("```")
	builder.WriteString("\n\n")
	builder.WriteString("#### curl 範例：取得完成後的 HTML 報告\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS \"http://127.0.0.1:9010/api/v1/backtests/runs/${run_id}/report\" -o report.html\n")
	builder.WriteString("curl -sS \"http://127.0.0.1:9010/api/v1/backtests/runs/${run_id}/reports/overview\" -o overview.html\n")
	builder.WriteString("```")
	builder.WriteString("\n\n")
	builder.WriteString("#### curl 範例：串流即時進度事件\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -N \"http://127.0.0.1:9010/api/v1/backtests/runs/${run_id}/events\"\n")
	builder.WriteString("```")
	builder.WriteString("\n\n")
}

func writeBacktestTutorial(builder *strings.Builder) {
	builder.WriteString("## 使用流程總覽\n\n")
	builder.WriteString("建議客戶端採用以下流程串接 DSL 回測：\n\n")
	builder.WriteString("1. 呼叫 `GET /api/v1/strategies` 取得可用內建策略與分類。\n")
	builder.WriteString("2. 如果使用自訂 DSL，先呼叫 `POST /api/v1/backtests/validate`，檢查語法、參數 schema、profile、期權鏈需求、資料可用性與 diagnostics。\n")
	builder.WriteString("3. 呼叫 `POST /api/v1/backtests/runs` 建立非同步回測。回應會包含 `run_id`、`status_url`、`events_url`、`report_url`。\n")
	builder.WriteString("4. 使用 `GET /api/v1/backtests/runs/{runID}` 輪詢狀態。完成後在 `result.summaries` 讀取核心績效結果。\n")
	builder.WriteString("5. 若需要即時進度，使用 `GET /api/v1/backtests/runs/{runID}/events` 讀取 SSE。斷線重連後仍應再查一次 status endpoint。\n")
	builder.WriteString("6. 若需要視覺化報告，回測完成後呼叫 `/report` 或 `/reports/{reportID}` 取得 HTML。\n\n")
	builder.WriteString("`StrategyBacktestRunStatus.status` 常見狀態包含 `pending`、`running`、`completed`、`failed`。當 `status=completed` 時，`result.summaries` 會包含 `final_equity`、`total_return`、`max_drawdown`、`total_trades`、`win_rate`、`report_url` 等欄位；當 `status=failed` 時，請讀取 `error`。\n\n")

	builder.WriteString("## DSL 快速教學\n\n")
	builder.WriteString("Toktik DSL 是接近 Pine Script v6 風格的策略語言，並接到 Toktik 回測引擎。建議使用 `//@version=6`、`strategy(...)`、4 空格縮排區塊，以及 `input.*` 宣告可調參數。\n\n")
	builder.WriteString("```pine\n")
	builder.WriteString("//@version=6\n")
	builder.WriteString("strategy(\"Golden Cross\")\n\n")
	builder.WriteString("fast_len = input.int(10, title=\"Fast SMA\")\n")
	builder.WriteString("slow_len = input.int(50, title=\"Slow SMA\")\n")
	builder.WriteString("position_pct = input.float(0.95, title=\"Position %\", minval=0.01, maxval=1.0)\n\n")
	builder.WriteString("fast = ta.sma(close, fast_len)\n")
	builder.WriteString("slow = ta.sma(close, slow_len)\n\n")
	builder.WriteString("plot(fast, title=\"Fast\")\n")
	builder.WriteString("plot(slow, title=\"Slow\")\n\n")
	builder.WriteString("if ta.crossover(fast, slow) and strategy.position_size == 0\n")
	builder.WriteString("    budget = math.min(strategy.cash, strategy.equity * position_pct)\n")
	builder.WriteString("    qty = budget / close\n")
	builder.WriteString("    if qty > 0\n")
	builder.WriteString("        strategy.entry(id=\"long\", direction=strategy.long, qty=qty)\n\n")
	builder.WriteString("if ta.crossunder(fast, slow) and strategy.position_size > 0\n")
	builder.WriteString("    strategy.close(id=\"long\")\n")
	builder.WriteString("```\n\n")
	builder.WriteString("`input.*` 會被 validate API 解析為參數 schema。提交回測時，可用 `dsl_params` 透過 input 的 `title` 或變數名覆蓋預設值。\n\n")

	builder.WriteString("## DSL 語法速查\n\n")
	builder.WriteString("### 腳本結構\n\n")
	builder.WriteString("```pine\n")
	builder.WriteString("//@version=6\n")
	builder.WriteString("strategy(\"Strategy Name\")\n\n")
	builder.WriteString("// inputs\n")
	builder.WriteString("// calculations\n")
	builder.WriteString("// orders\n")
	builder.WriteString("```\n\n")
	builder.WriteString("`//@version=6` 目前作為註釋處理，用於讓交付文件和 Pine v6 風格一致。支援 `strategy(...)`、`indicator(...)`、`library(...)` 宣告。\n\n")
	builder.WriteString("### 縮排區塊\n\n")
	builder.WriteString("```pine\n")
	builder.WriteString("if close > open\n")
	builder.WriteString("    signal = 1\n")
	builder.WriteString("else\n")
	builder.WriteString("    signal = 0\n")
	builder.WriteString("```\n\n")
	builder.WriteString("縮排可使用空格或 tab。為了交付一致，建議使用 4 個空格。舊版 `{ ... }` 區塊仍可解析，但新策略建議使用縮排風格。\n\n")
	builder.WriteString("### 變數與狀態\n\n")
	builder.WriteString("- `x = expr`：每根 bar 重新求值。\n")
	builder.WriteString("- `var x = expr`：第一次初始化後跨 bar 保留。\n")
	builder.WriteString("- `varip x = expr`：跨 bar 保留，並允許在 bar 內更新。\n")
	builder.WriteString("- `x := expr`、`x += expr`、`x++`：更新既有變數。\n")
	builder.WriteString("- `close[1]`：上一根 bar；`close[0]`：當前 bar。\n\n")
	builder.WriteString("系統會自動注入 `open`、`high`、`low`、`close`、`volume`、`bar_index`。普通數值變數也會維護 series 歷史，因此可作為 `ta.*` 或 `alpha.*` 的輸入。\n\n")
	builder.WriteString("### 常用語法\n\n")
	builder.WriteString("```pine\n")
	builder.WriteString("direction = close > open ? 1 : -1\n\n")
	builder.WriteString("for i = 1 to 5\n")
	builder.WriteString("    sum += i\n\n")
	builder.WriteString("for item in [1, 2, 3]\n")
	builder.WriteString("    sum += item\n\n")
	builder.WriteString("switch direction\n")
	builder.WriteString("1 =>\n")
	builder.WriteString("    regime = \"long\"\n")
	builder.WriteString("-1 =>\n")
	builder.WriteString("    regime = \"short\"\n")
	builder.WriteString("else =>\n")
	builder.WriteString("    regime = \"flat\"\n\n")
	builder.WriteString("double(float x) => x * 2\n\n")
	builder.WriteString("arr = [10, 20, 30]\n")
	builder.WriteString("first = arr[0]\n")
	builder.WriteString("[a, b] = [1, 2]\n")
	builder.WriteString("```\n\n")

	builder.WriteString("## DSL 內建模組速查\n\n")
	builder.WriteString("### 交易與輸出\n\n")
	builder.WriteString("- `plot(series, title, overlay, precision)`：輸出報告/指標序列。\n")
	builder.WriteString("- `strategy.entry(id, direction, qty, limit, stop, twap_bars, immediate, note)`\n")
	builder.WriteString("- `strategy.close(id)`、`strategy.exit(id)`、`buy(qty)`、`sell(qty)`\n")
	builder.WriteString("- `strategy.position_size`、`strategy.position_avg_price`、`strategy.equity`、`strategy.cash`\n")
	builder.WriteString("- `order.market`、`order.limit`、`order.stop`、`order.stop_limit`、`order.twap`、`order.submit`\n\n")
	builder.WriteString("### 指標、數學與字串\n\n")
	builder.WriteString("- `ta.sma`、`ta.ema`、`ta.rsi`、`ta.atr`、`ta.highest`、`ta.lowest`、`ta.stdev`、`ta.cci`\n")
	builder.WriteString("- `ta.crossover`、`ta.crossunder`、`ta.change`、`ta.cum`、`ta.wma`、`ta.bb`、`ta.barssince`、`ta.valuewhen`、`ta.percentrank`\n")
	builder.WriteString("- `math.abs`、`math.floor`、`math.ceil`、`math.round`、`math.sqrt`、`math.pow`、`math.max`、`math.min`、`math.avg`、`nz`、`na`、`len`\n")
	builder.WriteString("- `str.contains`、`str.length`、`str.upper`、`str.lower`、`str.split`、`str.join`、`str.tostring`、`str.format`\n\n")
	builder.WriteString("### 因子、組合與配置\n\n")
	builder.WriteString("- `request.security(market, symbol, interval, field)`、`request.factor(name, interval, field)`\n")
	builder.WriteString("- `alpha.rank`、`alpha.zscore`、`alpha.decay_linear`、`alpha.ts_rank`、`alpha.ts_corr`、`alpha.ts_delta`、`alpha.ts_mean`、`alpha.log_return` 等時序因子函數\n")
	builder.WriteString("- `portfolio.symbols()`、`portfolio.weights()`、`portfolio.items()`、`portfolio.weight(symbol, defval)`\n")
	builder.WriteString("- `config.get(name, defval)`、`config.string(name, defval)`、`ref.set/get/has/clear/inc/dec`\n\n")
	builder.WriteString("### 期權與價差\n\n")
	builder.WriteString("- `options.chain()`、`options.chain(market, symbol)`、`options.calls`、`options.puts`\n")
	builder.WriteString("- `options.expiry_range`、`options.delta_range`、`options.min_premium`、`options.strike_range`、`options.sort_by_delta`\n")
	builder.WriteString("- `contract.symbol`、`contract.underlying`、`contract.strike`、`contract.expiry`、`contract.dte`、`contract.delta`、`contract.bid`、`contract.ask`、`contract.mark`\n")
	builder.WriteString("- `leg.buy`、`leg.sell`、`spread.open_on`、`spread.close`、`spread.pnl`、`spread.leg_contract`、`spread.count`\n")
	builder.WriteString("- `group.open`、`group.close`、`group.add_spread`、`schedule.close_spread`、`schedule.close_group`\n\n")
	builder.WriteString("### 外部信號\n\n")
	builder.WriteString("- `signal.active`、`signal.count`、`signal.direction`、`signal.action`、`signal.qty`、`signal.name`、`signal.consume`\n")
	builder.WriteString("- `event.pending`、`event.peek`、`event.next`、`event.consume_all`、`event.is_init`、`event.is_add`、`event.is_close`、`event.is_roll`\n\n")
	writeDSLFunctionReference(builder)

	builder.WriteString("## 提交與查詢範例\n\n")
	builder.WriteString("### 驗證自訂 DSL 並讀取參數 schema\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://127.0.0.1:9010/api/v1/backtests/validate\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us\",\n")
	builder.WriteString("    \"asset\": \"SPY\",\n")
	builder.WriteString("    \"interval\": \"1d\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-03-31\",\n")
	builder.WriteString("    \"capital\": 100000,\n")
	builder.WriteString("    \"dsl\": \"//@version=6\\nstrategy(\\\"Demo\\\")\\nlength = input.int(20, title=\\\"Length\\\")\\nplot(ta.sma(close, length), title=\\\"SMA\\\")\"\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("### 使用 `dsl_params` 覆蓋輸入參數\n\n")
	builder.WriteString("```json\n")
	builder.WriteString("{\n")
	builder.WriteString("  \"dsl_params\": {\n")
	builder.WriteString("    \"Length\": 30\n")
	builder.WriteString("  }\n")
	builder.WriteString("}\n")
	builder.WriteString("```\n\n")
	builder.WriteString("### 查詢結果重點欄位\n\n")
	builder.WriteString("完成後呼叫 `GET /api/v1/backtests/runs/{runID}`。常用欄位：\n\n")
	builder.WriteString("- `status`：`completed` 表示完成，`failed` 表示失敗。\n")
	builder.WriteString("- `progress`：執行中可讀取 `phase`、`percent`、`message`。\n")
	builder.WriteString("- `result.summaries[]`：每個策略的績效摘要。\n")
	builder.WriteString("- `result.summaries[].final_equity`：期末資產。\n")
	builder.WriteString("- `result.summaries[].total_return`：總報酬。\n")
	builder.WriteString("- `result.summaries[].max_drawdown`：最大回撤。\n")
	builder.WriteString("- `result.summaries[].total_trades`：交易次數。\n")
	builder.WriteString("- `result.summaries[].report_url`：單策略 HTML 報告 URL。\n")
	builder.WriteString("- `result.overview_report_url`：總覽 HTML 報告 URL。\n\n")
	builder.WriteString("### 限制與注意事項\n\n")
	builder.WriteString("Toktik DSL 不是完整 TradingView Pine Script v6 實作；語法風格接近 Pine，但內建函數和交易模型以 Toktik 回測引擎為準。型別標註目前主要用於相容和可讀性，runtime 仍採動態值模型。期權、合約、spread、group 是 handle，應透過內建函數操作。`request.*` 和 `options.chain()` 若使用動態字串，分析器可能無法完整預先枚舉依賴。\n\n")
}

func writeDSLFunctionReference(builder *strings.Builder) {
	docs := runtime.BuiltinDocs(runtime.ProfileBacktest)
	grouped := make(map[string][]runtime.BuiltinDoc)
	var namespaces []string
	for _, doc := range docs {
		ns := docNamespace(doc.Name)
		if _, ok := grouped[ns]; !ok {
			namespaces = append(namespaces, ns)
		}
		grouped[ns] = append(grouped[ns], doc)
	}
	sort.Strings(namespaces)

	builder.WriteString("## DSL 函數參考\n\n")
	builder.WriteString("本節由 DSL runtime 的 builtin 註冊表自動產生；新增函數只要註冊到 backtest profile，就會出現在這裡。`Example` 欄提供可直接放進 DSL 腳本的最小調用形態。\n\n")
	builder.WriteString("| 模組 | 數量 |\n")
	builder.WriteString("| --- | --- |\n")
	for _, ns := range namespaces {
		builder.WriteString("| `")
		builder.WriteString(ns)
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", len(grouped[ns])))
		builder.WriteString(" |\n")
	}
	builder.WriteString("\n")

	for _, ns := range namespaces {
		builder.WriteString("### ")
		builder.WriteString(ns)
		builder.WriteString("\n\n")
		builder.WriteString("| 名稱 | 簽名 | 種類 | 回傳 | 範例 | 用途 |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
		for _, doc := range grouped[ns] {
			builder.WriteString("| `")
			builder.WriteString(doc.Name)
			builder.WriteString("` | `")
			builder.WriteString(builtinSignature(doc))
			builder.WriteString("` | `")
			builder.WriteString(builtinKindLabel(doc.Kind))
			builder.WriteString("` | `")
			builder.WriteString(escapePipes(doc.ReturnValue))
			builder.WriteString("` | `")
			builder.WriteString(escapePipes(doc.Example))
			builder.WriteString("` | ")
			builder.WriteString(escapePipes(doc.Summary))
			builder.WriteString(" |\n")
		}
		builder.WriteString("\n")
	}
}

func docNamespace(name string) string {
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		return name[:idx]
	}
	return "core"
}

func builtinSignature(doc runtime.BuiltinDoc) string {
	if doc.Kind == runtime.BuiltinProperty || doc.Kind == runtime.BuiltinConstant {
		return doc.Name
	}
	if len(doc.Params) == 0 {
		return doc.Name + "()"
	}
	return doc.Name + "(" + strings.Join(doc.Params, ", ") + ")"
}

func builtinKindLabel(kind runtime.BuiltinKind) string {
	switch kind {
	case runtime.BuiltinFunction:
		return "函數"
	case runtime.BuiltinProperty:
		return "屬性"
	case runtime.BuiltinConstant:
		return "常數"
	default:
		return string(kind)
	}
}

func addSchemaRefs(doc *swaggerDoc, schema *schemaRef, seen map[string]bool, ordered *[]string) {
	if schema == nil {
		return
	}
	if schema.Ref != "" {
		name := refName(schema.Ref)
		if !seen[name] {
			seen[name] = true
			*ordered = append(*ordered, name)
			definition := doc.Definitions[name]
			addSchemaRefs(doc, &definition, seen, ordered)
		}
		return
	}
	addSchemaRefs(doc, schema.Items, seen, ordered)
	if additional := additionalPropertiesSchema(schema); additional != nil {
		addSchemaRefs(doc, additional, seen, ordered)
	}
	for _, prop := range schema.Properties {
		prop := prop
		addSchemaRefs(doc, &prop, seen, ordered)
	}
}

func findOperation(doc *swaggerDoc, method string, path string) (*operation, error) {
	pathItem, ok := doc.Paths[path]
	if !ok {
		return nil, fmt.Errorf("path %s not found in swagger", path)
	}
	op, ok := pathItem[strings.ToLower(method)]
	if !ok {
		return nil, fmt.Errorf("method %s %s not found in swagger", method, path)
	}
	return &op, nil
}

func writeEndpoint(builder *strings.Builder, doc *swaggerDoc, spec endpointSpec, op *operation) {
	builder.WriteString("### ")
	builder.WriteString(spec.Label)
	builder.WriteString("\n\n")
	builder.WriteString("- Endpoint: `")
	builder.WriteString(strings.ToUpper(spec.Method))
	builder.WriteString(" ")
	builder.WriteString(joinBasePath(doc.BasePath, spec.Path))
	builder.WriteString("`\n")
	if len(op.Tags) > 0 {
		builder.WriteString("- Tags: `")
		builder.WriteString(strings.Join(op.Tags, "`, `"))
		builder.WriteString("`\n")
	}
	if op.Deprecated {
		builder.WriteString("- Deprecated: `true`\n")
	}
	if len(op.Consumes) > 0 {
		builder.WriteString("- Consumes: `")
		builder.WriteString(strings.Join(op.Consumes, "`, `"))
		builder.WriteString("`\n")
	}
	if len(op.Produces) > 0 {
		builder.WriteString("- Produces: `")
		builder.WriteString(strings.Join(op.Produces, "`, `"))
		builder.WriteString("`\n")
	}
	builder.WriteString("- Summary: ")
	builder.WriteString(op.Summary)
	builder.WriteString("\n")
	if strings.TrimSpace(op.Description) != "" {
		builder.WriteString("- Description: ")
		builder.WriteString(op.Description)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")

	builder.WriteString("#### Parameters\n\n")
	if len(op.Parameters) == 0 {
		builder.WriteString("No parameters.\n\n")
	} else {
		builder.WriteString("| Name | In | Type | Required | Description |\n")
		builder.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, param := range op.Parameters {
			builder.WriteString("| ")
			builder.WriteString(escapePipes(param.Name))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(param.In))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(parameterType(doc, param)))
			builder.WriteString(" | ")
			builder.WriteString(boolWord(param.Required))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(strings.TrimSpace(param.Description)))
			builder.WriteString(" |\n")
		}
		builder.WriteString("\n")
	}

	builder.WriteString("#### Responses\n\n")
	builder.WriteString("| Status | Schema | Description |\n")
	builder.WriteString("| --- | --- | --- |\n")
	for _, code := range sortedResponseCodes(op.Responses) {
		resp := op.Responses[code]
		builder.WriteString("| ")
		builder.WriteString(code)
		builder.WriteString(" | ")
		builder.WriteString(escapePipes(schemaTypeMarkdown(doc, resp.Schema)))
		builder.WriteString(" | ")
		builder.WriteString(escapePipes(strings.TrimSpace(resp.Description)))
		builder.WriteString(" |\n")
	}
	builder.WriteString("\n")
}

func writeSchemas(builder *strings.Builder, doc *swaggerDoc, schemaNames []string, intro string) {
	if len(schemaNames) == 0 {
		return
	}
	builder.WriteString("## Schemas\n\n")
	builder.WriteString(intro)
	builder.WriteString("\n\n")
	for _, name := range schemaNames {
		definition, ok := doc.Definitions[name]
		if !ok {
			continue
		}
		builder.WriteString("### ")
		builder.WriteString(schemaDisplayName(name))
		builder.WriteString("\n\n")
		builder.WriteString("- Schema: `")
		builder.WriteString(name)
		builder.WriteString("`\n")
		if strings.TrimSpace(definition.Description) != "" {
			builder.WriteString("- Description: ")
			builder.WriteString(strings.TrimSpace(definition.Description))
			builder.WriteString("\n")
		}
		builder.WriteString("- Type: `")
		builder.WriteString(schemaTypePlain(&definition))
		builder.WriteString("`\n\n")
		if len(definition.Properties) == 0 {
			builder.WriteString("No documented properties.\n\n")
			continue
		}
		builder.WriteString("| Field | Type | Required | Description |\n")
		builder.WriteString("| --- | --- | --- | --- |\n")
		for _, field := range sortedPropertyNames(definition.Properties) {
			prop := definition.Properties[field]
			builder.WriteString("| ")
			builder.WriteString(escapePipes(field))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(schemaTypeMarkdown(doc, &prop)))
			builder.WriteString(" | ")
			builder.WriteString(boolWord(isRequired(field, definition.Required)))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(strings.TrimSpace(prop.Description)))
			builder.WriteString(" |\n")
		}
		builder.WriteString("\n")
	}
}

func writeIndicatorExamples(builder *strings.Builder) {
	builder.WriteString("#### curl Example: Preset catalog\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS \"http://192.168.1.9:9010/api/v1/indicators/presets\" | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Built-in presets[]\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us-stocks\",\n")
	builder.WriteString("    \"symbol\": \"AAPL\",\n")
	builder.WriteString("    \"interval\": \"1h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-02-01\",\n")
	builder.WriteString("    \"presets\": [\"classic-volatility\", \"classic-momentum\"],\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Simplified indicators[]\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us-stocks\",\n")
	builder.WriteString("    \"symbol\": \"AAPL\",\n")
	builder.WriteString("    \"interval\": \"1h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-02-01\",\n")
	builder.WriteString("    \"indicators\": [\"ta.sma(close,5)\", \"ta.rsi(close,10)\"],\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Presets + custom expressions\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"crypto-spot\",\n")
	builder.WriteString("    \"symbol\": \"BTCUSDT\",\n")
	builder.WriteString("    \"interval\": \"4h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-03-01\",\n")
	builder.WriteString("    \"presets\": [\"classic-moving-averages\"],\n")
	builder.WriteString("    \"indicators\": [\"ta.percentrank(ta.rsi(close,14),20)\", \"ta.change(volume,5)\"],\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Legacy DSL\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us-stocks\",\n")
	builder.WriteString("    \"symbol\": \"AAPL\",\n")
	builder.WriteString("    \"interval\": \"1h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-02-01\",\n")
	builder.WriteString("    \"dsl\": \"plot(close, title=\\\"Close\\\")\\nplot(ta.sma(close,5), title=\\\"SMA\\\")\",\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
}

func joinBasePath(basePath string, path string) string {
	base := strings.TrimSuffix(basePath, "/")
	if base == "" {
		return path
	}
	return base + path
}

func parameterType(doc *swaggerDoc, param parameter) string {
	if param.Schema != nil {
		return schemaTypeMarkdown(doc, param.Schema)
	}
	if param.Type != "" {
		if param.Type == "array" && param.Items != nil {
			return "array<" + schemaTypeMarkdown(doc, param.Items) + ">"
		}
		return param.Type
	}
	return "object"
}

func schemaTypeMarkdown(doc *swaggerDoc, schema *schemaRef) string {
	if schema == nil {
		return "-"
	}
	if schema.Ref != "" {
		name := refName(schema.Ref)
		if _, ok := doc.Definitions[name]; ok {
			return "[" + schemaDisplayName(name) + "](#" + slug(schemaDisplayName(name)) + ")"
		}
		return name
	}
	if schema.Type == "array" && schema.Items != nil {
		return "array<" + schemaTypeMarkdown(doc, schema.Items) + ">"
	}
	if additional := additionalPropertiesSchema(schema); additional != nil {
		return "map<string," + schemaTypeMarkdown(doc, additional) + ">"
	}
	if schema.Type != "" {
		return schema.Type + schemaFormatSuffix(schema.Format)
	}
	return "object"
}

func schemaTypePlain(schema *schemaRef) string {
	if schema == nil {
		return "-"
	}
	if schema.Ref != "" {
		return refName(schema.Ref)
	}
	if schema.Type == "array" && schema.Items != nil {
		return "array<" + schemaTypePlain(schema.Items) + ">"
	}
	if additional := additionalPropertiesSchema(schema); additional != nil {
		return "map<string," + schemaTypePlain(additional) + ">"
	}
	if schema.Type != "" {
		return schema.Type + schemaFormatSuffix(schema.Format)
	}
	return "object"
}

func schemaFormatSuffix(format string) string {
	if format == "" {
		return ""
	}
	return "(" + format + ")"
}

func refName(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func schemaDisplayName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx < len(name)-1 {
		return name[idx+1:]
	}
	return name
}

func additionalPropertiesSchema(schema *schemaRef) *schemaRef {
	if schema == nil || len(schema.AdditionalProperties) == 0 || string(schema.AdditionalProperties) == "true" || string(schema.AdditionalProperties) == "false" {
		return nil
	}
	var additional schemaRef
	if err := json.Unmarshal(schema.AdditionalProperties, &additional); err != nil {
		return nil
	}
	return &additional
}

func sortedPropertyNames(properties map[string]schemaRef) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isRequired(field string, required []string) bool {
	for _, name := range required {
		if name == field {
			return true
		}
	}
	return false
}

func sortedResponseCodes(responses map[string]response) []string {
	codes := make([]string, 0, len(responses))
	for code := range responses {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool {
		return codes[i] < codes[j]
	})
	return codes
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func escapePipes(value string) string {
	if value == "" {
		return "-"
	}
	return strings.ReplaceAll(value, "|", "\\|")
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "&", "and")
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r):
			builder.WriteRune(r)
			lastDash = false
		case unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
