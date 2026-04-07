// Package bridge adapts a parsed Toktik DSL program into a backtest.Strategy.
package bridge

import (
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

// DslStrategy implements backtest.Strategy by interpreting a Toktik DSL script.
type DslStrategy struct {
	source string
	name   string
	prog   *ast.Program
	ip     *runtime.Interpreter
	errs   []string
}

// New creates a DslStrategy from DSL source code.
func New(source string) *DslStrategy {
	prog, errs := parser.Parse(source)
	name := extractStrategyName(prog)
	if name == "" {
		name = "dsl_strategy"
	}
	return &DslStrategy{
		source: source,
		name:   name,
		prog:   prog,
		errs:   errs,
	}
}

// ParseErrors returns any errors from parsing.
func (ds *DslStrategy) ParseErrors() []string { return ds.errs }

// Name implements backtest.Strategy.
func (ds *DslStrategy) Name() string { return ds.name }

// Init implements backtest.Strategy.
func (ds *DslStrategy) Init(ctx *backtest.SetupContext) error {
	ds.ip = runtime.NewInterpreter(ds.prog)
	runtime.RegisterTABuiltins(ds.ip)
	runtime.RegisterMathBuiltins(ds.ip)
	runtime.RegisterStrBuiltins(ds.ip)
	runtime.RegisterStrategyBuiltins(ds.ip)
	runtime.RegisterOptionsBuiltins(ds.ip)
	runtime.RegisterAlphaBuiltins(ds.ip)
	ds.ip.Init()
	return nil
}

// OnBar implements backtest.Strategy.
func (ds *DslStrategy) OnBar(ctx *backtest.BarContext) {
	ds.ip.Bridge = &barContextBridge{ctx: ctx}
	ds.ip.OnBar()
}

// barContextBridge adapts backtest.BarContext to the runtime.Bridge interface.
type barContextBridge struct {
	ctx *backtest.BarContext
}

func (b *barContextBridge) BarIndex() int                   { return b.ctx.BarIndex() }
func (b *barContextBridge) Close() float64                  { return b.ctx.Close() }
func (b *barContextBridge) Open() float64                   { return b.ctx.Open() }
func (b *barContextBridge) High() float64                   { return b.ctx.High() }
func (b *barContextBridge) Low() float64                    { return b.ctx.Low() }
func (b *barContextBridge) Volume() float64                 { return b.ctx.Volume() }
func (b *barContextBridge) Field(n string) float64          { return b.ctx.Field(n) }
func (b *barContextBridge) FieldAt(n string, o int) float64 { return b.ctx.FieldAt(n, o) }

func (b *barContextBridge) Buy(qty float64)  { b.ctx.Buy(b.primaryRef(), qty) }
func (b *barContextBridge) Sell(qty float64) { b.ctx.Sell(b.primaryRef(), qty) }

func (b *barContextBridge) EntryLong(id string, qty float64) {
	b.ctx.BuyWithNote(b.primaryRef(), qty, id)
}
func (b *barContextBridge) EntryShort(id string, qty float64) {
	b.ctx.SellWithNote(b.primaryRef(), qty, id)
}
func (b *barContextBridge) ExitLong(id string) {
	pos := b.ctx.Position(b.primaryRef())
	if pos > 0 {
		b.ctx.SellWithNote(b.primaryRef(), pos, "exit:"+id)
	}
}
func (b *barContextBridge) ExitShort(id string) {
	pos := b.ctx.Position(b.primaryRef())
	if pos < 0 {
		b.ctx.BuyWithNote(b.primaryRef(), -pos, "exit:"+id)
	}
}

func (b *barContextBridge) PositionSize() float64 {
	return b.ctx.Position(b.primaryRef())
}

func (b *barContextBridge) PositionAvgPrice() float64 {
	// Approximate: use current close if no direct API.
	return b.ctx.Close()
}

func (b *barContextBridge) Ind(name string) float64          { return b.ctx.Ind(name) }
func (b *barContextBridge) IndAt(name string, o int) float64 { return b.ctx.IndAt(name, o) }

func (b *barContextBridge) primaryRef() backtest.SecurityRef {
	return backtest.SecurityRef{Index: 0}
}

// --- OptionsBridge implementation ---

func (b *barContextBridge) OptionsChain() interface{} {
	ch := b.ctx.OptionsChain()
	if ch == nil || ch.Len() == 0 {
		return nil
	}
	return ch
}

func (b *barContextBridge) ChainCalls(chain interface{}) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.Calls()
	}
	return nil
}

func (b *barContextBridge) ChainPuts(chain interface{}) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.Puts()
	}
	return nil
}

func (b *barContextBridge) ChainExpiryNearest(chain interface{}, targetDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryNearest(targetDays)
	}
	return nil
}

func (b *barContextBridge) ChainExpiryRange(chain interface{}, minDays, maxDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryRange(minDays, maxDays)
	}
	return nil
}

func (b *barContextBridge) ChainExpiryMin(chain interface{}, minDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryMin(minDays)
	}
	return nil
}

func (b *barContextBridge) ChainExpiryMax(chain interface{}, maxDays int) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.ExpiryMax(maxDays)
	}
	return nil
}

func (b *barContextBridge) ChainDeltaRange(chain interface{}, minDelta, maxDelta float64) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.DeltaRange(minDelta, maxDelta)
	}
	return nil
}

func (b *barContextBridge) ChainMinPremium(chain interface{}, minBid float64) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.MinPremium(minBid)
	}
	return nil
}

func (b *barContextBridge) ChainStrikeRange(chain interface{}, min, max float64) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.StrikeRange(min, max)
	}
	return nil
}

func (b *barContextBridge) ChainLen(chain interface{}) int {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.Len()
	}
	return 0
}

func (b *barContextBridge) ChainBestSpread(chain interface{}) interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		return ch.BestSpread()
	}
	return nil
}

func (b *barContextBridge) ChainSortByDelta(chain interface{}, targetDelta float64) []interface{} {
	if ch, ok := chain.(*backtest.OptionsChain); ok {
		contracts := ch.SortByDelta(targetDelta)
		out := make([]interface{}, len(contracts))
		for i := range contracts {
			c := contracts[i]
			out[i] = &c
		}
		return out
	}
	return nil
}

// Contract field accessors.
func (b *barContextBridge) ContractSymbol(c interface{}) string {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.Symbol
	}
	return ""
}

func (b *barContextBridge) ContractType(c interface{}) string {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return string(oc.Type)
	}
	return ""
}

func (b *barContextBridge) ContractStrike(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.StrikePrice
	}
	return 0
}

func (b *barContextBridge) ContractDTE(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.DaysToExpiry(b.ctx.Time())
	}
	return 0
}

func (b *barContextBridge) ContractDelta(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.Delta
	}
	return 0
}

func (b *barContextBridge) ContractGamma(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.Gamma
	}
	return 0
}

func (b *barContextBridge) ContractVega(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.Vega
	}
	return 0
}

func (b *barContextBridge) ContractTheta(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.Theta
	}
	return 0
}

func (b *barContextBridge) ContractIV(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.IV
	}
	return 0
}

func (b *barContextBridge) ContractBid(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.BidPrice
	}
	return 0
}

func (b *barContextBridge) ContractAsk(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.AskPrice
	}
	return 0
}

func (b *barContextBridge) ContractMark(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.MarkPrice
	}
	return 0
}

func (b *barContextBridge) ContractVolume(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.Volume
	}
	return 0
}

func (b *barContextBridge) ContractOI(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.OpenInterest
	}
	return 0
}

// Spread management.
func (b *barContextBridge) OpenSpread(legs []runtime.SpreadLegInput, tag string) int {
	btLegs := convertLegs(legs, b.ctx)
	return b.ctx.OpenSpread(btLegs, tag)
}

func (b *barContextBridge) OpenSpreadInGroup(legs []runtime.SpreadLegInput, tag string, groupID int) int {
	btLegs := convertLegs(legs, b.ctx)
	return b.ctx.OpenSpreadInGroup(btLegs, tag, groupID)
}

func (b *barContextBridge) CloseSpread(spreadID int) {
	b.ctx.CloseSpread(spreadID, func(oc backtest.OptionContract) float64 {
		return oc.MarkPrice
	})
}

func (b *barContextBridge) CloseSpreadLeg(spreadID, legIndex int, closePrice float64) bool {
	return b.ctx.CloseSpreadLeg(spreadID, legIndex, closePrice)
}

func (b *barContextBridge) SpreadGet(spreadID int) runtime.SpreadInfo {
	st := b.ctx.Spreads()
	if st == nil {
		return runtime.SpreadInfo{}
	}
	sp := st.Get(spreadID)
	if sp == nil {
		return runtime.SpreadInfo{}
	}
	return runtime.SpreadInfo{
		ID:          sp.ID,
		Tag:         sp.Tag,
		BarsHeld:    sp.BarsHeld(b.ctx.BarIndex()),
		RealizedPnL: sp.TotalRealizedPnL(),
		IsOpen:      !sp.IsFullyClosed(),
		LegCount:    len(sp.Legs),
	}
}

func (b *barContextBridge) OpenSpreads() []int {
	st := b.ctx.Spreads()
	if st == nil {
		return nil
	}
	open := st.OpenSpreads()
	ids := make([]int, len(open))
	for i, sp := range open {
		ids[i] = sp.ID
	}
	return ids
}

func (b *barContextBridge) SpreadPnL(spreadID int) float64 {
	st := b.ctx.Spreads()
	if st == nil {
		return 0
	}
	sp := st.Get(spreadID)
	if sp == nil {
		return 0
	}
	return sp.TotalUnrealizedPnL(func(oc backtest.OptionContract) float64 {
		return oc.MarkPrice
	})
}

// Spread groups.
func (b *barContextBridge) GroupOpen(tag string, initAmount, decayFactor float64) int {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return 0
	}
	return gt.Open(tag, initAmount, decayFactor, b.ctx.Time())
}

func (b *barContextBridge) GroupClose(groupID int) {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return
	}
	gt.Close(groupID)
}

func (b *barContextBridge) GroupGet(groupID int) runtime.GroupInfo {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return runtime.GroupInfo{}
	}
	g := gt.Get(groupID)
	if g == nil {
		return runtime.GroupInfo{}
	}
	return runtime.GroupInfo{
		ID:        g.ID,
		Tag:       g.Tag,
		Amount:    g.CurrentAmount(),
		RollCount: g.RollCount,
		IsClosed:  g.Closed,
		SpreadIDs: g.SpreadIDs,
	}
}

func (b *barContextBridge) GroupAddSpread(groupID, spreadID int) {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return
	}
	gt.AddSpread(groupID, spreadID)
}

func (b *barContextBridge) OpenGroups() []int {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return nil
	}
	groups := gt.OpenGroups()
	ids := make([]int, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	return ids
}

// Scheduling.
func (b *barContextBridge) ScheduleCloseSpread(triggerBarOffset int, spreadID int) {
	// Approximate bar duration from context interval or use 1 hour default.
	dur := time.Duration(triggerBarOffset) * time.Hour
	b.ctx.ScheduleCloseAfter(dur, spreadID)
}

func (b *barContextBridge) ScheduleCloseLeg(triggerBarOffset int, spreadID, legIndex int) {
	dur := time.Duration(triggerBarOffset) * time.Hour
	b.ctx.ScheduleCloseLegAfter(dur, spreadID, legIndex)
}

// convertLegs converts DSL SpreadLegInput to backtest SpreadLeg.
func convertLegs(legs []runtime.SpreadLegInput, ctx *backtest.BarContext) []backtest.SpreadLeg {
	out := make([]backtest.SpreadLeg, 0, len(legs))
	for _, l := range legs {
		oc, ok := l.Contract.(*backtest.OptionContract)
		if !ok {
			continue
		}
		side := backtest.Buy
		if l.Side == "sell" {
			side = backtest.Sell
		}
		entryPrice := oc.MarkPrice
		if side == backtest.Buy && oc.AskPrice > 0 {
			entryPrice = oc.AskPrice
		} else if side == backtest.Sell && oc.BidPrice > 0 {
			entryPrice = oc.BidPrice
		}
		out = append(out, backtest.SpreadLeg{
			Contract:   *oc,
			Side:       side,
			Qty:        l.Qty,
			EntryPrice: entryPrice,
		})
	}
	return out
}

// extractStrategyName looks for a StrategyDecl node and pulls the first positional string arg.
func extractStrategyName(prog *ast.Program) string {
	if prog == nil {
		return ""
	}
	for _, stmt := range prog.Stmts {
		if sd, ok := stmt.(*ast.StrategyDecl); ok {
			for _, arg := range sd.Args {
				if arg.Name == "" {
					if sl, ok2 := arg.Value.(*ast.StringLit); ok2 {
						return sl.Value
					}
				}
			}
		}
	}
	return ""
}
