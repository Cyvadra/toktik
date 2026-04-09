package dslcatalog

import (
	"fmt"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	dualspreadsvol "github.com/Cyvadra/toktik/pkg/strategies/dual_spreads_btc_volatility"
)

func init() {
	const dualSpreadsDSL = `strategy(
  "dual-spreads-btc-volatility-dsl",
  signal_source="pkg/strategies/dual_spreads_btc_volatility/another_format_utc8.csv",
  signal_name="entry_signal",
  signal_direction_column="",
  signal_action_column="entry_add_count",
  signal_time_layout="2006-01-02 15:04",
  signal_timezone="UTC+8",
  expose_fields=["entry_signal", "entry_add_count", "hv_100_12h", "hv_pr_100_12h", "hv_q66_100_12h", "dvol_12h", "iv_pr_200_12h", "iv_q66_200_12h"]
)

// ─── Parameters ───────────────────────────────────────────────
amount_base      = input.float(2.0, title="Base Amount (BTC)")
min_dte          = input.int(20, title="Min DTE")
max_dte          = input.int(40, title="Max DTE")
vol_percentile   = input.float(66.0, title="Vol Percentile Threshold")
roll_profit_pct  = input.float(0.50, title="Roll Profit %")
roll_delta_inc   = input.float(0.20, title="Roll Delta Increase")
decay_factor     = input.float(0.90, title="Decay Factor")
min_long_delta   = input.float(0.20, title="Min Long Delta")
max_long_delta   = input.float(0.80, title="Max Long Delta")

plot(nz(entry_signal, 0), title="Entry Signal", precision=0)

// ─── Persistent state ─────────────────────────────────────────
varip active_group = 0
varip roll_count = 0

// ─── Manage existing groups ───────────────────────────────────
if active_group > 0 {
  gi = group.get(active_group)
  if gi[4] {
    // group is closed
    active_group = 0
    roll_count = 0
  } else {
    // Check each open spread for roll/TP
    open_ids = spread.open_ids()
    i = 0
    while i < len(open_ids) {
      sid = open_ids[i]
      info = spread.get(sid)
      if info[4] {
        pnl = spread.pnl(sid)
        if pnl > 0 {
          // Take profit or roll
          spread.close(sid)
        }
      }
      i = i + 1
    }
  }
}

// ─── Entry ────────────────────────────────────────────────────
sig = nz(entry_signal, 0)
if sig >= 1 {
  add_count = nz(entry_add_count, 0)
  hv = nz(hv_100_12h, 0)
  dvol = nz(dvol_12h, 0)
  hv_threshold = nz(hv_q66_100_12h, 0)
  iv_threshold = nz(iv_q66_200_12h, 0)
  hv_pr = nz(hv_pr_100_12h, 0)
  iv_pr = nz(iv_pr_200_12h, 0)

  // Init entry filter
  entry_ok = false
  if sig == 1 {
    // Volatility check for init entries
    if hv > 0 and dvol > 0 {
      if hv_threshold > 0 and hv <= hv_threshold {
        entry_ok = true
      }
      if not entry_ok and iv_threshold > 0 and dvol <= iv_threshold {
        entry_ok = true
      }
    }
  } else {
    // Add-on entries always allowed
    entry_ok = true
  }

  if entry_ok {
    chain = options.chain()
    if not na(chain) {
      // Dynamic long delta based on HV/IV percentiles
      long_delta = ((2 * hv_pr) + iv_pr) / 300 - 0.1
      if long_delta < min_long_delta {
        long_delta = min_long_delta
      }
      if long_delta > max_long_delta {
        long_delta = max_long_delta
      }

      // Amount with decay for add-ons
      amount = amount_base
      if add_count > 0 {
        d = 1
        while d <= add_count {
          amount = amount * decay_factor
          d = d + 1
        }
      }

      calls = options.calls(chain)
      calls = options.expiry_range(calls, min_dte, max_dte)

      if options.len(calls) > 0 {
        // Select long call
        long_candidates = options.sort_by_delta(calls, long_delta)
        if len(long_candidates) > 0 {
          long_c = long_candidates[0]
          long_price = contract.mark(long_c)
          long_strike = contract.strike(long_c)

          // Select short call (higher strike)
          short_delta = long_delta - 0.15
          if short_delta < 0.10 {
            short_delta = 0.10
          }
          short_candidates = options.sort_by_delta(calls, short_delta)
          if len(short_candidates) > 0 {
            short_c = short_candidates[0]
            short_price = contract.mark(short_c)
            short_strike = contract.strike(short_c)

            if short_strike > long_strike and long_price > short_price {
              spread_cost = long_price - short_price
              qty = amount / spread_cost

              if qty > 0 {
                if active_group == 0 {
                  active_group = group.open("dual-spreads-btc-vol", amount_base, decay_factor)
                }
                legs = [leg.buy(long_c, qty), leg.sell(short_c, qty)]
                sid = spread.open_in_group(legs, "bull-call-spread", active_group)
              }
            }
          }
        }
      }
    }
  }
}
`
	catalog.TryRegister(catalog.Registration{ //nolint:errcheck
		Name:    "dual-spreads-btc-volatility-dsl",
		Groups:  []string{"dsl"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeNone},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			h1, h2 := dualspreadsvol.DSLHooks()
			opts := bridge.Options{
				SignalSource: cfg.SignalSource,
				Config:       catalogToConfigMap(cfg),
				InitHook:     h1,
				PreloadHook:  h2,
			}
			ds := bridge.NewWithOptions(dualSpreadsDSL, opts)
			if errs := ds.ParseErrors(); len(errs) > 0 {
				return nil, fmt.Errorf("DSL parse errors in %q: %v", "dual-spreads-btc-volatility-dsl", errs)
			}
			return ds, nil
		},
	})
}
