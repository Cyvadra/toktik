# Toktik DSL 使用文档

Toktik DSL 是一个受 Pine Script 启发的策略语言子集，当前通过解释器运行，并通过 bridge 适配到现有的 `backtest.Strategy` 接口。

这份文档描述的是仓库里当前已经实现并可用的 DSL 能力，而不是规划中的完整语法。

## 设计目标

- 用接近 Pine Script 的语法快速编写现货、指标和期权策略
- 直接复用 `internal/backtest` 的行情、指标、下单和期权 spread 能力
- 支持时间序列运算、内置指标、期权链筛选以及 WorldQuant 风格 Alpha 算子

## 当前状态

当前 DSL 已实现以下模块：

- 词法分析器：注释、字符串、数字、关键字、运算符
- Pratt Parser：表达式优先级、函数调用、点访问、下标、三元表达式
- 解释器：变量、函数、条件、循环、switch、数组、series 历史引用、`input()` 参数覆盖
- 内置库：`input.*`、`ta.*`、`math.*`、`str.*`、`strategy.*`、`options.*`、`contract.*`、`spread.*`、`group.*`、`schedule.*`、`request.*`、`alpha.*`
- bridge：将 DSL 脚本适配到 Go 回测引擎
- catalog：可把 DSL 脚本注册成策略目录中的策略

## 执行模型

DSL 脚本会在每一个 bar 上执行一次。

- `strategy("name")` 只用于声明策略元数据，不参与每 bar 计算
- 普通 `x = expr` 会在每个 bar 重新求值
- `var x = expr` 只在第一次初始化时求值，之后跨 bar 持久保存
- `varip x = expr` 会持久保存，并允许你在后续 bar 内更新
- `x := expr`、`x += expr`、`x -= expr`、`x *= expr`、`x /= expr`、`x %= expr` 用于更新已有变量
- `close[1]` 表示上一根 bar 的值，`close[0]` 表示当前 bar

当前解释器会自动把 `open`、`high`、`low`、`close`、`volume` 注入为 series。普通 `x = expr` 也会在每个 bar 上维护历史序列，因此可以直接作为 `ta.*`、`alpha.*` 和 `request.*` 的输入。

## 基本语法

### 1. 策略声明

```toktik
strategy("EMA Cross", overlay=true)
```

当前 `strategy(...)` 主要用于提取策略名称。其余命名参数会被解析，但不全部参与运行期行为。

### 2. 注释

```toktik
// 单行注释

/*
多行注释
*/
```

### 3. 字面量

```toktik
x = 42
y = 3.14
flag = true
name = "btc"
empty = na
arr = [1, 2, 3]
```

### 4. 变量与赋值

```toktik
count = 0
var total = 0
varip state = 1

count := count + 1
total += 10
state *= 2
```

语义建议：

- 用 `=` 定义或每 bar 重算一个值
- 用 `var` 定义跨 bar 状态
- 用 `:=` 和复合赋值更新已有状态

### 5. 条件与三元表达式

```toktik
if close > open {
  signal = 1
} else if close < open {
  signal = -1
} else {
  signal = 0
}

color_code = close > open ? 1 : 0
```

### 6. 循环

```toktik
var sum = 0

for i = 1 to 5 {
  sum := sum + i
}

for i = 0 to 10 by 2 {
  sum := sum + i
}

for item in [1, 2, 3] {
  sum := sum + item
}

while sum < 100 {
  sum := sum + 1
}
```

### 7. switch

```toktik
switch signal
1 => {
  regime = "long"
}
-1 => {
  regime = "short"
}
else => {
  regime = "flat"
}
```

### 8. 函数

```toktik
fn double(x) {
  return x * 2
}

y = double(21)
```

也支持简单 lambda：

```toktik
adder = (x, y) => x + y
z = adder(1, 2)
```

### 9. 数组、历史引用与元组赋值

```toktik
arr = [10, 20, 30]
first = arr[0]
prev_close = close[1]

[a, b] = [1, 2]
```

### 10. 输入参数

`input()` 系列函数已经接入运行时，默认返回声明值，也可以通过 bridge 侧的输入覆盖注入。

```toktik
fast_len = input(10, title="Fast Length")
slow_len = input.int(20, title="Slow Length")
threshold = input.float(1.5, title="Threshold")
enabled = input.bool(true, title="Enabled")
mode = input.string("trend", title="Mode")
```

在 Go 里可以通过 bridge 侧配置输入覆盖；当前文档只描述 DSL 侧语义。

## 运算符

已支持：

- 算术：`+` `-` `*` `/` `%`
- 比较：`==` `!=` `<` `<=` `>` `>=`
- 逻辑：`and` `or` `not`
- 赋值：`=` `:=` `+=` `-=` `*=` `/=` `%=`
- 其他：`.` `[]` `? :`

## 内置数据

运行时默认可访问：

- `open`
- `high`
- `low`
- `close`
- `volume`
- `bar_index`

这些值都是按 bar 推进的 series，可直接用于 `ta.*` 和 `alpha.*` 计算。

## 内置函数总览

### `strategy.*`

用于现货/标的级别的买卖和仓位访问。

```toktik
strategy.entry(id="long", direction=strategy.long, qty=1)
strategy.entry(id="short", direction=strategy.short, qty=2)

strategy.close("long")
strategy.exit("short")

size = strategy.position_size()
avg = strategy.position_avg_price()

buy(1)
sell(1)
```

当前常量：

- `strategy.long`
- `strategy.short`

### `ta.*`

已实现：

- `ta.sma(source, length)`
- `ta.ema(source, length)`
- `ta.rsi(source, length)`
- `ta.atr(length)`
- `ta.cci(length)`
- `ta.cci(source, length)`
- `ta.highest(source, length)`
- `ta.lowest(source, length)`
- `ta.stdev(source, length)`
- `ta.crossover(a, b)`
- `ta.crossunder(a, b)`
- `ta.change(source, length=1)`
- `ta.cum(source)`
- `ta.wma(source, length)`
- `ta.bb(source, length, mult)`
- `ta.bb_upper(source, length, mult)`
- `ta.bb_lower(source, length, mult)`
- `ta.barssince(condition)`
- `ta.valuewhen(condition, source, occurrence)`
- `ta.percentrank(source, length)`

示例：

```toktik
fast = ta.sma(close, 10)
slow = ta.sma(close, 20)

if ta.crossover(fast, slow) {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}
```

### `math.*`

已实现：

- `math.abs(x)`
- `math.ceil(x)`
- `math.floor(x)`
- `math.round(x)`
- `math.sqrt(x)`
- `math.pow(x, y)`
- `math.log(x)`
- `math.log10(x)`
- `math.exp(x)`
- `math.max(a, b)`
- `math.min(a, b)`
- `math.sign(x)`
- `math.avg(...)`
- `nz(x, replacement=0)`
- `na(x)`

### `str.*`

已实现：

- `str.contains(s, sub)`
- `str.length(s)`
- `str.upper(s)`
- `str.lower(s)`
- `str.tostring(x)`
- `str.format(fmt, ...)`

### `input.*`

已实现：

- `input(defval, title=..., minval=..., maxval=..., step=...)`
- `input.int(defval, title=..., minval=..., maxval=..., step=...)`
- `input.float(defval, title=..., minval=..., maxval=..., step=...)`
- `input.bool(defval, title=...)`
- `input.string(defval, title=..., options=...)`

示例：

```toktik
fast_len = input(10, title="Fast Length")
use_filter = input.bool(true, title="Use Filter")
```

### `alpha.*`

这是面向因子/时序统计的 WorldQuant 风格算子集合。

已实现：

- `alpha.rank(x)`
- `alpha.zscore(x, window)`
- `alpha.decay_linear(x, window)`
- `alpha.ts_rank(x, window)`
- `alpha.ts_corr(x, y, window)`
- `alpha.ts_cov(x, y, window)`
- `alpha.ts_delta(x, period)`
- `alpha.ts_sum(x, window)`
- `alpha.ts_mean(x, window)`
- `alpha.ts_std(x, window)`
- `alpha.ts_min(x, window)`
- `alpha.ts_max(x, window)`
- `alpha.ts_argmin(x, window)`
- `alpha.ts_argmax(x, window)`
- `alpha.ts_skewness(x, window)`
- `alpha.ts_kurtosis(x, window)`
- `alpha.ts_median(x, window)`
- `alpha.signed_power(x, exp)`
- `alpha.scale(x, target_sum=1)`
- `alpha.log_return(x, period)`

示例：

```toktik
mom = alpha.ts_delta(close, 20)
z = alpha.zscore(close, 50)
rank = alpha.ts_rank(volume, 30)

if z > 2 and mom > 0 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}
```

## 期权 DSL

期权相关对象在 DSL 里是“opaque handle”，也就是不能直接通过 `contract.field` 读取字段，而是要通过 `contract.*`、`options.*`、`spread.*` 等内置函数访问。

### `options.*`

用于获取和筛选当前 bar 的期权链。

已实现：

- `options.chain()`
- `options.calls(chain)`
- `options.puts(chain)`
- `options.expiry_nearest(chain, target_days)`
- `options.expiry_range(chain, min_days, max_days)`
- `options.expiry_min(chain, min_days)`
- `options.expiry_max(chain, max_days)`
- `options.delta_range(chain, min_delta, max_delta)`
- `options.min_premium(chain, min_bid)`
- `options.strike_range(chain, min, max)`
- `options.len(chain)`
- `options.best_spread(chain)`
- `options.sort_by_delta(chain, target_delta)`

示例：

```toktik
chain = options.chain()
puts = options.puts(chain)
near_puts = options.expiry_nearest(puts, 30)
shortlist = options.delta_range(near_puts, -0.35, -0.15)
best = options.best_spread(shortlist)
```

### `contract.*`

用于从单个合约 handle 中读取字段。

已实现：

- `contract.symbol(c)`
- `contract.type(c)`
- `contract.strike(c)`
- `contract.dte(c)`
- `contract.delta(c)`
- `contract.gamma(c)`
- `contract.vega(c)`
- `contract.theta(c)`
- `contract.iv(c)`
- `contract.bid(c)`
- `contract.ask(c)`
- `contract.mark(c)`
- `contract.volume(c)`
- `contract.oi(c)`

示例：

```toktik
symbol = contract.symbol(best)
delta = contract.delta(best)
mark = contract.mark(best)
```

### `leg.*`

用于构造 spread 的腿。

- `leg.buy(contract, qty)`
- `leg.sell(contract, qty)`

### `spread.*`

用于开平仓和查询 multi-leg spread。

已实现：

- `spread.open(legs_array, tag)`
- `spread.open_in_group(legs_array, tag, group_id)`
- `spread.close(spread_id, reason?)`
- `spread.close_leg(spread_id, leg_index, close_price)`
- `spread.get(spread_id)`
- `spread.pnl(spread_id)`
- `spread.open_ids()`
- `spread.count()`

`spread.get(spread_id)` 当前返回数组：

```toktik
[id, tag, bars_held, realized_pnl, is_open, leg_count]
```

### `group.*`

用于管理 spread group。

已实现：

- `group.open(tag, init_amount, decay_factor)`
- `group.close(group_id)`
- `group.get(group_id)`
- `group.add_spread(group_id, spread_id)`
- `group.open_ids()`

### `schedule.*`

- `schedule.close_spread(offset_bars, spread_id, reason?)`

`group.get(group_id)` 当前返回数组：

```toktik
[id, tag, amount, roll_count, is_closed, spread_ids]
```

### `schedule.*`

用于未来关闭动作调度。

- `schedule.close_spread(offset, spread_id)`
- `schedule.close_leg(offset, spread_id, leg_index)`

注意：当前 bridge 中 `offset` 暂按小时近似映射，不是严格的“bar 数”。如果你要做精确调度，现阶段建议仍在 Go 策略中控制，或自行约束运行的 bar 间隔。

## 完整示例

### 趋势策略

```toktik
strategy("EMA Cross")

fast = ta.ema(close, 10)
slow = ta.ema(close, 30)

if ta.crossover(fast, slow) {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}

if ta.crossunder(fast, slow) {
  strategy.close("long")
}
```

### 带持久状态的策略

```toktik
strategy("Breakout Counter")

var breakout_count = 0
hh = ta.highest(high, 20)

if close >= hh {
  breakout_count := breakout_count + 1
}
```

### 简单期权 spread

```toktik
strategy("Short Strangle")

chain = options.chain()

if chain != na {
  puts = options.puts(chain)
  calls = options.calls(chain)

  near_puts = options.expiry_nearest(puts, 30)
  near_calls = options.expiry_nearest(calls, 30)

  sell_put = options.best_spread(options.delta_range(near_puts, -0.30, -0.10))
  sell_call = options.best_spread(options.delta_range(near_calls, 0.10, 0.30))

  if sell_put != na and sell_call != na and spread.count() == 0 {
    put_leg = leg.sell(sell_put, 1)
    call_leg = leg.sell(sell_call, 1)
    spread_id = spread.open([put_leg, call_leg], "short_strangle")
    schedule.close_spread(24, spread_id)
  }
}
```

### Alpha 因子策略

```toktik
strategy("Alpha Momentum")

z = alpha.zscore(close, 50)
mom = alpha.ts_delta(close, 20)
liq = alpha.ts_rank(volume, 30)

if z > 2 and mom > 0 and liq > 0.7 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}

if z < -2 {
  strategy.close("long")
}
```

## 在 Go 中使用 DSL

### 1. 直接创建 bridge strategy

```go
src := `strategy("My DSL Strategy")
fast = ta.sma(close, 10)
slow = ta.sma(close, 20)
if ta.crossover(fast, slow) {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}`

st := bridge.New(src)
if errs := st.ParseErrors(); len(errs) > 0 {
    panic(errs)
}
```

### 2. 注册到 catalog

```go
err := dslcatalog.RegisterDSL("ema-cross-dsl", src)
if err != nil {
    panic(err)
}
```

之后可以像其他 catalog 策略一样按名称解析。

## 已知限制

下面这些点需要明确：

- 当前文档描述的是已实现子集，不是完整 Pine Script 兼容层
- `import "..."` 语法已进入 parser，但运行期尚未完整打通，不建议在生产脚本里依赖
- 点访问主要用于命名空间函数和值，例如 `ta.sma`、`strategy.long`，不是通用对象系统
- `strategy.position_size`、`strategy.position_avg_price` 当前按函数调用方式使用更稳妥，即 `strategy.position_size()`
- 调度类 `schedule.*` 仍是早期版本，时间换算策略后续还会调整
- 解释器是 tree-walking 模式，优先目标是正确性与易扩展性，不是极致性能

## `request.*`

`request.security()` 和 `request.factor()` 已接入运行时，可在策略中请求其他标的或因子序列，并直接把返回值继续送入 `ta.*` 进行链式计算。

当前支持的是常量参数版本：

```toktik
alt_close = request.security("test", "ALT", "1h", "close")
alt_sma = ta.sma(alt_close, 20)

dvol = request.factor("dvol", "1h", "dvol")
```

如果脚本使用这些调用，bridge 会在初始化阶段预注册依赖，并在每个 bar 上把结果捕获为 series。

## 推荐实践

- 用 `var` 保存跨 bar 状态，避免把状态写成普通 `=` 变量
- 所有涉及历史序列的因子计算优先显式写窗口大小
- 期权脚本里先判断 `chain != na`，再做筛选和下单
- 把复杂 spread 结构拆成 `options.* -> contract.* -> leg.* -> spread.*` 四层，脚本可读性会好很多
- 如果策略需要精准的订单细节、复杂调度或多标的同步，优先用 DSL 做信号层，再在 Go strategy 中封装执行层