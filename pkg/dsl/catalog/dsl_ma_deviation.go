package dslcatalog

import (
	"fmt"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

func init() {
	const maDeviationDSL = `strategy(
  "ma-deviation-spread-outer-source-dsl",
  signal_source="pkg/strategies/ma_deviation_spread_outer_source/SF18_RE_Bearish_Divergence_Only_BINANCE_BTCUSD_2026-03-30.csv",
  signal_name="entry_signal",
  signal_time_layout="2006/1/2 15:04",
  signal_timezone="UTC+8",
  signal_entry_matchers=["进场", "开仓", "entry", "open", "做空", "空头", "bearish", "divergence"],
  signal_type_column="类型",
  expose_fields=["atr", "rsi200"]
)

// ─── Parameters ───────────────────────────────────────────────
position_size     = input.float(10.0, title="Position Size (BTC)")
high_rsi_dte      = input.int(40, title="Target DTE (High RSI)")
low_rsi_dte       = input.int(25, title="Target DTE (Low RSI)")
short_call_delta  = input.float(0.30, title="Short Call Delta")
long_put_delta    = input.float(-0.25, title="Long Put Delta")
put_budget_ratio  = input.float(0.70, title="Put Budget Ratio")
put_roll_delta    = input.float(0.50, title="Put Roll Delta Trigger")
put_roll_profit   = input.float(0.50, title="Put Roll Profit Trigger")
call_tp1          = input.float(0.70, title="Call Take Profit 1 %")
call_tp2          = input.float(0.88, title="Call Take Profit 2 %")

plot(nz(entry_signal, 0), title="Entry Signal", precision=0)

// ─── Persistent state ─────────────────────────────────────────
varip call_spread_0 = 0
varip call_spread_1 = 0
varip put_spread    = 0
varip group_id      = 0
varip put_amount    = position_size * put_budget_ratio

// ─── Manage open positions ────────────────────────────────────
has_open = false

// Call tranche management
if call_spread_0 > 0 {
  s0 = spread.get(call_spread_0)
  if s0[4] {
    has_open = true
    c0 = spread.leg_contract(call_spread_0, 0)
    if not na(c0) {
      if contract.dte(c0) <= 1 {
        spread.close(call_spread_0, "到期平仓 | 空call")
        call_spread_0 = 0
      } else {
        ep0 = spread.leg_entry_price(call_spread_0, 0)
        mk0 = contract.mark(c0)
        if ep0 > 0 and not na(mk0) {
          pnl_pct0 = (ep0 - mk0) / ep0
          if pnl_pct0 >= call_tp1 {
            spread.close(call_spread_0, "止盈平仓 | 空call 70%")
            call_spread_0 = 0
          }
        }
      }
    }
  } else {
    call_spread_0 = 0
  }
}
if call_spread_1 > 0 {
  s1 = spread.get(call_spread_1)
  if s1[4] {
    has_open = true
    c1 = spread.leg_contract(call_spread_1, 0)
    if not na(c1) {
      if contract.dte(c1) <= 1 {
        spread.close(call_spread_1, "到期平仓 | 空call")
        call_spread_1 = 0
      } else {
        ep1 = spread.leg_entry_price(call_spread_1, 0)
        mk1 = contract.mark(c1)
        if ep1 > 0 and not na(mk1) {
          pnl_pct1 = (ep1 - mk1) / ep1
          if pnl_pct1 >= call_tp2 {
            spread.close(call_spread_1, "止盈平仓 | 空call 88%")
            call_spread_1 = 0
          }
        }
      }
    }
  } else {
    call_spread_1 = 0
  }
}

// Put protection management
if put_spread > 0 {
  sp = spread.get(put_spread)
  if sp[4] {
    has_open = true
    p_c = spread.leg_contract(put_spread, 0)
    if not na(p_c) {
      if contract.dte(p_c) <= 1 {
        spread.close(put_spread, "到期平仓 | 多put")
        put_spread = 0
      } else {
        p_ep = spread.leg_entry_price(put_spread, 0)
        p_mk = contract.mark(p_c)
        p_abs_delta = math.abs(contract.delta(p_c))
        if p_ep > 0 and not na(p_mk) {
          p_pnl_pct = (p_mk - p_ep) / p_ep
          if p_abs_delta >= put_roll_delta or p_pnl_pct >= put_roll_profit {
            spread.close(put_spread, "换仓开仓 | 多put保护")
            put_spread = 0
            // Reopen put leg with fixed DTE range
            chain2 = options.chain()
            if not na(chain2) {
              puts2 = options.puts(chain2)
              puts2 = options.expiry_range(puts2, low_rsi_dte, high_rsi_dte)
              pcands2 = options.sort_by_delta(puts2, long_put_delta)
              if len(pcands2) > 0 {
                new_put = pcands2[0]
                new_put_price = contract.mark(new_put)
                if new_put_price > 0 and put_amount > 0 {
                  new_put_qty = put_amount / new_put_price
                  if new_put_qty > 0 {
                    new_pid = spread.open_in_group([leg.buy(new_put, new_put_qty)], "换仓开仓 | 多put保护", group_id)
                    if new_pid > 0 {
                      put_spread = new_pid
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  } else {
    put_spread = 0
  }
}

// Close group if all legs closed
if not has_open and group_id > 0 {
  group.close(group_id)
  group_id = 0
}

// ─── Entry ────────────────────────────────────────────────────
if not has_open and entry_signal == 1 {
  chain = options.chain()
  if not na(chain) {
    // Fixed DTE range: 25 to 40 (matching legacy selectShortCall behavior)
    min_dte = low_rsi_dte
    max_dte = high_rsi_dte

    // Select short call
    calls = options.calls(chain)
    calls = options.expiry_range(calls, min_dte, max_dte)
    call_candidates = options.sort_by_delta(calls, short_call_delta)

    if len(call_candidates) > 0 {
      sell_call = call_candidates[0]
      sell_price = contract.mark(sell_call)

      if sell_price > 0 {
        call_qty = position_size / sell_price

        // Select long put
        puts = options.puts(chain)
        puts = options.expiry_range(puts, min_dte, max_dte)
        put_candidates = options.sort_by_delta(puts, long_put_delta)

        if len(put_candidates) > 0 {
          buy_put = put_candidates[0]
          put_price = contract.mark(buy_put)
          put_budget = sell_price * call_qty * put_budget_ratio
          put_qty = put_budget / put_price

          if put_qty > 0 {
            group_id = group.open("ma-deviation-outer-source", position_size, 1.0)
            put_amount = put_budget

            // Open put protection
            put_legs = [leg.buy(buy_put, put_qty)]
            put_spread = spread.open_in_group(put_legs, "多put保护", group_id)

            // Open call tranche 0 (70% TP)
            call_qty_0 = call_qty * 0.4
            call_legs_0 = [leg.sell(sell_call, call_qty_0)]
            call_spread_0 = spread.open_in_group(call_legs_0, "空call止盈70%", group_id)

            // Open call tranche 1 (88% TP)
            call_qty_1 = call_qty * 0.6
            call_legs_1 = [leg.sell(sell_call, call_qty_1)]
            call_spread_1 = spread.open_in_group(call_legs_1, "空call止盈88%", group_id)
          }
        }
      }
    }
  }
}
`
	catalog.TryRegister(catalog.Registration{ //nolint:errcheck
		Name:    "ma-deviation-spread-outer-source-dsl",
		Groups:  []string{"dsl"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			opts := bridge.Options{
				SignalSource: cfg.SignalSource,
				Config:       catalogToConfigMap(cfg),
				InitHook:     maDeviationInitHook,
			}
			ds := bridge.NewWithOptions(maDeviationDSL, opts)
			if errs := ds.ParseErrors(); len(errs) > 0 {
				return nil, fmt.Errorf("DSL parse errors in %q: %v", "ma-deviation-spread-outer-source-dsl", errs)
			}
			return ds, nil
		},
	})
}

func maDeviationInitHook(ctx *backtest.SetupContext) error {
	ctx.Register("atr", backtest.ATR(20))
	ctx.Register("rsi200", backtest.RSI("close", 200))
	return nil
}
