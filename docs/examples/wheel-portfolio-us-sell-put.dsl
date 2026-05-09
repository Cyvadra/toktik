// Weighted wheel phase-1 example: rotate cash-secured short puts across a US portfolio.
// Current multi-symbol DSL/runtime can roll the short-put leg, but does not yet model
// assignment plus covered-call rotation, so this script focuses on the put-writing side
// of the wheel while preserving per-symbol state.

strategy("Weighted Wheel Put Writer")

min_dte = input.int(20, title="Min DTE", minval=5, maxval=90)
max_dte = input.int(45, title="Max DTE", minval=5, maxval=120)
target_delta = input.float(0.25, title="Target Abs Delta", minval=0.05, maxval=0.50, step=0.01)
delta_band = input.float(0.08, title="Delta Band", minval=0.02, maxval=0.20, step=0.01)
min_bid = input.float(0.80, title="Min Bid", minval=0.10, maxval=10.00, step=0.10)
profit_take = input.float(0.60, title="Profit Take Pct", minval=0.10, maxval=0.95, step=0.05)
roll_dte = input.int(7, title="Roll DTE", minval=1, maxval=30)
contract_budget = input.float(12000, title="Contract Budget", minval=1000, maxval=100000, step=500)
max_open = input.int(6, title="Max Open Symbols", minval=1, maxval=12)

plot(strategy.equity, title="equity", precision=2)
plot(spread.count(), title="open_spreads", precision=0)

for item in portfolio.items() {
  symbol = item[0]
  weight = item[1]
  state_key = str.format("wheel_%s", symbol)

  if ref.has(state_key) {
    sid = ref.get(state_key)
    short_put = spread.leg_contract(sid, 0)
    premium_base = spread.leg_entry_price(sid, 0) * spread.leg_qty(sid, 0)
    profit_ratio = premium_base > 0 ? spread.pnl(sid) / premium_base : 0

    if contract.dte(short_put) <= roll_dte or profit_ratio >= profit_take {
      spread.close(sid, str.format("%s_put_roll", symbol))
      ref.clear(state_key)
    }
  } else if spread.count() < max_open {
    chain = options.chain("us", symbol)
    if chain != na {
      puts = options.puts(chain)
      expiry_slice = options.expiry_range(puts, min_dte, max_dte)
      liquid = options.min_premium(expiry_slice, min_bid)
      shortlist = options.delta_range(liquid, -target_delta-delta_band, -target_delta+delta_band)
      ranked = options.sort_by_delta(shortlist, -target_delta)

      if len(ranked) > 0 {
        short_put = ranked[0]
        qty = math.max(1, math.floor((strategy.equity * weight) / contract_budget))
        sid = spread.open_on("us", symbol, [leg.sell(short_put, qty)], str.format("wheel_put_%s", symbol))
        if sid != na {
          ref.set(state_key, sid)
        }
      }
    }
  }
}