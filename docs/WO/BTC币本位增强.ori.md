在24h或 12h级别做 btc 币本位增强策略


1、当 rsi（20*10） 大于50，则 在 12h 或24h级别上找  macd或cci 的背离信号，当出现背离信号后出现一根阴线， 则开始建仓  sell call  和 buy put的头寸
开仓后的最低价 向上反弹3*atr spot平仓

初始资金100个btc， 每次开仓10个btc的名义价值。 选择delta为0.3的看涨期权，每次开仓，计算卖出call 的期权费，期权费中的 0.7，用来买 delta为-0.25的看跌期权，期限都选40天左右到期的


2、当rsi（20*10） 小于50，则在3h 或 6h 级别找 背离信号，建仓 sell call 和buyput 头寸；  选择delta为0.3的看涨期权，每次开仓，计算卖出call 的期权费，期权费中的 0.7，用来买 delta为-0.25的看跌期权，期限都选25天左右到期的
同


背离信号的定义包含两个条件：1st，macd背离； 2、波动率条件（std/mastd 过去100根k线，当前处在50分位以上）
// 顶背离 HH30 = HHV(HIGH, 30); PREV_HH = REF(HH30, 1); DIFF_HH = REF(DIFF, BARSSINCE(HIGH == HH30)); PREV_DIFF_HH = REF(DIFF, BARSSINCE(HIGH == PREV_HH)); SELL_SIGNAL = (HIGH == HH30) AND (HIGH > PREV_HH) AND (DIFF_HH < PREV_DIFF_HH);

动态管理：
1、当持有的put期权 delta绝对值大于0.5 或浮盈超过50%，则自动换仓。换仓按照之前的流程，  卖出call 的头寸 浮盈超过70%平一半，浮盈超过88%，全部平掉。
