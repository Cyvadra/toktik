package dslcatalog

func init() {
	const goldenCrossDSL = `//@version=6
strategy("golden-cross-dsl")

// Parameters
fast_period = input.int(10, title="Fast SMA Period")
slow_period = input.int(50, title="Slow SMA Period")
position_pct = input.float(0.95, title="Position % of Equity", minval=0.01, maxval=1.0)
entry_twap = input.int(1, title="Entry TWAP Bars", minval=1)

// Indicators
sma_fast = ta.sma(close, fast_period)
sma_slow = ta.sma(close, slow_period)
buy_sig = ta.crossover(sma_fast, sma_slow)
sell_sig = ta.crossunder(sma_fast, sma_slow)

plot(sma_fast, title="SMA Fast")
plot(sma_slow, title="SMA Slow")

// Entry
if buy_sig and strategy.position_size == 0
    budget = math.min(strategy.cash, strategy.equity * position_pct)
    qty = budget / close
    if qty > 0
        strategy.entry(id="long", direction=strategy.long, qty=qty, twap_bars=entry_twap)

// Exit
if sell_sig and strategy.position_size > 0
    strategy.close(id="long")
`
	_ = RegisterDSL("golden-cross-dsl", goldenCrossDSL)
}
