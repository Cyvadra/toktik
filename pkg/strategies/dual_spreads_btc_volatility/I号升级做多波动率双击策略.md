# I号升级做多波动率双击策略

strategy name: dual_spreads_btc_volatility

## 策略开仓点信号

使用 `pkg/strategies/dual_spreads_btc_volatility/another_format_utc8.csv` 中的，加仓与开仓信号；
直接按信号时间开仓，注意文件中的日期为UTC+8时区；
即lowercase之后，包含"init"，即为首仓；包含"add"，即为加仓；
忽略csv中的平仓信号。有init/add就开，不考虑既有持仓。
同一根K线上，最多开仓或加仓一次。

首仓额外过滤条件： 收到 init 开仓信号后，查询当前 12H DVOL，不得大于 `vol_std` 的 *n* 分位，否则忽略该次首仓信号。
n 默认值取 66，即 66%
根据 12H 的历史数据，计算 `vol_std` 和 IV。

其中：

- `std(20)` = 基于 12H `close` 的 20 根滚动标准差
- `sma(std(20), 20)` = 对上面的 `std(20)` 再做 20 根 12H 简单移动平均
- `vol_std = std(20) / sma(std(20), 20)`

因此 `vol_std` 首次有效需要至少 40 根 12H K 线；其后的百分位统计窗口也固定按 12H K 线计算，而不是按 `--interval` 运行周期计算。

波动率指标条件（或）：

- 12H `vol_std` <= percentile(`vol_std_12h`, 150, 66)；即当前 `vol_std` 不高于过去 150 根 12H `vol_std` 的 n 分位数
- 12H DVOL <= percentile(IV_12h, 150, 66)；即 DVOL 不高于过去 150 根 12H IV 的 n 分位数
- 12H K 线数量不足 200 根时，判断 12H DVOL / `vol_std` <= 96

初始资金： 100 BTC

仅交易spreads

备注：策略应运行在 12H 主周期上，`vol_std`/IV 的统计窗口按 12H K 线定义。

## 期权开仓/加仓

首仓 amount_base = 100 BTC * 2% = 2.0 BTC；该基数固定按初始总资产计算，不复利。
换仓时 amount_base 按上一次投入金额的 90% 递减。
根据 B 动态选择期限：

- B > 55 时，选取期限在 [20,35] 天内且最接近 35天 的 call 期权。

> 如果DTE范围内无可选期权，则选择 DTE >= 20 day, 且最接近 35 天 的期权，作为兜底。

- B <= 55 时，选取期限在 [30,45] 天内且最接近 45天 的 call 期权。

> 如果DTE范围内无可选期权，则选择 DTE >= 30 day, 且最接近 40 天 的期权，作为兜底。

买入腿 A 的目标 Delta 动态计算：

变量定义：

- A => 当前 `vol_std`，相当于 12H `vol_std` 历史 150 根的 A 分位（0-100）

- B => 当前 DVOL，相当于 12h IV 历史 150根 的 B 分位

- 若回测或实盘中无法取到有效分位值，默认 A=60，B=60

注意算法优化和性能保障

$$
|\Delta_{L}| = \text{Clamp} \left( 0.2, 0.8, \left( \frac{2A + B}{300} \right) - 0.05 \right)
$$
找到 delta 最接近 ΔL 的期权A，记录权利金 price2buy；

卖出腿(call) B 的选择规则：

- 根据买入腿 |ΔL| 分档选择行权价要求：

	- 当 |ΔL| < 0.4 时，K_sell >= K_buy * 120%
	- 当 0.4 <= |ΔL| <= 0.6 时，K_sell >= K_buy * 115%
	- 当 |ΔL| > 0.6 时，K_sell >= K_buy * 110%

- Delta 约束：要求 delta 落在 [0.05, 0.7]
- 若目标价差附近无满足条件的合约，则在满足对应 K_sell 下限的前提下，选择 delta 最接近边界的合约

记录权利金 price2sell；

计算 n = amount_base / (price2buy - price2sell)

买入 n 份的期权A，并卖出 n 份的期权B；

每次开仓或加仓，都按这种方式进行，两个订单为一组。

备注：首仓固定使用基于初始总资产 2% 计算得到的 2 BTC；后续换仓仅按 9 折递减，不做复利扩张。


## 换仓

### 触发条件（或）

- (price2buy - price2sell) 相对于开仓时，上涨50% （按mark计）

- 订单组中的多头腿A 实时 delta 相对开仓时增加 0.2

### 换仓系数

订单组每次换仓，投入金额衰减，按上次投入的90%，进行换仓的开仓；

例如 首仓2BTC ==> 换仓1.8BTC ==> 再次换仓时1.62BTC

### 换仓方法

平仓对应的订单组（buy call + sell call的组合，两单都平仓）

然后按"开仓/加仓"方法，基于当下最新的 A、B 与动态 Delta 规则重新选择并建立新的仓位。注意投入金额衰减。

## 期权平仓

期权到期前一日平仓。

