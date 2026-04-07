package bridge

import (
	"testing"
)

func TestDslStrategyParsesAndNames(t *testing.T) {
	src := `strategy("My Test Strategy")
var count = 0
count := count + 1
x = 2 + 3
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "My Test Strategy" {
		t.Errorf("expected name 'My Test Strategy', got %q", ds.Name())
	}
}

func TestDslStrategyDefaultName(t *testing.T) {
	src := `x = 1 + 2`
	ds := New(src)
	if ds.Name() != "dsl_strategy" {
		t.Errorf("expected default name 'dsl_strategy', got %q", ds.Name())
	}
}

func TestDslStrategyParseError(t *testing.T) {
	src := `strategy(`
	ds := New(src)
	if len(ds.ParseErrors()) == 0 {
		t.Error("expected parse errors for incomplete input")
	}
}

func TestDslStrategyFullScript(t *testing.T) {
	src := `strategy("EMA Cross")

// Parameters
fast_len = 10
slow_len = 20

// Variables
var position = 0

// Logic
sma_val = ta.sma(close, fast_len)
if sma_val > 0 {
  position := 1
}
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "EMA Cross" {
		t.Errorf("expected name 'EMA Cross', got %q", ds.Name())
	}
}

func TestDslStrategyOptionsScript(t *testing.T) {
	src := `strategy("Iron Condor Seller")

// Get options chain
chain = options.chain()
if chain != na {
  // Filter puts and calls
  puts = options.puts(chain)
  calls = options.calls(chain)

  // Find near-expiry contracts
  near_puts = options.expiry_nearest(puts, 30)
  near_calls = options.expiry_nearest(calls, 30)

  // Get best spread contract
  sell_put = options.best_spread(near_puts)
  sell_call = options.best_spread(near_calls)

  if sell_put != na {
    // Build legs
    put_leg = leg.sell(sell_put, 1)
    call_leg = leg.sell(sell_call, 1)
    legs = [put_leg, call_leg]

    // Open the spread
    spread_id = spread.open(legs, "iron_condor")
  }
}
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "Iron Condor Seller" {
		t.Errorf("expected name 'Iron Condor Seller', got %q", ds.Name())
	}
}

func TestDslStrategyAlphaScript(t *testing.T) {
	src := `strategy("Alpha Momentum")

// WorldQuant-style alpha factors
mom = alpha.ts_delta(close, 20)
vol = alpha.ts_std(close, 20)
zscore = alpha.zscore(close, 50)
rank = alpha.ts_rank(close, 100)
decay = alpha.decay_linear(close, 10)

if zscore > 2 {
  strategy.entry(id="long", direction=strategy.long, qty=1)
}
if zscore < -2 {
  strategy.entry(id="short", direction=strategy.short, qty=1)
}
`
	ds := New(src)
	if len(ds.ParseErrors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", ds.ParseErrors())
	}
	if ds.Name() != "Alpha Momentum" {
		t.Errorf("expected name 'Alpha Momentum', got %q", ds.Name())
	}
}
