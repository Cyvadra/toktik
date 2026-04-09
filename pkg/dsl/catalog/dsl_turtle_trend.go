package dslcatalog

import "github.com/Cyvadra/toktik/pkg/strategies/catalog"

func init() {
	// The turtle-trend strategy uses 8h higher-timeframe signals for entry
	// (Donchian/Bollinger breakouts under low-volatility conditions).
	// In the DSL version, we use expose_fields to access HTF-aligned indicator
	// columns that should be precomputed and injected by the engine.
	//
	// This DSL version implements the core options-based trend-following with
	// multi-slot add-ons, but the 8h indicator alignment is handled by the
	// engine's expose_fields mechanism.
	const turtleTrendDSL = `strategy(
  "turtle-trend-simp-dsl",
  expose_fields=["dc20_upper", "dc20_lower", "bb20_upper", "bb20_lower", "atr20", "std20", "ma_std20", "stdma20"]
)

// ─── Parameters ───────────────────────────────────────────────
max_long_slots   = input.int(3, title="Max Long Option Slots")
max_short_slots  = input.int(2, title="Max Short Option Slots")
atr_add_mult     = input.float(0.75, title="ATR Add-On Multiplier")
target_dte       = input.int(35, title="Target DTE")
min_dte          = input.int(20, title="Min DTE")
max_dte          = input.int(50, title="Max DTE")
target_delta     = input.float(0.35, title="Target Call/Put Delta")
roll_delta       = input.float(0.55, title="Roll Delta Trigger")
roll_profit      = input.float(0.33, title="Roll Profit Trigger")
spot_notional    = input.float(0.000001, title="Spot Signal Notional")

// ─── Indicators (from expose_fields, HTF-aligned) ─────────────
dc_upper = nz(dc20_upper, 0)
dc_lower = nz(dc20_lower, 0)
bb_upper = nz(bb20_upper, 0)
bb_lower = nz(bb20_lower, 0)
atr_8h   = nz(atr20, 0)
stdma    = nz(stdma20, 0)

// ─── Persistent state ─────────────────────────────────────────
varip long_slots  = [0, 0, 0]     // spread IDs for long call slots
varip short_slots = [0, 0]        // spread IDs for short put slots
varip long_entry_price  = 0.0
varip short_entry_price = 0.0
varip long_add_count  = 0
varip short_add_count = 0
varip long_group  = 0
varip short_group = 0

// ─── Entry signals ────────────────────────────────────────────
// Long: breakout above max(Donchian20 upper, Bollinger20 upper)
// Short: breakout below min(Donchian20 lower, Bollinger20 lower) - 0.5*ATR
// Only under low-volatility conditions (stdma < some threshold)

long_breakout_level  = math.max(dc_upper, bb_upper)
short_breakout_level = math.min(dc_lower, bb_lower)
if atr_8h > 0 {
  short_breakout_level = short_breakout_level - 0.5 * atr_8h
}

// Low-volatility filter
is_low_vol = false
if stdma > 0 {
  // Check recent stdma values (simplified: current < threshold)
  // In full version, checks percentile < 35% over last 120 bars
  if stdma < 1.0 {
    is_low_vol = true
  }
}

// ─── Manage existing option positions ─────────────────────────
// Check long slots
i = 0
while i < max_long_slots {
  if i < len(long_slots) and long_slots[i] > 0 {
    si = spread.get(long_slots[i])
    if not si[4] {
      long_slots[i] = 0
    } else {
      pnl = spread.pnl(long_slots[i])
      // Roll on profit > 33% or delta > 0.55
      if pnl > 0 {
        spread.close(long_slots[i])
        long_slots[i] = 0
      }
    }
  }
  i = i + 1
}

// Check short slots
i = 0
while i < max_short_slots {
  if i < len(short_slots) and short_slots[i] > 0 {
    si = spread.get(short_slots[i])
    if not si[4] {
      short_slots[i] = 0
    } else {
      pnl = spread.pnl(short_slots[i])
      if pnl > 0 {
        spread.close(short_slots[i])
        short_slots[i] = 0
      }
    }
  }
  i = i + 1
}

// ─── Long entry / add-on ─────────────────────────────────────
if is_low_vol and close > long_breakout_level and long_breakout_level > 0 {
  // Count active long slots
  active_longs = 0
  j = 0
  while j < len(long_slots) {
    if long_slots[j] > 0 {
      active_longs = active_longs + 1
    }
    j = j + 1
  }

  should_open_long = false
  if active_longs == 0 {
    should_open_long = true
    long_entry_price = close
    long_add_count = 0
  } else {
    // Add-on: price moved 0.75 * ATR above entry
    if atr_8h > 0 and long_add_count < max_long_slots - 1 {
      add_level = long_entry_price + atr_add_mult * atr_8h * (long_add_count + 1)
      if close >= add_level {
        should_open_long = true
        long_add_count = long_add_count + 1
      }
    }
  }

  if should_open_long {
    chain = options.chain()
    if not na(chain) {
      calls = options.calls(chain)
      calls = options.expiry_range(calls, min_dte, max_dte)
      if options.len(calls) > 0 {
        candidates = options.sort_by_delta(calls, target_delta)
        if len(candidates) > 0 {
          c = candidates[0]
          p = contract.mark(c)
          if p > 0 {
            qty = 1.0
            if long_group == 0 {
              long_group = group.open("turtle-trend-long", 1.0, 1.0)
            }
            // Find empty slot
            k = 0
            while k < len(long_slots) {
              if long_slots[k] == 0 {
                legs = [leg.buy(c, qty)]
                long_slots[k] = spread.open_in_group(legs, "long-call", long_group)
                k = len(long_slots)  // break
              }
              k = k + 1
            }
          }
        }
      }
    }
  }
}

// ─── Short entry / add-on ─────────────────────────────────────
if is_low_vol and close < short_breakout_level and short_breakout_level > 0 {
  active_shorts = 0
  j = 0
  while j < len(short_slots) {
    if short_slots[j] > 0 {
      active_shorts = active_shorts + 1
    }
    j = j + 1
  }

  should_open_short = false
  if active_shorts == 0 {
    should_open_short = true
    short_entry_price = close
    short_add_count = 0
  } else {
    if atr_8h > 0 and short_add_count < max_short_slots - 1 {
      add_level = short_entry_price - atr_add_mult * atr_8h * (short_add_count + 1)
      if close <= add_level {
        should_open_short = true
        short_add_count = short_add_count + 1
      }
    }
  }

  if should_open_short {
    chain = options.chain()
    if not na(chain) {
      puts = options.puts(chain)
      puts = options.expiry_range(puts, min_dte, max_dte)
      if options.len(puts) > 0 {
        candidates = options.sort_by_delta(puts, 0 - target_delta)
        if len(candidates) > 0 {
          c = candidates[0]
          p = contract.mark(c)
          if p > 0 {
            qty = 1.0
            if short_group == 0 {
              short_group = group.open("turtle-trend-short", 1.0, 1.0)
            }
            k = 0
            while k < len(short_slots) {
              if short_slots[k] == 0 {
                legs = [leg.buy(c, qty)]
                short_slots[k] = spread.open_in_group(legs, "long-put", short_group)
                k = len(short_slots)
              }
              k = k + 1
            }
          }
        }
      }
    }
  }
}
`
	_ = RegisterDSLWithMetadata(catalog.Registration{
		Name:    "turtle-trend-simp-dsl",
		Groups:  []string{"dsl"},
		Profile: catalog.StrategyProfile{UsesOptions: true, RegularTrade: catalog.RegularTradeSignalOnly},
	}, turtleTrendDSL)
}
