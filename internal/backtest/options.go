package backtest

import (
	"math"
	"sort"
	"time"
)

// OptionType distinguishes calls from puts.
type OptionType string

const (
	Call OptionType = "call"
	Put  OptionType = "put"
)

// OptionPriceMode controls how option legs are priced during spread lifecycle events.
type OptionPriceMode int

const (
	OptionPriceModeUnspecified OptionPriceMode = iota
	OptionPriceMarkClose
	OptionPriceBidAsk
)

// SpreadPricingConfig controls entry, exit, and valuation prices for option spreads.
type SpreadPricingConfig struct {
	EntryMode     OptionPriceMode
	ExitMode      OptionPriceMode
	ValuationMode OptionPriceMode
}

// SpreadPricingProvider exposes option spread pricing preferences to the engine.
type SpreadPricingProvider interface {
	SpreadPricingConfig() SpreadPricingConfig
}

// DefaultSpreadPricingConfig preserves the existing spread backtest behavior.
func DefaultSpreadPricingConfig() SpreadPricingConfig {
	return SpreadPricingConfig{
		EntryMode:     OptionPriceBidAsk,
		ExitMode:      OptionPriceMarkClose,
		ValuationMode: OptionPriceMarkClose,
	}
}

// WithDefaults fills any unspecified modes with the engine defaults.
func (cfg SpreadPricingConfig) WithDefaults() SpreadPricingConfig {
	defaults := DefaultSpreadPricingConfig()
	if cfg.EntryMode == OptionPriceModeUnspecified {
		cfg.EntryMode = defaults.EntryMode
	}
	if cfg.ExitMode == OptionPriceModeUnspecified {
		cfg.ExitMode = defaults.ExitMode
	}
	if cfg.ValuationMode == OptionPriceModeUnspecified {
		cfg.ValuationMode = defaults.ValuationMode
	}
	return cfg
}

// EntryPrice returns the price used to open a leg for the provided side.
func (mode OptionPriceMode) EntryPrice(side Side, contract OptionContract) float64 {
	switch mode {
	case OptionPriceBidAsk:
		if side == Buy {
			return optionPriceFallback(contract.AskPrice, contract.MarkPrice, optionMidPrice(contract), contract.BidPrice)
		}
		return optionPriceFallback(contract.BidPrice, contract.MarkPrice, optionMidPrice(contract), contract.AskPrice)
	case OptionPriceMarkClose:
		fallthrough
	default:
		return optionPriceFallback(contract.MarkPrice, optionMidPrice(contract), contract.BidPrice, contract.AskPrice)
	}
}

// ExitPrice returns the price used to close a leg based on its entry side.
func (mode OptionPriceMode) ExitPrice(entrySide Side, contract OptionContract) float64 {
	exitSide := Sell
	if entrySide == Sell {
		exitSide = Buy
	}
	return mode.EntryPrice(exitSide, contract)
}

func optionMidPrice(contract OptionContract) float64 {
	if optionPriceValid(contract.BidPrice) && optionPriceValid(contract.AskPrice) {
		return (contract.BidPrice + contract.AskPrice) / 2
	}
	return math.NaN()
}

func optionPriceFallback(values ...float64) float64 {
	for _, value := range values {
		if optionPriceValid(value) {
			return value
		}
	}
	return math.NaN()
}

func optionPriceValid(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

// OptionContract represents a snapshot of a single option at a point in time.
type OptionContract struct {
	Symbol      string
	Ref         SecurityRef
	Type        OptionType
	StrikePrice float64
	Expiration  time.Time

	// Greeks
	Delta float64
	Gamma float64
	Vega  float64
	Theta float64
	Rho   float64

	// Prices
	BidPrice  float64
	AskPrice  float64
	MarkPrice float64
	IV        float64

	// Market data
	UnderlyingPrice float64
	Volume          float64
	OpenInterest    float64
}

// SpreadRatio returns the bid-ask spread quality metric: (ask-bid)/(ask+bid).
// Lower is better. Returns +Inf if prices are invalid.
func (c *OptionContract) SpreadRatio() float64 {
	if c.AskPrice <= 0 || c.BidPrice <= 0 || c.AskPrice+c.BidPrice == 0 {
		return math.Inf(1)
	}
	return (c.AskPrice - c.BidPrice) / (c.AskPrice + c.BidPrice)
}

// DaysToExpiry returns the number of calendar days until expiration from the given time.
func (c *OptionContract) DaysToExpiry(now time.Time) float64 {
	return c.Expiration.Sub(now).Hours() / 24
}

// OptionsChain provides a fluent, filterable view of option contracts
// available at a given point in time.
type OptionsChain struct {
	contracts []OptionContract
	now       time.Time
}

// NewOptionsChain creates a chain from a slice of contracts and current time.
func NewOptionsChain(contracts []OptionContract, now time.Time) *OptionsChain {
	return &OptionsChain{contracts: contracts, now: now}
}

// Len returns the number of contracts in the chain.
func (ch *OptionsChain) Len() int { return len(ch.contracts) }

// Contracts returns the underlying slice.
func (ch *OptionsChain) Contracts() []OptionContract { return ch.contracts }

// Calls returns a new chain containing only call options.
func (ch *OptionsChain) Calls() *OptionsChain {
	return ch.filter(func(c *OptionContract) bool { return c.Type == Call })
}

// Puts returns a new chain containing only put options.
func (ch *OptionsChain) Puts() *OptionsChain {
	return ch.filter(func(c *OptionContract) bool { return c.Type == Put })
}

// ExpiryNearest returns contracts whose expiration is closest to targetDays from now.
// All contracts sharing the nearest expiry date are returned.
func (ch *OptionsChain) ExpiryNearest(targetDays int) *OptionsChain {
	if len(ch.contracts) == 0 {
		return ch
	}
	target := float64(targetDays)
	bestDiff := math.Inf(1)
	var bestExpiry time.Time
	for i := range ch.contracts {
		diff := math.Abs(ch.contracts[i].DaysToExpiry(ch.now) - target)
		if diff < bestDiff {
			bestDiff = diff
			bestExpiry = ch.contracts[i].Expiration
		}
	}
	return ch.filter(func(c *OptionContract) bool {
		return c.Expiration.Equal(bestExpiry)
	})
}

// ExpiryMin returns contracts with at least minDays until expiration.
func (ch *OptionsChain) ExpiryMin(minDays int) *OptionsChain {
	minDur := float64(minDays)
	return ch.filter(func(c *OptionContract) bool {
		return c.DaysToExpiry(ch.now) >= minDur
	})
}

// ExpiryMax returns contracts with at most maxDays until expiration.
func (ch *OptionsChain) ExpiryMax(maxDays int) *OptionsChain {
	maxDur := float64(maxDays)
	return ch.filter(func(c *OptionContract) bool {
		return c.DaysToExpiry(ch.now) <= maxDur
	})
}

// ExpiryRange returns contracts whose expiry is between minDays and maxDays from now.
func (ch *OptionsChain) ExpiryRange(minDays, maxDays int) *OptionsChain {
	return ch.ExpiryMin(minDays).ExpiryMax(maxDays)
}

// DeltaRange returns contracts whose delta falls within [minDelta, maxDelta].
func (ch *OptionsChain) DeltaRange(minDelta, maxDelta float64) *OptionsChain {
	return ch.filter(func(c *OptionContract) bool {
		return c.Delta >= minDelta && c.Delta <= maxDelta
	})
}

// MinPremium returns contracts whose bid price is at least minBid.
func (ch *OptionsChain) MinPremium(minBid float64) *OptionsChain {
	return ch.filter(func(c *OptionContract) bool {
		return c.BidPrice >= minBid
	})
}

// StrikeRange returns contracts whose strike is within [min, max].
func (ch *OptionsChain) StrikeRange(min, max float64) *OptionsChain {
	return ch.filter(func(c *OptionContract) bool {
		return c.StrikePrice >= min && c.StrikePrice <= max
	})
}

// SameExpiry returns contracts sharing the same expiration as the given contract.
func (ch *OptionsChain) SameExpiry(ref *OptionContract) *OptionsChain {
	return ch.filter(func(c *OptionContract) bool {
		return c.Expiration.Equal(ref.Expiration)
	})
}

// BestSpread returns the contract with the best (smallest) bid-ask spread ratio.
// Ties are broken by higher volume. Returns nil if the chain is empty.
func (ch *OptionsChain) BestSpread() *OptionContract {
	if len(ch.contracts) == 0 {
		return nil
	}
	sorted := make([]OptionContract, len(ch.contracts))
	copy(sorted, ch.contracts)
	sort.Slice(sorted, func(i, j int) bool {
		ri, rj := sorted[i].SpreadRatio(), sorted[j].SpreadRatio()
		if ri != rj {
			return ri < rj
		}
		return sorted[i].Volume > sorted[j].Volume
	})
	return &sorted[0]
}

// SortByDelta returns contracts sorted by absolute delta distance from the target.
func (ch *OptionsChain) SortByDelta(targetDelta float64) []OptionContract {
	sorted := make([]OptionContract, len(ch.contracts))
	copy(sorted, ch.contracts)
	sort.Slice(sorted, func(i, j int) bool {
		di := math.Abs(sorted[i].Delta - targetDelta)
		dj := math.Abs(sorted[j].Delta - targetDelta)
		if di != dj {
			return di < dj
		}
		return sorted[i].SpreadRatio() < sorted[j].SpreadRatio()
	})
	return sorted
}

func (ch *OptionsChain) filter(fn func(*OptionContract) bool) *OptionsChain {
	var out []OptionContract
	for i := range ch.contracts {
		if fn(&ch.contracts[i]) {
			out = append(out, ch.contracts[i])
		}
	}
	return &OptionsChain{contracts: out, now: ch.now}
}

// OptionsChainProvider supplies option contract snapshots during bar replay.
// The backtest engine calls AvailableContracts once per bar to get the
// full set of tradeable options.
type OptionsChainProvider interface {
	// AvailableContracts returns all option contracts with data at the given time.
	AvailableContracts(t time.Time) []OptionContract
}

// SpreadLeg represents one leg of a multi-leg options position.
type SpreadLeg struct {
	Contract    OptionContract
	Side        Side
	Qty         float64
	EntryPrice  float64
	EntryTime   time.Time
	Closed      bool
	ClosePrice  float64
	CloseTime   time.Time
	CloseReason string
}

// UnrealizedPnL returns the unrealized PnL for this leg at the given mark price.
func (sl *SpreadLeg) UnrealizedPnL(markPrice float64) float64 {
	if sl.Closed {
		return 0
	}
	if sl.Side == Sell {
		return sl.Qty * (sl.EntryPrice - markPrice)
	}
	return sl.Qty * (markPrice - sl.EntryPrice)
}

// RealizedPnL returns the realized PnL if the leg is closed.
func (sl *SpreadLeg) RealizedPnL() float64 {
	if !sl.Closed {
		return 0
	}
	if sl.Side == Sell {
		return sl.Qty * (sl.EntryPrice - sl.ClosePrice)
	}
	return sl.Qty * (sl.ClosePrice - sl.EntryPrice)
}

// SpreadPosition tracks a multi-leg options position as a single unit.
type SpreadPosition struct {
	ID       int
	Legs     []SpreadLeg
	OpenTime time.Time
	OpenBar  int
	Tag      string // user-defined label (e.g. "bull-put-spread")
}

// IsFullyClosed returns true if all legs are closed.
func (sp *SpreadPosition) IsFullyClosed() bool {
	for i := range sp.Legs {
		if !sp.Legs[i].Closed {
			return false
		}
	}
	return true
}

// TotalUnrealizedPnL returns the combined unrealized PnL across all open legs.
func (sp *SpreadPosition) TotalUnrealizedPnL(priceFn func(OptionContract) float64) float64 {
	total := 0.0
	for i := range sp.Legs {
		if !sp.Legs[i].Closed {
			total += sp.Legs[i].UnrealizedPnL(priceFn(sp.Legs[i].Contract))
		}
	}
	return total
}

// TotalRealizedPnL returns the combined realized PnL across all closed legs.
func (sp *SpreadPosition) TotalRealizedPnL() float64 {
	total := 0.0
	for i := range sp.Legs {
		total += sp.Legs[i].RealizedPnL()
	}
	return total
}

// LegUnrealizedPnLPct returns the unrealized profit as a percentage of entry premium.
// For a short (sell) leg: (entryPrice - markPrice) / entryPrice.
// Returns NaN if entry price is zero.
func (sp *SpreadPosition) LegUnrealizedPnLPct(legIndex int, markPrice float64) float64 {
	if legIndex < 0 || legIndex >= len(sp.Legs) {
		return math.NaN()
	}
	leg := &sp.Legs[legIndex]
	if leg.Closed || leg.EntryPrice == 0 {
		return math.NaN()
	}
	if leg.Side == Sell {
		return (leg.EntryPrice - markPrice) / leg.EntryPrice
	}
	return (markPrice - leg.EntryPrice) / leg.EntryPrice
}

// BarsHeld returns the number of bars since the spread was opened.
func (sp *SpreadPosition) BarsHeld(currentBar int) int {
	return currentBar - sp.OpenBar
}

// TimeHeld returns the duration since the spread was opened.
func (sp *SpreadPosition) TimeHeld(now time.Time) time.Duration {
	return now.Sub(sp.OpenTime)
}

// SpreadTracker manages open spread positions.
type SpreadTracker struct {
	spreads []*SpreadPosition
	nextID  int
}

// NewSpreadTracker creates a new tracker.
func NewSpreadTracker() *SpreadTracker {
	return &SpreadTracker{nextID: 1}
}

// Open creates a new spread position and returns its ID.
func (st *SpreadTracker) Open(legs []SpreadLeg, openTime time.Time, openBar int, tag string) int {
	id := st.nextID
	st.nextID++
	sp := &SpreadPosition{
		ID:       id,
		Legs:     legs,
		OpenTime: openTime,
		OpenBar:  openBar,
		Tag:      tag,
	}
	st.spreads = append(st.spreads, sp)
	return id
}

// Get returns a spread by ID, or nil if not found.
func (st *SpreadTracker) Get(id int) *SpreadPosition {
	for _, sp := range st.spreads {
		if sp.ID == id {
			return sp
		}
	}
	return nil
}

// OpenSpreads returns all spreads that have at least one open leg.
func (st *SpreadTracker) OpenSpreads() []*SpreadPosition {
	var result []*SpreadPosition
	for _, sp := range st.spreads {
		if !sp.IsFullyClosed() {
			result = append(result, sp)
		}
	}
	return result
}

// All returns all spread positions (open and closed).
func (st *SpreadTracker) All() []*SpreadPosition {
	return st.spreads
}

// CloseLeg marks a specific leg of a spread as closed at the given price.
func (st *SpreadTracker) CloseLeg(spreadID, legIndex int, closePrice float64, closeTime time.Time) bool {
	return st.CloseLegWithReason(spreadID, legIndex, closePrice, closeTime, "")
}

// CloseLegWithReason marks a specific leg of a spread as closed with a reason.
func (st *SpreadTracker) CloseLegWithReason(spreadID, legIndex int, closePrice float64, closeTime time.Time, closeReason string) bool {
	sp := st.Get(spreadID)
	if sp == nil || legIndex < 0 || legIndex >= len(sp.Legs) {
		return false
	}
	if sp.Legs[legIndex].Closed {
		return false
	}
	sp.Legs[legIndex].Closed = true
	sp.Legs[legIndex].ClosePrice = closePrice
	sp.Legs[legIndex].CloseTime = closeTime
	sp.Legs[legIndex].CloseReason = closeReason
	return true
}

// CloseAll marks all open legs of a spread as closed.
func (st *SpreadTracker) CloseAll(spreadID int, priceFn func(OptionContract) float64, closeTime time.Time) {
	sp := st.Get(spreadID)
	if sp == nil {
		return
	}
	for i := range sp.Legs {
		if !sp.Legs[i].Closed {
			sp.Legs[i].Closed = true
			sp.Legs[i].ClosePrice = priceFn(sp.Legs[i].Contract)
			sp.Legs[i].CloseTime = closeTime
		}
	}
}

// ScheduledAction represents a time-triggered action for the engine to process.
type ScheduledAction struct {
	TriggerTime time.Time
	SpreadID    int
	LegIndex    int // -1 means close all legs
	ActionType  ScheduledActionType

	// Trigger behavior for pending spread actions.
	OrderType    SpreadOrderType
	TriggerSide  Side
	TriggerPrice float64

	// Optional per-action slippage override (fraction, e.g. 0.002 = 20 bps).
	// Values <= 0 mean "use engine default slippage".
	SlippagePct float64

	// Open action payload.
	OpenLegs []SpreadLeg
	OpenTag  string

	// Close action payload.
	CloseReason string
}

// ScheduledActionType enumerates the kinds of scheduled actions.
type ScheduledActionType int

const (
	// ScheduleOpenSpread opens a spread when trigger conditions are met.
	ScheduleOpenSpread ScheduledActionType = iota
	// ScheduleCloseLeg closes a specific leg of a spread.
	ScheduleCloseLeg
	// ScheduleCloseSpread closes all legs of a spread.
	ScheduleCloseSpread
)

// SpreadOrderType defines trigger style for scheduled spread actions.
type SpreadOrderType int

const (
	SpreadOrderMarket SpreadOrderType = iota
	SpreadOrderLimit
	SpreadOrderStop
)
