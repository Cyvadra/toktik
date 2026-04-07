// Package bridge adapts a parsed Toktik DSL program into a backtest.Strategy.
package bridge

import (
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/signals"
	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

type Options struct {
	SignalSource string
}

type strategyMetadata struct {
	SignalSource        string
	SignalName          string
	SignalTimeLayouts   []string
	SignalTimezone      string
	SignalTimestampCols []string
	SignalTypeCols      []string
	SignalValueCols     []string
	SignalEntryMatchers []string
	SignalTextHasIndex  bool
	ExposeFields        []string
	Requests            []requestSpec
}

type requestSpec struct {
	Kind     string
	Market   string
	Symbol   string
	Name     string
	Interval string
	Field    string
	Key      string
}

// DslStrategy implements backtest.Strategy by interpreting a Toktik DSL script.
type DslStrategy struct {
	source  string
	name    string
	prog    *ast.Program
	ip      *runtime.Interpreter
	errs    []string
	opts    Options
	meta    strategyMetadata
	secRefs map[string]backtest.SecurityRef
	facRefs map[string]backtest.FactorRef
}

// New creates a DslStrategy from DSL source code.
func New(source string) *DslStrategy {
	return NewWithOptions(source, Options{})
}

// NewWithOptions creates a DslStrategy with runtime configuration overrides.
func NewWithOptions(source string, opts Options) *DslStrategy {
	prog, errs := parser.Parse(source)
	name := extractStrategyName(prog)
	if name == "" {
		name = "dsl_strategy"
	}
	return &DslStrategy{
		source:  source,
		name:    name,
		prog:    prog,
		errs:    errs,
		opts:    opts,
		meta:    extractMetadata(prog),
		secRefs: make(map[string]backtest.SecurityRef),
		facRefs: make(map[string]backtest.FactorRef),
	}
}

// ParseErrors returns any errors from parsing.
func (ds *DslStrategy) ParseErrors() []string { return ds.errs }

// Name implements backtest.Strategy.
func (ds *DslStrategy) Name() string { return ds.name }

// Init implements backtest.Strategy.
func (ds *DslStrategy) Init(ctx *backtest.SetupContext) error {
	for _, req := range ds.meta.Requests {
		switch req.Kind {
		case "security":
			if _, ok := ds.secRefs[req.Key]; !ok {
				ds.secRefs[req.Key] = ctx.AddSecurity(req.Market, req.Symbol, req.Interval)
			}
		case "factor":
			if _, ok := ds.facRefs[req.Key]; !ok {
				ds.facRefs[req.Key] = ctx.AddFactor(req.Name, req.Interval)
			}
		}
	}
	ds.ip = runtime.NewInterpreter(ds.prog)
	runtime.RegisterTABuiltins(ds.ip)
	runtime.RegisterMathBuiltins(ds.ip)
	runtime.RegisterStrBuiltins(ds.ip)
	runtime.RegisterStrategyBuiltins(ds.ip)
	runtime.RegisterInputBuiltins(ds.ip)
	runtime.RegisterRequestBuiltins(ds.ip, ds.requestSecurityBuiltin(), ds.requestFactorBuiltin())
	runtime.RegisterOptionsBuiltins(ds.ip)
	runtime.RegisterAlphaBuiltins(ds.ip)
	ds.ip.Init()
	return nil
}

// Preload implements backtest.StrategyPreloader for signal-driven DSL scripts.
func (ds *DslStrategy) Preload(ctx *backtest.PreloadContext) error {
	signalPaths := splitCSVOrList(ds.opts.SignalSource)
	if len(signalPaths) == 0 {
		signalPaths = splitCSVOrList(ds.meta.SignalSource)
	}
	if len(signalPaths) == 0 {
		return nil
	}
	outputName := strings.TrimSpace(ds.meta.SignalName)
	if outputName == "" {
		outputName = "entry_signal"
	}
	location, err := parseLocation(ds.meta.SignalTimezone)
	if err != nil {
		return err
	}
	times, err := signals.LoadTimes(signals.Config{
		Paths:             signalPaths,
		TimestampColumns:  defaultStrings(ds.meta.SignalTimestampCols, []string{"日期和时间", "timestamp", "time", "datetime"}),
		TypeColumns:       ds.meta.SignalTypeCols,
		SignalColumns:     ds.meta.SignalValueCols,
		TimeLayouts:       defaultStrings(ds.meta.SignalTimeLayouts, []string{"Jan 2, 2006, 15:04", "2006/1/2 15:04", time.RFC3339}),
		Location:          location,
		TextLocation:      time.UTC,
		EntryMatchers:     ds.meta.SignalEntryMatchers,
		SkipMissing:       true,
		TextOptionalIndex: ds.meta.SignalTextHasIndex,
	})
	if err != nil {
		return fmt.Errorf("dsl signal preload: %w", err)
	}
	series := signals.BuildBinarySeries(ctx.Primary().Timestamps(), times)
	if err := ctx.Primary().SetColumn(outputName, series); err != nil {
		return err
	}
	return nil
}

// OnBar implements backtest.Strategy.
func (ds *DslStrategy) OnBar(ctx *backtest.BarContext) {
	ds.ip.Bridge = &barContextBridge{ctx: ctx}
	for _, field := range ds.exposedFields() {
		ds.ip.SetNamedField(field, ctx.Field(field))
	}
	ds.ip.OnBar()
}

// ReportColumns exposes DSL plot() series as backtest report columns.
func (ds *DslStrategy) ReportColumns() []backtest.ReportColumn {
	if ds.ip == nil {
		return nil
	}
	plots := ds.ip.PlotColumns()
	if len(plots) == 0 {
		return nil
	}
	columns := make([]backtest.ReportColumn, len(plots))
	for i, plot := range plots {
		columns[i] = backtest.ReportColumn{
			Source:   plot.Source,
			Label:    plot.Title,
			Decimals: plot.Decimals,
			Overlay:  plot.Overlay,
		}
	}
	return columns
}

// ReportSeries exposes DSL-generated plot series to the result pipeline.
func (ds *DslStrategy) ReportSeries() map[string][]float64 {
	if ds.ip == nil {
		return nil
	}
	return ds.ip.PlotSeries()
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

func (ds *DslStrategy) exposedFields() []string {
	fields := append([]string(nil), ds.meta.ExposeFields...)
	if name := strings.TrimSpace(ds.meta.SignalName); name != "" {
		fields = append(fields, name)
	} else if ds.meta.SignalSource != "" || ds.opts.SignalSource != "" {
		fields = append(fields, "entry_signal")
	}
	return uniqStrings(fields)
}

func (ds *DslStrategy) requestSecurityBuiltin() func(args []runtime.Value) runtime.Value {
	return func(args []runtime.Value) runtime.Value {
		if len(args) < 4 {
			return runtime.NaVal()
		}
		market := strings.TrimSpace(args[0].Str())
		symbol := strings.TrimSpace(args[1].Str())
		interval := strings.TrimSpace(args[2].Str())
		field := strings.TrimSpace(args[3].Str())
		key := requestSecurityKey(market, symbol, interval)
		ref, ok := ds.secRefs[key]
		if !ok || ds.ip == nil || ds.ip.Bridge == nil {
			return runtime.NaVal()
		}
		bridge, ok := ds.ip.Bridge.(*barContextBridge)
		if !ok {
			return runtime.NaVal()
		}
		value := bridge.ctx.Security(ref).Field(field)
		return ds.ip.CaptureSeries("request.security."+key+"."+field, value)
	}
}

func (ds *DslStrategy) requestFactorBuiltin() func(args []runtime.Value) runtime.Value {
	return func(args []runtime.Value) runtime.Value {
		if len(args) < 3 {
			return runtime.NaVal()
		}
		name := strings.TrimSpace(args[0].Str())
		interval := strings.TrimSpace(args[1].Str())
		field := strings.TrimSpace(args[2].Str())
		key := requestFactorKey(name, interval)
		ref, ok := ds.facRefs[key]
		if !ok || ds.ip == nil || ds.ip.Bridge == nil {
			return runtime.NaVal()
		}
		bridge, ok := ds.ip.Bridge.(*barContextBridge)
		if !ok {
			return runtime.NaVal()
		}
		value := bridge.ctx.Factor(ref).Field(field)
		return ds.ip.CaptureSeries("request.factor."+key+"."+field, value)
	}
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

func extractMetadata(prog *ast.Program) strategyMetadata {
	meta := strategyMetadata{}
	for _, stmt := range prog.Stmts {
		if sd, ok := stmt.(*ast.StrategyDecl); ok {
			for _, arg := range sd.Args {
				name := strings.TrimSpace(arg.Name)
				if name == "" {
					continue
				}
				switch name {
				case "signal_source":
					meta.SignalSource = literalString(arg.Value)
				case "signal_name":
					meta.SignalName = literalString(arg.Value)
				case "signal_time_layout":
					meta.SignalTimeLayouts = []string{literalString(arg.Value)}
				case "signal_time_layouts":
					meta.SignalTimeLayouts = literalStringArray(arg.Value)
				case "signal_timezone":
					meta.SignalTimezone = literalString(arg.Value)
				case "signal_timestamp_column":
					meta.SignalTimestampCols = []string{literalString(arg.Value)}
				case "signal_timestamp_columns":
					meta.SignalTimestampCols = literalStringArray(arg.Value)
				case "signal_type_column":
					meta.SignalTypeCols = []string{literalString(arg.Value)}
				case "signal_type_columns":
					meta.SignalTypeCols = literalStringArray(arg.Value)
				case "signal_value_column":
					meta.SignalValueCols = []string{literalString(arg.Value)}
				case "signal_value_columns":
					meta.SignalValueCols = literalStringArray(arg.Value)
				case "signal_entry_matchers":
					meta.SignalEntryMatchers = literalStringArray(arg.Value)
				case "signal_optional_index":
					meta.SignalTextHasIndex = literalBool(arg.Value)
				case "expose_fields":
					meta.ExposeFields = literalStringArray(arg.Value)
				}
			}
		}
		collectRequestSpecs(stmt, &meta.Requests)
	}
	return meta
}

func collectRequestSpecs(node ast.Node, out *[]requestSpec) {
	switch n := node.(type) {
	case *ast.Program:
		for _, stmt := range n.Stmts {
			collectRequestSpecs(stmt, out)
		}
	case *ast.VarDecl:
		collectRequestSpecsFromExpr(n.Value, out)
	case *ast.AssignStmt:
		collectRequestSpecsFromExpr(n.Value, out)
	case *ast.TupleAssign:
		collectRequestSpecsFromExpr(n.Value, out)
	case *ast.ExprStmt:
		collectRequestSpecsFromExpr(n.Expression, out)
	case *ast.IfStmt:
		collectRequestSpecsFromExpr(n.Condition, out)
		collectRequestSpecs(n.Body, out)
		for _, item := range n.ElseIfs {
			collectRequestSpecsFromExpr(item.Condition, out)
			collectRequestSpecs(item.Body, out)
		}
		if n.Else != nil {
			collectRequestSpecs(n.Else, out)
		}
	case *ast.Block:
		for _, stmt := range n.Stmts {
			collectRequestSpecs(stmt, out)
		}
	case *ast.ForStmt:
		collectRequestSpecsFromExpr(n.Start, out)
		collectRequestSpecsFromExpr(n.End, out)
		collectRequestSpecsFromExpr(n.Step, out)
		collectRequestSpecs(n.Body, out)
	case *ast.ForInStmt:
		collectRequestSpecsFromExpr(n.Collection, out)
		collectRequestSpecs(n.Body, out)
	case *ast.WhileStmt:
		collectRequestSpecsFromExpr(n.Condition, out)
		collectRequestSpecs(n.Body, out)
	case *ast.SwitchStmt:
		collectRequestSpecsFromExpr(n.Tag, out)
		for _, c := range n.Cases {
			collectRequestSpecsFromExpr(c.Value, out)
			collectRequestSpecs(c.Body, out)
		}
		if n.Default != nil {
			collectRequestSpecs(n.Default, out)
		}
	case *ast.ReturnStmt:
		collectRequestSpecsFromExpr(n.Value, out)
	}
}

func collectRequestSpecsFromExpr(expr ast.Expr, out *[]requestSpec) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if spec, ok := parseRequestSpec(e); ok {
			*out = append(*out, spec)
		}
		collectRequestSpecsFromExpr(e.Callee, out)
		for _, arg := range e.Args {
			collectRequestSpecsFromExpr(arg.Value, out)
		}
	case *ast.BinaryExpr:
		collectRequestSpecsFromExpr(e.Left, out)
		collectRequestSpecsFromExpr(e.Right, out)
	case *ast.UnaryExpr:
		collectRequestSpecsFromExpr(e.Operand, out)
	case *ast.DotExpr:
		collectRequestSpecsFromExpr(e.Object, out)
	case *ast.IndexExpr:
		collectRequestSpecsFromExpr(e.Left, out)
		collectRequestSpecsFromExpr(e.Index, out)
	case *ast.TernaryExpr:
		collectRequestSpecsFromExpr(e.Condition, out)
		collectRequestSpecsFromExpr(e.Then, out)
		collectRequestSpecsFromExpr(e.Else, out)
	case *ast.ArrayLit:
		for _, item := range e.Elements {
			collectRequestSpecsFromExpr(item, out)
		}
	case *ast.LambdaExpr:
		collectRequestSpecsFromExpr(e.Body, out)
	}
}

func parseRequestSpec(call *ast.CallExpr) (requestSpec, bool) {
	dot, ok := call.Callee.(*ast.DotExpr)
	if !ok {
		return requestSpec{}, false
	}
	obj, ok := dot.Object.(*ast.IdentExpr)
	if !ok || obj.Name != "request" {
		return requestSpec{}, false
	}
	get := func(name string, idx int) string {
		for i, arg := range call.Args {
			if arg.Name == name {
				return literalString(arg.Value)
			}
			if arg.Name == "" && i == idx {
				return literalString(arg.Value)
			}
		}
		return ""
	}
	switch dot.Field {
	case "security":
		market := get("market", 0)
		symbol := get("symbol", 1)
		interval := get("interval", 2)
		field := get("field", 3)
		if market == "" || symbol == "" || interval == "" || field == "" {
			return requestSpec{}, false
		}
		return requestSpec{Kind: "security", Market: market, Symbol: symbol, Interval: interval, Field: field, Key: requestSecurityKey(market, symbol, interval)}, true
	case "factor":
		name := get("name", 0)
		interval := get("interval", 1)
		field := get("field", 2)
		if name == "" || interval == "" || field == "" {
			return requestSpec{}, false
		}
		return requestSpec{Kind: "factor", Name: name, Interval: interval, Field: field, Key: requestFactorKey(name, interval)}, true
	default:
		return requestSpec{}, false
	}
}

func literalString(expr ast.Expr) string {
	if s, ok := expr.(*ast.StringLit); ok {
		return strings.TrimSpace(s.Value)
	}
	return ""
}

func literalBool(expr ast.Expr) bool {
	if b, ok := expr.(*ast.BoolLit); ok {
		return b.Value
	}
	return false
}

func literalStringArray(expr ast.Expr) []string {
	arr, ok := expr.(*ast.ArrayLit)
	if !ok {
		if single := literalString(expr); single != "" {
			return []string{single}
		}
		return nil
	}
	out := make([]string, 0, len(arr.Elements))
	for _, item := range arr.Elements {
		if s := literalString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitCSVOrList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func defaultStrings(values []string, fallback []string) []string {
	if len(values) == 0 {
		return fallback
	}
	return values
}

func uniqStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func requestSecurityKey(market, symbol, interval string) string {
	return strings.Join([]string{market, symbol, interval}, "|")
}

func requestFactorKey(name, interval string) string {
	return strings.Join([]string{name, interval}, "|")
}

func parseLocation(raw string) (*time.Location, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "UTC") {
		return time.UTC, nil
	}
	if strings.EqualFold(trimmed, "UTC+8") || strings.EqualFold(trimmed, "GMT+8") {
		return time.FixedZone("UTC+8", 8*3600), nil
	}
	loc, err := time.LoadLocation(trimmed)
	if err == nil {
		return loc, nil
	}
	return nil, fmt.Errorf("unsupported signal timezone %q", raw)
}
