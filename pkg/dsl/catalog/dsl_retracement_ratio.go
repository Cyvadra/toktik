package dslcatalog

import (
	"fmt"
	"os"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

func init() {
	const retracementRatioDSL = `strategy(
  "retracement-ratio-protective-spread-dsl",
  signal_source="data/signals/retracement_ratio_protective_spread/12h_short.csv,data/signals/retracement_ratio_protective_spread/12h_long.csv",
  signal_name="entry_signal",
  signal_name_column="信号",
  signal_time_layout="2006-01-02 15:04",
  signal_timezone="UTC+8",
  expose_fields=["hv_pr_100", "iv_pr_200"]
)

// ─── Parameters ───────────────────────────────────────────────
ambush_dte        = input.int(70, title="Ambush Target DTE")
ambush_min_dte    = input.int(55, title="Ambush Min DTE")
ambush_max_dte    = input.int(85, title="Ambush Max DTE")
ambush_premium    = input.float(5.0, title="Ambush Premium (BTC)")
ambush_tp1        = input.float(0.33, title="Ambush TP1 %")
ambush_tp2        = input.float(0.60, title="Ambush TP2 %")
ambush_roll_min_dte = input.float(20.0, title="Ambush Roll Min DTE")
ambush_roll_max_dte = input.float(40.0, title="Ambush Roll Max DTE")
ambush_roll_atr_dist = input.float(2.0, title="Ambush Roll ATR Distance")
ambush_stop_atr_mult = input.float(8.0, title="Ambush Stop ATR Multiplier")

trend_dte         = input.int(35, title="Trend Target DTE")
trend_min_dte     = input.int(25, title="Trend Min DTE")
trend_max_dte     = input.int(40, title="Trend Max DTE")
trend_amount      = input.float(2.0, title="Trend Amount (BTC)")
trend_decay       = input.float(0.90, title="Trend Decay Factor")
trend_roll_profit = input.float(0.50, title="Trend Roll Profit %")
trend_roll_delta_inc = input.float(0.20, title="Trend Roll Delta Increase")

min_long_delta    = input.float(0.20, title="Min Long Delta")
max_long_delta    = input.float(0.80, title="Max Long Delta")
min_trend_leg_delta = input.float(0.10, title="Min Trend Leg Delta")
max_trend_leg_delta = input.float(0.80, title="Max Trend Leg Delta")
short_strike_max_multiple = input.float(0.80, title="Short Trend Strike Max Multiple")
long_strike_min_multiple = input.float(1.15, title="Long Trend Strike Min Multiple")

// ─── Multi-state persistent storage ──────────────────────────
// Each index i represents one active state slot.
// st_sc[i] = number of spread IDs owned by state i; st_sf = flat spread IDs for all states concatenated.
varip st_gids   = []   // group_id per state
varip st_phases = []   // 1=ambush, 2=trend
varip st_sides  = []   // 1=short, 2=long
varip st_pt     = []   // partial_taken (0/1)
varip st_as     = []   // ambush_long_spend
varip st_ep     = []   // entry_underlying
varip st_ea     = []   // entry_atr
varip st_ed     = []   // entry_long_abs_delta
varip st_sc     = []   // spread count per state
varip st_sf     = []   // flat spread IDs across all states

hv_pr   = nz(hv_pr_100, 50)
iv_pr   = nz(iv_pr_200, 50)
atr_val = nz(ta.atr(20), 0)
chain   = options.chain()

// ─── Manage existing states (build new_* arrays) ─────────────
new_gids   = []
new_phases = []
new_sides  = []
new_pt     = []
new_as     = []
new_ep     = []
new_ea     = []
new_ed     = []
new_sc     = []
new_sf     = []

si = 0
while si < len(st_gids) {
  cur_gid   = st_gids[si]
  cur_phase = st_phases[si]
  cur_side  = st_sides[si]
  cur_pt_v  = st_pt[si]
  cur_as_v  = st_as[si]
  cur_ep_v  = st_ep[si]
  cur_ea_v  = st_ea[si]
  cur_ed_v  = st_ed[si]

  // Compute base offset for this state's sids in st_sf
  si_off = 0
  k = 0
  while k < si {
    si_off = si_off + st_sc[k]
    k = k + 1
  }

  // Extract this state's spread IDs
  cur_sids = []
  k = 0
  while k < st_sc[si] {
    cur_sids = cur_sids + [st_sf[si_off + k]]
    k = k + 1
  }

  // Filter to open spreads only
  open_sids = []
  k = 0
  while k < len(cur_sids) {
    sinfo = spread.get(cur_sids[k])
    if sinfo[4] {
      open_sids = open_sids + [cur_sids[k]]
    }
    k = k + 1
  }

  gi        = group.get(cur_gid)
  side_note = "short"
  if cur_side == 2 {
    side_note = "long"
  }
  state_done = gi[4] or len(open_sids) == 0
  tp2_done   = false

  if not state_done {
    // ── Phase 1: Ambush management ──────────────────────────
    if cur_phase == 1 {
      total_pnl = 0.0
      k = 0
      while k < len(open_sids) {
        total_pnl = total_pnl + spread.pnl(open_sids[k])
        k = k + 1
      }
      tp1_thresh = cur_as_v * ambush_tp1
      tp2_thresh = cur_as_v * ambush_tp2

      // TP1: partial close (one tranche)
      if cur_pt_v == 0 and len(open_sids) > 0 and total_pnl >= tp1_thresh {
        spread.close(open_sids[0], side_note + "|一期止盈33%减仓")
        cur_pt_v  = 1
        remaining = []
        k = 1
        while k < len(open_sids) {
          remaining = remaining + [open_sids[k]]
          k = k + 1
        }
        open_sids = remaining
      }

      // TP2: close all ambush spreads and open trend immediately (new group)
      if total_pnl >= tp2_thresh and not na(chain) {
        k = 0
        while k < len(open_sids) {
          spread.close(open_sids[k], side_note + "|一期止盈60%转二期")
          k = k + 1
        }
        group.close(cur_gid)
        state_done = true
        tp2_done   = true

        tp2_is_long    = cur_side == 2
        tp2_amount     = trend_amount
        tp2_tgt_delta  = ((2 * hv_pr) + iv_pr) / 300
        if tp2_tgt_delta < min_long_delta { tp2_tgt_delta = min_long_delta }
        if tp2_tgt_delta > max_long_delta { tp2_tgt_delta = max_long_delta }
        tp2_contracts = options.puts(chain)
        tp2_srt_delta = -1 * tp2_tgt_delta
        if tp2_is_long {
          tp2_contracts = options.calls(chain)
          tp2_srt_delta = tp2_tgt_delta
        }
        tp2_contracts = options.expiry_range(tp2_contracts, trend_min_dte, trend_max_dte)
        tp2_best_long        = na
        tp2_best_short       = na
        tp2_best_long_price  = 0.0
        tp2_best_short_price = 0.0
        if options.len(tp2_contracts) > 0 and tp2_amount > 0 {
          tp2_ordered = options.sort_by_delta(tp2_contracts, tp2_srt_delta)
          tp2_idx = 0
          while tp2_idx < len(tp2_ordered) and na(tp2_best_long) {
            tp2_lc  = tp2_ordered[tp2_idx]
            tp2_lab = math.abs(contract.delta(tp2_lc))
            tp2_lp  = contract.mark(tp2_lc)
            if tp2_lab >= min_trend_leg_delta and tp2_lab <= max_trend_leg_delta and tp2_lp > 0 {
              tp2_sthresh = contract.strike(tp2_lc) * short_strike_max_multiple
              if tp2_is_long { tp2_sthresh = contract.strike(tp2_lc) * long_strike_min_multiple }
              tp2_j = 0
              while tp2_j < len(tp2_ordered) and na(tp2_best_short) {
                tp2_sc2       = tp2_ordered[tp2_j]
                tp2_sc2_delta = math.abs(contract.delta(tp2_sc2))
                tp2_sc2_price = contract.mark(tp2_sc2)
                tp2_same_exp  = contract.expiry(tp2_sc2) == contract.expiry(tp2_lc)
                tp2_stk_ok    = contract.symbol(tp2_sc2) != contract.symbol(tp2_lc)
                if tp2_is_long {
                  tp2_stk_ok = tp2_stk_ok and contract.strike(tp2_sc2) >= tp2_sthresh
                } else {
                  tp2_stk_ok = tp2_stk_ok and contract.strike(tp2_sc2) <= tp2_sthresh
                }
                if tp2_same_exp and tp2_stk_ok and tp2_sc2_delta >= min_trend_leg_delta and tp2_sc2_delta <= max_trend_leg_delta and tp2_sc2_price > 0 and tp2_lp > tp2_sc2_price {
                  tp2_best_long        = tp2_lc
                  tp2_best_short       = tp2_sc2
                  tp2_best_long_price  = tp2_lp
                  tp2_best_short_price = tp2_sc2_price
                }
                tp2_j = tp2_j + 1
              }
            }
            tp2_idx = tp2_idx + 1
          }
        }
        if not na(tp2_best_long) and not na(tp2_best_short) {
          tp2_spread_cost = tp2_best_long_price - tp2_best_short_price
          if tp2_spread_cost > 0 {
            tp2_qty = tp2_amount / tp2_spread_cost
            if tp2_qty > 0 {
              tp2_gtag = "retracement-ratio-short-trend"
              if tp2_is_long { tp2_gtag = "retracement-ratio-long-trend" }
              tp2_gid = group.open(tp2_gtag, tp2_amount, trend_decay)
              if tp2_gid > 0 {
                tp2_side_pfx = "short"
                if tp2_is_long { tp2_side_pfx = "long" }
                tp2_stag = tp2_side_pfx + "|二期借记价差|一期止盈转换|amt=" + str.tostring(tp2_amount)
                tp2_legs = [leg.buy(tp2_best_long, tp2_qty), leg.sell(tp2_best_short, tp2_qty)]
                tp2_sid  = spread.open_in_group(tp2_legs, tp2_stag, tp2_gid)
                if tp2_sid > 0 {
                  new_gids   = new_gids   + [tp2_gid]
                  new_phases = new_phases + [2]
                  new_sides  = new_sides  + [cur_side]
                  new_pt     = new_pt     + [0]
                  new_as     = new_as     + [0.0]
                  new_ep     = new_ep     + [close]
                  new_ea     = new_ea     + [atr_val]
                  new_ed     = new_ed     + [math.abs(contract.delta(tp2_best_long))]
                  new_sc     = new_sc     + [1]
                  new_sf     = new_sf     + [tp2_sid]
                } else {
                  group.close(tp2_gid)
                }
              }
            }
          }
        }
      }

      // Stop loss (only when TP2 did not fire)
      if not tp2_done and not state_done {
        stop_triggered = false
        if cur_side == 2 and cur_ea_v > 0 and close <= cur_ep_v - ambush_stop_atr_mult * cur_ea_v {
          stop_triggered = true
        }
        if cur_side == 1 and cur_ea_v > 0 and close >= cur_ep_v + ambush_stop_atr_mult * cur_ea_v {
          stop_triggered = true
        }
        if stop_triggered {
          k = 0
          while k < len(open_sids) {
            spread.close(open_sids[k], side_note + "|一期反向8ATR退出")
            k = k + 1
          }
          group.close(cur_gid)
          state_done = true
        }
      }

      // Roll condition: close + immediately reopen ambush (only when nothing else fired)
      if not tp2_done and not state_done and not na(chain) {
        min_open_dte = 999999.0
        k = 0
        while k < len(open_sids) {
          sid_k   = open_sids[k]
          lc_cnt  = spread.get(sid_k)[5]
          li = 0
          while li < lc_cnt {
            if spread.leg_open(sid_k, li) {
              lc = spread.leg_contract(sid_k, li)
              ld = contract.dte(lc)
              if ld < min_open_dte { min_open_dte = ld }
            }
            li = li + 1
          }
          k = k + 1
        }
        roll_ambush_cond = atr_val > 0 and min_open_dte > ambush_roll_min_dte and min_open_dte < ambush_roll_max_dte and math.abs(close - cur_ep_v) <= ambush_roll_atr_dist * atr_val
        if roll_ambush_cond {
          k = 0
          while k < len(open_sids) {
            spread.close(open_sids[k], side_note + "|一期近月滚动")
            k = k + 1
          }
          group.close(cur_gid)
          state_done = true

          // Open replacement ambush immediately ────────────────
          ra_is_long     = cur_side == 2
          ra_contracts   = options.puts(chain)
          ra_tgt_delta   = -0.5
          if ra_is_long {
            ra_contracts = options.calls(chain)
            ra_tgt_delta = 0.5
          }
          ra_contracts  = options.expiry_range(ra_contracts, ambush_min_dte, ambush_max_dte)
          ra_best_short = na
          ra_short_price = 0.0
          if options.len(ra_contracts) > 0 {
            ra_sorted = options.sort_by_delta(ra_contracts, ra_tgt_delta)
            ra_si = 0
            while ra_si < len(ra_sorted) and na(ra_best_short) {
              ra_cand = ra_sorted[ra_si]
              ra_cp   = contract.mark(ra_cand)
              if ra_cp > 0 {
                ra_best_short = ra_cand
                ra_short_price = ra_cp
              }
              ra_si = ra_si + 1
            }
          }
          ra_best_lower       = na
          ra_best_upper       = na
          ra_best_lower_price = 0.0
          ra_best_upper_price = 0.0
          if not na(ra_best_short) and ra_short_price > 0 {
            ra_tgt_half  = ra_short_price / 2
            ra_best_score = 999999.0
            ra_sorted2 = options.sort_by_delta(ra_contracts, ra_tgt_delta)
            ra_i = 0
            while ra_i < len(ra_sorted2) {
              ra_lc  = ra_sorted2[ra_i]
              ra_lp  = contract.mark(ra_lc)
              ra_lok = contract.symbol(ra_lc) != contract.symbol(ra_best_short) and contract.expiry(ra_lc) == contract.expiry(ra_best_short) and ra_lp > 0
              if ra_is_long {
                ra_lok = ra_lok and contract.strike(ra_lc) > contract.strike(ra_best_short)
              } else {
                ra_lok = ra_lok and contract.strike(ra_lc) < contract.strike(ra_best_short)
              }
              if ra_lok {
                ra_j = ra_i + 1
                while ra_j < len(ra_sorted2) {
                  ra_uc  = ra_sorted2[ra_j]
                  ra_up  = contract.mark(ra_uc)
                  ra_uok = contract.symbol(ra_uc) != contract.symbol(ra_best_short) and contract.expiry(ra_uc) == contract.expiry(ra_best_short) and ra_up > 0
                  if ra_is_long {
                    ra_uok = ra_uok and contract.strike(ra_uc) > contract.strike(ra_best_short)
                  } else {
                    ra_uok = ra_uok and contract.strike(ra_uc) < contract.strike(ra_best_short)
                  }
                  if ra_uok {
                    ra_pair_score = math.abs((ra_lp + ra_up) - ra_short_price) + 0.5 * (math.abs(ra_tgt_half - ra_lp) + math.abs(ra_tgt_half - ra_up))
                    if ra_pair_score < ra_best_score {
                      ra_best_score       = ra_pair_score
                      ra_best_lower       = ra_lc
                      ra_best_upper       = ra_uc
                      ra_best_lower_price = ra_lp
                      ra_best_upper_price = ra_up
                    }
                  }
                  ra_j = ra_j + 1
                }
              }
              ra_i = ra_i + 1
            }
          }
          if not na(ra_best_lower) and not na(ra_best_upper) {
            ra_sq        = ambush_premium / ra_short_price
            ra_lc_total  = ra_best_lower_price + ra_best_upper_price
            ra_lq        = ambush_premium / ra_lc_total
            if ra_sq > 0 and ra_lq > 0 and ra_lc_total > 0 {
              ra_gtag = "retracement-ratio-short-ambush"
              if ra_is_long { ra_gtag = "retracement-ratio-long-ambush" }
              ra_gid = group.open(ra_gtag, ambush_premium, 1.0)
              if ra_gid > 0 {
                ra_side_str = "short"
                if ra_is_long { ra_side_str = "long" }
                ra_new_sids = []
                ra_tr = 0
                while ra_tr < 3 {
                  ra_stag = ra_side_str + "|一期比例价差|一期滚动重建|tranche=" + str.tostring(ra_tr + 1)
                  ra_legs = [leg.sell(ra_best_short, ra_sq / 3), leg.buy(ra_best_lower, ra_lq / 3), leg.buy(ra_best_upper, ra_lq / 3)]
                  ra_sid  = spread.open_in_group(ra_legs, ra_stag, ra_gid)
                  if ra_sid > 0 {
                    ra_new_sids = ra_new_sids + [ra_sid]
                    ra_close_bars = (contract.dte(ra_best_short) - 1) * 12
                    if ra_close_bars < 1 { ra_close_bars = 1 }
                    schedule.close_spread(ra_close_bars, ra_sid, ra_side_str + "|一期到期前平仓")
                  }
                  ra_tr = ra_tr + 1
                }
                if len(ra_new_sids) > 0 {
                  new_gids   = new_gids   + [ra_gid]
                  new_phases = new_phases + [1]
                  new_sides  = new_sides  + [cur_side]
                  new_pt     = new_pt     + [0]
                  new_as     = new_as     + [ra_lq * ra_lc_total]
                  new_ep     = new_ep     + [close]
                  new_ea     = new_ea     + [atr_val]
                  new_ed     = new_ed     + [0.0]
                  new_sc     = new_sc     + [len(ra_new_sids)]
                  k = 0
                  while k < len(ra_new_sids) {
                    new_sf = new_sf + [ra_new_sids[k]]
                    k = k + 1
                  }
                } else {
                  group.close(ra_gid)
                }
              }
            }
          }
        }
      }

      // Keep ambush state if still alive
      if not tp2_done and not state_done {
        new_gids   = new_gids   + [cur_gid]
        new_phases = new_phases + [1]
        new_sides  = new_sides  + [cur_side]
        new_pt     = new_pt     + [cur_pt_v]
        new_as     = new_as     + [cur_as_v]
        new_ep     = new_ep     + [cur_ep_v]
        new_ea     = new_ea     + [cur_ea_v]
        new_ed     = new_ed     + [cur_ed_v]
        new_sc     = new_sc     + [len(open_sids)]
        k = 0
        while k < len(open_sids) {
          new_sf = new_sf + [open_sids[k]]
          k = k + 1
        }
      }
    }

    // ── Phase 2: Trend management ──────────────────────────
    if cur_phase == 2 {
      trend_sid = 0
      k = 0
      while k < len(open_sids) {
        s2info = spread.get(open_sids[k])
        if s2info[4] {
          trend_sid = open_sids[k]
          k = len(open_sids)
        }
        k = k + 1
      }

      if trend_sid == 0 {
        group.close(cur_gid)
        state_done = true
      } else {
        tleg0 = spread.leg_contract(trend_sid, 0)
        tleg1 = spread.leg_contract(trend_sid, 1)
        expiry_close = contract.dte(tleg0) <= 1 or contract.dte(tleg1) <= 1
        if expiry_close {
          spread.close(trend_sid, side_note + "|二期到期前平仓")
          group.close(cur_gid)
          state_done = true
        } else {
          tlong_entry  = spread.leg_entry_price(trend_sid, 0)
          tshort_entry = spread.leg_entry_price(trend_sid, 1)
          tentry_cost  = tlong_entry - tshort_entry
          tlong_mark   = contract.mark(tleg0)
          tshort_mark  = contract.mark(tleg1)
          tcurr_val    = tlong_mark - tshort_mark
          tpnl_pct     = -999999.0
          if tentry_cost > 0 { tpnl_pct = (tcurr_val - tentry_cost) / tentry_cost }
          tcurr_delta  = math.abs(contract.delta(tleg0))
          tdelta_gain  = tcurr_delta - cur_ed_v
          roll_trend_cond = tpnl_pct >= trend_roll_profit or tdelta_gain >= trend_roll_delta_inc

          if roll_trend_cond and not na(chain) {
            spread.close(trend_sid)
            group.increment_roll(cur_gid)
            troll_amount = group.get(cur_gid)[2]
            if troll_amount <= 0 {
              group.close(cur_gid)
              state_done = true
            } else {
              // Open new trend spread in same group ─────────────
              tr_is_long    = cur_side == 2
              tr_tgt_delta  = ((2 * hv_pr) + iv_pr) / 300
              if tr_tgt_delta < min_long_delta { tr_tgt_delta = min_long_delta }
              if tr_tgt_delta > max_long_delta { tr_tgt_delta = max_long_delta }
              tr_contracts  = options.puts(chain)
              tr_srt_delta  = -1 * tr_tgt_delta
              if tr_is_long {
                tr_contracts = options.calls(chain)
                tr_srt_delta = tr_tgt_delta
              }
              tr_contracts = options.expiry_range(tr_contracts, trend_min_dte, trend_max_dte)
              tr_best_long        = na
              tr_best_short       = na
              tr_best_long_price  = 0.0
              tr_best_short_price = 0.0
              if options.len(tr_contracts) > 0 and troll_amount > 0 {
                tr_ordered = options.sort_by_delta(tr_contracts, tr_srt_delta)
                tr_idx = 0
                while tr_idx < len(tr_ordered) and na(tr_best_long) {
                  tr_lc  = tr_ordered[tr_idx]
                  tr_lab = math.abs(contract.delta(tr_lc))
                  tr_lp  = contract.mark(tr_lc)
                  if tr_lab >= min_trend_leg_delta and tr_lab <= max_trend_leg_delta and tr_lp > 0 {
                    tr_sthresh = contract.strike(tr_lc) * short_strike_max_multiple
                    if tr_is_long { tr_sthresh = contract.strike(tr_lc) * long_strike_min_multiple }
                    tr_j = 0
                    while tr_j < len(tr_ordered) and na(tr_best_short) {
                      tr_sc2       = tr_ordered[tr_j]
                      tr_sc2_delta = math.abs(contract.delta(tr_sc2))
                      tr_sc2_price = contract.mark(tr_sc2)
                      tr_same_exp  = contract.expiry(tr_sc2) == contract.expiry(tr_lc)
                      tr_stk_ok    = contract.symbol(tr_sc2) != contract.symbol(tr_lc)
                      if tr_is_long {
                        tr_stk_ok = tr_stk_ok and contract.strike(tr_sc2) >= tr_sthresh
                      } else {
                        tr_stk_ok = tr_stk_ok and contract.strike(tr_sc2) <= tr_sthresh
                      }
                      if tr_same_exp and tr_stk_ok and tr_sc2_delta >= min_trend_leg_delta and tr_sc2_delta <= max_trend_leg_delta and tr_sc2_price > 0 and tr_lp > tr_sc2_price {
                        tr_best_long        = tr_lc
                        tr_best_short       = tr_sc2
                        tr_best_long_price  = tr_lp
                        tr_best_short_price = tr_sc2_price
                      }
                      tr_j = tr_j + 1
                    }
                  }
                  tr_idx = tr_idx + 1
                }
              }
              if not na(tr_best_long) and not na(tr_best_short) {
                tr_spread_cost = tr_best_long_price - tr_best_short_price
                if tr_spread_cost > 0 {
                  tr_qty = troll_amount / tr_spread_cost
                  if tr_qty > 0 {
                    tr_stag = "trend-bear-debit"
                    if tr_is_long { tr_stag = "trend-bull-debit" }
                    tr_legs = [leg.buy(tr_best_long, tr_qty), leg.sell(tr_best_short, tr_qty)]
                    tr_sid  = spread.open_in_group(tr_legs, tr_stag, cur_gid)
                    if tr_sid > 0 {
                      new_gids   = new_gids   + [cur_gid]
                      new_phases = new_phases + [2]
                      new_sides  = new_sides  + [cur_side]
                      new_pt     = new_pt     + [0]
                      new_as     = new_as     + [0.0]
                      new_ep     = new_ep     + [close]
                      new_ea     = new_ea     + [atr_val]
                      new_ed     = new_ed     + [math.abs(contract.delta(tr_best_long))]
                      new_sc     = new_sc     + [1]
                      new_sf     = new_sf     + [tr_sid]
                      state_done = true
                    } else {
                      group.close(cur_gid)
                      state_done = true
                    }
                  } else {
                    group.close(cur_gid)
                    state_done = true
                  }
                } else {
                  group.close(cur_gid)
                  state_done = true
                }
              } else {
                group.close(cur_gid)
                state_done = true
              }
            }
          }

          // Keep trend state when no roll
          if not state_done {
            new_gids   = new_gids   + [cur_gid]
            new_phases = new_phases + [2]
            new_sides  = new_sides  + [cur_side]
            new_pt     = new_pt     + [0]
            new_as     = new_as     + [0.0]
            new_ep     = new_ep     + [cur_ep_v]
            new_ea     = new_ea     + [cur_ea_v]
            new_ed     = new_ed     + [cur_ed_v]
            new_sc     = new_sc     + [1]
            new_sf     = new_sf     + [trend_sid]
          }
        }
      }
    }
  }

  si = si + 1
}

// Commit state updates
st_gids   = new_gids
st_phases = new_phases
st_sides  = new_sides
st_pt     = new_pt
st_as     = new_as
st_ep     = new_ep
st_ea     = new_ea
st_ed     = new_ed
st_sc     = new_sc
st_sf     = new_sf

// ─── Signal entry: open new ambush if conditions met ─────────
if len(st_gids) < 4 and signal.active() and not na(chain) {
  sig_name   = str.lower(signal.name())
  sig_source = str.lower(signal.source())
  dir_mode   = str.lower(config.string("direction", "both"))

  is_init_signal  = str.contains(sig_name, "init")
  is_short_signal = str.contains(sig_source, "short")
  is_long_signal  = str.contains(sig_source, "long")

  if dir_mode == "long_only"  { is_short_signal = false }
  if dir_mode == "short_only" { is_long_signal  = false }

  if is_init_signal and iv_pr <= 95 {
    open_side = 0
    if is_short_signal { open_side = 1 }
    if is_long_signal  { open_side = 2 }

    if open_side > 0 {
      sig_is_long    = open_side == 2
      sig_contracts  = options.puts(chain)
      sig_tgt_delta  = -0.5
      if sig_is_long {
        sig_contracts = options.calls(chain)
        sig_tgt_delta = 0.5
      }
      sig_contracts  = options.expiry_range(sig_contracts, ambush_min_dte, ambush_max_dte)
      sig_best_short = na
      sig_short_price = 0.0
      if options.len(sig_contracts) > 0 {
        sig_sorted = options.sort_by_delta(sig_contracts, sig_tgt_delta)
        sig_i = 0
        while sig_i < len(sig_sorted) and na(sig_best_short) {
          sig_cand = sig_sorted[sig_i]
          sig_cp   = contract.mark(sig_cand)
          if sig_cp > 0 {
            sig_best_short = sig_cand
            sig_short_price = sig_cp
          }
          sig_i = sig_i + 1
        }
      }
      sig_best_lower       = na
      sig_best_upper       = na
      sig_best_lower_price = 0.0
      sig_best_upper_price = 0.0
      if not na(sig_best_short) and sig_short_price > 0 {
        sig_tgt_half  = sig_short_price / 2
        sig_best_score = 999999.0
        sig_sorted2 = options.sort_by_delta(sig_contracts, sig_tgt_delta)
        sig_i2 = 0
        while sig_i2 < len(sig_sorted2) {
          sig_lc  = sig_sorted2[sig_i2]
          sig_lp  = contract.mark(sig_lc)
          sig_lok = contract.symbol(sig_lc) != contract.symbol(sig_best_short) and contract.expiry(sig_lc) == contract.expiry(sig_best_short) and sig_lp > 0
          if sig_is_long {
            sig_lok = sig_lok and contract.strike(sig_lc) > contract.strike(sig_best_short)
          } else {
            sig_lok = sig_lok and contract.strike(sig_lc) < contract.strike(sig_best_short)
          }
          if sig_lok {
            sig_j = sig_i2 + 1
            while sig_j < len(sig_sorted2) {
              sig_uc  = sig_sorted2[sig_j]
              sig_up  = contract.mark(sig_uc)
              sig_uok = contract.symbol(sig_uc) != contract.symbol(sig_best_short) and contract.expiry(sig_uc) == contract.expiry(sig_best_short) and sig_up > 0
              if sig_is_long {
                sig_uok = sig_uok and contract.strike(sig_uc) > contract.strike(sig_best_short)
              } else {
                sig_uok = sig_uok and contract.strike(sig_uc) < contract.strike(sig_best_short)
              }
              if sig_uok {
                sig_pair_score = math.abs((sig_lp + sig_up) - sig_short_price) + 0.5 * (math.abs(sig_tgt_half - sig_lp) + math.abs(sig_tgt_half - sig_up))
                if sig_pair_score < sig_best_score {
                  sig_best_score       = sig_pair_score
                  sig_best_lower       = sig_lc
                  sig_best_upper       = sig_uc
                  sig_best_lower_price = sig_lp
                  sig_best_upper_price = sig_up
                }
              }
              sig_j = sig_j + 1
            }
          }
          sig_i2 = sig_i2 + 1
        }
      }
      if not na(sig_best_lower) and not na(sig_best_upper) {
        sig_sq       = ambush_premium / sig_short_price
        sig_lc_total = sig_best_lower_price + sig_best_upper_price
        sig_lq       = ambush_premium / sig_lc_total
        if sig_sq > 0 and sig_lq > 0 and sig_lc_total > 0 {
          sig_gtag = "retracement-ratio-short-ambush"
          if sig_is_long { sig_gtag = "retracement-ratio-long-ambush" }
          sig_gid = group.open(sig_gtag, ambush_premium, 1.0)
          if sig_gid > 0 {
            sig_side_str  = "short"
            if sig_is_long { sig_side_str = "long" }
            sig_new_sids = []
            sig_tr = 0
            while sig_tr < 3 {
              sig_stag = sig_side_str + "|一期比例价差|外部信号开仓|tranche=" + str.tostring(sig_tr + 1)
              sig_legs = [leg.sell(sig_best_short, sig_sq / 3), leg.buy(sig_best_lower, sig_lq / 3), leg.buy(sig_best_upper, sig_lq / 3)]
              sig_new_sid = spread.open_in_group(sig_legs, sig_stag, sig_gid)
              if sig_new_sid > 0 {
                sig_new_sids = sig_new_sids + [sig_new_sid]
                sig_close_bars = (contract.dte(sig_best_short) - 1) * 12
                if sig_close_bars < 1 { sig_close_bars = 1 }
                schedule.close_spread(sig_close_bars, sig_new_sid, sig_side_str + "|一期到期前平仓")
              }
              sig_tr = sig_tr + 1
            }
            if len(sig_new_sids) > 0 {
              st_gids   = st_gids   + [sig_gid]
              st_phases = st_phases + [1]
              st_sides  = st_sides  + [open_side]
              st_pt     = st_pt     + [0]
              st_as     = st_as     + [sig_lq * sig_lc_total]
              st_ep     = st_ep     + [close]
              st_ea     = st_ea     + [atr_val]
              st_ed     = st_ed     + [0.0]
              st_sc     = st_sc     + [len(sig_new_sids)]
              k = 0
              while k < len(sig_new_sids) {
                st_sf = st_sf + [sig_new_sids[k]]
                k = k + 1
              }
            } else {
              group.close(sig_gid)
            }
          }
        }
      }
    }
  }
}

plot(nz(entry_signal, 0), title="Entry Signal", precision=0)
`
	_ = catalog.TryRegister(catalog.Registration{
		Name:   "retracement-ratio-protective-spread-dsl",
		Groups: []string{"dsl", "options", "spread", "timed"},
		Profile: catalog.StrategyProfile{
			UsesOptions:  true,
			RegularTrade: catalog.RegularTradeNone,
		},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			signalSource, err := retracementDSLSignalSource(cfg)
			if err != nil {
				return nil, err
			}
			opts := bridge.Options{SignalSource: signalSource, Config: catalogToConfigMap(cfg)}
			ds := bridge.NewWithOptions(retracementRatioDSL, opts)
			if errs := ds.ParseErrors(); len(errs) > 0 {
				return nil, fmt.Errorf("DSL parse errors in %q: %v", "retracement-ratio-protective-spread-dsl", errs)
			}
			return ds, nil
		},
	})
}

func retracementDSLSignalSource(cfg catalog.Config) (string, error) {
	raw := strings.TrimSpace(cfg.SignalSource)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("RETRACEMENT_RATIO_PROTECTIVE_SPREAD_SIGNAL_SOURCE"))
	}
	source := strings.ToLower(raw)
	if source == "" {
		source = "12h"
	}
	if source != "12h" && source != "1d" {
		return "", fmt.Errorf("invalid signal source %q; expected one of: 12h, 1d", raw)
	}
	base := "data/signals/retracement_ratio_protective_spread/"
	shortPath := base + source + "_short.csv"
	longPath := base + source + "_long.csv"
	switch cfg.Direction {
	case catalog.DirectionLongOnly:
		return longPath, nil
	case catalog.DirectionShortOnly:
		return shortPath, nil
	default:
		return shortPath + "," + longPath, nil
	}
}
