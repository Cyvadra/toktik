//@version=6
strategy("strong-momentum-dsl")

universe_code = input.string("strong_momentum", title="Universe Code")
top_n = input.int(5, title="Top N", minval=1, maxval=12)
target_delta = input.float(0.35, title="Target Delta", minval=0.05, maxval=0.8, step=0.01)
qty = input.float(1, title="Contracts", minval=1, maxval=20, step=1)
valuation_percentile = input.float(50, title="Valuation Percentile", minval=0, maxval=100, step=1)

symbols = universe.symbols("strong_momentum")

rsi14 = ta.rsi(close, 14)
cci20 = ta.cci(high, low, close, 20)
sma5 = ta.sma(close, 5)
sma20 = ta.sma(close, 20)
hv30 = request.factor("volatility", "1d", "hv30")
ivp = request.factor("volatility", "1d", "iv_percentile")
hvp = ta.percentrank(hv30, 80)

ctx = market.context(rsi14, cci20, hvp, ivp, valuation_percentile)
trend = market.trend_state(ctx)
strategies = options.strategies(ctx, "momentum")

plot(rsi14, title="RSI14", precision=2)
plot(cci20, title="CCI20", precision=2)
plot(len(symbols), title="universe_size", precision=0)
plot(spread.count(), title="open_spreads", precision=0)

varip opened = false

range_ok = trend != "range" or math.abs(cci20) < 101
momentum_ok = sma5 > sma20 and rsi14 > 60 and range_ok and len(strategies) > 0

if not opened and momentum_ok
    chain = options.chain()
    if chain != na
        name = strategies[0]
        sid = options.open_strategy(chain, name, qty, target_delta, "strong_momentum")
        if sid != na
            opened = true