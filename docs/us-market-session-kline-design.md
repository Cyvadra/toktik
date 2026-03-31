# 美股时段感知 K 线设计

## 背景

当前美股导入链路会把 Polygon 的 1 分钟股票与期权 bar 原样写入 ClickHouse，再通过物化视图按自然时间聚合成 `5m/15m/30m/1h/2h/4h/1d`。

现状有两个结构性问题：

1. 多周期聚合按 UTC 自然时间对齐，而不是按美股交易时段对齐。
2. 交易日、数据覆盖校验、重复导入判断，当前都默认按 UTC 日界线处理。

这会导致以下偏差：

- 若股票源数据包含盘前或盘后 bar，`1h/2h/4h/1d` 会被低流动性时段污染。
- TradingView 风格的常规交易时段图表通常默认隐藏扩展时段，但当前聚合无法做到。
- 冬令时盘后可延续到次日 `01:00 UTC`，按 UTC 日切分会让“同一交易日”的数据散落到两个 UTC 日期中。
- `1h/2h/4h` 使用 `toStartOfHour()` 之类的自然整点规则，会生成 `14:00, 15:00...` 对齐的 bar，而不是 TradingView 常见的 `09:30, 10:30...` 会话对齐 bar。

## 目标

以 TradingView 的默认常规时段展示习惯为标准，形成一套同时支持原始数据保留和通用多周期图表查询的方案。

目标如下：

1. 原始 1 分钟数据完整保留，不丢弃扩展时段数据。
2. 默认图表使用 regular trading hours，隐藏低流动性盘前盘后数据。
3. 多周期 bar 按交易所会话开盘时间对齐，而不是按 UTC 整点对齐。
4. 正确处理 DST、节假日、提前收盘。
5. 兼容股票和股票期权，并保留后续开放 extended hours 图表的可能性。

## 参考规则

### 股票默认展示规则

- 常规交易时段：`09:30-16:00 America/New_York`
- 盘前：通常 `04:00-09:30 America/New_York`
- 盘后：通常 `16:00-20:00 America/New_York`

TradingView 对美股默认更接近“常规时段优先”：

- 默认图表常隐藏盘前盘后。
- 若启用 extended hours，再叠加扩展时段 bar。
- `1h/2h/4h` 等周期更像是按会话开盘时间滚动分桶，而不是按 UTC/本地整点切桶。

### 股票期权默认展示规则

- 标准美股期权常规交易时段可按 `09:30-16:00 America/New_York` 处理。
- 若后续接入确实存在扩展时段的品类，应通过交易所会话表单独标记，而不是在聚合层写死。

本设计的默认策略是：股票和股票期权统一以 regular session 生成通用图表 K 线；原始表保留全部数据，扩展时段只在显式请求时使用。

## 设计总览

采用“三层数据模型”：

1. `raw 1m`：原始导入层，保留全部分钟 bar。
2. `sessionized 1m`：给每根 1 分钟 bar 打上交易所会话属性。
3. `session-aligned multi-timeframe`：按 regular session 对齐聚合出图表层 K 线。

## 一、原始表不变，但补充会话元数据

不建议在导入时直接过滤掉盘前盘后。原因：

- 会损失以后做成交量分析、开盘跳空分析、extended hours 图表的能力。
- 无法回溯修正交易时段规则。

建议在现有 1 分钟表上补充以下字段，或创建一张派生表承载这些字段：

- `market_date Date`：按 `America/New_York` 会话归属后的交易日，不等于 UTC 日期。
- `session_kind Enum`：`premarket | regular | postmarket | closed | unknown`
- `is_regular_session UInt8`
- `session_open DateTime('UTC')`
- `session_close DateTime('UTC')`
- `session_seq UInt16`：当前分钟在该交易日会话内的序号，regular session 从 0 开始。

其中 `market_date` 是关键字段，后续所有“按天”的逻辑都应基于它，而不是基于 UTC `timestamp` 直接截日。

## 二、引入交易所会话日历表

需要一张显式的交易日历维表，例如：`us_equity_sessions`。

建议字段：

- `market_date Date`
- `timezone String`，固定为 `America/New_York`
- `regular_open_utc DateTime('UTC')`
- `regular_close_utc DateTime('UTC')`
- `premarket_open_utc DateTime('UTC')`
- `postmarket_close_utc DateTime('UTC')`
- `is_holiday UInt8`
- `is_early_close UInt8`
- `options_close_utc DateTime('UTC')`

这张表的作用：

- 处理 DST，避免把 `09:30 ET` 硬编码成固定 UTC 时刻。
- 处理提前收盘，例如美股 `13:00 ET` 收盘的日期。
- 为股票和期权保留不同 close 时间的扩展能力。

数据来源可以来自交易所日历或稳定的市场日历库；关键不是来源，而是入库后由系统内部统一消费。

## 三、1 分钟 bar 会话分类

导入后或导入时，对每根 1 分钟 bar 关联 `us_equity_sessions`，生成 sessionized 结果。

规则建议如下：

1. 先把 `timestamp` 转到 `America/New_York` 所在交易日。
2. 再根据当日会话表判断属于：
   - `premarket`
   - `regular`
   - `postmarket`
   - `closed`
3. 对 regular session 内 bar，计算：
   - `session_seq = floor((timestamp - regular_open_utc) / 60s)`

如果存在不在任何已知区间内的分钟 bar：

- 原始表保留
- `session_kind = unknown`
- 默认图表查询不返回

## 四、按会话对齐生成多周期 K 线

这是本次最核心的调整。

### 当前问题

当前 `1h/2h/4h` 基于自然整点聚合，例如：

- `toStartOfHour(timestamp)`
- `toStartOfInterval(timestamp, INTERVAL 2 hour)`

这不符合美股 regular session 的图表预期。以常规交易日为例：

- 正确的 1h regular bar 应该从 `09:30, 10:30, 11:30, ...` 开始。
- 正确的 4h regular bar 应该从 `09:30` 开始，第二根通常是一个尾部残缺 bar。

### TradingView 风格的推荐分桶

对 regular session 内 bar，按下面公式计算目标桶起点：

`bucket_start = session_open_utc + floor((timestamp - session_open_utc) / interval) * interval`

而不是：

`bucket_start = toStartOfHour(timestamp)`

这样可以得到：

- `1h`: `09:30, 10:30, 11:30, 12:30, 13:30, 14:30, 15:30`
- `2h`: `09:30, 11:30, 13:30, 15:30`
- `4h`: `09:30, 13:30`

最后一个桶允许是不完整桶。这和常见图表软件的会话尾部行为更一致，也比强行丢弃尾部数据更合理。

### 提前收盘处理

若某日提前收盘，例如 `13:00 ET`：

- `1h`: `09:30, 10:30, 11:30, 12:30(半小时残缺桶)`
- `2h`: `09:30, 11:30(1.5 小时残缺桶)`
- `4h`: `09:30(3.5 小时残缺桶)`

也就是：

- 会话结束时允许最后一根 bar 不满周期。
- 不做跨日拼接。

## 五、建议的表设计

### 方案 A：保留现有原始表，新增 regular 专用聚合表

推荐优先落地这个方案，改动最小。

新增：

- `us_stocks_bar_1m_sessioned`
- `us_options_bar_1m_sessioned`
- `us_stocks_bar_{iv}_rth_agg`
- `us_stocks_bar_{iv}_rth`
- `us_options_bar_{iv}_rth_agg`
- `us_options_bar_{iv}_rth`

其中：

- `sessioned` 表负责保存 `market_date / session_kind / session_open / session_seq`
- `rth` 视图只聚合 `is_regular_session = 1` 的数据

优点：

- 原始表零破坏。
- 可以并行保留未来的 `eth` 或 `all-session` 图表。
- API 层可以很清楚地做默认路由：默认读 `rth`，用户显式要求时再读扩展视图。

### 方案 B：直接给原始表加字段并重建聚合视图

也可行，但迁移风险更高：

- 需要修改现有物化视图来源
- 更容易影响现有导入流程

如果当前线上数据量还不大，可以接受；否则建议先走方案 A。

## 六、ClickHouse 聚合实现建议

不要再用当前固定的 `TimeFunc` 直接表达 `1h/2h/4h` 的 regular 聚合。

建议把聚合键改成“基于会话开盘的偏移分桶”，核心思路如下：

1. `sessionized 1m` 表中持久化：
   - `session_open_utc`
   - `seconds_from_session_open`
2. 对每个 interval，聚合时计算：
   - `bucket_offset = intDiv(seconds_from_session_open, interval_seconds) * interval_seconds`
   - `bucket_start = session_open_utc + toIntervalSecond(bucket_offset)`

这样就可以在 ClickHouse 里稳定表达“从 09:30 开始的 1h/2h/4h 分桶”。

## 七、API 与前端默认语义

建议把会话语义显式暴露到 API，而不是让前端自己拼判断。

请求建议增加：

- `session=regular | extended | all`

默认值：

- `session=regular`

行为建议：

- `regular`: 读取 `rth` 视图或 `is_regular_session=1` 聚合结果
- `extended`: 返回 regular + pre/post 的完整会话图
- `all`: 返回原始所有分钟，包括异常或未知时段

这样前端若要模拟 TradingView 默认行为，直接不传参数即可。

## 八、导入流程的修正点

### 1. 交易日判定不能再依赖 UTC 日期

当前以下逻辑都依赖 `fileDate -> nextDay` 的 UTC 范围：

- 是否已导入
- 股票覆盖校验

这应改成基于 `market_date`：

- `CountExistingStockBarsByMarketDate(market_date)`
- `CountExistingOptionBarsByMarketDate(market_date)`
- `LoadStockCloseMap(..., market_date)`

### 2. Option greek 依赖底层股票分钟线时，键应基于真实分钟时间戳，不受 UTC 跨日影响

当前用 UTC 秒级时间戳作为键本身没问题，但“加载哪一天的股票数据”不能只靠 UTC `[date, date+1)` 区间。

### 3. 文件日期不应被直接视为 UTC 日期

文件名 `2023-01-03.csv.gz` 更准确的语义应是：

- `market_date = 2023-01-03`

而不是：

- 查询 `2023-01-03T00:00:00Z ~ 2023-01-04T00:00:00Z`

## 九、推荐实施顺序

### 第一阶段：先修正确性

1. 新增 `us_equity_sessions` 交易日历表。
2. 给股票与期权 1m 数据增加 `market_date` 与 `session_kind` 派生层。
3. 修正 `skip-existing`、覆盖校验等逻辑，全部按 `market_date` 工作。

### 第二阶段：新增 regular-session 聚合层

1. 新建 `rth` 专用聚合表与视图。
2. `1h/2h/4h/1d` 改为按 `session_open` 对齐。
3. API 默认切到 `rth`。

### 第三阶段：补 extended-hours 能力

1. 新增 `session=extended` 查询模式。
2. 前端允许切换是否显示盘前盘后。

## 十、默认行为建议

为了更接近 TradingView 平台默认体验，建议系统默认行为如下：

1. 原始导入保留全部分钟 bar。
2. 图表默认只返回 `regular session`。
3. `1h/2h/4h` 全部按 `09:30 ET` 会话开盘对齐。
4. 最后一根不完整 bar 保留。
5. 节假日无 bar，提前收盘日允许出现更短的最后一根 bar。

## 对当前代码的直接结论

现有实现中，以下点需要后续重构：

- `cmd/us-market-import/main.go` 的按日期跳过逻辑
- `internal/usmarket/greeks.go` 中基于 `fileDate.AddDate(0, 0, 1)` 的股票覆盖加载
- `internal/usmarket/kline.go` 中所有基于自然整点的 `TimeFunc`