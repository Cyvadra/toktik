package dslcatalog

func init() {
	const emaAtrSpotDSL = `//@version=6
strategy("ema-atr-spot-dsl")

// Parameters
fast_period = input.int(20, title="Fast EMA Period")
slow_period = input.int(50, title="Slow EMA Period")
atr_period = input.int(14, title="ATR Period")
atr_mult = input.float(2.0, title="ATR Trailing Multiplier")
vol_period = input.int(20, title="Volume SMA Period")
vol_ratio_min = input.float(1.2, title="Min Volume Ratio")
position_pct = input.float(0.95, title="Position % of Equity", minval=0.01, maxval=1.0)

// Indicators
ema_fast = ta.ema(close, fast_period)
ema_slow = ta.ema(close, slow_period)
atr_val = ta.atr(atr_period)
vol_sma = ta.sma(volume, vol_period)
vol_ratio = volume / nz(vol_sma, 1)
buy_cross = ta.crossover(ema_fast, ema_slow)

plot(ema_fast, title="EMA Fast")
plot(ema_slow, title="EMA Slow")

// Persistent state
varip highest_since = na

// Entry and exit
pos = strategy.position_size
if pos <= 0
    highest_since = na
    if buy_cross and close > 0 and close > open and close > ema_fast and ema_fast > ema_slow and vol_ratio >= vol_ratio_min
        budget = math.min(strategy.cash, strategy.equity * position_pct)
        qty = budget / close
        if qty > 0
            strategy.entry(id="long", direction=strategy.long, qty=qty)
else
    if na(highest_since) or high > highest_since
        highest_since = high

    if close < ema_fast and ema_fast < ema_slow
        strategy.close(id="long")
        highest_since = na
    else
        if atr_val > 0 and not na(highest_since)
            stop_price = highest_since - atr_mult * atr_val
            if low <= stop_price
                strategy.close(id="long")
                highest_since = na
`
	_ = RegisterDSL("ema-atr-spot-dsl", emaAtrSpotDSL)
}
