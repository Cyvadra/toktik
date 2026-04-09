package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	const coveredCallDSL = `strategy(
  "covered-call-0330-tvsig-dsl",
  signal_source="data/signals/covered_call_0330_tvsig/12h.txt,data/signals/covered_call_0330_tvsig/6h.txt",
  signal_name="entry_signal",
  signal_time_layout="Jan 2, 2006, 15:04",
  signal_timezone="UTC",
  signal_optional_index=true
)

// ─── Parameters ───────────────────────────────────────────────
call_amount_total   = input.float(10.0, title="Call Amount Total (BTC)")
put_amount_init     = input.float(7.0, title="Put Amount Initial (BTC)")
put_decay_factor    = input.float(0.80, title="Put Roll Decay Factor")

call_target_dte     = input.int(25, title="Call Target DTE")
call_bias_dte       = input.int(8, title="Call DTE Bias")
put_target_dte      = input.int(35, title="Put Target DTE")
put_bias_dte        = input.int(10, title="Put DTE Bias")

sell_call_delta      = input.float(0.35, title="Sell Call Target Delta")
buy_call_hedge_delta = input.float(0.10, title="Buy Call Hedge Delta")
buy_put_delta        = input.float(-0.25, title="Buy Put Delta")

tp1_pct             = input.float(0.60, title="Tranche0 TP Profit %")
tp2_pct             = input.float(0.85, title="Tranche1 TP Profit %")
sl_mult             = input.float(2.0, title="Stop-Loss Price Multiple")

tranche0_pct        = input.float(0.40, title="Tranche0 Fraction")
tranche1_pct        = input.float(0.60, title="Tranche1 Fraction")

prot_roll_delta     = input.float(0.50, title="Prot Roll Delta Limit")
prot_roll_profit    = input.float(0.30, title="Prot Roll Profit Limit")
expiry_close_days   = input.int(1, title="Close Before Expiry Days")

plot(entry_signal, title="Entry Signal", precision=0)

// ─── Persistent state ─────────────────────────────────────────
varip call_spread_0 = 0
varip call_spread_1 = 0
varip prot_leg_id   = 0
varip prot_is_call  = 0
varip prot_amount   = put_amount_init
varip group_id      = 0

// ─── Helper: check if any position is open ────────────────────
has_open = false
if call_spread_0 > 0 {
  info0 = spread.get(call_spread_0)
  if info0[4] {
    has_open = true
  } else {
    call_spread_0 = 0
  }
}
if call_spread_1 > 0 {
  info1 = spread.get(call_spread_1)
  if info1[4] {
    has_open = true
  } else {
    call_spread_1 = 0
  }
}
if prot_leg_id > 0 {
  pinfo = spread.get(prot_leg_id)
  if pinfo[4] {
    has_open = true
  } else {
    prot_leg_id = 0
  }
}

// ─── Position management ──────────────────────────────────────
stop_loss_triggered = false
if has_open {
  chain = options.chain()
  if not na(chain) {
    // ── Call spread management ──────────────────────────────
    // Tranche 0: TP at tp1_pct; stop-loss at 2× entry credit
    if call_spread_0 > 0 and not stop_loss_triggered {
      s0_open_0 = spread.leg_open(call_spread_0, 0)
      s0_open_1 = spread.leg_open(call_spread_0, 1)
      if not s0_open_0 and not s0_open_1 {
        call_spread_0 = 0
      } else {
        s0_c0 = spread.leg_contract(call_spread_0, 0)
        s0_c1 = spread.leg_contract(call_spread_0, 1)
        s0_ep0 = spread.leg_entry_price(call_spread_0, 0)
        s0_ep1 = spread.leg_entry_price(call_spread_0, 1)
        s0_entry_credit = s0_ep0 - s0_ep1
        if not na(s0_c0) and contract.dte(s0_c0) <= expiry_close_days {
          spread.close(call_spread_0, "到期平仓 | 空call spread")
          call_spread_0 = 0
        } else if not na(s0_c0) and not na(s0_c1) and s0_entry_credit > 0 {
          s0_mark0 = contract.mark(s0_c0)
          s0_mark1 = contract.mark(s0_c1)
          s0_close_cost = s0_mark0 - s0_mark1
          if s0_close_cost >= s0_entry_credit * sl_mult {
            if call_spread_0 > 0 {
              spread.close(call_spread_0, "止损全仓 | CALL spread价差翻倍")
              call_spread_0 = 0
            }
            if call_spread_1 > 0 {
              spread.close(call_spread_1, "止损全仓 | CALL spread价差翻倍")
              call_spread_1 = 0
            }
            if prot_leg_id > 0 {
              spread.close(prot_leg_id, "止损全仓 | CALL spread价差翻倍")
              prot_leg_id = 0
            }
            if group_id > 0 {
              group.close(group_id)
              group_id = 0
            }
            stop_loss_triggered = true
          } else {
            s0_pnl_pct = (s0_entry_credit - s0_close_cost) / s0_entry_credit
            if s0_pnl_pct >= tp1_pct {
              spread.close(call_spread_0, "止盈40% | 空call spread 60%盈利")
              call_spread_0 = 0
            }
          }
        }
      }
    }

    // Tranche 1: TP at tp2_pct
    if call_spread_1 > 0 and not stop_loss_triggered {
      s1_open_0 = spread.leg_open(call_spread_1, 0)
      s1_open_1 = spread.leg_open(call_spread_1, 1)
      if not s1_open_0 and not s1_open_1 {
        call_spread_1 = 0
      } else {
        s1_c0 = spread.leg_contract(call_spread_1, 0)
        s1_c1 = spread.leg_contract(call_spread_1, 1)
        s1_ep0 = spread.leg_entry_price(call_spread_1, 0)
        s1_ep1 = spread.leg_entry_price(call_spread_1, 1)
        s1_entry_credit = s1_ep0 - s1_ep1
        if not na(s1_c0) and contract.dte(s1_c0) <= expiry_close_days {
          spread.close(call_spread_1, "到期平仓 | 空call spread")
          call_spread_1 = 0
        } else if not na(s1_c0) and not na(s1_c1) and s1_entry_credit > 0 {
          s1_mark0 = contract.mark(s1_c0)
          s1_mark1 = contract.mark(s1_c1)
          s1_close_cost = s1_mark0 - s1_mark1
          s1_pnl_pct = (s1_entry_credit - s1_close_cost) / s1_entry_credit
          if s1_pnl_pct >= tp2_pct {
            spread.close(call_spread_1, "止盈全仓 | 空call spread 85%盈利")
            call_spread_1 = 0
          }
        }
      }
    }

    // ── Protective leg management ───────────────────────────
    if prot_leg_id > 0 and not stop_loss_triggered {
      p_open = spread.leg_open(prot_leg_id, 0)
      if not p_open {
        prot_leg_id = 0
      } else {
        p_c = spread.leg_contract(prot_leg_id, 0)
        p_ep = spread.leg_entry_price(prot_leg_id, 0)
        if not na(p_c) {
          if contract.dte(p_c) <= expiry_close_days {
            spread.close(prot_leg_id, "到期平仓 | 保护仓")
            prot_leg_id = 0
          } else {
            p_mark = contract.mark(p_c)
            p_abs_delta = math.abs(contract.delta(p_c))
            p_pnl_pct = (p_mark - p_ep) / p_ep
            if p_abs_delta >= prot_roll_delta or p_pnl_pct >= prot_roll_profit {
              spread.close(prot_leg_id, "换仓 | 平保护仓")
              prot_leg_id = 0
              prot_amount = prot_amount * put_decay_factor
              prot_is_call = 1
              // Reopen protective leg as buy call
              rc = options.calls(chain)
              rc = options.expiry_range(rc, put_target_dte - put_bias_dte, put_target_dte + put_bias_dte)
              if options.len(rc) > 0 {
                roll_cands = options.sort_by_delta(rc, math.abs(buy_put_delta))
                if len(roll_cands) > 0 {
                  roll_c = roll_cands[0]
                  roll_price = contract.mark(roll_c)
                  roll_qty = prot_amount / roll_price
                  if roll_qty > 0 {
                    new_prot_id = spread.open_in_group([leg.buy(roll_c, roll_qty)], "换仓 | 多call", group_id)
                    if new_prot_id > 0 {
                      prot_leg_id = new_prot_id
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }

  // Check if everything closed
  if call_spread_0 == 0 and call_spread_1 == 0 and prot_leg_id == 0 and group_id > 0 {
    group.close(group_id)
    group_id = 0
  }
}

// ─── Entry ────────────────────────────────────────────────────
if not has_open and entry_signal == 1 {
  chain = options.chain()
  if not na(chain) {
    // Reset protective amount for new cycle
    prot_amount = put_amount_init
    prot_is_call = 0

    // ── Select bear call spread ────────────────────────────
    min_dte = call_target_dte - call_bias_dte
    max_dte = call_target_dte + call_bias_dte
    calls = options.calls(chain)
    calls = options.expiry_range(calls, min_dte, max_dte)

    if options.len(calls) > 0 {
      // Find sell call (delta ~ 0.35) and buy call hedge (delta ~ 0.10)
      sell_candidates = options.sort_by_delta(calls, sell_call_delta)
      buy_candidates  = options.sort_by_delta(calls, buy_call_hedge_delta)

      if len(sell_candidates) > 0 and len(buy_candidates) > 0 {
        sell_call = sell_candidates[0]
        buy_call  = buy_candidates[0]
        sell_price = contract.mark(sell_call)
        buy_price  = contract.mark(buy_call)
        credit = sell_price - buy_price

        if credit > 0 {
          total_qty = call_amount_total / credit
          qty0 = total_qty * tranche0_pct
          qty1 = total_qty * tranche1_pct

          // ── Select protective put ───────────────────────
          puts = options.puts(chain)
          puts = options.expiry_range(puts, put_target_dte - put_bias_dte, put_target_dte + put_bias_dte)

          if options.len(puts) > 0 {
            put_candidates = options.sort_by_delta(puts, buy_put_delta)
            if len(put_candidates) > 0 {
              prot_put = put_candidates[0]
              put_price = contract.mark(prot_put)
              put_qty = prot_amount / put_price

              if put_qty > 0 {
                // Open group
                group_id = group.open("covered-call-0330-tvsig", call_amount_total, 1.0)

                // Open protective put first
                prot_legs = [leg.buy(prot_put, put_qty)]
                prot_leg_id = spread.open_in_group(prot_legs, "多put保护", group_id)

                // Open call spread tranche 0
                legs0 = [leg.sell(sell_call, qty0), leg.buy(buy_call, qty0)]
                call_spread_0 = spread.open_in_group(legs0, "空call spread (40%仓)", group_id)

                // Open call spread tranche 1
                legs1 = [leg.sell(sell_call, qty1), leg.buy(buy_call, qty1)]
                call_spread_1 = spread.open_in_group(legs1, "空call spread (60%仓)", group_id)
              }
            }
          }
        }
      }
    }
  }
}
`
	_ = RegisterDSLWithMetadata(catalog.Registration{
		Name:    "covered-call-0330-tvsig-dsl",
		Groups:  []string{"dsl"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
	}, coveredCallDSL)
}
