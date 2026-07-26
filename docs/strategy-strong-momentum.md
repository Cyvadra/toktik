# 强势波段策略

本文描述 `pkg/dsl/scripts/strategies/strong-momentum.toktik` 当前可执行逻辑。策略不调用 LLM；候选排序、期权结构选择和选腿全部由 DSL runtime 确定性完成。价值配置策略不在本文范围内。

## 股票池

策略读取时间点有效的 `strong_momentum` universe。该 universe 由后端维护，回测必须使用 point-in-time membership，不能用当前成员回填历史。

默认 universe source 是 `turnover_intersection_union`：

1. 分别计算 7、20、60、120 个交易日窗口的股票和期权滚动成交额。
2. 每个窗口只保留同日同时存在股票与期权数据的 underlying，并按两者成交额之和排序。
3. 每个窗口每日保留前 60 个非 ETF 标的。
4. 将四个窗口的成员按 `(date, underlying)` 去重后取并集，形成当日 point-in-time universe。

因此每日成员数可以高于 60；它不是四个榜单的交集。`universe.symbols("strong_momentum")` 读取的是已持久化的该日成员区间。

## 候选过滤

每个交易日为 universe 成员读取以下日线数据：

- 收盘价
- SMA(5)、SMA(20)
- RSI(14)
- CCI(20)
- 20 日收益率
- HV20 及其历史分位
- IV percentile

只有同时满足以下条件的标的进入强势候选：

- 数据完整且价格大于 0
- `SMA(5) > SMA(20)`
- `RSI(14) >= 55`
- `CCI(20) > 0`
- 20 日收益率大于 0

这组条件用于排除仅因短期超跌或波动上升而进入高 RSI 排名、但趋势并未确认的标的。

## 排序和选择

1. 对已通过策略过滤的候选按 RSI(14) 降序排列。
2. RSI 相同时，优先 20 日收益率较高的标的，再按 symbol 稳定排序。
3. 不限制候选数量，全部合格候选都可以进入期权生成阶段。
4. 每个 bar 按上述顺序尝试确定性组腿，找到第一个尚未持仓且可执行的标的后开仓，当日不再继续开仓。

空链或无法组腿的候选不会阻止脚本继续检查当日后续候选。

## 确定性策略和选腿

策略只使用 runtime 已完整支持、风险有界的牛市垂直价差：

| 条件 | 结构 |
| --- | --- |
| IV percentile >= 70 | Bull Put Spread |
| IV percentile < 70 | Bull Call Spread |

期权链先过滤到 25-65 DTE，再选择最接近 45 DTE 的单一到期日。这样可保证垂直价差两条腿到期日一致。

选腿由 `options.build_strategy` 完成：

- Bull Put Spread：卖出 delta 接近 `-0.35` 的 put，买入更低绝对 delta 的保护 put。
- Bull Call Spread：买入 delta 接近 `0.35` 的 call，卖出 delta 接近 `0.175` 的 call。
- 对 Bull Put Spread，卖方腿 bid 必须至少为 0.50。
- 合约必须有有效 mark、bid 或 ask，且两腿属于同一市场、underlying 和到期日。

策略不使用 Covered Call 或 Collar，因为当前回测请求是纯期权资金模式，没有与期权部位绑定的现货底仓。策略也不使用 LLM 选择或重排腿。

## 仓位和退出

- DSL 当前没有 Pine Script `pyramiding` 声明参数；本策略用显式状态控制仓位。
- 每个日线 bar 都运行候选与开仓流程，每个 bar 最多新开一个 spread。
- 不设置组合层面的 spread 数量上限。
- 同一 underlying 同时最多持有一个 spread。
- underlying 掉出 point-in-time `strong_momentum` universe 时立即平仓并清除持仓引用。
- 当前设计没有其他主动退出条件；若标的一直留在股票池中，则由引擎在期权到期时自动平仓。
- 当前每个 spread 使用固定 1 组 contracts，参数可由回测请求中的 DSL input override 调整。

## 诊断指标

HTML 报告输出以下 plot：

- `universe_size`
- `qualified_candidates`
- `momentum_candidates`
- `selected_with_strategy`
- `chain_ready_count`
- `legs_ready_count`
- `opened_spreads`
- `closed_out_of_universe`
- `open_spreads`

trace 区分开仓成功、掉池平仓、空链、组腿失败、premium 不足和 scope 拒绝。评估覆盖度时，应同时查看 `momentum_open`、`momentum_close_out_of_universe`、`momentum_legs_empty`、`momentum_chain_empty` 与 `momentum_premium_rejected` 的数量，不能只看最终交易数。

## 回测评估

策略迭代至少比较：

- 总 spread 数与年度分布
- 盈利 spread 数、亏损 spread 数和胜率
- 总收益、年化收益、Sharpe 和最大回撤
- 单一标的对总 PnL 的贡献集中度
- 候选到 chain、legs、open 各阶段的转化率
- 到期自动平仓比例

普通 `winning_trades` 字段不代表 spread 胜负；纯期权 spread 策略应使用 `spread_summary`。
