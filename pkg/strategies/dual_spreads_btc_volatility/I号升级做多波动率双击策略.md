# I号升级做多波动率双击策略

strategy name: dual_spreads_btc_volatility

## 策略开仓点信号

使用 `pkg/strategies/dual_spreads_btc_volatility/another_format_utc8.csv` 中的，加仓与开仓信号；
直接按信号时间开仓，注意文件中的日期为UTC+8时区；
即lowercase之后，包含"init"，即为首仓；包含"add"，即为加仓；
忽略csv中的平仓信号。有init/add就开，不考虑既有持仓。
同一根K线上，最多开仓或加仓一次。

首仓额外过滤条件： 收到 init 开仓信号后，查询当前 12H DVOL，不得大于历史波动率的 *n* 分位，否则忽略该次首仓信号。
n 默认值取 66，即 66%
根据12H的历史数据，计算历史波动率 HV 和 IV

波动率指标条件（或）：

- 12H HV <= percentile(HV_12h, 100, 66) ； HV <= 100根12h HV的 n 分位数
- 12H DVOL <= percentile(IV_12h, 200, 66) ； DVOL <= 200根12h IV的 n 分位数
- 12H K线数量不足 200 根时，判断 12H DVOL/HV <= 60

初始资金： 100 BTC

仅交易spreads

备注：策略应运行在 12H 主周期上，HV 的统计窗口按 12H K 线定义。

## 期权开仓/加仓

amount_base = 2.0 BTC;
选取期限在 [20,40] 天，最接近 40 天的 call 期权。

买入腿 A 的目标 Delta 动态计算：

变量定义：

- A => 当前 HV，相当于 12h HV 历史 100根 的 A 分位 （0-100）

- B => 当前 DVOL，相当于 12h IV 历史 200根 的 B 分位

注意算法优化和性能保障，精确度最高保留至整数即可，必要时可降低至±2

$$
|\Delta_{L}| = \text{Clamp} \left( 0.2, 0.8, \left( \frac{2A + B}{300} \right) - 0.1 \right)
$$
找到 delta 最接近 ΔL 的期权A，记录权利金 price2buy；

卖出腿(call) B 的选择规则：

- 行权价要求：K_sell >= K_buy * 120%
- Delta 约束：要求 delta 落在 [0.1, 0.8] 
- 若 20% 价差附近无满足条件的合约，则在满足 K_sell >= K_buy * 120% 的前提下，选择 delta 最接近边界的合约

记录权利金 price2sell；

计算 n = amount_base / (price2buy - price2sell)

买入 n 份的期权A，并卖出 n 份的期权B；

每次开仓或加仓，都按这种方式进行，两个订单为一组。

备注：当前实现仍然固定每次开仓/加仓投入 2 BTC，不根据 A/B 动态调整首仓金额。


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

