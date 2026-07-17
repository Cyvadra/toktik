// Package bridge adapts a parsed Toktik DSL program into a backtest.Strategy.
package bridge

import (
	"fmt"
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
	// SpreadPricing controls how DSL option spreads are opened, closed, and valued.
	SpreadPricing backtest.SpreadPricingConfig
	// Params provides mixed-type parameter overrides for DSL input.*() calls.
	// Keys are matched against input titles. Values can be float64, int, bool, or string.
	Params map[string]interface{}
	// Config provides catalog-level configuration values accessible via config.get().
	Config map[string]interface{}
	// Universe is the single point-in-time snapshot used for both runtime
	// membership and dependency preloading.
	Universe *UniverseSnapshot
	// InitHook is called during Init after standard setup, allowing strategies to
	// register custom computed fields (via ctx.Register) that the DSL accesses via expose_fields.
	InitHook func(ctx *backtest.SetupContext) error
	// PreloadHook is called during Preload after signal loading, allowing strategies to
	// pre-compute series columns (via ctx.Primary().SetColumn) that the DSL accesses via expose_fields.
	PreloadHook func(ctx *backtest.PreloadContext) error
}

type strategyMetadata = analysis.SignalMetadata

type requestSpec = analysis.RequestSpec

// ChainRequestSpec describes one constant options.chain(market, symbol)
// dependency discovered in a DSL program.
type ChainRequestSpec struct {
	Market string
	Symbol string
	Key    string
}

type Manifest = analysis.Manifest

type UniverseProvider interface {
	SymbolsAt(code string, ts time.Time) []string
}

type UniverseSnapshot struct {
	Provider UniverseProvider
	Members  map[string][]string
}

type DependencyPlan struct {
	Universe *UniverseSnapshot
	Requests []analysis.RequestSpec
}

func buildDependencyPlan(manifest analysis.Manifest, universe *UniverseSnapshot) DependencyPlan {
	plan := DependencyPlan{Universe: universe}
	seen := make(map[string]struct{})
	add := func(request analysis.RequestSpec) {
		if request.Dynamic || request.Key == "" {
			return
		}
		key := request.Kind + ":" + request.Key
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		plan.Requests = append(plan.Requests, request)
	}
	for _, request := range manifest.Requests {
		if !request.IsUniverseExpanded() {
			add(request)
			continue
		}
		if universe == nil {
			continue
		}
		for _, symbol := range universe.Members[request.UniverseCode] {
			concrete := request
			concrete.Symbol = strings.TrimSpace(symbol)
			concrete.Dynamic = false
			concrete.Tier = analysis.RequestTierStatic
			switch concrete.Kind {
			case "security":
				concrete.Key = requestSecurityKey(concrete.Market, concrete.Symbol, concrete.Interval)
			case "factor":
				concrete.Key = analysis.RequestSymbolFactorKey(concrete.Name, concrete.Symbol, concrete.Interval)
			case "fundamental":
				concrete.Key = analysis.RequestFundamentalKey(concrete.Market, concrete.Symbol, concrete.Name, concrete.Mode)
			}
			add(concrete)
		}
	}
	return plan
}

// DslStrategy implements backtest.Strategy by interpreting a Toktik DSL script.
type DslStrategy struct {
	source             string
	name               string
	prog               *ast.Program
	ip                 *runtime.Interpreter
	errs               []string
	opts               Options
	manifest           analysis.Manifest
	dependencyPlan     DependencyPlan
	meta               strategyMetadata
	secRefs            map[string]backtest.SecurityRef
	facRefs            map[string]backtest.FactorRef
	remoteFacRefs      map[string]backtest.FactorRef
	aliasExprs         map[string]ast.Expr
	runtimeDiagnostics diagnostics.List
	missingRequestKeys map[string]struct{}
	events             []signals.SignalEvent // structured events loaded during Preload
}

// New creates a DslStrategy from DSL source code.
func New(source string) *DslStrategy {
	return NewWithOptions(source, Options{})
}

// NewWithOptions creates a DslStrategy with runtime configuration overrides.
func NewWithOptions(source string, opts Options) *DslStrategy {
	prog, errs := parser.Parse(source)
	manifest := analysis.AnalyzeWithParams(prog, opts.Params)
	name := manifest.StrategyName
	if name == "" {
		name = "dsl_strategy"
	}
	return &DslStrategy{
		source:             source,
		name:               name,
		prog:               prog,
		errs:               errs,
		opts:               opts,
		manifest:           manifest,
		dependencyPlan:     buildDependencyPlan(manifest, opts.Universe),
		meta:               manifest.Metadata,
		secRefs:            make(map[string]backtest.SecurityRef),
		facRefs:            make(map[string]backtest.FactorRef),
		remoteFacRefs:      make(map[string]backtest.FactorRef),
		aliasExprs:         collectAliasExprs(prog),
		missingRequestKeys: make(map[string]struct{}),
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

func (ds *DslStrategy) Diagnostics() []diagnostics.Diagnostic {
	out := append([]diagnostics.Diagnostic(nil), ds.manifest.Diagnostics...)
	out = append(out, ds.runtimeDiagnostics...)
	return out
}

// Name implements backtest.Strategy.
func (ds *DslStrategy) Name() string { return ds.name }

// SpreadPricingConfig implements backtest.SpreadPricingProvider.
func (ds *DslStrategy) SpreadPricingConfig() backtest.SpreadPricingConfig {
	return ds.opts.SpreadPricing.WithDefaults()
}

// Init implements backtest.Strategy.
func (ds *DslStrategy) Init(ctx *backtest.SetupContext) error {
	ds.secRefs = make(map[string]backtest.SecurityRef)
	ds.facRefs = make(map[string]backtest.FactorRef)
	ds.remoteFacRefs = make(map[string]backtest.FactorRef)
	ds.missingRequestKeys = make(map[string]struct{})
	for _, req := range ds.dependencyPlan.Requests {
		ds.registerRequest(ctx, req)
	}
	ds.ip = runtime.NewInterpreter(ds.prog)
	ds.runtimeDiagnostics = nil
	ds.ip.Diagnostics = &ds.runtimeDiagnostics
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

func (ds *DslStrategy) registerRequest(ctx *backtest.SetupContext, req requestSpec) {
	switch req.Kind {
	case "security":
		if _, ok := ds.secRefs[req.Key]; !ok {
			ds.secRefs[req.Key] = ctx.AddSecurity(req.Market, req.Symbol, req.Interval)
		}
		if !req.ExpressionMode {
			return
		}
		expr := ds.resolveRemoteExpr(req.Expression)
		for _, factor := range collectRemoteFactorRequests(expr) {
			key := remoteFactorKey(req.Key, factor)
			if _, ok := ds.remoteFacRefs[key]; ok {
				continue
			}
			interval := factor.Interval
			if strings.TrimSpace(strings.ToLower(interval)) == "primary" {
				interval = req.Interval
			}
			market := strings.TrimSpace(factor.Market)
			if market == "" {
				market = req.Market
			}
			symbol := strings.TrimSpace(factor.Symbol)
			if symbol == "" {
				symbol = req.Symbol
			}
			ds.remoteFacRefs[key] = ctx.AddSymbolFactor(factor.Name, market, symbol, interval, factor.Mode)
		}
	case "factor":
		if _, ok := ds.facRefs[req.Key]; !ok {
			if strings.TrimSpace(req.Symbol) == "" {
				ds.facRefs[req.Key] = ctx.AddFactor(req.Name, req.Interval)
			} else {
				ds.facRefs[req.Key] = ctx.AddSymbolFactor(req.Name, ctx.PrimaryRef().Market, req.Symbol, req.Interval, "")
			}
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
	ds.ip.Bridge = &barContextBridge{ctx: ctx, events: barEvents, config: ds.opts.Config, ds: ds, spreadPricing: ds.SpreadPricingConfig()}
	for _, field := range defaultRawPriceFields() {
		ds.ip.SetNamedField(field, ctx.Field(field))
	}
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
	ctx           *backtest.BarContext
	events        []signals.SignalEvent // events at current bar
	config        map[string]interface{}
	ds            *DslStrategy
	spreadPricing backtest.SpreadPricingConfig
}

func (b *barContextBridge) BarIndex() int                   { return b.ctx.BarIndex() }
func (b *barContextBridge) Close() float64                  { return b.ctx.Close() }
func (b *barContextBridge) Open() float64                   { return b.ctx.Open() }
func (b *barContextBridge) High() float64                   { return b.ctx.High() }
func (b *barContextBridge) Low() float64                    { return b.ctx.Low() }
func (b *barContextBridge) Volume() float64                 { return b.ctx.Volume() }
func (b *barContextBridge) Field(n string) float64          { return b.ctx.Field(n) }
func (b *barContextBridge) FieldAt(n string, o int) float64 { return b.ctx.FieldAt(n, o) }

func (b *barContextBridge) UniverseSymbols(code string) []string {
	if b.ds == nil || b.ds.dependencyPlan.Universe == nil || b.ds.dependencyPlan.Universe.Provider == nil {
		return nil
	}
	return b.ds.dependencyPlan.Universe.Provider.SymbolsAt(code, b.ctx.Time())
}

func (ds *DslStrategy) ConcreteDataRequestCount() int {
	count := 0
	for _, request := range ds.dependencyPlan.Requests {
		if request.Kind == "security" || request.Kind == "factor" || request.Kind == "fundamental" {
			count++
		}
	}
	return count
}

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

func defaultRawPriceFields() []string {
	return []string{"open_raw", "high_raw", "low_raw", "close_raw"}
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
			if !ok {
				ds.addMissingRequestDiagnostic("request.security", key, -1)
			}
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
		symbol, interval, field := "", "", ""
		if len(args) >= 4 {
			symbol = strings.TrimSpace(args[1].Str())
			interval = strings.TrimSpace(args[2].Str())
			field = strings.TrimSpace(args[3].Str())
		} else {
			interval = strings.TrimSpace(args[1].Str())
			field = strings.TrimSpace(args[2].Str())
		}
		key := requestFactorKey(name, interval)
		if symbol != "" {
			key = analysis.RequestSymbolFactorKey(name, symbol, interval)
		}
		ref, ok := ds.facRefs[key]
		if !ok || ds.ip == nil || ds.ip.Bridge == nil {
			if !ok {
				ds.addMissingRequestDiagnostic("request.factor", key, -1)
			}
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
			if !ok {
				ds.addMissingRequestDiagnostic("request.fundamental", key, -1)
			}
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

func (ds *DslStrategy) addMissingRequestDiagnostic(function, key string, barIndex int) {
	if ds == nil || key == "" {
		return
	}
	diagnosticKey := function + ":" + key
	if _, seen := ds.missingRequestKeys[diagnosticKey]; seen {
		return
	}
	ds.missingRequestKeys[diagnosticKey] = struct{}{}
	var barIndexPtr *int
	if barIndex >= 0 {
		barIndexPtr = &barIndex
	}
	ds.runtimeDiagnostics.Add(diagnostics.Diagnostic{
		Severity: diagnostics.SeverityError,
		Code:     "dsl.request_not_preloaded",
		Function: function,
		Message:  fmt.Sprintf("request dependency %q was not preloaded", key),
		BarIndex: barIndexPtr,
		Hint:     "Use static request arguments or provide a preloaded universe/runtime request provider.",
	})
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
	return analysis.RequestSecurityKey(market, symbol, interval)
}

func requestFactorKey(name, interval string) string {
	return analysis.RequestFactorKey(name, interval)
}

func remoteFactorKey(securityKey string, spec requestSpec) string {
	return strings.Join([]string{
		securityKey,
		strings.TrimSpace(strings.ToLower(spec.Market)),
		strings.TrimSpace(strings.ToUpper(spec.Symbol)),
		strings.TrimSpace(strings.ToLower(spec.Name)),
		strings.TrimSpace(strings.ToLower(spec.Interval)),
		strings.TrimSpace(strings.ToLower(spec.Mode)),
	}, "|")
}

func collectAliasExprs(prog *ast.Program) map[string]ast.Expr {
	out := make(map[string]ast.Expr)
	if prog == nil {
		return out
	}
	for _, stmt := range prog.Stmts {
		decl, ok := stmt.(*ast.VarDecl)
		if !ok || decl.Persist || decl.Varip || strings.TrimSpace(decl.Name) == "" || decl.Value == nil {
			continue
		}
		out[decl.Name] = decl.Value
	}
	return out
}

func (ds *DslStrategy) resolveRemoteExpr(expr ast.Expr) ast.Expr {
	return ds.expandRemoteExpr(expr, make(map[string]bool))
}

func (ds *DslStrategy) expandRemoteExpr(expr ast.Expr, stack map[string]bool) ast.Expr {
	if expr == nil || ds == nil || len(ds.aliasExprs) == 0 {
		return expr
	}
	switch node := expr.(type) {
	case *ast.IdentExpr:
		name := strings.TrimSpace(node.Name)
		if name == "" || stack[name] {
			return expr
		}
		alias, ok := ds.aliasExprs[name]
		if !ok {
			return expr
		}
		stack[name] = true
		resolved := ds.expandRemoteExpr(alias, stack)
		delete(stack, name)
		return resolved
	case *ast.BinaryExpr:
		copy := *node
		copy.Left = ds.expandRemoteExpr(node.Left, stack)
		copy.Right = ds.expandRemoteExpr(node.Right, stack)
		return &copy
	case *ast.UnaryExpr:
		copy := *node
		copy.Operand = ds.expandRemoteExpr(node.Operand, stack)
		return &copy
	case *ast.CallExpr:
		copy := *node
		copy.Callee = ds.expandRemoteExpr(node.Callee, stack)
		copy.Args = make([]ast.CallArg, len(node.Args))
		for i, arg := range node.Args {
			copy.Args[i] = ast.CallArg{Name: arg.Name, Value: ds.expandRemoteExpr(arg.Value, stack)}
		}
		return &copy
	case *ast.DotExpr:
		copy := *node
		copy.Object = ds.expandRemoteExpr(node.Object, stack)
		return &copy
	case *ast.IndexExpr:
		copy := *node
		copy.Left = ds.expandRemoteExpr(node.Left, stack)
		copy.Index = ds.expandRemoteExpr(node.Index, stack)
		return &copy
	case *ast.TernaryExpr:
		copy := *node
		copy.Condition = ds.expandRemoteExpr(node.Condition, stack)
		copy.Then = ds.expandRemoteExpr(node.Then, stack)
		copy.Else = ds.expandRemoteExpr(node.Else, stack)
		return &copy
	case *ast.ArrayLit:
		copy := *node
		copy.Elements = make([]ast.Expr, len(node.Elements))
		for i, element := range node.Elements {
			copy.Elements[i] = ds.expandRemoteExpr(element, stack)
		}
		return &copy
	case *ast.LambdaExpr:
		copy := *node
		copy.Body = ds.expandRemoteExpr(node.Body, stack)
		return &copy
	default:
		return expr
	}
}

func collectRemoteFactorRequests(expr ast.Expr) []requestSpec {
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
			} else if isRequestFundamentalCall(n) {
				market := positionalStringArg(n, "market", 0)
				symbol := positionalStringArg(n, "symbol", 1)
				factor := positionalStringArg(n, "factor", 2)
				mode := positionalStringArg(n, "mode", 3)
				if mode == "" {
					mode = "filled"
				}
				if factor != "" {
					out = append(out, requestSpec{Kind: "fundamental", Market: market, Symbol: symbol, Name: factor, Interval: "primary", Mode: mode, Field: "value"})
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
