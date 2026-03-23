# 扎针抄底卖期权策略设计文档 (Strategy Design: Buy Flash Low with Put Selling)

## 1. 策略概述 (Overview)

本策略结合**现货扎针抄底信号**与**期权波动率过滤**，通过卖出看跌期权 (Sell Put) 获取权利金，并利用现货价格反弹和波动率收敛获利。

现货系统与期权系统共享同一套**扎针信号系统**（第 2 章），在信号触发后各自独立执行开仓与平仓逻辑。期权系统在此基础上额外叠加 DVOL 波动率过滤条件。

---

## 2. 公共模块：扎针信号系统 (Shared Module: Flash Low Signal)

> 以下逻辑为现货系统与期权系统的**共同触发基础**，两者均依据此信号决定是否开仓。

### 2.1 支撑边界判断 (Support Boundary)

基础周期 `lookback = 20`。

| 变量 | 定义 |
|------|------|
| `l20` | `lowest(low, 20)[1]`，前一根 K 线的 20 期最低价 |
| `ATR20` | `ATR(20)` |
| `in_bot` | `low ≤ l20 + 0.7 × ATR20` **且** `high ≥ l20` |

当前 K 线须落入支撑底部区域（刺穿或触及近期低点）方可进入后续评分。

### 2.2 形态过滤 (Shape Filter)

| 变量 | 定义 |
|------|------|
| `is_pin_shape` | `close > 0.5 × (high + low)`，收盘价高于 K 线中位（下影线为主） |
| `current_amp` | `(high − low) / close`，当根 K 线振幅比 |
| `amp_pr_100` | `percentrank(current_amp, 100)`，振幅在近 100 根 K 线中的百分位 |
| `shape_entry` | `is_pin_shape` **且** `amp_pr_100 > 66` |

### 2.3 评分引擎 (Scoring Engine)

总分上限 **5 分**，由振幅得分（最高 2 分）与成交量得分（最高 3 分）组成。

#### 振幅得分 (Amplitude Score, max 2)

| 条件 | 得分 |
|------|------|
| `amp_pr_100 > 77` | +1 |
| `amp_pr_100 > 90` | +1 |

#### 成交量得分 (Volume Score, max 3)

| 条件 | 得分 |
|------|------|
| 成交量在近 20 根 K 线中排名前 3 | +1 |
| 成交量在近 60 根 K 线中排名前 6 | +1 |
| 成交量在近 180 根 K 线中排名前 10 **且** 成交量 > 2 × MA(100) | +1 |

### 2.4 市场环境与触发阈值 (Market Regime & Trigger Threshold)

使用 5 条简单均线（MA2 / MA6 / MA10 / MA15 / MA20）判断当前是否处于空头排列，动态调整触发分值门槛。

| 变量 | 定义 |
|------|------|
| `buffer` | `0.05 × ATR(20)`，防止均线粘连时误判 |
| `is_bearish` | MA2 < MA6+buffer **且** MA6 < MA10+buffer **且** MA10 < MA15+buffer **且** MA15 < MA20+buffer |
| `threshold` | `is_bearish` 时为 **5 分**，否则为 **3 分** |

**最终信号触发条件：**

```
buy_signal = in_bot AND shape_entry AND total_score ≥ threshold
```

> **注**：以 Pine 脚本（`均线过滤版震荡下沿插针评分系统_V2_均线过滤版.tv.pine`）为最终确认版本。

---

## 3. 现货系统 (Spot System)

### 3.1 执行 (Execution)

*   依据第 2 章扎针信号触发买入开多，**无额外过滤条件**。
*   现货仓位仅保留极小 notional，用于追踪入场价、最高点与移动止损，不作为主要收益来源。

### 3.2 平仓 (Exit)

*   持续记录买入后的最高价（`highest_since_entry`）。
*   当 `highest_since_entry − close > 2 × ATR(20)` 时触发移动止损，全部平仓。

---

## 4. 期权系统 (Options System)

### 4.1 额外入场条件：波动率过滤 (Additional Entry Condition: Volatility Filter)

在第 2 章扎针信号触发的基础上，期权系统额外要求：

*   **指标**：DVOL 指数（Deribit 隐含波动率指数）。
*   **要求**：当前 DVOL 处于最近 90 或 360 根 K 线的 **60 分位以上**。
*   **目的**：确保在隐含波动率较高时卖出期权，获得更高的权利金。

### 4.2 执行 (Execution)

*   **标的**：BTC。
*   **方向**：卖出看跌期权 (Sell Put)。
*   **选约规则**：优先选择剩余到期日在 **7 到 15 天**之间、Delta 约 **−0.15 到 −0.35** 的 Put；若无完全匹配，则退化为同到期层中最接近目标 Delta 的合约。
*   **仓位管理**：
    *   基准本金：100 BTC。
    *   单次开仓目标期权费收入：**3 BTC**。
    *   合约张数限制：总张数 **≤ 100 张**。
    *   开仓时拆成两个等权 short put tranche，以支持"先平一半、再留一半"的管理逻辑。

### 4.3 平仓 (Exit)

*   **减仓**：第一笔 Sell Put tranche 盈利超过 **70%** 时平仓。
*   **全平**：第二笔 tranche 持有至接近到期，或盈利超过 **88%** 时平仓。

---

**相关引用 (References):**
*   策略逻辑详见：[扎针抄底卖期权.md](pkg/strategies/buy_flash_low/扎针抄底卖期权.md)
*   现货实现逻辑：[strategy.go](pkg/strategies/buy_flash_low/strategy.go)
*   Pine 脚本（现货最终确认逻辑）：[均线过滤版震荡下沿插针评分系统_V2_均线过滤版.tv.pine](pkg/strategies/buy_flash_low/均线过滤版震荡下沿插针评分系统_V2_均线过滤版.tv.pine)
