# Strategy Reuse Guide

This repository now has a shared options-strategy helper package at `pkg/strategies/optutil`.
When writing a new options strategy, prefer composing these helpers first and only add local code for genuinely strategy-specific logic.

## What To Reuse

Use `optutil.PricingMixin` when the strategy needs option entry, exit, or valuation price modes.

Use `optutil.GroupMixin` when the strategy opens a coordinated set of legs or wants a single position-group lifecycle.

Use `optutil.PendingRefCounter` when the strategy schedules spread opens for a future bar and later needs to resolve them back into `spreadID`s.

Use `optutil.BuildContractMap` and `optutil.ResolveContract` when a strategy needs the latest chain snapshot for open legs.

Use `optutil.PercentileRank`, `optutil.RollingStdDevIndicator`, and `optutil.PercentChange` for recurring indicator logic that already exists in multiple strategies.

## Recommended Strategy Skeleton

```go
type myStrategy struct {
	optutil.PricingMixin
	optutil.GroupMixin
	optutil.PendingRefCounter

	// strategy-specific config
	lookback int

	// runtime state
	spreadIDs [2]int
}

func init() {
	catalog.Register(catalog.Registration{
		Name: "my-strategy",
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			return &myStrategy{
				PricingMixin: optutil.PricingMixin{
					EntryPriceMode:     cfg.EntryPriceMode,
					ExitPriceMode:      cfg.ExitPriceMode,
					ValuationPriceMode: cfg.ValuationPriceMode,
				},
				PendingRefCounter: optutil.PendingRefCounter{Prefix: "MyStrategy"},
			}, nil
		},
	})
}

func (s *myStrategy) applyDefaults() {
	s.ApplyPricingDefaults()
	if s.lookback <= 0 {
		s.lookback = 20
	}
}
```

## Reuse Patterns

### 1. Price-mode defaults

Old pattern:

```go
pricingDefaults := backtest.DefaultSpreadPricingConfig()
if s.EntryPriceMode == backtest.OptionPriceModeUnspecified {
	s.EntryPriceMode = pricingDefaults.EntryMode
}
...
```

New pattern:

```go
s.ApplyPricingDefaults()
```

### 2. Spread pricing provider

If the strategy embeds `PricingMixin`, it already satisfies `backtest.SpreadPricingProvider` through the promoted `SpreadPricingConfig()` method.

### 3. Current chain contract lookup

```go
contractMap := optutil.BuildContractMap(ctx.OptionsChain())
contract := optutil.ResolveContract(leg.Contract, contractMap)
```

This avoids duplicating local `buildContractMap()` and `currentContract()` helpers.

### 4. Leg exit and mark pricing

```go
exitPrice := s.LegExitPrice(leg, contractMap)
markPrice := s.LegValuationPrice(leg, contractMap)
```

This avoids strategy-local `exitPrice()` and `valuationPrice()` wrappers.

### 5. Group lifecycle

```go
groupID := s.OpenGroup(ctx, tag, amount, decay)
spreadID := s.OpenSpreadInGroup(ctx, legs, note, groupID)
s.CloseGroup(ctx, groupID)
```

Use this whenever several spreads or legs belong to one logical trade.

### 6. Deferred spread refs

```go
pendingRef := s.Next("short-put")
ctx.ScheduleOpenSpreadInGroupWithRef(triggerTime, legs, tag, pendingRef, groupID)

if spread := optutil.FindSpreadByRef(ctx.Spreads(), pendingRef); spread != nil {
	spreadID = spread.ID
}
```

Use this instead of open-coded `nextPendingRef()` and tracker scans.

### 7. Expiry-close rules

```go
if optutil.ShouldCloseForExpiry(contract, now, 1) {
	...
}
```

Keep the threshold explicit at the call site so the behavior stays readable.

## Current Strategies Using These Patterns

These strategies are already migrated and can be used as references:

- `pkg/strategies/dual_spreads_btc_volatility`
- `pkg/strategies/covered_call_0330_tvsig`
- `pkg/strategies/ma_deviation_spread_outer_source`
- `pkg/strategies/buy_flash_low`
- `pkg/strategies/turtle_trend_simp`

## Rule Of Thumb

Before adding a helper inside a strategy file, check whether it is actually one of these recurring concerns:

- price-mode defaults
- contract snapshot resolution
- per-leg exit or valuation pricing
- position-group open/close lifecycle
- pending spread ref generation or resolution
- generic indicator math

If yes, extend `pkg/strategies/optutil` instead of copying another local helper.