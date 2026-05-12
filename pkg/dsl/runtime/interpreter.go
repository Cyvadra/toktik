package runtime

import (
	"fmt"
	"math"
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
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

	// Inputs: user-supplied parameter overrides keyed by input title.
	// When an input(defval, title=T) call is evaluated, Inputs[T] takes priority.
	Inputs map[string]float64

	// InputStrings: string parameter overrides keyed by input title.
	// Used by input.string() when the override is not numeric.
	InputStrings map[string]string

	// Bridge for strategy/trading calls (set externally).
	Bridge Bridge

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

// NewInterpreter creates a new interpreter for the given program.
func NewInterpreter(prog *ast.Program) *Interpreter {
	ip := &Interpreter{
		Program:      prog,
		Global:       NewScope(),
		persist:      make(map[string]Value),
		varip:        make(map[string]Value),
		seriesMap:    make(map[string]*Series),
		builtins:     make(map[string]Value),
		propertyFns:  make(map[string]func() Value),
		queuedFields: make(map[string]float64),
	}
	RegisterCoreBuiltins(ip)
	return ip
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
	// Update built-in series from bridge.
	if ip.Bridge != nil {
		ip.setBarField("close", ip.Bridge.Close())
		ip.setBarField("open", ip.Bridge.Open())
		ip.setBarField("high", ip.Bridge.High())
		ip.setBarField("low", ip.Bridge.Low())
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

// ---------- statement execution ----------

func (ip *Interpreter) execBlock(stmts []ast.Stmt, scope *Scope) Value {
	var last Value
	for _, stmt := range stmts {
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
		// Strategy metadata — nothing to execute at bar-time.
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
			scope.Set(d.Name, val)
			return val
		}
		if _, ok := ip.varip[d.Name]; ok {
			val := ip.evalExpr(d.Value, scope)
			ip.varip[d.Name] = val
			scope.Set(d.Name, val)
			return val
		}
	}
	if d.Persist {
		// var: evaluate once, persist across bars.
		if v, ok := ip.persist[d.Name]; ok {
			scope.Set(d.Name, v)
			return v
		}
		val := ip.evalExpr(d.Value, scope)
		ip.persist[d.Name] = val
		scope.Set(d.Name, val)
		return val
	}
	if d.Varip {
		// varip: persist and update in-place every bar.
		if v, ok := ip.varip[d.Name]; ok {
			scope.Set(d.Name, v)
			return v
		}
		val := ip.evalExpr(d.Value, scope)
		ip.varip[d.Name] = val
		scope.Set(d.Name, val)
		return val
	}
	// Normal: re-evaluate each bar.
	val := ip.evalExpr(d.Value, scope)
	scope.Set(d.Name, val)

	// Track in series map for scalar/bool/series types so history subscript
	// and TA builtins (ta.sma, ta.barssince, etc.) work correctly.
	// Array/object values are NOT series-promoted — keep them as-is.
	if val.tag == TagFloat || val.tag == TagBool || val.tag == TagNa || val.tag == TagSeries {
		s, ok := ip.seriesMap[d.Name]
		if !ok {
			s = NewSeries()
			ip.seriesMap[d.Name] = s
		}
		s.Append(val.Float())
		// Update scope to SeriesVal so history subscript and TA builtins work.
		scope.Set(d.Name, SeriesVal(s))
	}

	return val
}

func (ip *Interpreter) execAssign(a *ast.AssignStmt, scope *Scope) Value {
	val := ip.evalExpr(a.Value, scope)
	switch a.Op {
	case token.Eq:
		if !scope.Update(a.Name, val) {
			scope.Set(a.Name, val)
		}
	case token.ColonEq:
		if !scope.Update(a.Name, val) {
			scope.Set(a.Name, val)
		}
	case token.PlusEq:
		old, _ := scope.Get(a.Name)
		val = FloatVal(old.Float() + val.Float())
		scope.Update(a.Name, val)
	case token.MinusEq:
		old, _ := scope.Get(a.Name)
		val = FloatVal(old.Float() - val.Float())
		scope.Update(a.Name, val)
	case token.StarEq:
		old, _ := scope.Get(a.Name)
		val = FloatVal(old.Float() * val.Float())
		scope.Update(a.Name, val)
	case token.SlashEq:
		old, _ := scope.Get(a.Name)
		val = FloatVal(old.Float() / val.Float())
		scope.Update(a.Name, val)
	case token.PercentEq:
		old, _ := scope.Get(a.Name)
		val = FloatVal(math.Mod(old.Float(), val.Float()))
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
	// Update series.
	if s, ok := ip.seriesMap[a.Name]; ok {
		s.Set(val.Float())
	}
	return val
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
	limit := 100_000
	for i := 0; i < limit; i++ {
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

func snapshotContainerValue(v Value) Value {
	switch v.tag {
	case TagSeries:
		return FloatVal(v.Float())
	case TagArray:
		if v.obj != nil {
			return v
		}
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
	case token.And:
		return BoolVal(left.Bool() && right.Bool())
	case token.Or:
		return BoolVal(left.Bool() || right.Bool())
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
	callee := ip.evalExpr(e.Callee, scope)
	if callee.tag != TagFn || callee.fn == nil {
		return NaVal()
	}
	fn := callee.fn

	args := make([]Value, len(e.Args))
	for i, a := range e.Args {
		args[i] = ip.evalExpr(a.Value, scope)
	}

	// Handle named args by mapping them to positional slots.
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
		for _, a := range e.Args {
			val := ip.evalExpr(a.Value, scope)
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
	}
	// For series: field lookup (not implemented beyond above convention).
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
	}
	return NaVal()
}

// valEqual compares two Values for equality.
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
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
