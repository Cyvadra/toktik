package backtest

import (
	"math"
	"sort"
	"strings"
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
	Symbol           string
	Underlying       string
	Market           string
	UnderlyingMarket string
	Ref              SecurityRef
	Type             OptionType
	StrikePrice      float64
	Expiration       time.Time

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

// ChainUnderlying returns the best available logical underlying symbol for the
// contract. Empty strings are normalized away so callers can reliably use the
// result as a lookup key.
func (c OptionContract) ChainUnderlying() string {
	return strings.TrimSpace(c.Underlying)
}

// ChainMarket returns the logical market namespace for option-chain lookups.
// UnderlyingMarket is preferred when available because it reflects the
// underlying domain (for example "us"), while Market may refer to a concrete
// feed implementation.
func (c OptionContract) ChainMarket() string {
	if market := strings.TrimSpace(c.UnderlyingMarket); market != "" {
		return market
	}
	return strings.TrimSpace(c.Market)
}

// ChainLookupKey builds a stable key for option-chain scoped lookups.
func ChainLookupKey(market, underlying string) string {
	return strings.ToLower(strings.TrimSpace(market)) + "|" + strings.ToUpper(strings.TrimSpace(underlying))
}

func chainMarketMatches(providerMarket, requestedMarket string) bool {
	providerMarket = strings.ToLower(strings.TrimSpace(providerMarket))
	requestedMarket = strings.ToLower(strings.TrimSpace(requestedMarket))
	if requestedMarket == "" || providerMarket == requestedMarket {
		return true
	}
	return isUSChainMarket(providerMarket) && isUSChainMarket(requestedMarket)
}

func isUSChainMarket(market string) bool {
	switch strings.ToLower(strings.TrimSpace(market)) {
	case "us", "us-stocks", "us-stock", "us-underlying", "stocks":
		return true
	default:
		return false
	}
}

// ContractLookupKey builds a stable key for contract snapshots inside a
// specific underlying option chain.
func ContractLookupKey(market, underlying, symbol string) string {
	return ChainLookupKey(market, underlying) + "|" + strings.TrimSpace(symbol)
}

// ContractLookupKeys returns the preferred lookup key followed by compatible
// fallbacks for older call sites that only keyed by symbol.
func ContractLookupKeys(contract OptionContract) []string {
	keys := []string{ContractLookupKey(contract.ChainMarket(), contract.ChainUnderlying(), contract.Symbol)}
	if symbol := strings.TrimSpace(contract.Symbol); symbol != "" {
		keys = append(keys, symbol)
	}
	return keys
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

// ExpiryNextMonth returns contracts expiring in the next calendar month.
func (ch *OptionsChain) ExpiryNextMonth() *OptionsChain {
	loc := ch.now.Location()
	nowLocal := ch.now.In(loc)
	start := time.Date(nowLocal.Year(), nowLocal.Month()+1, 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)

	return ch.filter(func(c *OptionContract) bool {
		expiryLocal := c.Expiration.In(loc)
		return !expiryLocal.Before(start) && expiryLocal.Before(end)
	})
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

// OptionsChainLookupProvider extends OptionsChainProvider with symbol-aware
// chain selection so one replay can trade multiple option underlyings.
type OptionsChainLookupProvider interface {
	OptionsChainProvider
	AvailableContractsFor(t time.Time, market, underlying string) []OptionContract
}

// OptionsChainSnapshot stores a compact per-underlying, per-timestamp artifact
// that can be replayed without retaining the raw chain loader/provider.
type OptionsChainSnapshot struct {
	Market      string
	Underlying  string
	ByTimestamp map[int64][]OptionContract
}

// NewOptionsChainSnapshot copies all contracts served by provider for the given
// timestamps. The resulting artifact is independent from provider ownership.
func NewOptionsChainSnapshot(provider OptionsChainProvider, market, underlying string, timestamps []time.Time) *OptionsChainSnapshot {
	snapshot := &OptionsChainSnapshot{
		Market:      strings.TrimSpace(market),
		Underlying:  strings.ToUpper(strings.TrimSpace(underlying)),
		ByTimestamp: make(map[int64][]OptionContract, len(timestamps)),
	}
	for _, ts := range timestamps {
		key := ts.UTC().Unix()
		contracts := AvailableContractsFor(provider, ts, market, underlying)
		if len(contracts) == 0 {
			continue
		}
		cloned := make([]OptionContract, len(contracts))
		copy(cloned, contracts)
		snapshot.ByTimestamp[key] = cloned
	}
	return snapshot
}

// SnapshotOptionsChainProvider serves precomputed option chain snapshots. It is
// intentionally lightweight and does not keep any raw data loader state alive.
type SnapshotOptionsChainProvider struct {
	market      string
	underlying  string
	byTimestamp map[int64][]OptionContract
}

func NewSnapshotOptionsChainProvider(snapshot *OptionsChainSnapshot) *SnapshotOptionsChainProvider {
	if snapshot == nil {
		return &SnapshotOptionsChainProvider{}
	}
	return &SnapshotOptionsChainProvider{
		market:      strings.TrimSpace(snapshot.Market),
		underlying:  strings.ToUpper(strings.TrimSpace(snapshot.Underlying)),
		byTimestamp: snapshot.ByTimestamp,
	}
}

func (p *SnapshotOptionsChainProvider) AvailableContracts(t time.Time) []OptionContract {
	if p == nil || p.byTimestamp == nil {
		return nil
	}
	return p.byTimestamp[t.UTC().Unix()]
}

func (p *SnapshotOptionsChainProvider) AvailableContractsFor(t time.Time, market, underlying string) []OptionContract {
	if p == nil {
		return nil
	}
	if !chainMarketMatches(p.market, market) {
		return nil
	}
	if strings.TrimSpace(underlying) != "" && !strings.EqualFold(p.underlying, underlying) {
		return nil
	}
	return p.AvailableContracts(t)
}

// MultiOptionsChainProvider aggregates multiple underlying-scoped providers
// into a single replay-facing provider.
type MultiOptionsChainProvider struct {
	providers map[string]OptionsChainProvider
}

// NewMultiOptionsChainProvider creates an aggregate provider from a keyed map
// of (market, underlying) chain providers.
func NewMultiOptionsChainProvider(providers map[string]OptionsChainProvider) *MultiOptionsChainProvider {
	cloned := make(map[string]OptionsChainProvider, len(providers))
	for key, provider := range providers {
		if provider == nil {
			continue
		}
		cloned[key] = provider
	}
	return &MultiOptionsChainProvider{providers: cloned}
}

func (p *MultiOptionsChainProvider) AvailableContracts(t time.Time) []OptionContract {
	if p == nil || len(p.providers) == 0 {
		return nil
	}
	out := make([]OptionContract, 0, len(p.providers)*32)
	for _, provider := range p.providers {
		out = append(out, provider.AvailableContracts(t)...)
	}
	return out
}

func (p *MultiOptionsChainProvider) AvailableContractsFor(t time.Time, market, underlying string) []OptionContract {
	if p == nil || len(p.providers) == 0 {
		return nil
	}
	if provider, ok := p.providers[ChainLookupKey(market, underlying)]; ok {
		return AvailableContractsFor(provider, t, market, underlying)
	}
	if isUSChainMarket(market) {
		for providerKey, provider := range p.providers {
			parts := strings.SplitN(providerKey, "|", 2)
			if len(parts) != 2 || !isUSChainMarket(parts[0]) || !strings.EqualFold(parts[1], underlying) {
				continue
			}
			return AvailableContractsFor(provider, t, market, underlying)
		}
	}
	return nil
}

// AvailableContractsFor returns the contracts for the requested market and
// underlying. Providers that only support a single chain fall back to the
// legacy AvailableContracts method when the request matches or leaves the scope
// unspecified.
func AvailableContractsFor(provider OptionsChainProvider, t time.Time, market, underlying string) []OptionContract {
	if provider == nil {
		return nil
	}
	if lookup, ok := provider.(OptionsChainLookupProvider); ok {
		return lookup.AvailableContractsFor(t, market, underlying)
	}
	contracts := provider.AvailableContracts(t)
	if strings.TrimSpace(market) == "" && strings.TrimSpace(underlying) == "" {
		return contracts
	}
	filtered := make([]OptionContract, 0, len(contracts))
	hasScopedContracts := false
	for _, contract := range contracts {
		if contract.ChainMarket() != "" || contract.ChainUnderlying() != "" {
			hasScopedContracts = true
		}
		if strings.TrimSpace(market) != "" && !chainMarketMatches(contract.ChainMarket(), market) {
			continue
		}
		if strings.TrimSpace(underlying) != "" && !strings.EqualFold(contract.ChainUnderlying(), underlying) {
			continue
		}
		filtered = append(filtered, contract)
	}
	if len(filtered) == 0 && !hasScopedContracts && strings.TrimSpace(market) == "" {
		return contracts
	}
	return filtered
}

// TradeCustomData stores arbitrary key/value metadata for trade or spread
// lifecycle events so reports can surface strategy-specific annotations.
type TradeCustomData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

const TradeCustomDataKeyCloseDelta = "close_delta"
const TradeCustomDataKeyCloseTriggerTime = "close_trigger_time"
const TradeCustomDataKeyEntryDelta = "entry_delta"

func cloneTradeCustomData(items []TradeCustomData) []TradeCustomData {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]TradeCustomData, len(items))
	copy(cloned, items)
	return cloned
}

func upsertTradeCustomData(items []TradeCustomData, key, value string) []TradeCustomData {
	cloned := cloneTradeCustomData(items)
	for index := range cloned {
		if cloned[index].Key == key {
			cloned[index].Value = value
			return cloned
		}
	}
	return append(cloned, TradeCustomData{Key: key, Value: value})
}

// SpreadLeg represents one leg of a multi-leg options position.
type SpreadLeg struct {
	Contract        OptionContract
	Side            Side
	Qty             float64
	EntryPrice      float64
	EntryTime       time.Time
	EntryCustomData []TradeCustomData
	Closed          bool
	ClosePrice      float64
	CloseTime       time.Time
	CloseReason     string
	CloseCustomData []TradeCustomData
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
	Ref      string // internal reference for strategy-level tracking of delayed executions
	GroupID  int    // spread group ID (0 = ungrouped)
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
	open      []*SpreadPosition
	closed    []*SpreadPosition
	spreadMap map[int]*SpreadPosition // O(1) lookup by ID
	nextID    int
}

// NewSpreadTracker creates a new tracker.
func NewSpreadTracker() *SpreadTracker {
	return &SpreadTracker{nextID: 1, spreadMap: make(map[int]*SpreadPosition)}
}

// Open creates a new spread position and returns its ID.
func (st *SpreadTracker) Open(legs []SpreadLeg, openTime time.Time, openBar int, tag string) int {
	return st.OpenWithRef(legs, openTime, openBar, tag, "")
}

// OpenWithRef creates a new spread position and returns its ID.
func (st *SpreadTracker) OpenWithRef(legs []SpreadLeg, openTime time.Time, openBar int, tag, ref string) int {
	return st.OpenFull(legs, openTime, openBar, tag, ref, 0)
}

// OpenFull creates a new spread position with all fields and returns its ID.
func (st *SpreadTracker) OpenFull(legs []SpreadLeg, openTime time.Time, openBar int, tag, ref string, groupID int) int {
	id := st.nextID
	st.nextID++
	sp := &SpreadPosition{
		ID:       id,
		Legs:     legs,
		OpenTime: openTime,
		OpenBar:  openBar,
		Tag:      tag,
		Ref:      ref,
		GroupID:  groupID,
	}
	st.open = append(st.open, sp)
	st.spreadMap[id] = sp
	return id
}

// Get returns a spread by ID in O(1), or nil if not found.
func (st *SpreadTracker) Get(id int) *SpreadPosition {
	return st.spreadMap[id]
}

// OpenSpreads returns all spreads that have at least one open leg.
func (st *SpreadTracker) OpenSpreads() []*SpreadPosition {
	return st.open
}

// All returns all spread positions (open and closed).
func (st *SpreadTracker) All() []*SpreadPosition {
	all := make([]*SpreadPosition, 0, len(st.open)+len(st.closed))
	all = append(all, st.closed...)
	all = append(all, st.open...)
	return all
}

// archiveIfClosed moves a fully-closed spread from the open list to the closed
// archive. Called internally after close operations.
func (st *SpreadTracker) archiveIfClosed(sp *SpreadPosition) {
	if !sp.IsFullyClosed() {
		return
	}
	for i, s := range st.open {
		if s.ID == sp.ID {
			st.open = append(st.open[:i], st.open[i+1:]...)
			st.closed = append(st.closed, sp)
			return
		}
	}
}

// CloseLeg marks a specific leg of a spread as closed at the given price.
func (st *SpreadTracker) CloseLeg(spreadID, legIndex int, closePrice float64, closeTime time.Time) bool {
	return st.CloseLegWithReason(spreadID, legIndex, closePrice, closeTime, "")
}

// CloseLegWithReason marks a specific leg of a spread as closed with a reason.
func (st *SpreadTracker) CloseLegWithReason(spreadID, legIndex int, closePrice float64, closeTime time.Time, closeReason string) bool {
	return st.CloseLegWithReasonAndData(spreadID, legIndex, closePrice, closeTime, closeReason, nil)
}

// CloseLegWithReasonAndData marks a specific leg of a spread as closed with custom report data.
func (st *SpreadTracker) CloseLegWithReasonAndData(spreadID, legIndex int, closePrice float64, closeTime time.Time, closeReason string, closeCustomData []TradeCustomData) bool {
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
	sp.Legs[legIndex].CloseCustomData = cloneTradeCustomData(closeCustomData)
	st.archiveIfClosed(sp)
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
	st.archiveIfClosed(sp)
}

// ScheduledAction represents a time-triggered action for the engine to process.
type ScheduledAction struct {
	TriggerTime   time.Time
	SpreadID      int
	LegIndex      int // -1 means close all legs
	ActionType    ScheduledActionType
	SecurityOrder Order

	// Trigger behavior for pending spread actions.
	OrderType    SpreadOrderType
	TriggerSide  Side
	TriggerPrice float64

	// Optional per-action slippage override (fraction, e.g. 0.002 = 20 bps).
	// Values <= 0 mean "use engine default slippage".
	SlippagePct float64

	// Open action payload.
	OpenLegs    []SpreadLeg
	OpenTag     string
	OpenRef     string
	OpenGroupID int

	// Close action payload.
	CloseReason     string
	CloseCustomData []TradeCustomData
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
	// ScheduleSecurityOrder executes a security order on the trigger bar.
	ScheduleSecurityOrder
)

// SpreadOrderType defines trigger style for scheduled spread actions.
type SpreadOrderType int

const (
	SpreadOrderMarket SpreadOrderType = iota
	SpreadOrderLimit
	SpreadOrderStop
)
