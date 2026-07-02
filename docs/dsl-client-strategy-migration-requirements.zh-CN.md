# 客户端选股选权逻辑迁移至 DSL 需求文档

## 背景与当前目标

当前目标是把 consumer 侧的美股选股、选权和期权策略执行逻辑迁移到 Toktik 的 DSL 与回测通用接口。迁移完成后，客户端不再拼装大量专用 API 调用和本地筛选逻辑，而是提交 DSL 脚本、参数和必要的 universe code，由后端完成股票池展开、因子读取、期权策略筛选、期权选腿、回测执行和过程追踪。

本阶段不是一次性建设完整策略平台，而是先完成扎实可靠的底座能力，并用三类策略做粗略验证：

- 强势波段：优先验证通用热门股票池、技术指标、波动/趋势规则、期权策略筛选、选腿和回测下单闭环。
- 价值配置：优先验证外部持仓类 universe 的接口、估值分位、价值策略规则和选腿闭环；短期可用手工或 mock 持仓数据替代真实 SEC-13F / 大 V 数据源。
- 指数期权：仅覆盖 `SPY` 与 `QQQ`，优先验证指数估值、IV/HV、趋势兜底和风险闸门。

三大策略以 DSL 脚本交付。平台不把它们固化为内置策略模板，只内置可复用 primitives：universe 访问、factor 读取、环境分类、期权策略筛选、期权选腿、资源限制和校验。

## 设计原则

1. **先跑通闭环，再平台化**：Phase 1 只做足以支撑可执行回测的纵向切片，复杂的通用 factor 物化、DSL 生成 universe、多 provider 治理延后。
2. **复用现有 backtest API**：继续使用 `POST /api/v1/backtests/validate`、`POST /api/v1/backtests/runs`、run status、SSE 和报告接口，不新增独立推荐工作流。
3. **策略在 DSL，确定性能力在 Go**：DSL 负责组合参数、排序、过滤和交易决策；Go builtin 负责股票池展开、批量数据访问、期权策略候选、选腿、拓扑校验和资源保护。
4. **结果允许偏差，口径必须可解释**：第一版不追求完全复刻 consumer 或 `../options-today/backend`，但每个跳过项、替代数据源和策略匹配原因都要有 trace 或 warning。
5. **Redis 只做加速，不做事实来源**：随时间变化的 universe membership 需要可追溯、可重建；Redis 仅作为热点缓存。

## 当前代码基础

### 已具备能力

- `PortfolioBacktestService` 已支持动态 DSL 解析、manifest 提取、参数校验、行情预加载、期权链预加载、run status、SSE 和报告输出。
- DSL runtime 已支持 `request.security`、`request.factor`、`request.fundamental`、`portfolio.symbols()`、`portfolio.items()` 等基础能力。
- 期权 DSL 已支持 `options.chain`、`options.calls`、`options.puts`、`options.expiry_range`、`options.delta_range`、`options.sort_by_delta`、`options.best_spread`、`contract.*`、`spread.*` 等底层选链和开平仓能力。
- 美股 screener 已有 `ScreenUSTurnoverIntersection`；observed US stock pool 当前使用 20、60、120 日 turnover intersection top 60 并集。
- US options chain provider 已有预计算 chain cache 优先、bar table 回退的路径。
- feature volatility factor feed 已能通过 `request.factor("volatility", "1d", field)` 读取 `iv`、`hv30`、`iv_percentile`、`iv_rank` 等字段。
- fundamentals / macro observation 已覆盖部分 symbol-bound 与 macro 数据，可作为 PE、PB、VIX、DVOL、Shiller/PE 等因子的优先复用层。

### 当前缺口

- 没有通用 universe 模块，无法把“股票池定义、物化、查询、刷新、回测展开”作为可复用底座。
- DSL 对动态 `request.security`、动态 `options.chain` 的预加载依赖较强。当前如果 DSL 动态请求期权链，仍需要调用方提供 `symbols` 或 `portfolio`，否则无法安全预加载。
- 期权链大规模调用缺少面向 DSL consumer 的资源计划、限额、统计和拒绝策略。
- 没有内置“期权策略筛选”primitive，无法统一输出某个标的适合哪些策略及其原因。
- 没有内置“期权选腿”primitive，复杂价差、跨式、铁鹰、日历等组合仍需要 DSL 逐个拼装，难以保证拓扑和价格约束。
- SEC-13F、大 V 持仓、World PE Ratio、BTC NVT 等外部数据源尚未实现，短期只能预留接口或使用手工数据。

## 本阶段交付范围

### 必须完成

- 最小 universe 模块：支持定义、刷新/重建任务、membership 查询和 backtest run 展开。
- `turnover_intersection_union` universe：固定由 7、20、60、120 日 `ScreenUSTurnoverIntersection` top N 非 ETF 标的合并去重生成。
- 7 日 lookback 加入 observed pool 与 Redis 暖缓存逻辑，使缓存口径与新 universe 一致。
- `preset_symbols` universe：支持 YAML 或请求参数提供基础预设标的，并能与热门股票池合并去重。
- `provider_holdings` 最小接口：支持手工或 mock snapshot，先用于价值配置验证，不要求真实 SEC-13F / 大 V 抓取。
- DSL/backtest 能读取 universe symbols，或在 run resolve 阶段把 universe 展开为 `symbols` / `portfolio.symbols()`。
- 内置市场环境分类 primitive：输出趋势、HV 状态、IV 状态、估值状态、scenario label、reason codes 和 warning。
- 内置期权策略筛选 primitive：根据估值、IV、HV、趋势、RSI、CCI 等上下文返回候选策略及原因。
- 内置期权选腿 primitive：按策略名和约束返回标准化 legs、组合指标、warning 和失败原因。
- validate/dry-run 返回资源计划：universe size、期权链 underlyings 数、时间范围、DTE 范围、预估合约数和风险提示。
- run trace 记录候选池、过滤原因、策略匹配、选腿结果、跳过原因、链加载数量和缓存命中情况。

### 明确延后

- 通用 factor materialization 控制面。
- 用户通过 DSL 创建长期 universe。
- 完整 SEC-13F / 大 V / World PE Ratio / Blockchain.info provider。
- 新闻、事件、财报窗口等外部上下文。
- 完整 RBAC、多租户配额和复杂队列治理。
- 追求与 `../options-today/backend` 完全一致的策略得分、事件过滤和流动性过滤。

## 核心底座需求

### Universe 模块

建议新增 `internal/universe` 或同等职责模块，作为股票池定义、物化、查询与维护层。

最小模型：

- `universe_definition`：`code`、`market`、`source_type`、`parameters`、`version`、`active`、`created_at`、`updated_at`。
- `universe_membership`：`universe_code`、`market`、`symbol`、`valid_from`、`valid_to`、`score`、`rank`、`source_run_id`、`metadata`。
- `universe_run`：`definition_code`、`version`、`status`、`from`、`to`、`params_hash`、`idempotency_key`、`stats`、`error`。

存储建议：

- MySQL 存 definition、run、版本和调度元数据。
- ClickHouse 存 membership 时间片段，适合按日期范围和 symbol 扫描。
- Redis 只缓存热点查询结果，不作为事实来源。

membership 查询粒度始终为日频，按 `valid_from <= as_of < valid_to` 查询。如果每日 membership 未变化，应延长上一段 `valid_to`，避免重复写每日快照。

第一批 universe 类型：

- `turnover_intersection_union`：7、20、60、120 日 turnover intersection 并集，默认 `NonETFOnly=true`，默认 top limit 可沿用 observed pool 的 60，策略参数可覆盖。
- `preset_symbols`：配置或请求传入的基础标的。
- `provider_holdings`：手工或 mock 持仓快照；真实外部 provider 延后。

同族去重先作为可配置规则实现，第一版只需覆盖明确家族，例如 `SPY/VOO/SPX`、`QQQ/QQQM`、`IBIT` 相关替代物；无法识别的标的不做推断。

### Factor 与环境分类

本轮优先复用现有 factor feed、fundamental observation、macro observation 和 feature snapshot，不新增通用 factor 物化控制面。需要补齐的是“策略可直接使用的聚合语义”，而不是新的大平台。

环境分类 primitive 输入：

- `market`、`symbol`、`as_of`。
- 可选显式上下文：`rsi14`、`cci20`、`hv30`、`hv_percentile`、`iv`、`iv_percentile`、`valuation_percentile`、`price`。
- 缺省字段可由 primitive 从现有行情、factor 或 fundamental feed 读取；无法读取时返回 warning。

环境分类 primitive 输出：

- `trend_state`：`up`、`down`、`range`、`unknown`。
- `hv_state`：`high`、`mid`、`low`、`unknown`。
- `iv_state`：`high`、`mid`、`low`、`unknown`。
- `valuation_state`：`undervalued`、`fair`、`overvalued`、`unknown`。
- `scenario_label`、`risk_label`、`reason_codes`、`warnings`。

规则口径：

- RSI：`> 60` 为上行，`< 40` 为下行，`40 <= RSI <= 60` 为震荡，缺失为 unknown。
- HV30 分位：`>= 70` 为高，`>= 35 && < 70` 为中，`< 35` 为低。
- IV 分位：`>= 70` 为高，`>= 35 && < 70` 为中，`< 35` 为低。
- 估值分位：`< 35` 为低估，`35-80` 为合理，`>= 80` 为高估。
- 极端 IV：`IV 分位 >= 88` 且趋势上行为“高 IV 融涨 / 亢奋”；`IV 分位 >= 88` 且非上行为“恐慌抛售”；`IV 分位 <= 30` 且高估为“高位低波”；`IV 分位 <= 30` 且非高估为“低波突破前”。

估值来源优先级：

- `SPY`：Shiller PE / PE10。
- `QQQ`：Nasdaq 100 PE 或现有虚拟 fundamental。
- `IBIT`：BTC NVT 或加密估值代理，数据源未接入前可返回 unknown。
- 其他：PE/PB 历史分位，缺失时用价格历史分位兜底。

分位窗口第一版按参数传入并保持回测/线上一致即可：估值默认 900 自然日，VIX 默认 2000 自然日，DVOL 默认 1500 自然日，HV30 默认 80 日。若实现改用交易日窗口，必须在 trace 中记录实际窗口。

### 期权策略筛选 primitive

建议提供 `options.strategies(...)` 或等价 DSL builtin，用于回答“该标的当前可考虑哪些期权策略”。

输入：

- `market`、`symbol`、`as_of`。
- 环境分类结果或显式上下文。
- DTE、delta、premium、spread、最大 legs 等约束。
- 可选策略 family，例如 `value`、`trend`、`index`。

输出：

- 策略名称列表，例如 `SELL_PUT`、`COVERED_CALL`、`BUY_CALL`、`BUY_PUT`、`SELL_CALL`、`BEAR_CALL_SPREAD`、`BULL_CALL_SPREAD`、`BULL_PUT_SPREAD`、`SHORT_STRANGLE`、`IRON_CONDOR`、`BUY_STRADDLE`、`BUY_STRADDLE_SKEW`、`CALENDAR_SPREAD`。
- 每个策略的 `score`、`risk_label`、`reason_codes`、`warnings`、`rejected_reasons`。

第一版只要求实现文档中三策略会用到的规则子集。liquidity、事件风险、财报窗口等缺失能力可以输出 warning，不阻塞回测。

### 期权选腿 primitive

建议提供 `options.build_strategy(...)` 或等价 DSL builtin，用于把策略名称和约束转换为可交易 legs。

输入：

- `market`、`symbol`、`strategy_name`、`as_of`。
- DTE 范围、目标 delta、premium 下限、最大 spread、最大 legs、价格模式。
- 可选 option chain；缺省时从 US options chain provider 读取并复用 chain cache。

输出：

- legs：`side`、`right`、`expiry`、`strike`、`contract_symbol`、`quantity`、`price_source`、`mid`、`bid`、`ask`、`delta`、`iv`、`open_interest`、`volume`。
- 组合指标：`net_debit_credit`、`max_profit`、`max_loss`、`breakevens`、`dte`、`greeks`、`warnings`、`data_quality`。
- 失败时返回结构化 reason，DSL 应记录并跳过该标的。

第一版必须保证的拓扑：

- 单腿：买 call、买 put、卖 put、卖 call。
- 垂直价差：bull call、bear call、bull put、bear put。
- Short strangle：卖出 OTM put + 卖出 OTM call。
- Iron condor：`longPut < shortPut < shortCall < longCall` 且 net credit > 0。
- Calendar spread：同 strike，short front + long back，long expiry 至少比 short expiry 多 7 DTE。

价格优先级：mid bid/ask，其次 last trade，再其次 day close。若价格不可用，必须拒绝选腿并记录原因。

### Backtest 集成与资源保护

validate 阶段需要解析 DSL manifest、universe 依赖、factor 依赖、option chain 目标和资源计划。动态 option chain 请求如果无法静态确定，必须要求调用方提供 universe 或 symbols，并给出明确错误。

run 阶段需要：

- 在 prepare phase 展开 universe，生成实际 symbols。
- 对需要期权链的 symbols 批量预加载 chain provider。
- 将 universe symbols 注入 `portfolio.symbols()` 或 DSL 可读上下文。
- 在 trace 中记录每个 phase 的候选数、过滤数、策略命中数、选腿成功/失败数、链加载数和缓存命中率。

资源限制至少包括：

- 最大 universe size。
- 最大 option chain underlyings 数。
- 最大回测时间范围。
- 最大 DTE 范围。
- 最大合约扫描数。
- 单 run 内存预算或近似保护。
- 全局同时运行任务数。

## 三个 DSL 策略的粗验证需求

### 强势波段

输入池：

- `turnover_intersection_union` + `preset_symbols`。
- 合并去重后执行同族去重。
- 强势波段专用预设标的也要进入实际数据拉取范围。

策略逻辑：

- 对候选池读取日线 OHLCV、SMA(5/20)、RSI(14)、CCI(20)、ATR(14)。
- 读取或计算 HV、IV、IV 分位、HV 分位、基本面和期权链。
- 调用环境分类和 `options.strategies`，筛出至少一个可交易策略的标的。
- 震荡兜底必须满足 `abs(CCI20) < 101`。
- 若高估 + 上行导致无保护追涨，需要按覆盖规则降低激进度，例如 call 改为 calendar 或 defined-risk spread。
- 候选按 RSI 降序，先取 top 12，再取 top N 交易，默认 N=5。
- 对入选标的调用 `options.build_strategy`，选腿成功后在回测中开仓。

验收：

- 一个强势波段 DSL 可以从 universe 展开候选池，不需要客户端手工调用 turnover screener、行情、feature 和 option chain 多个 endpoint。
- 回测报告中能看到交易、候选过滤 trace、策略命中原因和选腿失败原因。

### 价值配置

输入池：

- `provider_holdings` universe，短期可由手工或 mock snapshot 提供。
- 来源语义为 SEC-13F 持仓和价值大 V 持仓的整体股票池。

过滤要求：

- 排除 `SPY`、`QQQ`、`IBIT` 等专用指数或 ETF 标的。
- PE 或 PB 分位、综合估值分位、IV 分位、HV 分位有效。
- IV 状态、市场状态、价格有效。
- 扩展期权链非空。
- 至少命中一条价值策略规则。
- 在 DTE 和 delta 范围内至少有一条可选合约。

价值策略规则：

| 估值状态 | IV 状态 | HV 条件 | 候选策略 |
| --- | --- | --- | --- |
| 低估 | 高 IV | 不限 | 卖看跌期权、备兑看涨期权 |
| 低估 | 低 IV | 高 HV | 买入偏斜跨式 |
| 低估 | 低 IV | 非高 HV | 买入看涨期权 |
| 高估 | 低 IV | 不限 | 买入看跌期权 |
| 高估 | 高 IV | 不限 | 卖看涨期权、熊市看涨价差 |
| 合理 | 高 IV | 不限 | 卖出宽跨式、铁鹰式 |
| 合理 | 低 IV | 不限 | 日历价差 |

IV 为中间状态时不命中价值规则。

排序与交易：

- 通过过滤的候选按估值分位升序排列，最多取 12 个进入策略匹配和选腿。
- top N 由 DSL 参数控制，默认可先取 5。
- 后端对 DSL 选择的策略和 `options.build_strategy` 输出 legs 做确定性校验。

验收：

- 在手工导入持仓池上，价值配置 DSL 能生成候选、过滤原因、策略命中、legs 和回测交易。
- 缺少真实持仓 provider 不阻塞策略验证。

### 指数期权

范围：仅 `SPY` 与 `QQQ`。

策略逻辑：

- 先尝试“估值 + IV + HV”规则；未命中时再用“趋势 + HV”规则兜底。
- `SPY` 使用 Shiller PE / PE10。
- `QQQ` 使用 Nasdaq 100 PE 或现有虚拟 fundamental。
- VIX 使用 2000 日分位，DVOL 使用 1500 日分位。
- HV30 使用 80 日分位，状态为高、中、低。
- IV 分位状态为高、中、低，并识别极端 IV。
- 高估 + 低 IV 或高估 + 下跌时降低或避免激进策略。

趋势 + HV 兜底规则：

| 趋势 | HV | 候选策略 |
| --- | --- | --- |
| 上行 | 高 HV | 卖出看跌期权、备兑看涨期权 |
| 上行 | 中 HV | 买入看涨期权、卖出看跌期权 |
| 上行 | 低 HV | 买入看涨期权、日历价差 |
| 震荡 | 高 HV | 铁鹰期权、卖出宽跨式 |
| 震荡 | 中 HV | 日历价差、买入跨式 |
| 震荡 | 低 HV | 买入跨式、日历价差 |
| 下行 | 高 HV | 熊市看涨价差、买入看跌期权 |
| 下行 | 中 HV | 买入看跌期权、熊市看涨价差 |
| 下行 | 低 HV | 买入看跌期权、日历价差 |

震荡兜底必须满足 `abs(CCI20) < 101`。

高估覆盖规则：

- 高估 + 上行 + 原策略为买入看涨：若 IV 分位 `<= 40`，改为日历价差；否则改为牛市看涨价差。
- 高估 + 上行 + 原策略为卖出看跌：改为牛市看跌价差。
- 高估 + 下行：保留防守/看跌方向，并标记高估下行防守。
- IV 分位 `<= 30` + 高估 + 低 IV：优先买入看跌期权。
- IV 分位 `<= 30` + 非高估 + 低 IV：优先买入跨式。

验收：

- 一个指数期权 DSL 能对 `SPY`、`QQQ` 匹配策略、选择合约 legs、生成交易并完成回测。
- 风险等级、估值来源、策略规则来源和选腿细节可在 trace 或回测明细中追踪。

## CLI 与调试工具

优先扩展现有工具或新增轻量命令，不必一次性建设完整控制台。

- `cmd/universe` 或并入 `cmd/data-sync-pipeline`：`list`、`refresh`、`rebuild`、`status`、`members`、`diff`。
- `cmd/dsl-backtest` 或复用现有 API smoke/backtest 工具：`validate`、`run`、`explain`、`dump-candidates`、`dump-option-strategies`、`dump-legs`。

调试输出必须复用 service 层，不复制业务 SQL。

## 分阶段交付

### Phase 1：底座闭环 + 强势波段样板

- 新增最小 universe 模块。
- 实现 `turnover_intersection_union`、`preset_symbols` 和 7/20/60/120 日缓存口径。
- 支持 backtest validate/run 展开 universe。
- 实现环境分类、期权策略筛选、期权选腿的最小子集。
- 支持强势波段 DSL 跑通候选、筛选、选腿、交易和 trace。
- 添加 universe members/status 调试能力。

### Phase 2：价值配置验证

- 实现 `provider_holdings` 手工或 mock snapshot。
- 补齐 PE/PB/综合估值分位读取和兜底。
- 实现价值策略规则、选腿和 legs 校验。
- 价值配置 DSL 在手工持仓池上完成回测。

### Phase 3：指数期权验证

- 固化 `SPY`、`QQQ` 估值源映射和数据质量 warning。
- 补齐 VIX、DVOL、HV、IV 分位读取或缓存。
- 实现指数期权估值规则、趋势兜底、高估覆盖和极端 IV 覆盖。
- 指数期权 DSL 对 `SPY`、`QQQ` 完成回测。

### Phase 4：通用化治理

- 根据前三阶段实际瓶颈，再决定是否建设 factor materialization 控制面。
- 支持真实外部 provider。
- 完善 dry-run、配额、队列、监控、缓存命中统计和更完整的选腿/风控规则。

## 关键决策

### 策略是否写死到平台

不写死。平台只内置 certified primitives，三大策略保持 DSL 形式。这样可以先迁移现有 consumer 逻辑，也避免每个新策略都变成 Go 代码改动。

### 是否现在建设通用 factor materialization

暂不建设。当前更紧迫的是让 DSL 能稳定读取已有 factor，并把三策略跑起来。只有当长窗口分位、横截面排名或大 universe 回测出现明确性能瓶颈时，再推进物化控制面。

### 是否新增独立推荐 API

暂不新增。候选、策略匹配和选腿过程作为 backtest trace/log 暴露；订单、绩效和风险仍归属于 backtest result。

### 第一版与原策略偏差如何处理

允许偏差，但要记录：

- 使用了哪个数据源或兜底口径。
- 哪些规则因为数据缺失被跳过。
- 哪些策略因拓扑、价格、DTE、delta 或资源限制被拒绝。
- 最终选腿为什么成立。

## 推荐下一步

1. 将 observed US stock pool lookback 从 20/60/120 扩为 7/20/60/120，并同步测试暖缓存与去重行为。
2. 实现 `turnover_intersection_union` universe adapter，先不追求完整 DSL universe 定义能力。
3. 在 backtest validate/run 中支持 universe 展开和资源计划输出。
4. 先实现强势波段所需的 `market_context`、`options.strategies`、`options.build_strategy` 最小子集。
5. 用一个强势波段 DSL 样板验证候选展开、feature/factor 读取、option chain 预加载、策略匹配、选腿和回测交易。
