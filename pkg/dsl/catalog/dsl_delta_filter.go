package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	const deltaFilterDSL = `//@version=6
strategy("delta-filter-dsl")

// Parameters
rsi_period = input.int(14, title="RSI Period", minval=2, maxval=100)
rsi_entry = input.float(30.0, title="RSI Entry", minval=1.0, maxval=80.0, step=0.5)
rsi_exit = input.float(70.0, title="RSI Exit", minval=20.0, maxval=99.0, step=0.5)
target_dte = input.int(30, title="Target DTE", minval=5, maxval=180)
delta_min = input.float(0.30, title="Delta Min", minval=0.05, maxval=0.95, step=0.01)
delta_max = input.float(0.70, title="Delta Max", minval=0.05, maxval=0.95, step=0.01)
target_delta = input.float(0.50, title="Target Delta", minval=0.05, maxval=0.95, step=0.01)
contract_budget = input.float(10000, title="Contract Budget", minval=1000, maxval=1000000, step=1000)
max_contracts = input.int(10, title="Max Contracts", minval=1, maxval=100)

// Indicators
rsi_val = ta.rsi(close, rsi_period)
rsi_entry_signal = ta.crossunder(rsi_val, rsi_entry)

plot(rsi_val, title="RSI", precision=2)
plot(spread.count(), title="open_spreads", precision=0)

varip spread_id = na

has_position = not na(spread_id)

if has_position
    current_contract = spread.leg_contract(spread_id, 0)
    current_delta = contract.delta(current_contract)
    delta_in_range = current_delta >= delta_min and current_delta <= delta_max

    if not delta_in_range or rsi_val >= rsi_exit
        spread.close(spread_id, "delta_or_rsi_exit")
        spread_id = na

if not has_position and rsi_entry_signal
    chain = options.chain()
    if chain != na
        calls = options.calls(chain)
        near = options.expiry_nearest(calls, target_dte)
        candidates = options.delta_range(near, delta_min, delta_max)
        ranked = options.sort_by_delta(candidates, target_delta)

        if len(ranked) > 0
            contract = ranked[0]
            qty = math.min(max_contracts, math.max(1, math.floor(strategy.equity / contract_budget)))
            sid = spread.open([leg.buy(contract, qty)], "delta_filter_call")
            if sid != na
                spread_id = sid
`
	_ = RegisterDSLWithMetadata(catalog.Registration{
		Name:    "delta-filter-dsl",
		Groups:  []string{"dsl"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
	}, deltaFilterDSL)
}
