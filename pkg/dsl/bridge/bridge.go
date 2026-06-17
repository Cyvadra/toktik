// Package bridge adapts a parsed Toktik DSL program into a backtest.Strategy.
package bridge

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/signals"
	"github.com/Cyvadra/toktik/pkg/dsl/analysis"
	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

type Options struct {
	SignalSource string
	// Params provides mixed-type parameter overrides for DSL input.*() calls.
	// Keys are matched against input titles. Values can be float64, int, bool, or string.
	Params map[string]interface{}
	// Config provides catalog-level configuration values accessible via config.get().
	Config map[string]interface{}
	// InitHook is called during Init after standard setup, allowing strategies to
	// register custom computed fields (via ctx.Register) that the DSL accesses via expose_fields.
	InitHook func(ctx *backtest.SetupContext) error
	// PreloadHook is called during Preload after signal loading, allowing strategies to
	// pre-compute series columns (via ctx.Primary().SetColumn) that the DSL accesses via expose_fields.
	PreloadHook func(ctx *backtest.PreloadContext) error
}

type strategyMetadata = analysis.SignalMetadata

type requestSpec = analysis.RequestSpec

/*type strategyMetadata struct {
	SignalSource        string
	SignalName          string
	SignalTimeLayouts   []string
	SignalTimezone      string
	SignalTimestampCols []string
	SignalTypeCols      []string
	SignalValueCols     []string
	SignalEntryMatchers []string
	SignalTextHasIndex  bool
	// Rich event column mappings.
	SignalNameColumn      string
	SignalDirectionColumn string
	SignalActionColumn    string
	SignalRemarksColumn   string
	SignalQtyColumn       string
	ExposeFields          []string
	Requests              []requestSpec
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
*/

// ChainRequestSpec describes one constant options.chain(market, symbol)
// dependency discovered in a DSL program.
type ChainRequestSpec struct {
	Market string
	Symbol string
	Key    string
}

type Manifest = analysis.Manifest

// DslStrategy implements backtest.Strategy by interpreting a Toktik DSL script.
type DslStrategy struct {
	source        string
	name          string
	prog          *ast.Program
	ip            *runtime.Interpreter
	errs          []string
	opts          Options
	manifest      analysis.Manifest
	meta          strategyMetadata
	secRefs       map[string]backtest.SecurityRef
	facRefs       map[string]backtest.FactorRef
	remoteFacRefs map[string]backtest.FactorRef
	deferredExprs map[string]ast.Expr
	events        []signals.SignalEvent // structured events loaded during Preload
}

// New creates a DslStrategy from DSL source code.
func New(source string) *DslStrategy {
	return NewWithOptions(source, Options{})
}

// NewWithOptions creates a DslStrategy with runtime configuration overrides.
func NewWithOptions(source string, opts Options) *DslStrategy {
	prog, errs := parser.Parse(source)
	manifest := analysis.Analyze(prog)
	name := manifest.StrategyName
	if name == "" {
		name = "dsl_strategy"
	}
	return &DslStrategy{
		source:        source,
		name:          name,
		prog:          prog,
		errs:          errs,
		opts:          opts,
		manifest:      manifest,
		meta:          manifest.Metadata,
		secRefs:       make(map[string]backtest.SecurityRef),
		facRefs:       make(map[string]backtest.FactorRef),
		remoteFacRefs: make(map[string]backtest.FactorRef),
		deferredExprs: collectDeferredExprs(prog),
	}
}

// ParseErrors returns any errors from parsing.
func (ds *DslStrategy) ParseErrors() []string { return ds.errs }

// OptionChainRequests returns the constant option-chain dependencies that were
// statically discovered in the DSL source.
func (ds *DslStrategy) OptionChainRequests() []ChainRequestSpec {
	requests := ds.manifest.OptionChainRequests()
	if len(requests) == 0 {
		return nil
	}
	out := make([]ChainRequestSpec, 0, len(requests))
	for _, req := range requests {
		out = append(out, ChainRequestSpec{Market: req.Market, Symbol: req.Symbol, Key: req.Key})
	}
	return out
}

func (ds *DslStrategy) Manifest() analysis.Manifest { return ds.manifest }

func (ds *DslStrategy) Diagnostics() []diagnostics.Diagnostic { return ds.manifest.Diagnostics }

// Name implements backtest.Strategy.
func (ds *DslStrategy) Name() string { return ds.name }

// Init implements backtest.Strategy.
func (ds *DslStrategy) Init(ctx *backtest.SetupContext) error {
	ds.secRefs = make(map[string]backtest.SecurityRef)
	ds.facRefs = make(map[string]backtest.FactorRef)
	ds.remoteFacRefs = make(map[string]backtest.FactorRef)
	for _, req := range ds.meta.Requests {
		switch req.Kind {
		case "security":
			if _, ok := ds.secRefs[req.Key]; !ok {
				ds.secRefs[req.Key] = ctx.AddSecurity(req.Market, req.Symbol, req.Interval)
			}
			if req.ExpressionMode {
				expr := ds.resolveDeferredExpr(req.Expression)
				for _, factor := range collectFactorRequests(expr) {
					key := remoteFactorKey(req.Key, factor.Name, factor.Interval)
					if _, ok := ds.remoteFacRefs[key]; !ok {
						ds.remoteFacRefs[key] = ctx.AddSymbolFactor(factor.Name, req.Market, req.Symbol, factor.Interval, factor.Mode)
					}
				}
			}
		case "factor":
			if _, ok := ds.facRefs[req.Key]; !ok {
				ds.facRefs[req.Key] = ctx.AddFactor(req.Name, req.Interval)
			}
		case "fundamental":
			if _, ok := ds.facRefs[req.Key]; !ok {
				interval := req.Interval
				if strings.TrimSpace(strings.ToLower(interval)) == "primary" {
					interval = ctx.PrimaryRef().Interval
				}
				ds.facRefs[req.Key] = ctx.AddSymbolFactor(req.Name, req.Market, req.Symbol, interval, req.Mode)
			}
		}
	}
	ds.ip = runtime.NewInterpreter(ds.prog)
	ApplyParams(ds.ip, ds.opts.Params)
	runtime.RegisterBacktestProfile(ds.ip, ds.requestSecurityBuiltin(), ds.requestFactorBuiltin(), ds.requestFundamentalBuiltin())
	ds.ip.Init()
	if ds.opts.InitHook != nil {
		if err := ds.opts.InitHook(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Preload implements backtest.StrategyPreloader for signal-driven DSL scripts.
func (ds *DslStrategy) Preload(ctx *backtest.PreloadContext) error {
	signalPaths := splitCSVOrList(ds.opts.SignalSource)
	if len(signalPaths) == 0 {
		signalPaths = splitCSVOrList(ds.meta.SignalSource)
	}
	if len(signalPaths) == 0 {
		if ds.opts.PreloadHook != nil {
			return ds.opts.PreloadHook(ctx)
		}
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
	cfg := signals.Config{
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
		// Rich event column mappings from metadata.
		NameColumn:      ds.meta.SignalNameColumn,
		DirectionColumn: ds.meta.SignalDirectionColumn,
		ActionColumn:    ds.meta.SignalActionColumn,
		RemarksColumn:   ds.meta.SignalRemarksColumn,
		QtyColumn:       ds.meta.SignalQtyColumn,
	}
	// Load structured events (superset of LoadTimes).
	events, err := signals.LoadEvents(cfg)
	if err != nil {
		return fmt.Errorf("dsl signal preload: %w", err)
	}
	ds.events = events

	// Inject backward-compatible binary series.
	times := signals.EventsToTimes(events)
	series := signals.BuildBinarySeries(ctx.Primary().Timestamps(), times)
	if err := ctx.Primary().SetColumn(outputName, series); err != nil {
		return err
	}

	// Inject rich multi-column event series.
	eventSeries := signals.BuildEventSeries(ctx.Primary().Timestamps(), events, outputName)
	for colName, colData := range eventSeries {
		if colName == outputName {
			continue // already set above
		}
		if err := ctx.Primary().SetColumn(colName, colData); err != nil {
			return err
		}
	}
	if ds.opts.PreloadHook != nil {
		if err := ds.opts.PreloadHook(ctx); err != nil {
			return err
		}
	}
	return nil
}

// OnBar implements backtest.Strategy.
func (ds *DslStrategy) OnBar(ctx *backtest.BarContext) {
	barEvents := signals.EventsAtTime(ds.events, ctx.Time())
	ds.ip.Bridge = &barContextBridge{ctx: ctx, events: barEvents, config: ds.opts.Config, ds: ds}
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

// ParamSchema returns the extracted parameter declarations from the DSL script.
// This enables external tools (API, UI, optimizer) to discover available parameters
// and their types, defaults, and constraints without executing the script.
func (ds *DslStrategy) ParamSchema() []ParamSchema {
	return ds.manifest.Inputs
}

// Events returns the structured signal events loaded during Preload.
func (ds *DslStrategy) Events() []signals.SignalEvent {
	return ds.events
}

// barContextBridge adapts backtest.BarContext to the runtime.Bridge interface.
type barContextBridge struct {
	ctx    *backtest.BarContext
	events []signals.SignalEvent // events at current bar
	config map[string]interface{}
	ds     *DslStrategy
}

func (b *barContextBridge) BarIndex() int                   { return b.ctx.BarIndex() }
func (b *barContextBridge) Close() float64                  { return b.ctx.Close() }
func (b *barContextBridge) Open() float64                   { return b.ctx.Open() }
func (b *barContextBridge) High() float64                   { return b.ctx.High() }
func (b *barContextBridge) Low() float64                    { return b.ctx.Low() }
func (b *barContextBridge) Volume() float64                 { return b.ctx.Volume() }
func (b *barContextBridge) Field(n string) float64          { return b.ctx.Field(n) }
func (b *barContextBridge) FieldAt(n string, o int) float64 { return b.ctx.FieldAt(n, o) }

// SignalEvents implements runtime.SignalBridge for signal.* builtins.
func (b *barContextBridge) SignalEvents() []signals.SignalEvent { return b.events }

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
	return b.ctx.PositionAvgEntryPrice(b.primaryRef())
}

func (b *barContextBridge) Equity() float64 { return b.ctx.Equity() }
func (b *barContextBridge) Cash() float64   { return b.ctx.Cash() }

func (b *barContextBridge) Ind(name string) float64          { return b.ctx.Ind(name) }
func (b *barContextBridge) IndAt(name string, o int) float64 { return b.ctx.IndAt(name, o) }

// EvalSpecialForm implements runtime.SpecialFormBridge for request.security's
// expression mode. The fourth argument remains an AST template and is evaluated
// with field/factor reads redirected to the requested security context.
func (b *barContextBridge) EvalSpecialForm(ip *runtime.Interpreter, call *ast.CallExpr, scope *runtime.Scope) (runtime.Value, bool) {
	if b == nil || b.ds == nil || call == nil || !isRequestSecurityCall(call) {
		return runtime.Value{}, false
	}
	if len(call.Args) < 4 {
		return runtime.NaVal(), true
	}
	marketValue := ip.EvalExpression(call.Args[0].Value, scope)
	symbolValue := ip.EvalExpression(call.Args[1].Value, scope)
	intervalValue := ip.EvalExpression(call.Args[2].Value, scope)
	market := strings.TrimSpace(marketValue.Str())
	symbol := strings.TrimSpace(symbolValue.Str())
	interval := strings.TrimSpace(intervalValue.Str())
	fieldExpr := call.Args[3].Value
	if field, ok := fieldExpr.(*ast.StringLit); ok {
		ref, found := b.ds.secRefs[requestSecurityKey(market, symbol, interval)]
		if !found {
			return runtime.NaVal(), true
		}
		value := b.ctx.Security(ref).Field(strings.TrimSpace(field.Value))
		return ip.CaptureSeries("request.security."+requestSecurityKey(market, symbol, interval)+"."+strings.TrimSpace(field.Value), value), true
	}
	key := requestSecurityKey(market, symbol, interval)
	ref, found := b.ds.secRefs[key]
	if !found {
		return runtime.NaVal(), true
	}
	expr := b.ds.resolveDeferredExpr(fieldExpr)
	previous := ip.Bridge
	ip.Bridge = &remoteContextBridge{parent: b, securityRef: ref, securityKey: key}
	restore := bindRemoteFields(ip, scope, key, b.ctx.Security(ref))
	value := ip.EvalExpression(expr, scope)
	restore()
	ip.Bridge = previous
	return ip.CaptureSeries("request.security."+key+".__expr", value.Float()), true
}

func bindRemoteFields(ip *runtime.Interpreter, scope *runtime.Scope, key string, acc *backtest.SecurityAccessor) func() {
	fields := []string{"open", "high", "low", "close", "volume"}
	previous := make(map[string]runtime.Value, len(fields))
	for _, field := range fields {
		value, _ := scope.Get(field)
		previous[field] = value
		remoteValue := math.NaN()
		if acc != nil {
			remoteValue = acc.Field(field)
		}
		scope.Set(field, ip.CaptureSeries("request.security."+key+"."+field+".__remote", remoteValue))
	}
	return func() {
		for _, field := range fields {
			scope.Set(field, previous[field])
		}
	}
}

type remoteContextBridge struct {
	parent      *barContextBridge
	securityRef backtest.SecurityRef
	securityKey string
}

func (r *remoteContextBridge) accessor() *backtest.SecurityAccessor {
	if r == nil || r.parent == nil {
		return nil
	}
	return r.parent.ctx.Security(r.securityRef)
}

func (r *remoteContextBridge) BarIndex() int   { return r.parent.BarIndex() }
func (r *remoteContextBridge) Close() float64  { return r.Field("close") }
func (r *remoteContextBridge) Open() float64   { return r.Field("open") }
func (r *remoteContextBridge) High() float64   { return r.Field("high") }
func (r *remoteContextBridge) Low() float64    { return r.Field("low") }
func (r *remoteContextBridge) Volume() float64 { return r.Field("volume") }
func (r *remoteContextBridge) Field(name string) float64 {
	if acc := r.accessor(); acc != nil {
		return acc.Field(name)
	}
	return math.NaN()
}
func (r *remoteContextBridge) FieldAt(name string, offset int) float64 {
	if acc := r.accessor(); acc != nil {
		return acc.FieldAt(name, offset)
	}
	return math.NaN()
}
func (r *remoteContextBridge) Buy(qty float64)                       { r.parent.Buy(qty) }
func (r *remoteContextBridge) Sell(qty float64)                      { r.parent.Sell(qty) }
func (r *remoteContextBridge) EntryLong(id string, qty float64)      { r.parent.EntryLong(id, qty) }
func (r *remoteContextBridge) EntryShort(id string, qty float64)     { r.parent.EntryShort(id, qty) }
func (r *remoteContextBridge) ExitLong(id string)                    { r.parent.ExitLong(id) }
func (r *remoteContextBridge) ExitShort(id string)                   { r.parent.ExitShort(id) }
func (r *remoteContextBridge) PositionSize() float64                 { return r.parent.PositionSize() }
func (r *remoteContextBridge) PositionAvgPrice() float64             { return r.parent.PositionAvgPrice() }
func (r *remoteContextBridge) Equity() float64                       { return r.parent.Equity() }
func (r *remoteContextBridge) Cash() float64                         { return r.parent.Cash() }
func (r *remoteContextBridge) Ind(name string) float64               { return r.Field(name) }
func (r *remoteContextBridge) IndAt(name string, offset int) float64 { return r.FieldAt(name, offset) }

func (r *remoteContextBridge) EvalSpecialForm(ip *runtime.Interpreter, call *ast.CallExpr, scope *runtime.Scope) (runtime.Value, bool) {
	if call == nil || !isRequestFactorCall(call) {
		return runtime.Value{}, false
	}
	if len(call.Args) < 3 || r.parent == nil || r.parent.ds == nil {
		return runtime.NaVal(), true
	}
	name := strings.TrimSpace(ip.EvalExpression(call.Args[0].Value, scope).Str())
	interval := strings.TrimSpace(ip.EvalExpression(call.Args[1].Value, scope).Str())
	field := strings.TrimSpace(ip.EvalExpression(call.Args[2].Value, scope).Str())
	ref, ok := r.parent.ds.remoteFacRefs[remoteFactorKey(r.securityKey, name, interval)]
	if !ok {
		return runtime.NaVal(), true
	}
	value := r.parent.ctx.Factor(ref).Field(field)
	return ip.CaptureSeries("request.factor."+r.securityKey+"."+requestFactorKey(name, interval)+"."+field, value), true
}

// SubmitOrder implements runtime.OrderBridge by delegating to OrderBuilder.
func (b *barContextBridge) SubmitOrder(intent runtime.OrderIntent) int {
	ref := b.primaryRef()
	ob := b.ctx.Order(ref)

	switch intent.Side {
	case runtime.SideBuy:
		ob.Buy()
	case runtime.SideSell:
		ob.Sell()
	default:
		ob.Buy()
	}

	if intent.Notional > 0 {
		ob.Notional(intent.Notional)
	} else {
		qty := intent.Qty
		if qty == 0 {
			qty = 1
		}
		ob.Qty(qty)
	}

	if intent.Note != "" {
		ob.Note(intent.Note)
	}

	switch intent.Type {
	case runtime.OrderLimit:
		ob.Limit(intent.LimitPrice)
	case runtime.OrderStop:
		ob.Stop(intent.StopPrice)
	case runtime.OrderStopLimit:
		ob.StopLimit(intent.StopPrice, intent.LimitPrice)
	case runtime.OrderTWAP:
		ob.TWAP(intent.TWAPBars)
	}

	if intent.Immediate {
		ob.Immediate()
	}

	return ob.Submit()
}

// ConfigFloat implements runtime.ConfigBridge.
func (b *barContextBridge) ConfigFloat(name string, defval float64) float64 {
	if b.config == nil {
		return defval
	}
	v, ok := b.config[name]
	if !ok {
		return defval
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	default:
		return defval
	}
}

// ConfigString implements runtime.ConfigBridge.
func (b *barContextBridge) ConfigString(name string, defval string) string {
	if b.config == nil {
		return defval
	}
	v, ok := b.config[name]
	if !ok {
		return defval
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

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

func (ds *DslStrategy) requestFundamentalBuiltin() func(args []runtime.Value) runtime.Value {
	return func(args []runtime.Value) runtime.Value {
		if len(args) < 3 {
			return runtime.NaVal()
		}
		market := strings.TrimSpace(args[0].Str())
		symbol := strings.TrimSpace(args[1].Str())
		factor := strings.TrimSpace(args[2].Str())
		mode := "filled"
		if len(args) >= 4 {
			mode = strings.TrimSpace(args[3].Str())
		}
		if mode == "" {
			mode = "filled"
		}
		if factor == "" {
			return runtime.NaVal()
		}
		key := analysis.RequestFundamentalKey(market, symbol, factor, mode)
		ref, ok := ds.facRefs[key]
		if !ok || ds.ip == nil || ds.ip.Bridge == nil {
			return runtime.NaVal()
		}
		bridge, ok := ds.ip.Bridge.(*barContextBridge)
		if !ok {
			return runtime.NaVal()
		}
		value := bridge.ctx.Factor(ref).Field("value")
		return ds.ip.CaptureSeries("request.fundamental."+key+".value", value)
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

func (b *barContextBridge) OptionsChainFor(market, underlying string) interface{} {
	ch := b.ctx.OptionsChainFor(market, underlying)
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

func (b *barContextBridge) ContractUnderlying(c interface{}) string {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.ChainUnderlying()
	}
	return ""
}

func (b *barContextBridge) ContractMarket(c interface{}) string {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return oc.ChainMarket()
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

func (b *barContextBridge) ContractExpiry(c interface{}) float64 {
	if oc, ok := c.(*backtest.OptionContract); ok {
		return float64(oc.Expiration.UTC().Unix())
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
		return backtest.OptionPriceMarkClose.EntryPrice(backtest.Buy, *oc)
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
	b.CloseSpreadWithReason(spreadID, "")
}

func (b *barContextBridge) CloseSpreadWithReason(spreadID int, reason string) {
	sp := b.ctx.Spreads().Get(spreadID)
	if sp == nil {
		return
	}
	for i := range sp.Legs {
		if sp.Legs[i].Closed {
			continue
		}
		price := backtest.OptionPriceMarkClose.EntryPrice(backtest.Buy, sp.Legs[i].Contract)
		if reason != "" {
			b.ctx.CloseSpreadLegWithReason(spreadID, i, price, reason)
		} else {
			b.ctx.CloseSpreadLeg(spreadID, i, price)
		}
	}
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
		return backtest.OptionPriceMarkClose.EntryPrice(backtest.Buy, oc)
	})
}

func (b *barContextBridge) SpreadLegContract(spreadID, legIndex int) interface{} {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return nil
	}
	contract := leg.Contract
	return &contract
}

func (b *barContextBridge) SpreadLegEntryPrice(spreadID, legIndex int) float64 {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return 0
	}
	return leg.EntryPrice
}

func (b *barContextBridge) SpreadLegQty(spreadID, legIndex int) float64 {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return 0
	}
	return leg.Qty
}

func (b *barContextBridge) SpreadLegSide(spreadID, legIndex int) string {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return ""
	}
	if leg.Side == backtest.Sell {
		return "sell"
	}
	return "buy"
}

func (b *barContextBridge) SpreadLegIsOpen(spreadID, legIndex int) bool {
	leg := b.spreadLeg(spreadID, legIndex)
	if leg == nil {
		return false
	}
	return !leg.Closed
}

func (b *barContextBridge) spreadLeg(spreadID, legIndex int) *backtest.SpreadLeg {
	st := b.ctx.Spreads()
	if st == nil {
		return nil
	}
	sp := st.Get(spreadID)
	if sp == nil || legIndex < 0 || legIndex >= len(sp.Legs) {
		return nil
	}
	return &sp.Legs[legIndex]
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
		ID:          g.ID,
		Tag:         g.Tag,
		Amount:      g.CurrentAmount(),
		RollCount:   g.RollCount,
		IsClosed:    g.Closed,
		SpreadIDs:   g.SpreadIDs,
		SpreadCount: len(g.SpreadIDs),
	}
}

func (b *barContextBridge) GroupAddSpread(groupID, spreadID int) {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return
	}
	gt.AddSpread(groupID, spreadID)
}

func (b *barContextBridge) GroupIncrementRoll(groupID int) {
	gt := b.ctx.SpreadGroups()
	if gt == nil {
		return
	}
	gt.IncrementRoll(groupID)
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
	b.ctx.ScheduleCloseAfter(b.barOffsetDuration(triggerBarOffset), spreadID)
}

func (b *barContextBridge) ScheduleCloseSpreadWithReason(triggerBarOffset int, spreadID int, reason string) {
	b.ctx.ScheduleCloseSpreadOrder(
		b.ctx.Time().Add(b.barOffsetDuration(triggerBarOffset)),
		spreadID,
		backtest.SpreadOrderMarket,
		backtest.Sell,
		math.NaN(),
		0,
		reason,
	)
}

func (b *barContextBridge) ScheduleCloseLeg(triggerBarOffset int, spreadID, legIndex int) {
	b.ctx.ScheduleCloseLegAfter(b.barOffsetDuration(triggerBarOffset), spreadID, legIndex)
}

func (b *barContextBridge) ScheduleCloseGroup(triggerBarOffset int, groupID int) {
	info := b.GroupGet(groupID)
	if info.ID == 0 {
		return
	}
	for _, spreadID := range info.SpreadIDs {
		if spreadID > 0 {
			b.ScheduleCloseSpread(triggerBarOffset, spreadID)
		}
	}
}

func (b *barContextBridge) barOffsetDuration(triggerBarOffset int) time.Duration {
	if triggerBarOffset <= 0 {
		return 0
	}
	barDur := time.Hour
	if next := b.ctx.NextBarTime(); !next.IsZero() && next.After(b.ctx.Time()) {
		barDur = next.Sub(b.ctx.Time())
	}
	return time.Duration(triggerBarOffset) * barDur
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
				case "signal_name_column":
					meta.SignalNameColumn = literalString(arg.Value)
				case "signal_direction_column":
					meta.SignalDirectionColumn = literalString(arg.Value)
				case "signal_action_column":
					meta.SignalActionColumn = literalString(arg.Value)
				case "signal_remarks_column":
					meta.SignalRemarksColumn = literalString(arg.Value)
				case "signal_qty_column":
					meta.SignalQtyColumn = literalString(arg.Value)
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
	if !ok {
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
		if obj.Name != "request" {
			return requestSpec{}, false
		}
		market := get("market", 0)
		symbol := get("symbol", 1)
		interval := get("interval", 2)
		field := get("field", 3)
		if market == "" || symbol == "" || interval == "" || field == "" {
			return requestSpec{}, false
		}
		return requestSpec{Kind: "security", Market: market, Symbol: symbol, Interval: interval, Field: field, Key: requestSecurityKey(market, symbol, interval)}, true
	case "chain":
		if obj.Name != "options" {
			return requestSpec{}, false
		}
		market := get("market", 0)
		symbol := get("symbol", 1)
		if market == "" || symbol == "" {
			return requestSpec{}, false
		}
		return requestSpec{Kind: "option_chain", Market: market, Symbol: symbol, Key: backtest.ChainLookupKey(market, symbol)}, true
	case "factor":
		if obj.Name != "request" {
			return requestSpec{}, false
		}
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

func remoteFactorKey(securityKey, name, interval string) string {
	return strings.Join([]string{securityKey, name, interval}, "|")
}

func collectDeferredExprs(prog *ast.Program) map[string]ast.Expr {
	out := make(map[string]ast.Expr)
	if prog == nil {
		return out
	}
	for _, stmt := range prog.Stmts {
		decl, ok := stmt.(*ast.VarDecl)
		if !ok || strings.TrimSpace(decl.Name) == "" || decl.Value == nil {
			continue
		}
		if isDeferredTemplateExpr(decl.Value) {
			out[decl.Name] = decl.Value
		}
	}
	return out
}

func (ds *DslStrategy) resolveDeferredExpr(expr ast.Expr) ast.Expr {
	if id, ok := expr.(*ast.IdentExpr); ok && ds != nil && ds.deferredExprs != nil {
		if resolved, found := ds.deferredExprs[id.Name]; found {
			return resolved
		}
	}
	if value, ok := expr.(*ast.IdentExpr); ok {
		_ = value
	}
	return expr
}

func isDeferredTemplateExpr(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	return isRequestFactorCall(call) || isRequestFundamentalCall(call)
}

func collectFactorRequests(expr ast.Expr) []requestSpec {
	var out []requestSpec
	var walk func(ast.Expr)
	walk = func(node ast.Expr) {
		switch n := node.(type) {
		case nil:
			return
		case *ast.BinaryExpr:
			walk(n.Left)
			walk(n.Right)
		case *ast.UnaryExpr:
			walk(n.Operand)
		case *ast.CallExpr:
			if isRequestFactorCall(n) {
				name := positionalStringArg(n, "name", 0)
				interval := positionalStringArg(n, "interval", 1)
				field := positionalStringArg(n, "field", 2)
				if name != "" && interval != "" && field != "" {
					out = append(out, requestSpec{Kind: "factor", Name: name, Interval: interval, Field: field, Key: requestFactorKey(name, interval)})
				}
			}
			walk(n.Callee)
			for _, arg := range n.Args {
				walk(arg.Value)
			}
		case *ast.DotExpr:
			walk(n.Object)
		case *ast.IndexExpr:
			walk(n.Left)
			walk(n.Index)
		case *ast.TernaryExpr:
			walk(n.Condition)
			walk(n.Then)
			walk(n.Else)
		case *ast.ArrayLit:
			for _, element := range n.Elements {
				walk(element)
			}
		case *ast.LambdaExpr:
			walk(n.Body)
		}
	}
	walk(expr)
	return out
}

func positionalStringArg(call *ast.CallExpr, name string, idx int) string {
	if call == nil {
		return ""
	}
	for _, arg := range call.Args {
		if arg.Name == name {
			return literalString(arg.Value)
		}
	}
	if idx >= 0 && idx < len(call.Args) && call.Args[idx].Name == "" {
		return literalString(call.Args[idx].Value)
	}
	return ""
}

func isRequestSecurityCall(call *ast.CallExpr) bool {
	return isRequestCall(call, "security")
}

func isRequestFactorCall(call *ast.CallExpr) bool {
	return isRequestCall(call, "factor")
}

func isRequestFundamentalCall(call *ast.CallExpr) bool {
	return isRequestCall(call, "fundamental")
}

func isRequestCall(call *ast.CallExpr, field string) bool {
	if call == nil {
		return false
	}
	dot, ok := call.Callee.(*ast.DotExpr)
	if !ok || dot.Field != field {
		return false
	}
	obj, ok := dot.Object.(*ast.IdentExpr)
	return ok && obj.Name == "request"
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
