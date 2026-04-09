package dslcatalog

import (
	"fmt"
	"math"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

func init() {
	const buyFlashLowDSL = `strategy(
  "buy-flash-low-dsl",
  expose_fields=["atr", "sma_2", "sma_6", "sma_10", "sma_15", "sma_20", "vol_norm", "vol_sma100", "l_prev", "amp_pr100", "amp_score", "vol_score"]
)

// ─── Parameters ───────────────────────────────────────────────
lookback        = input.int(20, title="Lookback Period")
min_amp_pr      = input.float(66.0, title="Min Amplitude Percentile")
score_threshold = input.int(3, title="Score Threshold")
strict_score    = input.int(5, title="Strict Score (Bearish)")
atr_trail_mult  = input.float(2.0, title="ATR Trail Multiplier")
spot_notional   = input.float(0.000001, title="Spot Notional (BTC)")

// Target DTE & delta for short put options leg
target_dte      = input.int(15, title="Target DTE")
min_dte         = input.int(7, title="Min DTE")
short_delta_min = input.float(0.15, title="Short Put Delta Min")
short_delta_max = input.float(0.35, title="Short Put Delta Max")
premium_target  = input.float(3.0, title="Premium Target")
tp1_pct         = input.float(0.70, title="Take-Profit 1 %")
tp2_pct         = input.float(0.88, title="Take-Profit 2 %")

// ─── Indicators (injected via expose_fields) ──────────────────
// atr, sma_*, vol_*, l_prev, amp_pr100, amp_score, vol_score
// are computed by the engine (registered in Init) and exposed here

plot(nz(atr, 0), title="ATR")
plot(nz(l_prev, 0), title="Support")

// ─── Persistent state ─────────────────────────────────────────
varip highest_since = na
varip spread_id_0 = 0
varip spread_id_1 = 0

// ─── Skip warmup ─────────────────────────────────────────────
if bar_index < 100 {
  // no-op during warmup
} else {

  pos = strategy.position_size

  // ── Manage short put spreads ──────────────────────────────
  if spread_id_0 > 0 {
    s0 = spread.get(spread_id_0)
    if not s0[4] {
      spread_id_0 = 0
    } else {
      pnl0 = spread.pnl(spread_id_0)
      if pnl0 > 0 {
        spread.close(spread_id_0)
        spread_id_0 = 0
      }
    }
  }
  if spread_id_1 > 0 {
    s1 = spread.get(spread_id_1)
    if not s1[4] {
      spread_id_1 = 0
    } else {
      pnl1 = spread.pnl(spread_id_1)
      if pnl1 > 0 {
        spread.close(spread_id_1)
        spread_id_1 = 0
      }
    }
  }

  // ── Exit: trailing ATR stop ───────────────────────────────
  if pos > 0 {
    if na(highest_since) {
      highest_since = close
    } else {
      if high > highest_since {
        highest_since = high
      }
    }
    atr_val = nz(atr, 0)
    if atr_val > 0 and not na(highest_since) {
      if highest_since - close > atr_trail_mult * atr_val {
        strategy.close(id="long")
        highest_since = na
      }
    }
  } else {
    highest_since = na
  }

  // ── Entry signal ──────────────────────────────────────────
  if pos <= 0 {
    atr_val = nz(atr, 0)
    lp = nz(l_prev, 0)
    apr = nz(amp_pr100, 0)
    a_score = nz(amp_score, 0)
    v_score = nz(vol_score, 0)

    // MA bearish alignment check
    m2 = nz(sma_2, 0)
    m6 = nz(sma_6, 0)
    m10 = nz(sma_10, 0)
    m15 = nz(sma_15, 0)
    m20 = nz(sma_20, 0)

    buf = 0.05 * atr_val
    is_bearish = m2 < m6 + buf and m6 < m10 + buf and m10 < m15 + buf and m15 < m20 + buf
    threshold = score_threshold
    if is_bearish {
      threshold = strict_score
    }

    // Flash low detection
    in_bot = low <= (lp + 0.7 * atr_val) and high >= lp
    is_pin = close > 0.5 * (high + low)
    shape_ok = is_pin and apr > min_amp_pr

    if atr_val > 0 and lp > 0 and in_bot and shape_ok {
      total_score = a_score + v_score
      if total_score >= threshold and close > 0 {
        highest_since = close
        qty = spot_notional / close
        strategy.entry(id="long", direction=strategy.long, qty=qty)

        // Open short put options if chain available and no open positions
        if spread_id_0 == 0 and spread_id_1 == 0 {
          chain = options.chain()
          if not na(chain) {
            puts = options.puts(chain)
            puts = options.expiry_range(puts, min_dte, target_dte)
            puts = options.delta_range(puts, 0 - short_delta_max, 0 - short_delta_min)
            if options.len(puts) > 0 {
              sorted = options.sort_by_delta(puts, 0 - (short_delta_min + short_delta_max) / 2)
              if len(sorted) > 0 {
                c = sorted[0]
                p = contract.mark(c)
                if p > 0 {
                  put_qty = premium_target / p
                  q0 = put_qty * 0.4
                  q1 = put_qty * 0.6
                  legs0 = [leg.sell(c, q0)]
                  legs1 = [leg.sell(c, q1)]
                  spread_id_0 = spread.open(legs0, "sell put tranche0")
                  spread_id_1 = spread.open(legs1, "sell put tranche1")
                }
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
		Name:    "buy-flash-low-dsl",
		Groups:  []string{"dsl"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeSignalOnly},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			opts := bridge.Options{
				SignalSource: cfg.SignalSource,
				Config:       catalogToConfigMap(cfg),
				InitHook:     buyFlashLowInitHook,
			}
			ds := bridge.NewWithOptions(buyFlashLowDSL, opts)
			if errs := ds.ParseErrors(); len(errs) > 0 {
				return nil, fmt.Errorf("DSL parse errors in %q: %v", "buy-flash-low-dsl", errs)
			}
			return ds, nil
		},
	})
}

// buyFlashLowInitHook registers the custom computed fields that buy-flash-low-dsl
// accesses via expose_fields. This mirrors the ctx.Register calls in the legacy strategy.
func buyFlashLowInitHook(ctx *backtest.SetupContext) error {
	const lookback = 20
	ctx.Register("atr", backtest.ATR(lookback))
	ctx.Register("sma_2", backtest.SMA("close", 2))
	ctx.Register("sma_6", backtest.SMA("close", 6))
	ctx.Register("sma_10", backtest.SMA("close", 10))
	ctx.Register("sma_15", backtest.SMA("close", 15))
	ctx.Register("sma_20", backtest.SMA("close", 20))
	ctx.Register("vol_norm", backtest.CustomOptional(
		[]string{},
		[]string{"volume", "tick_count"},
		func(inputs map[string][]float64) []float64 {
			if col, ok := inputs["volume"]; ok && !bflAllNaN(col) {
				return col
			}
			if col, ok := inputs["tick_count"]; ok && !bflAllNaN(col) {
				return col
			}
			if col, ok := inputs["volume"]; ok {
				return col
			}
			if col, ok := inputs["tick_count"]; ok {
				return col
			}
			return make([]float64, 0)
		},
	))
	ctx.Register("vol_sma100", backtest.SMA("vol_norm", 100))
	ctx.Register("l_prev", backtest.Custom(
		[]string{"low"},
		func(inputs map[string][]float64) []float64 {
			low := inputs["low"]
			n := len(low)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if i < lookback {
					out[i] = math.NaN()
					continue
				}
				minVal := math.Inf(1)
				for j := i - lookback; j < i; j++ {
					if !math.IsNaN(low[j]) && low[j] < minVal {
						minVal = low[j]
					}
				}
				if math.IsInf(minVal, 1) {
					out[i] = math.NaN()
				} else {
					out[i] = minVal
				}
			}
			return out
		},
	))
	ctx.Register("amp", backtest.Custom(
		[]string{"high", "low", "close"},
		func(inputs map[string][]float64) []float64 {
			high, low, cls := inputs["high"], inputs["low"], inputs["close"]
			n := len(high)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if math.IsNaN(high[i]) || math.IsNaN(low[i]) || math.IsNaN(cls[i]) || cls[i] == 0 {
					out[i] = math.NaN()
					continue
				}
				out[i] = (high[i] - low[i]) / cls[i]
			}
			return out
		},
	))
	const prPeriod = 100
	ctx.Register("amp_pr100", backtest.Custom(
		[]string{"amp"},
		func(inputs map[string][]float64) []float64 {
			amp := inputs["amp"]
			n := len(amp)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				if i < prPeriod || math.IsNaN(amp[i]) {
					out[i] = math.NaN()
					continue
				}
				count := 0
				for j := i - prPeriod; j < i; j++ {
					if !math.IsNaN(amp[j]) && amp[j] < amp[i] {
						count++
					}
				}
				out[i] = float64(count) / float64(prPeriod) * 100
			}
			return out
		},
	))
	ctx.Register("amp_score", backtest.Custom(
		[]string{"amp_pr100"},
		func(inputs map[string][]float64) []float64 {
			ampPr100 := inputs["amp_pr100"]
			n := len(ampPr100)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				score := 0
				if ampPr100[i] > 77 {
					score++
				}
				if ampPr100[i] > 90 {
					score++
				}
				out[i] = float64(score)
			}
			return out
		},
	))
	ctx.Register("vol_rank_10", backtest.Custom([]string{"vol_norm"}, bflMakeVolRank(20)))
	ctx.Register("vol_rank_20", backtest.Custom([]string{"vol_norm"}, bflMakeVolRank(60)))
	ctx.Register("vol_rank_100", backtest.Custom([]string{"vol_norm"}, bflMakeVolRank(180)))
	ctx.Register("vol_score", backtest.Custom(
		[]string{"vol_rank_10", "vol_rank_20", "vol_rank_100", "vol_sma100", "vol_norm"},
		func(inputs map[string][]float64) []float64 {
			v10 := inputs["vol_rank_10"]
			v20 := inputs["vol_rank_20"]
			v100 := inputs["vol_rank_100"]
			vsma := inputs["vol_sma100"]
			vol := inputs["vol_norm"]
			n := len(vol)
			out := make([]float64, n)
			for i := 0; i < n; i++ {
				score := 0
				if !math.IsNaN(v10[i]) && v10[i] <= 3 {
					score++
				}
				if !math.IsNaN(v20[i]) && v20[i] <= 6 {
					score++
				}
				if !math.IsNaN(v100[i]) && v100[i] <= 10 &&
					!math.IsNaN(vsma[i]) && vsma[i] > 0 && vol[i] > 2*vsma[i] {
					score++
				}
				out[i] = float64(score)
			}
			return out
		},
	))
	return nil
}

func bflMakeVolRank(window int) func(inputs map[string][]float64) []float64 {
	return func(inputs map[string][]float64) []float64 {
		vol := inputs["vol_norm"]
		n := len(vol)
		out := make([]float64, n)
		for i := 0; i < n; i++ {
			if math.IsNaN(vol[i]) {
				out[i] = math.NaN()
				continue
			}
			rank := 1
			start := i - window
			if start < 0 {
				start = 0
			}
			for j := start; j < i; j++ {
				if !math.IsNaN(vol[j]) && vol[j] > vol[i] {
					rank++
				}
			}
			out[i] = float64(rank)
		}
		return out
	}
}

func bflAllNaN(s []float64) bool {
	for _, v := range s {
		if !math.IsNaN(v) {
			return false
		}
	}
	return true
}
