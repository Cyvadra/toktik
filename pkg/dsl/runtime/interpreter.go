package runtime

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
	"github.com/Cyvadra/toktik/pkg/dsl/token"
)

type PlotSpec struct {
	Source    string
	Title     string
	Decimals  int
	Overlay   bool
	series    *Series
	lastValue Value
}

// signal types returned by control flow statements.
type signal int

const (
	sigNone signal = iota
	sigReturn
	sigBreak
	sigContinue
)

const defaultExecutionBudget = 100_000

// Interpreter walks the AST and produces side-effects via a Bridge.
type Interpreter struct {
	Program *ast.Program
	Global  *Scope

	// Bar state.
	BarIndex int

	// Persistent var/varip storage (survives across bars).
	persist map[string]Value // var
	varip   map[string]Value // varip

	// Series tracking: every var gets a series so history works.
	seriesMap map[string]*Series

	// Builtins registered at init time.
	builtins map[string]Value
	// propertyFns holds zero-arg native functions that are auto-invoked when
	// accessed as dot-properties (e.g. strategy.position_size, strategy.equity).
	propertyFns  map[string]func() Value
	plots        []*PlotSpec
	queuedFields map[string]float64
	traceKeys    map[string]struct{}

	// unresolvedIdents dedupes "unknown identifier" diagnostics so a typo
	// referenced on every bar only reports once instead of flooding the
	// diagnostics list.
	unresolvedIdents map[string]struct{}

	// builtinFailures dedupes "builtin call failed" diagnostics (missing
	// bridge capability, unusable arguments, etc.) per function+reason so a
	// failing call on every bar only reports once instead of flooding the
	// diagnostics list.
	builtinFailures map[string]struct{}
	runtimeWarnings map[string]struct{}

	// ExecutionBudget bounds total interpreter work for a single bar. It is
	// shared by statements, loop iterations, and user-defined function calls.
	// Values <= 0 use defaultExecutionBudget.
	ExecutionBudget int
	executionLeft   int
	executionHalted bool

	// Inputs: user-supplied parameter overrides keyed by input title.
	// When an input(defval, title=T) call is evaluated, Inputs[T] takes priority.
	Inputs map[string]float64

	// InputStrings: string parameter overrides keyed by input title.
	// Used by input.string() when the override is not numeric.
	InputStrings map[string]string

	// Bridge for strategy/trading calls (set externally).
	Bridge        Bridge
	Diagnostics   diagnostics.Collector
	readOnlyDepth int

	// last signal and return value
	sig    signal
	retVal Value
}

// Bridge is the interface the interpreter uses to talk to the backtest engine.
type Bridge interface {
	// OnBar fields.
	BarIndex() int
	Close() float64
	Open() float64
	High() float64
	Low() float64
	Volume() float64
	Field(name string) float64
	FieldAt(name string, offset int) float64

	// Orders.
	Buy(qty float64)
	Sell(qty float64)
	EntryLong(id string, qty float64)
	EntryShort(id string, qty float64)
	ExitLong(id string)
	ExitShort(id string)

	// Position.
	PositionSize() float64
	PositionAvgPrice() float64

	// Account.
	Equity() float64
	Cash() float64

	// Indicators.
	Ind(name string) float64
	IndAt(name string, offset int) float64
}

// SpecialFormBridge can evaluate selected calls before the interpreter eagerly
// evaluates all arguments. This is used for context-switching calls such as
// request.security(..., expr), where expr must remain an AST template.
type SpecialFormBridge interface {
	EvalSpecialForm(ip *Interpreter, call *ast.CallExpr, scope *Scope) (Value, bool)
}

// NewInterpreter creates a new interpreter for the given program.
func NewInterpreter(prog *ast.Program) *Interpreter {
	ip := &Interpreter{
		Program:          prog,
		Global:           NewScope(),
		persist:          make(map[string]Value),
		varip:            make(map[string]Value),
		seriesMap:        make(map[string]*Series),
		builtins:         make(map[string]Value),
		propertyFns:      make(map[string]func() Value),
		queuedFields:     make(map[string]float64),
		traceKeys:        make(map[string]struct{}),
		unresolvedIdents: make(map[string]struct{}),
		builtinFailures:  make(map[string]struct{}),
		runtimeWarnings:  make(map[string]struct{}),
	}
	RegisterCoreBuiltins(ip)
	return ip
}

// reportUnresolvedIdent records a diagnostic the first time an identifier
// name is referenced but not found in any scope. Unlike silently returning
// na, this surfaces typos and undeclared references (e.g. postion_size) so a
// broken condition does not masquerade as a valid backtest result.
func (ip *Interpreter) reportUnresolvedIdent(name string) {
	if ip.Diagnostics == nil {
		return
	}
	if _, seen := ip.unresolvedIdents[name]; seen {
		return
	}
	ip.unresolvedIdents[name] = struct{}{}
	barIndex := ip.BarIndex
	ip.Diagnostics.Add(diagnostics.Diagnostic{
		Severity: diagnostics.SeverityError,
		Code:     "dsl.unresolved_identifier",
		Message:  fmt.Sprintf("identifier %q is not defined", name),
		BarIndex: &barIndex,
		Hint:     "Check for typos in variable or builtin names; undeclared identifiers evaluate to na and silently break conditions.",
	})
}

// ReportBuiltinFailure records a diagnostic the first time a side-effecting
// builtin (spread.open, group.open, options.open_strategy, etc.) is invoked
// but cannot complete — a missing bridge capability, unusable legs/qty, or a
// scope mismatch. Builtins that fail this way should also return an explicit
// sentinel (e.g. -1 for a spread/group id) rather than na, so a failed
// creation doesn't silently propagate as an unresolvable id, or as undefined
// behavior when a caller casts na to int.
func (ip *Interpreter) ReportBuiltinFailure(function, reason string) {
	if ip.Diagnostics == nil {
		return
	}
	key := function + ":" + reason
	if _, seen := ip.builtinFailures[key]; seen {
		return
	}
	ip.builtinFailures[key] = struct{}{}
	barIndex := ip.BarIndex
	ip.Diagnostics.Add(diagnostics.Diagnostic{
		Severity: diagnostics.SeverityError,
		Code:     "dsl.builtin_call_failed",
		Function: function,
		Message:  fmt.Sprintf("%s failed: %s", function, reason),
		BarIndex: &barIndex,
		Hint:     "Check that the bridge/backtest configuration supports this call and that arguments (legs, qty, scope) are valid.",
	})
}

// RegisterBuiltin adds a native function.
func (ip *Interpreter) RegisterBuiltin(name string, fn func(args []Value) Value) {
	ip.RegisterBuiltinWithParams(name, nil, fn)
}

// RegisterBuiltinWithParams adds a native function with named-argument metadata.
func (ip *Interpreter) RegisterBuiltinWithParams(name string, params []string, fn func(args []Value) Value) {
	ip.builtins[name] = FnVal(&Fn{Name: name, Params: params, Native: fn})
}

// RegisterNamespace adds a namespace object (e.g. strategy.*, ta.*).
func (ip *Interpreter) RegisterNamespace(name string, v Value) {
	ip.builtins[name] = v
}

// RegisterProperty registers a zero-arg function as an auto-invoked property.
// When the DSL accesses `namespace.field` (e.g. strategy.position_size) via dot
// notation without calling it as a function, this fn is called instead.
func (ip *Interpreter) RegisterProperty(name string, fn func() Value) {
	ip.propertyFns[name] = fn
}

// Init runs the top-level strategy/input declarations (called once before bars).
func (ip *Interpreter) Init() {
	// Bind builtins into global scope.
	for k, v := range ip.builtins {
		ip.Global.Set(k, v)
	}
	// Bind built-in constants.
	ip.Global.Set("math_pi", FloatVal(math.Pi))
	ip.Global.Set("math_e", FloatVal(math.E))
	ip.Global.Set("math_phi", FloatVal(math.Phi))
}

// OnBar executes the program for a single bar.
func (ip *Interpreter) OnBar() {
	ip.BarIndex++
	ip.executionLeft = ip.executionBudget()
	ip.executionHalted = false
	// Update built-in series from bridge.
	if ip.Bridge != nil {
		closePrice := ip.Bridge.Close()
		openPrice := ip.Bridge.Open()
		highPrice := ip.Bridge.High()
		lowPrice := ip.Bridge.Low()
		ip.setBarField("close", closePrice)
		ip.setBarField("open", openPrice)
		ip.setBarField("high", highPrice)
		ip.setBarField("low", lowPrice)
		ip.setBarField("hl2", (highPrice+lowPrice)/2)
		ip.setBarField("hlc3", (highPrice+lowPrice+closePrice)/3)
		ip.setBarField("ohlc4", (openPrice+highPrice+lowPrice+closePrice)/4)
		ip.setBarField("volume", ip.Bridge.Volume())
		ip.Global.Set("bar_index", FloatVal(float64(ip.Bridge.BarIndex())))
	}
	for name, value := range ip.queuedFields {
		ip.setBarField(name, value)
		delete(ip.queuedFields, name)
	}
	for _, plot := range ip.plots {
		plot.series.Append(math.NaN())
		plot.lastValue = NaVal()
	}

	ip.execBlock(ip.Program.Stmts, ip.Global)
}

func (ip *Interpreter) registerPlot(title string, decimals int, overlay bool) *PlotSpec {
	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		trimmedTitle = fmt.Sprintf("plot_%d", len(ip.plots)+1)
	}
	for _, plot := range ip.plots {
		if plot.Title == trimmedTitle {
			plot.Decimals = decimals
			plot.Overlay = overlay
			return plot
		}
	}
	series := NewSeries()
	for i := 0; i < ip.BarIndex; i++ {
		series.Append(math.NaN())
	}
	plot := &PlotSpec{
		Source:   fmt.Sprintf("plot.%d", len(ip.plots)+1),
		Title:    trimmedTitle,
		Decimals: decimals,
		Overlay:  overlay,
		series:   series,
	}
	ip.plots = append(ip.plots, plot)
	return plot
}

func (ip *Interpreter) setPlotValue(title string, value Value, decimals int, overlay bool) Value {
	plot := ip.registerPlot(title, decimals, overlay)
	plot.lastValue = value
	plot.series.Set(value.Float())
	return value
}

func (ip *Interpreter) PlotColumns() []PlotSpec {
	if len(ip.plots) == 0 {
		return nil
	}
	out := make([]PlotSpec, len(ip.plots))
	for i, plot := range ip.plots {
		out[i] = PlotSpec{
			Source:   plot.Source,
			Title:    plot.Title,
			Decimals: plot.Decimals,
			Overlay:  plot.Overlay,
		}
	}
	return out
}

func (ip *Interpreter) PlotSeries() map[string][]float64 {
	if len(ip.plots) == 0 {
		return nil
	}
	out := make(map[string][]float64, len(ip.plots))
	for _, plot := range ip.plots {
		data := plot.series.Data()
		dup := make([]float64, len(data))
		copy(dup, data)
		out[plot.Source] = dup
	}
	return out
}

func (ip *Interpreter) setBarField(name string, val float64) {
	s, ok := ip.seriesMap[name]
	if !ok {
		s = NewSeries()
		ip.seriesMap[name] = s
	}
	s.Append(val)
	ip.Global.Set(name, SeriesVal(s))
}

func (ip *Interpreter) SetNamedField(name string, val float64) {
	if strings.TrimSpace(name) == "" {
		return
	}
	ip.queuedFields[name] = val
}

func (ip *Interpreter) CaptureSeries(name string, val float64) Value {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return FloatVal(val)
	}
	s, ok := ip.seriesMap[trimmed]
	if !ok {
		s = NewSeries()
		for i := 0; i < ip.BarIndex-1; i++ {
			s.Append(math.NaN())
		}
		ip.seriesMap[trimmed] = s
	}
	if s.Len() < ip.BarIndex {
		s.Append(val)
	} else {
		s.Set(val)
	}
	v := SeriesVal(s)
	ip.Global.Set(trimmed, v)
	return v
}

func (ip *Interpreter) executionBudget() int {
	if ip.ExecutionBudget > 0 {
		return ip.ExecutionBudget
	}
	return defaultExecutionBudget
}

func (ip *Interpreter) consumeExecution() bool {
	if ip.executionHalted {
		return false
	}
	ip.executionLeft--
	if ip.executionLeft >= 0 {
		return true
	}
	ip.executionHalted = true
	if ip.Diagnostics != nil {
		barIndex := ip.BarIndex
		ip.Diagnostics.Add(diagnostics.Diagnostic{
			Severity: diagnostics.SeverityError,
			Code:     "dsl.execution_budget_exceeded",
			Message:  "DSL execution budget exceeded for bar",
			BarIndex: &barIndex,
			Hint:     "Reduce loop work, simplify nested function calls, or raise the execution budget for this trusted strategy.",
		})
	}
	return false
}

// ---------- statement execution ----------

func (ip *Interpreter) execBlock(stmts []ast.Stmt, scope *Scope) Value {
	var last Value
	for _, stmt := range stmts {
		if !ip.consumeExecution() {
			return last
		}
		last = ip.execStmt(stmt, scope)
		if ip.sig != sigNone {
			return last
		}
	}
	return last
}

func (ip *Interpreter) execStmt(stmt ast.Stmt, scope *Scope) Value {
	switch s := stmt.(type) {
	case *ast.StrategyDecl:
		// strategy()/indicator()/library() metadata — nothing to execute at bar-time.
		return NaVal()
	case *ast.VarDecl:
		return ip.execVarDecl(s, scope)
	case *ast.AssignStmt:
		return ip.execAssign(s, scope)
	case *ast.IndexAssignStmt:
		return ip.execIndexAssign(s, scope)
	case *ast.TupleAssign:
		return ip.execTupleAssign(s, scope)
	case *ast.ExprStmt:
		return ip.evalExpr(s.Expression, scope)
	case *ast.IfStmt:
		return ip.execIf(s, scope)
	case *ast.ForStmt:
		return ip.execFor(s, scope)
	case *ast.ForInStmt:
		return ip.execForIn(s, scope)
	case *ast.WhileStmt:
		return ip.execWhile(s, scope)
	case *ast.SwitchStmt:
		return ip.execSwitch(s, scope)
	case *ast.FnDecl:
		return ip.execFnDecl(s, scope)
	case *ast.ReturnStmt:
		if s.Value != nil {
			ip.retVal = ip.evalExpr(s.Value, scope)
		} else {
			ip.retVal = NaVal()
		}
		ip.sig = sigReturn
		return ip.retVal
	case *ast.BreakStmt:
		ip.sig = sigBreak
		return NaVal()
	case *ast.ContinueStmt:
		ip.sig = sigContinue
		return NaVal()
	case *ast.Block:
		child := scope.Child()
		return ip.execBlock(s.Stmts, child)
	default:
		return NaVal()
	}
}

func (ip *Interpreter) execVarDecl(d *ast.VarDecl, scope *Scope) Value {
	if !d.Persist && !d.Varip {
		if _, ok := ip.persist[d.Name]; ok {
			val := ip.evalExpr(d.Value, scope)
			ip.persist[d.Name] = val
			return ip.promoteScalarSeries(d.Name, val, scope)
		}
		if _, ok := ip.varip[d.Name]; ok {
			val := ip.evalExpr(d.Value, scope)
			ip.varip[d.Name] = val
			return ip.promoteScalarSeries(d.Name, val, scope)
		}
	}
	if d.Persist {
		// var: evaluate once, persist across bars.
		if v, ok := ip.persist[d.Name]; ok {
			return ip.promoteScalarSeries(d.Name, v, scope)
		}
		val := ip.evalExpr(d.Value, scope)
		ip.persist[d.Name] = val
		return ip.promoteScalarSeries(d.Name, val, scope)
	}
	if d.Varip {
		// varip: persist and update in-place every bar.
		if v, ok := ip.varip[d.Name]; ok {
			return ip.promoteScalarSeries(d.Name, v, scope)
		}
		val := ip.evalExpr(d.Value, scope)
		ip.varip[d.Name] = val
		return ip.promoteScalarSeries(d.Name, val, scope)
	}
	// Normal: re-evaluate each bar.
	val := ip.evalExpr(d.Value, scope)
	return ip.promoteScalarSeries(d.Name, val, scope)
}

// promoteScalarSeries binds name=val in scope and, for scalar/bool/na/series
// values, keeps a bar-index-aligned Series backing the name so history
// subscript (x[n]) and TA builtins (ta.sma, ta.barssince, ...) work — including
// for var/varip accumulators. Array/object values are left as-is (not
// series-promoted). It returns the value bound into scope.
func (ip *Interpreter) promoteScalarSeries(name string, val Value, scope *Scope) Value {
	if val.tag != TagFloat && val.tag != TagBool && val.tag != TagNa && val.tag != TagSeries {
		scope.Set(name, val)
		return val
	}
	s, ok := ip.seriesMap[name]
	if !ok {
		s = NewSeries()
		ip.seriesMap[name] = s
	}
	// This declaration may live inside an if/for/switch body and not run on
	// every bar. Pad any bars that were skipped since the last append with NaN
	// so the series stays bar-index aligned: s.At(n) and ta.* builtins must see
	// this bar's value at offset 0, not shifted by however many bars the branch
	// was skipped.
	for s.Len() < ip.BarIndex-1 {
		s.Append(math.NaN())
	}
	if s.Len() < ip.BarIndex {
		s.Append(val.Float())
	} else {
		s.Set(val.Float())
	}
	// Update scope to SeriesVal so history subscript and TA builtins work.
	sv := SeriesVal(s)
	scope.Set(name, sv)
	return sv
}

func (ip *Interpreter) execAssign(a *ast.AssignStmt, scope *Scope) Value {
	val := ip.evalExpr(a.Value, scope)
	switch a.Op {
	case token.Eq, token.ColonEq:
		// The DSL does not distinguish declare (`:=`) from reassign (`=`)
		// at the interpreter level; both simply set-or-update the target.
		if !scope.Update(a.Name, val) {
			scope.Set(a.Name, val)
		}
	case token.PlusEq:
		old, _ := scope.Get(a.Name)
		if old.tag == TagString || val.tag == TagString {
			// Match binary `+`'s string-concatenation semantics.
			val = StringVal(old.String() + val.String())
		} else {
			rhs := ip.compoundNumeric(a.Name, "+=", old, val)
			if rhs.tag == TagNa {
				val = NaVal()
			} else {
				val = FloatVal(old.Float() + rhs.Float())
			}
		}
		scope.Update(a.Name, val)
	case token.MinusEq:
		old, _ := scope.Get(a.Name)
		val = ip.compoundNumeric(a.Name, "-=", old, val)
		if val.tag != TagNa {
			val = FloatVal(old.Float() - val.Float())
		}
		scope.Update(a.Name, val)
	case token.StarEq:
		old, _ := scope.Get(a.Name)
		val = ip.compoundNumeric(a.Name, "*=", old, val)
		if val.tag != TagNa {
			val = FloatVal(old.Float() * val.Float())
		}
		scope.Update(a.Name, val)
	case token.SlashEq:
		old, _ := scope.Get(a.Name)
		rhs := ip.compoundNumeric(a.Name, "/=", old, val)
		if rhs.tag == TagNa || rhs.Float() == 0 {
			val = NaVal()
		} else {
			val = FloatVal(old.Float() / rhs.Float())
		}
		scope.Update(a.Name, val)
	case token.PercentEq:
		old, _ := scope.Get(a.Name)
		rhs := ip.compoundNumeric(a.Name, "%=", old, val)
		if rhs.tag == TagNa || rhs.Float() == 0 {
			val = NaVal()
		} else {
			val = FloatVal(math.Mod(old.Float(), rhs.Float()))
		}
		scope.Update(a.Name, val)
	case token.PlusPlus:
		old, _ := scope.Get(a.Name)
		if isNumericLike(old) {
			val = FloatVal(old.Float() + 1)
		} else {
			ip.reportCompoundTypeError(a.Name, "++")
			val = NaVal()
		}
		scope.Update(a.Name, val)
	case token.MinusMinus:
		old, _ := scope.Get(a.Name)
		if isNumericLike(old) {
			val = FloatVal(old.Float() - 1)
		} else {
			ip.reportCompoundTypeError(a.Name, "--")
			val = NaVal()
		}
		scope.Update(a.Name, val)
	}
	// Update persist storage if applicable.
	if _, ok := ip.persist[a.Name]; ok {
		ip.persist[a.Name] = val
	}
	// Update varip storage if applicable.
	if _, ok := ip.varip[a.Name]; ok {
		ip.varip[a.Name] = val
	}
	// Update series. Only scalar values (float/bool/na/series) are
	// series-backed; a reassignment to a string/array/object leaves the
	// scope binding as that value and does not touch the series.
	if val.tag == TagFloat || val.tag == TagBool || val.tag == TagNa || val.tag == TagSeries {
		if s, ok := ip.seriesMap[a.Name]; ok {
			s.Set(val.Float())
			// Keep the scope binding series-backed after reassignment so
			// history subscript (x[n]) and TA builtins continue to resolve
			// against the live series rather than a detached scalar value.
			if !scope.Update(a.Name, SeriesVal(s)) {
				scope.Set(a.Name, SeriesVal(s))
			}
		}
	}
	return val
}

// isNumericLike reports whether a value can participate in arithmetic
// (float/bool/na/series) without silently coercing to NaN. Strings, arrays,
// functions, and objects are not numeric-like.
func isNumericLike(v Value) bool {
	switch v.tag {
	case TagFloat, TagBool, TagNa, TagSeries:
		return true
	}
	return false
}

// compoundNumeric validates that both operands of a numeric compound
// assignment (-=, *=, /=, %=) are numeric-like. If either is not (e.g. a
// string, array, or object), it reports a diagnostic once and returns na so
// the caller does not silently overwrite the target with a coerced NaN
// without explanation.
func (ip *Interpreter) compoundNumeric(name, op string, old, rhs Value) Value {
	if isNumericLike(old) && isNumericLike(rhs) {
		return rhs
	}
	ip.reportCompoundTypeError(name, op)
	return NaVal()
}

// reportCompoundTypeError records a diagnostic the first time a compound
// assignment operator is applied to a non-numeric operand.
func (ip *Interpreter) reportCompoundTypeError(name, op string) {
	if ip.Diagnostics == nil {
		return
	}
	key := "compound:" + name + ":" + op
	if _, seen := ip.builtinFailures[key]; seen {
		return
	}
	ip.builtinFailures[key] = struct{}{}
	barIndex := ip.BarIndex
	ip.Diagnostics.Add(diagnostics.Diagnostic{
		Severity: diagnostics.SeverityError,
		Code:     "dsl.compound_assign_type_error",
		Message:  fmt.Sprintf("%s%s applied to a non-numeric operand", name, op),
		BarIndex: &barIndex,
		Hint:     "Compound assignment operators (+=, -=, *=, /=, %=, ++, --) require numeric operands; += also allows string concatenation.",
	})
}

func (ip *Interpreter) execIndexAssign(s *ast.IndexAssignStmt, scope *Scope) Value {
	left := ip.evalExpr(s.Left, scope)
	idx := int(ip.evalExpr(s.Index, scope).Float())
	val := snapshotContainerValue(ip.evalExpr(s.Value, scope))

	if left.tag == TagArray && idx >= 0 && idx < len(left.array) {
		left.array[idx] = val
	}
	return val
}

func (ip *Interpreter) execTupleAssign(t *ast.TupleAssign, scope *Scope) Value {
	val := ip.evalExpr(t.Value, scope)
	arr := val.Array()
	for i, name := range t.Names {
		if i < len(arr) {
			scope.Set(name, arr[i])
		} else {
			scope.Set(name, NaVal())
		}
	}
	return val
}

func (ip *Interpreter) execIf(s *ast.IfStmt, scope *Scope) Value {
	// Pine Script semantics: variables assigned in if/else bodies are visible
	// in the enclosing scope, so we pass scope directly (no child scope).
	if ip.evalExpr(s.Condition, scope).Bool() {
		return ip.execBlock(s.Body.Stmts, scope)
	}
	for _, ei := range s.ElseIfs {
		if ip.evalExpr(ei.Condition, scope).Bool() {
			return ip.execBlock(ei.Body.Stmts, scope)
		}
	}
	if s.Else != nil {
		return ip.execBlock(s.Else.Stmts, scope)
	}
	return NaVal()
}

func (ip *Interpreter) execFor(s *ast.ForStmt, scope *Scope) Value {
	start := ip.evalExpr(s.Start, scope).Float()
	end := ip.evalExpr(s.End, scope).Float()
	step := 1.0
	if s.Step != nil {
		step = ip.evalExpr(s.Step, scope).Float()
	}
	if step == 0 {
		return NaVal()
	}

	var last Value
	for i := start; (step > 0 && i <= end) || (step < 0 && i >= end); i += step {
		if !ip.consumeExecution() {
			return last
		}
		if !scope.Update(s.Var, FloatVal(i)) {
			scope.Set(s.Var, FloatVal(i))
		}
		last = ip.execBlock(s.Body.Stmts, scope)
		if ip.sig == sigBreak {
			ip.sig = sigNone
			break
		}
		if ip.sig == sigContinue {
			ip.sig = sigNone
			continue
		}
		if ip.sig == sigReturn {
			return last
		}
	}
	return last
}

func (ip *Interpreter) execForIn(s *ast.ForInStmt, scope *Scope) Value {
	coll := ip.evalExpr(s.Collection, scope)
	arr := coll.Array()
	var last Value
	for _, elem := range arr {
		if !ip.consumeExecution() {
			return last
		}
		if !scope.Update(s.Var, elem) {
			scope.Set(s.Var, elem)
		}
		last = ip.execBlock(s.Body.Stmts, scope)
		if ip.sig == sigBreak {
			ip.sig = sigNone
			break
		}
		if ip.sig == sigContinue {
			ip.sig = sigNone
			continue
		}
		if ip.sig == sigReturn {
			return last
		}
	}
	return last
}

func (ip *Interpreter) execWhile(s *ast.WhileStmt, scope *Scope) Value {
	var last Value
	const limit = 100_000
	i := 0
	for ; i < limit; i++ {
		if !ip.consumeExecution() {
			return last
		}
		if !ip.evalExpr(s.Condition, scope).Bool() {
			break
		}
		last = ip.execBlock(s.Body.Stmts, scope)
		if ip.sig == sigBreak {
			ip.sig = sigNone
			break
		}
		if ip.sig == sigContinue {
			ip.sig = sigNone
			continue
		}
		if ip.sig == sigReturn {
			return last
		}
	}
	// Only warn when the loop was cut off by the cap rather than a natural
	// break/condition-false/return exit, and only once per site's bar.
	if i == limit && ip.Diagnostics != nil {
		barIndex := ip.BarIndex
		ip.Diagnostics.Add(diagnostics.Diagnostic{
			Severity: diagnostics.SeverityWarning,
			Code:     "dsl.while_iteration_cap",
			Message:  "while loop reached the interpreter iteration cap",
			BarIndex: &barIndex,
			Hint:     "Check loop conditions and prefer bounded for loops when possible.",
		})
	}
	return last
}

func (ip *Interpreter) execSwitch(s *ast.SwitchStmt, scope *Scope) Value {
	var tagVal Value
	if s.Tag != nil {
		tagVal = ip.evalExpr(s.Tag, scope)
	}
	for _, c := range s.Cases {
		caseVal := ip.evalExpr(c.Value, scope)
		match := false
		if s.Tag != nil {
			match = valEqual(tagVal, caseVal)
		} else {
			match = caseVal.Bool()
		}
		if match {
			return ip.execBlock(c.Body.Stmts, scope.Child())
		}
	}
	if s.Default != nil {
		return ip.execBlock(s.Default.Stmts, scope.Child())
	}
	return NaVal()
}

func (ip *Interpreter) execFnDecl(d *ast.FnDecl, scope *Scope) Value {
	params := make([]string, len(d.Params))
	for i, p := range d.Params {
		params[i] = p.Name
	}
	fn := &Fn{
		Name:    d.Name,
		Params:  params,
		Body:    d.Body, // stored as *ast.Block
		Closure: scope,
	}
	scope.Set(d.Name, FnVal(fn))
	return NaVal()
}

// ---------- expression evaluation ----------

func (ip *Interpreter) evalExpr(expr ast.Expr, scope *Scope) Value {
	switch e := expr.(type) {
	case *ast.NumberLit:
		return FloatVal(e.Value)
	case *ast.StringLit:
		return StringVal(e.Value)
	case *ast.BoolLit:
		return BoolVal(e.Value)
	case *ast.NaLit:
		return NaVal()
	case *ast.IdentExpr:
		v, ok := scope.Get(e.Name)
		if !ok {
			ip.reportUnresolvedIdent(e.Name)
			return NaVal()
		}
		return v
	case *ast.BinaryExpr:
		return ip.evalBinary(e, scope)
	case *ast.UnaryExpr:
		return ip.evalUnary(e, scope)
	case *ast.CallExpr:
		return ip.evalCall(e, scope)
	case *ast.DotExpr:
		return ip.evalDot(e, scope)
	case *ast.IndexExpr:
		return ip.evalIndex(e, scope)
	case *ast.TernaryExpr:
		if ip.evalExpr(e.Condition, scope).Bool() {
			return ip.evalExpr(e.Then, scope)
		}
		return ip.evalExpr(e.Else, scope)
	case *ast.ArrayLit:
		vals := make([]Value, len(e.Elements))
		for i, el := range e.Elements {
			vals[i] = snapshotContainerValue(ip.evalExpr(el, scope))
		}
		return ArrayVal(vals)
	case *ast.LambdaExpr:
		params := make([]string, len(e.Params))
		for i, p := range e.Params {
			params[i] = p.Name
		}
		return FnVal(&Fn{
			Name:    "<lambda>",
			Params:  params,
			Body:    e.Body, // single expression – handled in callFn
			Closure: scope,
		})
	default:
		return NaVal()
	}
}

// EvalExpression evaluates an AST expression in the provided scope. It is
// intended for bridge special forms that need to evaluate a deferred expression
// under a temporary context.
func (ip *Interpreter) EvalExpression(expr ast.Expr, scope *Scope) Value {
	return ip.evalExpr(expr, scope)
}

func (ip *Interpreter) EvalReadOnlyExpression(expr ast.Expr, scope *Scope) Value {
	ip.readOnlyDepth++
	defer func() { ip.readOnlyDepth-- }()
	return ip.evalExpr(expr, scope)
}

func (ip *Interpreter) AllowSideEffect(function string) bool {
	if ip == nil || ip.readOnlyDepth == 0 {
		return true
	}
	if ip.Diagnostics != nil {
		barIndex := ip.BarIndex
		ip.Diagnostics.Add(diagnostics.Diagnostic{
			Severity: diagnostics.SeverityError,
			Code:     "dsl.readonly_side_effect",
			Function: function,
			Message:  "side-effecting DSL calls are not allowed inside read-only expressions",
			BarIndex: &barIndex,
			Hint:     "Move trading, spread, group, or scheduling calls out of request.security expression arguments.",
		})
	}
	return false
}

func snapshotContainerValue(v Value) Value {
	switch v.tag {
	case TagSeries:
		return FloatVal(v.Float())
	case TagArray:
		cloned := make([]Value, len(v.array))
		for i := range v.array {
			cloned[i] = snapshotContainerValue(v.array[i])
		}
		return ArrayVal(cloned)
	default:
		return v
	}
}

func (ip *Interpreter) evalBinary(e *ast.BinaryExpr, scope *Scope) Value {
	left := ip.evalExpr(e.Left, scope)
	if e.Op == token.And {
		if !left.Bool() {
			return BoolVal(false)
		}
		return BoolVal(ip.evalExpr(e.Right, scope).Bool())
	}
	if e.Op == token.Or {
		if left.Bool() {
			return BoolVal(true)
		}
		return BoolVal(ip.evalExpr(e.Right, scope).Bool())
	}
	right := ip.evalExpr(e.Right, scope)

	// String concatenation.
	if e.Op == token.Plus && (left.tag == TagString || right.tag == TagString) {
		return StringVal(left.String() + right.String())
	}
	if e.Op == token.Plus && left.tag == TagArray && right.tag == TagArray {
		joined := make([]Value, 0, len(left.array)+len(right.array))
		joined = append(joined, left.array...)
		joined = append(joined, right.array...)
		return ArrayVal(joined)
	}

	lf, rf := left.Float(), right.Float()
	switch e.Op {
	case token.Plus:
		return FloatVal(lf + rf)
	case token.Minus:
		return FloatVal(lf - rf)
	case token.Star:
		return FloatVal(lf * rf)
	case token.Slash:
		if rf == 0 {
			return NaVal()
		}
		return FloatVal(lf / rf)
	case token.Percent:
		if rf == 0 {
			return NaVal()
		}
		return FloatVal(math.Mod(lf, rf))
	case token.EqEq:
		return BoolVal(valEqual(left, right))
	case token.BangEq:
		return BoolVal(!valEqual(left, right))
	case token.Lt:
		return BoolVal(lf < rf)
	case token.Gt:
		return BoolVal(lf > rf)
	case token.LtEq:
		return BoolVal(lf <= rf)
	case token.GtEq:
		return BoolVal(lf >= rf)
	}
	return NaVal()
}

func (ip *Interpreter) evalUnary(e *ast.UnaryExpr, scope *Scope) Value {
	val := ip.evalExpr(e.Operand, scope)
	switch e.Op {
	case token.Minus:
		return FloatVal(-val.Float())
	case token.Not:
		return BoolVal(!val.Bool())
	}
	return NaVal()
}

func (ip *Interpreter) evalCall(e *ast.CallExpr, scope *Scope) Value {
	if bridge, ok := ip.Bridge.(SpecialFormBridge); ok {
		if value, handled := bridge.EvalSpecialForm(ip, e, scope); handled {
			return value
		}
	}
	callee := ip.evalExpr(e.Callee, scope)
	if callee.tag != TagFn || callee.fn == nil {
		return NaVal()
	}
	fn := callee.fn

	args := make([]Value, len(e.Args))
	for i, a := range e.Args {
		args[i] = ip.evalExpr(a.Value, scope)
	}

	// Handle named args by mapping them to positional slots. Reuse the
	// already-evaluated `args` values instead of re-evaluating each
	// expression, so side-effecting arguments (ref.inc(), order.submit(),
	// etc.) fire exactly once regardless of named-arg usage.
	if len(fn.Params) > 0 && hasNamedArgs(e.Args) {
		mapped := make([]Value, len(fn.Params))
		for i := range mapped {
			mapped[i] = NaVal()
		}
		paramIdx := make(map[string]int)
		for i, p := range fn.Params {
			paramIdx[p] = i
		}
		posIdx := 0
		for i, a := range e.Args {
			val := args[i]
			if a.Name != "" {
				if idx, ok := paramIdx[a.Name]; ok {
					mapped[idx] = val
				}
			} else {
				if posIdx < len(mapped) {
					mapped[posIdx] = val
				}
				posIdx++
			}
		}
		args = mapped
	}

	return ip.callFn(fn, args)
}

func hasNamedArgs(args []ast.CallArg) bool {
	for _, a := range args {
		if a.Name != "" {
			return true
		}
	}
	return false
}

func (ip *Interpreter) callFn(fn *Fn, args []Value) Value {
	// Native function.
	if fn.Native != nil {
		return fn.Native(args)
	}
	if !ip.consumeExecution() {
		return NaVal()
	}
	// User-defined function.
	fnScope := fn.Closure.Child()
	for i, p := range fn.Params {
		if i < len(args) {
			fnScope.Set(p, args[i])
		} else {
			fnScope.Set(p, NaVal())
		}
	}

	// Body may be *ast.Block or ast.Expr (lambda).
	switch body := fn.Body.(type) {
	case *ast.Block:
		result := ip.execBlock(body.Stmts, fnScope)
		if ip.sig == sigReturn {
			ip.sig = sigNone
			return ip.retVal
		}
		return result
	case ast.Expr:
		return ip.evalExpr(body, fnScope)
	}
	return NaVal()
}

func (ip *Interpreter) evalDot(e *ast.DotExpr, scope *Scope) Value {
	// Always try namespace convention first: "namespace.field" or "namespace_field"
	if id, ok := e.Object.(*ast.IdentExpr); ok {
		composedName := id.Name + "." + e.Field
		// Check if this is a registered auto-invoked property.
		if fn, ok2 := ip.propertyFns[composedName]; ok2 {
			return fn()
		}
		if v, ok2 := scope.Get(composedName); ok2 {
			return v
		}
		composedName2 := id.Name + "_" + e.Field
		if v, ok2 := scope.Get(composedName2); ok2 {
			return v
		}
		ip.reportRuntimeWarning(
			"dsl.unknown_field",
			composedName,
			fmt.Sprintf("field %q is not defined", composedName),
			"Check for typos or unsupported nested field access; unresolved fields evaluate to na.",
		)
		return NaVal()
	}
	ip.reportRuntimeWarning(
		"dsl.unknown_field",
		e.Field,
		fmt.Sprintf("field %q cannot be resolved on this expression", e.Field),
		"Nested field access is not supported; unresolved fields evaluate to na.",
	)
	return NaVal()
}

func (ip *Interpreter) evalIndex(e *ast.IndexExpr, scope *Scope) Value {
	left := ip.evalExpr(e.Left, scope)
	idx := ip.evalExpr(e.Index, scope)

	switch left.tag {
	case TagSeries:
		if left.series != nil {
			offset := int(idx.Float())
			return FloatVal(left.series.At(offset))
		}
	case TagArray:
		i := int(idx.Float())
		if i >= 0 && i < len(left.array) {
			return left.array[i]
		}
		ip.reportRuntimeWarning(
			"dsl.index_out_of_range",
			fmt.Sprintf("%d:%d", i, len(left.array)),
			fmt.Sprintf("array index %d is out of range for length %d", i, len(left.array)),
			"Check array bounds before indexing; out-of-range access evaluates to na.",
		)
	}
	return NaVal()
}

func (ip *Interpreter) reportRuntimeWarning(code, key, message, hint string) {
	if ip.Diagnostics == nil {
		return
	}
	dedupeKey := code + ":" + key
	if _, seen := ip.runtimeWarnings[dedupeKey]; seen {
		return
	}
	ip.runtimeWarnings[dedupeKey] = struct{}{}
	barIndex := ip.BarIndex
	ip.Diagnostics.Add(diagnostics.Diagnostic{
		Severity: diagnostics.SeverityWarning,
		Code:     code,
		Message:  message,
		BarIndex: &barIndex,
		Hint:     hint,
	})
}

// valEqual compares two Values for equality. Arrays are compared
// element-wise; functions, objects, and expressions are compared by
// identity rather than falling back to an unstable Sprintf comparison of
// their (possibly pointer-containing) internal representation.
func valEqual(a, b Value) bool {
	if a.tag == TagNa || b.tag == TagNa {
		return a.tag == TagNa && b.tag == TagNa
	}
	if a.tag == TagSeries || b.tag == TagSeries {
		return a.Float() == b.Float()
	}
	switch a.tag {
	case TagFloat:
		return a.fval == b.Float()
	case TagBool:
		return a.bval == b.Bool()
	case TagString:
		return a.sval == b.Str()
	case TagArray:
		if b.tag != TagArray || len(a.array) != len(b.array) {
			return false
		}
		for i := range a.array {
			if !valEqual(a.array[i], b.array[i]) {
				return false
			}
		}
		return true
	case TagFn:
		return b.tag == TagFn && a.fn == b.fn
	case TagObject:
		return b.tag == TagObject && safeObjEqual(a.obj, b.obj)
	case TagExpr:
		return b.tag == TagExpr && a.expr == b.expr
	}
	return false
}

// safeObjEqual compares two opaque object payloads (e.g. strategyCandidate,
// OptionsChain, OptionContract) without risking a Go runtime panic. Plain `==`
// on an interface{} panics if its dynamic type is not comparable (e.g. a
// struct embedding a slice/map field, such as strategyCandidate's Payload
// Value which holds an `array []Value`). DSL scripts are untrusted input, so
// an `==`/`switch` over two such objects must never crash the backtest
// goroutine; non-comparable dynamic types are treated as never-equal instead.
func safeObjEqual(a, b interface{}) (eq bool) {
	if a == nil || b == nil {
		return a == b
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if !ta.Comparable() {
		return false
	}
	defer func() {
		if recover() != nil {
			eq = false
		}
	}()
	return a == b
}
