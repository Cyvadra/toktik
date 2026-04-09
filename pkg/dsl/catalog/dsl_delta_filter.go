package dslcatalog

func init() {
	const deltaFilterDSL = `strategy("delta-filter-dsl", expose_fields=["delta"])

// ─── Parameters ───────────────────────────────────────────────
position_pct = input.float(0.50, title="Position % of Equity", minval=0.01, maxval=1.0)
entry_twap   = input.int(1, title="Entry TWAP Bars", minval=1)

// ─── Indicators ───────────────────────────────────────────────
rsi14     = ta.rsi(close, 14)
delta_val = nz(delta, 0)
delta_ok  = delta_val > 0.3 and delta_val < 0.7

// ─── Entry ────────────────────────────────────────────────────
pos = strategy.position_size

if delta_ok and rsi14 < 30 and pos == 0 {
  budget = math.min(strategy.cash, strategy.equity * position_pct)
  qty    = budget / close
  if qty > 0 {
    strategy.entry(id="long", direction=strategy.long, qty=qty, twap_bars=entry_twap)
  }
}

// ─── Exit ─────────────────────────────────────────────────────
if (not delta_ok or rsi14 > 70) and pos > 0 {
  strategy.close(id="long")
}
`
	_ = RegisterDSL("delta-filter-dsl", deltaFilterDSL)
}
