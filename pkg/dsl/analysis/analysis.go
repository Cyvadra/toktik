package analysis

import (
	"math"
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
)

type ParamType string

const (
	ParamFloat  ParamType = "float"
	ParamInt    ParamType = "int"
	ParamBool   ParamType = "bool"
	ParamString ParamType = "string"
)

type ParamSchema struct {
	Name    string
	Title   string
	Type    ParamType
	Default interface{}
	Min     *float64
	Max     *float64
	Step    *float64
	Options []string
}

func (p ParamSchema) LookupKey() string {
	if p.Title != "" {
		return p.Title
	}
	return p.Name
}

type SignalMetadata struct {
	SignalSource          string
	SignalName            string
	SignalTimeLayouts     []string
	SignalTimezone        string
	SignalTimestampCols   []string
	SignalTypeCols        []string
	SignalValueCols       []string
	SignalEntryMatchers   []string
	SignalTextHasIndex    bool
	SignalNameColumn      string
	SignalDirectionColumn string
	SignalActionColumn    string
	SignalRemarksColumn   string
	SignalQtyColumn       string
	ExposeFields          []string
	Requests              []RequestSpec
}

type RequestSpec struct {
	Kind           string
	Market         string
	Symbol         string
	Name           string
	Interval       string
	Mode           string
	Field          string
	Key            string
	Tier           RequestTier
	Dynamic        bool
	UniverseCode   string
	ExpressionMode bool
	Expression     ast.Expr
}

type RequestTier string

const (
	RequestTierStatic         RequestTier = "static"
	RequestTierSemiDynamic    RequestTier = "semi_dynamic"
	RequestTierUniverseExpand RequestTier = "universe_expanded"
	RequestTierRuntimeDynamic RequestTier = "runtime_dynamic"
)

func requestTier(dynamic bool) RequestTier {
	if dynamic {
		return RequestTierRuntimeDynamic
	}
	return RequestTierStatic
}

type ChainRequestSpec struct {
	Market string
	Symbol string
	Key    string
}

type UniverseRequestSpec struct {
	Code string
	Key  string
}

type Manifest struct {
	StrategyName      string
	Inputs            []ParamSchema
	Metadata          SignalMetadata
	Requests          []RequestSpec
	UsesOptions       bool
	UsesRegularOrders bool
	Diagnostics       diagnostics.List
}

func Analyze(prog *ast.Program) Manifest {
	return AnalyzeWithParams(prog, nil)
}

// AnalyzeWithParams extracts a manifest after applying caller-provided string
// input overrides to dependency-driving expressions.
func AnalyzeWithParams(prog *ast.Program, params map[string]interface{}) Manifest {
	m := Manifest{
		StrategyName: ExtractStrategyName(prog),
		Inputs:       ExtractParams(prog),
	}
	if prog == nil {
		return m
	}
	stringOverrides := normalizeStringOverrides(params)
	stringBindings := collectStaticStringBindings(prog, stringOverrides)
	w := newWalkContext(prog, &m, stringBindings, stringOverrides)
	for _, stmt := range prog.Stmts {
		if sd, ok := stmt.(*ast.StrategyDecl); ok {
			extractStrategyMetadata(sd, &m.Metadata)
		}
		w.stmt(stmt, universeScope{})
	}
	m.Metadata.Requests = append([]RequestSpec(nil), m.Requests...)
	return m
}

// UniverseRequestTemplatesForPreload returns the requests whose symbol argument
// is a loop variable proven to originate from a literal universe.symbols()
// call. The service/bridge later expands these templates against the
// point-in-time universe membership before replay.
func (m Manifest) UniverseRequestTemplatesForPreload() []RequestSpec {
	var out []RequestSpec
	for _, req := range m.Requests {
		if req.Tier == RequestTierUniverseExpand {
			out = append(out, req)
		}
	}
	return out
}

// IsUniverseExpanded reports whether the request is a universe-bound template
// that the bridge expands into concrete preloaded dependencies before replay.
func (r RequestSpec) IsUniverseExpanded() bool {
	return r.Tier == RequestTierUniverseExpand
}

func (m Manifest) OptionChainRequests() []ChainRequestSpec {
	if len(m.Requests) == 0 {
		return nil
	}
	out := make([]ChainRequestSpec, 0, len(m.Requests))
	seen := make(map[string]struct{}, len(m.Requests))
	for _, req := range m.Requests {
		if req.Kind != "option_chain" || req.Dynamic || req.Key == "" {
			continue
		}
		if _, ok := seen[req.Key]; ok {
			continue
		}
		seen[req.Key] = struct{}{}
		out = append(out, ChainRequestSpec{Market: req.Market, Symbol: req.Symbol, Key: req.Key})
	}
	return out
}

func (m Manifest) HasDynamicOptionChainRequest() bool {
	for _, req := range m.Requests {
		if req.Kind == "option_chain" && req.Dynamic {
			return true
		}
	}
	return false
}

func (m Manifest) UniverseRequests() []UniverseRequestSpec {
	if len(m.Requests) == 0 {
		return nil
	}
	out := make([]UniverseRequestSpec, 0, len(m.Requests))
	seen := make(map[string]struct{}, len(m.Requests))
	for _, req := range m.Requests {
		if req.Kind != "universe" || req.Dynamic || req.Key == "" {
			continue
		}
		if _, ok := seen[req.Key]; ok {
			continue
		}
		seen[req.Key] = struct{}{}
		out = append(out, UniverseRequestSpec{Code: req.Name, Key: req.Key})
	}
	return out
}

func (m Manifest) HasDynamicUniverseRequest() bool {
	for _, req := range m.Requests {
		if req.Kind == "universe" && req.Dynamic {
			return true
		}
	}
	return false
}

func ExtractStrategyName(prog *ast.Program) string {
	if prog == nil {
		return ""
	}
	for _, stmt := range prog.Stmts {
		if sd, ok := stmt.(*ast.StrategyDecl); ok {
			for _, arg := range sd.Args {
				if arg.Name == "" {
					if sl, ok := arg.Value.(*ast.StringLit); ok {
						return sl.Value
					}
				}
			}
		}
	}
	return ""
}

func ExtractParams(prog *ast.Program) []ParamSchema {
	if prog == nil {
		return nil
	}
	var params []ParamSchema
	for _, stmt := range prog.Stmts {
		switch node := stmt.(type) {
		case *ast.InputDecl:
			params = append(params, extractInputDecl(node))
		case *ast.VarDecl:
			call, ok := node.Value.(*ast.CallExpr)
			if !ok || !isInputCall(call.Callee) {
				continue
			}
			params = append(params, extractInputCall(node.Name, call))
		}
	}
	return params
}

func extractStrategyMetadata(sd *ast.StrategyDecl, meta *SignalMetadata) {
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

func collectStaticStringBindings(prog *ast.Program, overrides map[string]string) map[string]string {
	if prog == nil {
		return nil
	}
	mutable := collectAssignedNames(prog)
	out := make(map[string]string)
	for _, stmt := range prog.Stmts {
		decl, ok := stmt.(*ast.VarDecl)
		if !ok || decl.Persist || decl.Varip || strings.TrimSpace(decl.Name) == "" || mutable[decl.Name] {
			continue
		}
		if value := staticStringWithOverrides(decl.Value, out, overrides); value != "" {
			out[decl.Name] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func staticString(expr ast.Expr, bindings map[string]string) string {
	return staticStringWithOverrides(expr, bindings, nil)
}

func staticStringWithOverrides(expr ast.Expr, bindings, overrides map[string]string) string {
	switch node := expr.(type) {
	case *ast.StringLit:
		return strings.TrimSpace(node.Value)
	case *ast.IdentExpr:
		if bindings == nil {
			return ""
		}
		return strings.TrimSpace(bindings[node.Name])
	case *ast.CallExpr:
		name := qualifiedName(node.Callee)
		if name != "input.string" && name != "config.string" {
			return ""
		}
		if name == "input.string" {
			title := ""
			for _, arg := range node.Args {
				if arg.Name == "title" {
					title = staticStringWithOverrides(arg.Value, bindings, nil)
				}
			}
			if title == "" && len(node.Args) >= 2 && node.Args[1].Name == "" {
				title = staticStringWithOverrides(node.Args[1].Value, bindings, nil)
			}
			if value := overrides[strings.ToLower(strings.TrimSpace(title))]; value != "" {
				return value
			}
		}
		for _, arg := range node.Args {
			if name == "config.string" && arg.Name == "defval" {
				return staticStringWithOverrides(arg.Value, bindings, overrides)
			}
			if name == "input.string" && arg.Name == "defval" {
				return staticStringWithOverrides(arg.Value, bindings, overrides)
			}
		}
		if name == "config.string" && len(node.Args) >= 2 && node.Args[1].Name == "" {
			return staticStringWithOverrides(node.Args[1].Value, bindings, overrides)
		}
		if name == "input.string" && len(node.Args) >= 1 && node.Args[0].Name == "" {
			return staticStringWithOverrides(node.Args[0].Value, bindings, overrides)
		}
	}
	return ""
}

func normalizeStringOverrides(params map[string]interface{}) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, raw := range params {
		value, ok := raw.(string)
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func collectAssignedNames(prog *ast.Program) map[string]bool {
	out := make(map[string]bool)
	var walkStmt func(ast.Stmt)
	var walkBlock func(*ast.Block)
	walkBlock = func(block *ast.Block) {
		if block == nil {
			return
		}
		for _, stmt := range block.Stmts {
			walkStmt(stmt)
		}
	}
	walkStmt = func(stmt ast.Stmt) {
		switch node := stmt.(type) {
		case *ast.AssignStmt:
			out[node.Name] = true
		case *ast.IfStmt:
			walkBlock(node.Body)
			for _, branch := range node.ElseIfs {
				walkBlock(branch.Body)
			}
			walkBlock(node.Else)
		case *ast.ForStmt:
			walkBlock(node.Body)
		case *ast.ForInStmt:
			walkBlock(node.Body)
		case *ast.WhileStmt:
			walkBlock(node.Body)
		case *ast.SwitchStmt:
			for _, switchCase := range node.Cases {
				walkBlock(switchCase.Body)
			}
			walkBlock(node.Default)
		case *ast.FnDecl:
			walkBlock(node.Body)
		case *ast.Block:
			walkBlock(node)
		}
	}
	for _, stmt := range prog.Stmts {
		walkStmt(stmt)
	}
	return out
}

// walkContext threads analysis state (and the active universe loop scope)
// through a single AST traversal.
type walkContext struct {
	m               *Manifest
	bindings        map[string]string
	overrides       map[string]string
	collectionCodes map[string]string
}

// universeScope carries the active universe code and the loop variable bound to
// its membership while walking a `for symbol in universe.symbols(code)` body.
type universeScope struct {
	code      string
	symbolVar string
}

func newWalkContext(prog *ast.Program, m *Manifest, bindings, overrides map[string]string) *walkContext {
	codes := make(map[string]string)
	if prog != nil {
		mutable := collectAssignedNames(prog)
		for _, stmt := range prog.Stmts {
			decl, ok := stmt.(*ast.VarDecl)
			if !ok || mutable[decl.Name] {
				continue
			}
			if code := literalUniverseCode(decl.Value, bindings, overrides); code != "" {
				codes[decl.Name] = code
			}
		}
	}
	return &walkContext{m: m, bindings: bindings, overrides: overrides, collectionCodes: codes}
}

// childUniverseScope resolves the scope that applies inside a for-in loop body.
// Loops iterating a literal universe (directly or via a top-level variable)
// bind their loop variable to that universe; unrelated loops that reuse the
// outer loop variable name shadow and therefore clear the scope.
func (w *walkContext) childUniverseScope(node *ast.ForInStmt, scope universeScope) universeScope {
	code := literalUniverseCode(node.Collection, w.bindings, w.overrides)
	if code == "" {
		if ident, ok := node.Collection.(*ast.IdentExpr); ok {
			code = w.collectionCodes[ident.Name]
		}
	}
	if code != "" {
		return universeScope{code: code, symbolVar: node.Var}
	}
	if node.Var == scope.symbolVar {
		return universeScope{}
	}
	return scope
}

func literalUniverseCode(expr ast.Expr, stringBindings, overrides map[string]string) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok || qualifiedName(call.Callee) != "universe.symbols" {
		return ""
	}
	for _, arg := range call.Args {
		if arg.Name == "code" {
			return strings.ToLower(strings.TrimSpace(staticStringWithOverrides(arg.Value, stringBindings, overrides)))
		}
	}
	if len(call.Args) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(staticStringWithOverrides(call.Args[0].Value, stringBindings, overrides)))
}

// universeRequestTemplate recognizes request.security/request.fundamental calls
// whose symbol argument is the active universe loop variable and returns a
// preloadable template (symbol left blank; expanded per-member before replay).
func universeRequestTemplate(call *ast.CallExpr, scope universeScope, stringBindings, overrides map[string]string) (RequestSpec, bool) {
	if scope.code == "" || scope.symbolVar == "" {
		return RequestSpec{}, false
	}
	dot, ok := call.Callee.(*ast.DotExpr)
	if !ok {
		return RequestSpec{}, false
	}
	argExpr := func(name string, index int) ast.Expr {
		for _, arg := range call.Args {
			if arg.Name == name {
				return arg.Value
			}
		}
		if index < len(call.Args) && call.Args[index].Name == "" {
			return call.Args[index].Value
		}
		return nil
	}
	symbolMatchesLoop := func(expr ast.Expr) bool {
		ident, ok := expr.(*ast.IdentExpr)
		return ok && ident.Name == scope.symbolVar
	}
	switch qualifiedName(dot) {
	case "request.security":
		market := staticStringWithOverrides(argExpr("market", 0), stringBindings, overrides)
		interval := staticStringWithOverrides(argExpr("interval", 2), stringBindings, overrides)
		fieldExpr := argExpr("field", 3)
		if market == "" || interval == "" || fieldExpr == nil || !symbolMatchesLoop(argExpr("symbol", 1)) {
			return RequestSpec{}, false
		}
		field := literalString(fieldExpr)
		return RequestSpec{Kind: "security", Market: market, Interval: interval, Field: field, Tier: RequestTierUniverseExpand, UniverseCode: scope.code, ExpressionMode: field == "", Expression: fieldExpr}, true
	case "request.fundamental":
		market := staticStringWithOverrides(argExpr("market", 0), stringBindings, overrides)
		factor := staticStringWithOverrides(argExpr("factor", 2), stringBindings, overrides)
		mode := staticStringWithOverrides(argExpr("mode", 3), stringBindings, overrides)
		if market == "" || factor == "" || !symbolMatchesLoop(argExpr("symbol", 1)) {
			return RequestSpec{}, false
		}
		if mode == "" {
			mode = "filled"
		}
		return RequestSpec{Kind: "fundamental", Market: market, Name: factor, Interval: "primary", Mode: mode, Field: "value", Tier: RequestTierUniverseExpand, UniverseCode: scope.code}, true
	default:
		return RequestSpec{}, false
	}
}

func (w *walkContext) stmt(stmt ast.Stmt, scope universeScope) {
	if stmt == nil || w == nil || w.m == nil {
		return
	}
	switch node := stmt.(type) {
	case *ast.StrategyDecl:
		for _, arg := range node.Args {
			w.expr(arg.Value, scope)
		}
	case *ast.InputDecl:
		for _, arg := range node.Args {
			w.expr(arg.Value, scope)
		}
	case *ast.VarDecl:
		w.expr(node.Value, scope)
	case *ast.AssignStmt:
		w.expr(node.Value, scope)
	case *ast.IndexAssignStmt:
		w.expr(node.Left, scope)
		w.expr(node.Index, scope)
		w.expr(node.Value, scope)
	case *ast.TupleAssign:
		w.expr(node.Value, scope)
	case *ast.ExprStmt:
		w.expr(node.Expression, scope)
	case *ast.IfStmt:
		w.expr(node.Condition, scope)
		w.block(node.Body, scope)
		for _, branch := range node.ElseIfs {
			w.expr(branch.Condition, scope)
			w.block(branch.Body, scope)
		}
		w.block(node.Else, scope)
	case *ast.ForStmt:
		w.expr(node.Start, scope)
		w.expr(node.End, scope)
		w.expr(node.Step, scope)
		w.block(node.Body, scope)
	case *ast.ForInStmt:
		w.expr(node.Collection, scope)
		w.block(node.Body, w.childUniverseScope(node, scope))
	case *ast.WhileStmt:
		w.expr(node.Condition, scope)
		w.block(node.Body, scope)
	case *ast.SwitchStmt:
		w.expr(node.Tag, scope)
		for _, switchCase := range node.Cases {
			w.expr(switchCase.Value, scope)
			w.block(switchCase.Body, scope)
		}
		w.block(node.Default, scope)
	case *ast.FnDecl:
		for _, param := range node.Params {
			w.expr(param.Default, scope)
		}
		w.block(node.Body, scope)
	case *ast.ReturnStmt:
		w.expr(node.Value, scope)
	case *ast.Block:
		w.block(node, scope)
	}
}

func (w *walkContext) block(block *ast.Block, scope universeScope) {
	if block == nil {
		return
	}
	for _, stmt := range block.Stmts {
		w.stmt(stmt, scope)
	}
}

func (w *walkContext) expr(expr ast.Expr, scope universeScope) {
	if expr == nil || w == nil || w.m == nil {
		return
	}
	m := w.m
	switch node := expr.(type) {
	case *ast.BinaryExpr:
		w.expr(node.Left, scope)
		w.expr(node.Right, scope)
	case *ast.UnaryExpr:
		w.expr(node.Operand, scope)
	case *ast.CallExpr:
		name := qualifiedName(node.Callee)
		if isOptionsReference(name) {
			m.UsesOptions = true
		}
		if isRegularTradeCall(name) {
			m.UsesRegularOrders = true
		}
		if template, ok := universeRequestTemplate(node, scope, w.bindings, w.overrides); ok {
			m.Requests = append(m.Requests, template)
		} else if spec, ok := parseRequestSpec(node, w.bindings, w.overrides); ok {
			m.Requests = append(m.Requests, spec)
			if spec.Tier == RequestTierRuntimeDynamic && spec.Kind != "option_chain" {
				m.Diagnostics.Add(diagnostics.Diagnostic{
					Severity: diagnostics.SeverityWarning,
					Code:     "dsl.runtime_dynamic_request",
					Function: requestDiagnosticFunction(spec.Kind),
					Message:  "request uses runtime-dynamic arguments and will require a runtime request provider/cache instead of the static preload path",
					Hint:     "Prefer literals, input/config-resolvable values, or top-level constants when possible so requests can be preloaded before replay.",
				})
			}
			if spec.Dynamic && spec.Kind == "option_chain" {
				m.Diagnostics.Add(diagnostics.Diagnostic{
					Severity: diagnostics.SeverityWarning,
					Code:     "dsl.dynamic_option_chain",
					Function: "options.chain",
					Message:  "options.chain uses runtime-dynamic market or symbol arguments and must be backed by request-level symbols, portfolio scope, or a runtime chain provider/cache",
					Hint:     "Prefer literal or input/config-resolvable chain arguments so option chains can be preloaded deterministically.",
				})
			}
			if spec.Dynamic && spec.Kind == "universe" {
				m.Diagnostics.Add(diagnostics.Diagnostic{
					Severity: diagnostics.SeverityWarning,
					Code:     "dsl.dynamic_universe",
					Function: "universe.symbols",
					Message:  "universe.symbols uses a runtime-dynamic code and cannot be expanded during validate/run planning",
					Hint:     "Use a literal universe code so the backtest service can resolve membership and estimate resources before replay.",
				})
			}
		}
		w.expr(node.Callee, scope)
		for _, arg := range node.Args {
			w.expr(arg.Value, scope)
		}
	case *ast.DotExpr:
		name := qualifiedName(node)
		if isOptionsReference(name) {
			m.UsesOptions = true
		}
		w.expr(node.Object, scope)
	case *ast.IndexExpr:
		w.expr(node.Left, scope)
		w.expr(node.Index, scope)
	case *ast.TernaryExpr:
		w.expr(node.Condition, scope)
		w.expr(node.Then, scope)
		w.expr(node.Else, scope)
	case *ast.ArrayLit:
		for _, element := range node.Elements {
			w.expr(element, scope)
		}
	case *ast.LambdaExpr:
		w.expr(node.Body, scope)
	}
}

func parseRequestSpec(call *ast.CallExpr, stringBindings, overrides map[string]string) (RequestSpec, bool) {
	dot, ok := call.Callee.(*ast.DotExpr)
	if !ok {
		return RequestSpec{}, false
	}
	obj, ok := dot.Object.(*ast.IdentExpr)
	if !ok {
		return RequestSpec{}, false
	}
	argExpr := func(name string, idx int) ast.Expr {
		for _, arg := range call.Args {
			if arg.Name == name {
				return arg.Value
			}
		}
		if idx >= 0 && idx < len(call.Args) {
			arg := call.Args[idx]
			if arg.Name == "" {
				return arg.Value
			}
		}
		return nil
	}
	get := func(name string, idx int) (string, bool) {
		expr := argExpr(name, idx)
		if expr == nil {
			return "", true
		}
		value := staticStringWithOverrides(expr, stringBindings, overrides)
		return value, value == ""
	}
	switch dot.Field {
	case "security":
		if obj.Name != "request" {
			return RequestSpec{}, false
		}
		market, dynMarket := get("market", 0)
		symbol, dynSymbol := get("symbol", 1)
		interval, dynInterval := get("interval", 2)
		fieldExpr := argExpr("field", 3)
		field, dynField := "", true
		if fieldExpr != nil {
			field = literalString(fieldExpr)
			dynField = field == ""
		}
		if fieldExpr != nil && dynField && !(dynMarket || dynSymbol || dynInterval) {
			return RequestSpec{Kind: "security", Market: market, Symbol: symbol, Interval: interval, Key: RequestSecurityKey(market, symbol, interval), Tier: RequestTierStatic, ExpressionMode: true, Expression: fieldExpr}, true
		}
		dynamic := dynMarket || dynSymbol || dynInterval || dynField
		if dynamic {
			return RequestSpec{Kind: "security", Tier: requestTier(true), Dynamic: true}, true
		}
		return RequestSpec{Kind: "security", Market: market, Symbol: symbol, Interval: interval, Field: field, Key: RequestSecurityKey(market, symbol, interval), Tier: RequestTierStatic}, true
	case "chain":
		if obj.Name != "options" {
			return RequestSpec{}, false
		}
		market, dynMarket := get("market", 0)
		symbol, dynSymbol := get("symbol", 1)
		if len(call.Args) == 0 {
			return RequestSpec{}, true
		}
		if dynMarket || dynSymbol || market == "" || symbol == "" {
			return RequestSpec{Kind: "option_chain", Tier: requestTier(true), Dynamic: true}, true
		}
		return RequestSpec{Kind: "option_chain", Market: market, Symbol: symbol, Key: ChainLookupKey(market, symbol), Tier: RequestTierStatic}, true
	case "factor":
		if obj.Name != "request" {
			return RequestSpec{}, false
		}
		name, dynName := get("name", 0)
		interval, dynInterval := get("interval", 1)
		field, dynField := get("field", 2)
		if dynName || dynInterval || dynField {
			return RequestSpec{Kind: "factor", Tier: requestTier(true), Dynamic: true}, true
		}
		return RequestSpec{Kind: "factor", Name: name, Interval: interval, Field: field, Key: RequestFactorKey(name, interval), Tier: RequestTierStatic}, true
	case "fundamental":
		if obj.Name != "request" {
			return RequestSpec{}, false
		}
		market, dynMarket := get("market", 0)
		symbol, dynSymbol := get("symbol", 1)
		factor, dynFactor := get("factor", 2)
		mode, dynMode := get("mode", 3)
		if dynMarket || dynSymbol || dynFactor {
			return RequestSpec{Kind: "fundamental", Tier: requestTier(true), Dynamic: true}, true
		}
		if dynMode || mode == "" {
			mode = "filled"
		}
		return RequestSpec{Kind: "fundamental", Market: market, Symbol: symbol, Name: factor, Interval: "primary", Mode: mode, Field: "value", Key: RequestFundamentalKey(market, symbol, factor, mode), Tier: RequestTierStatic}, true
	case "symbols":
		if obj.Name != "universe" {
			return RequestSpec{}, false
		}
		code, dynCode := get("code", 0)
		if len(call.Args) == 0 || dynCode || code == "" {
			return RequestSpec{Kind: "universe", Tier: requestTier(true), Dynamic: true}, true
		}
		code = strings.ToLower(strings.TrimSpace(code))
		return RequestSpec{Kind: "universe", Name: code, Key: UniverseKey(code), Tier: RequestTierStatic}, true
	default:
		return RequestSpec{}, false
	}
}

func requestDiagnosticFunction(kind string) string {
	return RequestDiagnosticFunction(kind)
}

// RequestDiagnosticFunction maps a RequestSpec.Kind to the DSL builtin name
// it originates from, for use in diagnostics and validation error messages.
func RequestDiagnosticFunction(kind string) string {
	switch kind {
	case "security":
		return "request.security"
	case "factor":
		return "request.factor"
	case "fundamental":
		return "request.fundamental"
	case "option_chain":
		return "options.chain"
	case "universe":
		return "universe.symbols"
	default:
		return "request"
	}
}

func UniverseKey(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

func RequestFundamentalKey(market, symbol, factor, mode string) string {
	market = strings.TrimSpace(strings.ToLower(market))
	symbol = strings.TrimSpace(strings.ToUpper(symbol))
	factor = strings.TrimSpace(strings.ToLower(factor))
	mode = strings.TrimSpace(strings.ToLower(mode))
	return market + "|" + symbol + "|" + factor + "|" + mode
}

func ChainLookupKey(market, underlying string) string {
	return strings.ToLower(strings.TrimSpace(market)) + "|" + strings.ToUpper(strings.TrimSpace(underlying))
}

func qualifiedName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.IdentExpr:
		return strings.ToLower(strings.TrimSpace(node.Name))
	case *ast.DotExpr:
		left := qualifiedName(node.Object)
		field := strings.ToLower(strings.TrimSpace(node.Field))
		if left == "" {
			return field
		}
		if field == "" {
			return left
		}
		return left + "." + field
	default:
		return ""
	}
}

func isOptionsReference(name string) bool {
	switch {
	case strings.HasPrefix(name, "options."), strings.HasPrefix(name, "spread."), strings.HasPrefix(name, "leg."), strings.HasPrefix(name, "contract."):
		return true
	default:
		return false
	}
}

func isRegularTradeCall(name string) bool {
	switch name {
	case "buy", "sell", "strategy.entry", "strategy.close", "strategy.exit", "strategy.order", "order.market", "order.market_notional", "order.limit", "order.stop", "order.stop_limit", "order.twap", "order.immediate", "order.submit":
		return true
	default:
		return false
	}
}

func extractInputCall(name string, call *ast.CallExpr) ParamSchema {
	funcName := inputCallName(call.Callee)
	return extractInputSchema(name, funcName, call.Args)
}

func extractInputDecl(id *ast.InputDecl) ParamSchema {
	funcName := strings.TrimSpace(id.Token.Literal)
	return extractInputSchema(id.Name, funcName, id.Args)
}

func extractInputSchema(name, funcName string, argsList []ast.CallArg) ParamSchema {
	ps := ParamSchema{Name: name}
	switch {
	case funcName == "input.int":
		ps.Type = ParamInt
	case funcName == "input.float":
		ps.Type = ParamFloat
	case funcName == "input.bool":
		ps.Type = ParamBool
	case funcName == "input.string":
		ps.Type = ParamString
	default:
		ps.Type = ParamFloat
	}
	args := resolveInputArgs(argsList, ps.Type)
	if t, ok := args["title"]; ok {
		ps.Title, _ = t.(string)
	}
	if d, ok := args["defval"]; ok {
		ps.Default = d
	} else {
		ps.Default = defaultForType(ps.Type)
	}
	if funcName == "input" || funcName == "" {
		ps.Type = inferType(ps.Default, ps.Type)
	}
	if v, ok := args["minval"]; ok {
		if f, fok := toFloat(v); fok {
			ps.Min = &f
		}
	}
	if v, ok := args["maxval"]; ok {
		if f, fok := toFloat(v); fok {
			ps.Max = &f
		}
	}
	if v, ok := args["step"]; ok {
		if f, fok := toFloat(v); fok {
			ps.Step = &f
		}
	}
	if v, ok := args["options"]; ok {
		if arr, aok := v.([]string); aok {
			ps.Options = arr
		}
	}
	return ps
}

func isInputCall(expr ast.Expr) bool {
	name := inputCallName(expr)
	return name == "input" || name == "input.int" || name == "input.float" || name == "input.bool" || name == "input.string"
}

func inputCallName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.IdentExpr:
		return strings.TrimSpace(node.Name)
	case *ast.DotExpr:
		left := inputCallName(node.Object)
		if left == "" {
			return strings.TrimSpace(node.Field)
		}
		return left + "." + strings.TrimSpace(node.Field)
	default:
		return ""
	}
}

func resolveInputArgs(args []ast.CallArg, ptype ParamType) map[string]interface{} {
	var positionalNames []string
	switch ptype {
	case ParamBool:
		positionalNames = []string{"defval", "title"}
	case ParamString:
		positionalNames = []string{"defval", "title", "options"}
	default:
		positionalNames = []string{"defval", "title", "minval", "maxval", "step"}
	}
	out := make(map[string]interface{}, len(args))
	posIdx := 0
	for _, arg := range args {
		key := strings.TrimSpace(arg.Name)
		val := literalValue(arg.Value)
		if key == "" {
			if posIdx < len(positionalNames) {
				key = positionalNames[posIdx]
			}
			posIdx++
		}
		if key != "" {
			out[key] = val
		}
	}
	return out
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

func literalValue(expr ast.Expr) interface{} {
	switch n := expr.(type) {
	case *ast.NumberLit:
		return n.Value
	case *ast.StringLit:
		return n.Value
	case *ast.BoolLit:
		return n.Value
	case *ast.NaLit:
		return math.NaN()
	case *ast.ArrayLit:
		var items []string
		for _, el := range n.Elements {
			if s, ok := el.(*ast.StringLit); ok {
				items = append(items, s.Value)
			}
		}
		return items
	default:
		return nil
	}
}

func defaultForType(pt ParamType) interface{} {
	switch pt {
	case ParamInt:
		return 0
	case ParamFloat:
		return 0.0
	case ParamBool:
		return false
	case ParamString:
		return ""
	default:
		return 0.0
	}
}

func inferType(defval interface{}, fallback ParamType) ParamType {
	switch defval.(type) {
	case bool:
		return ParamBool
	case string:
		return ParamString
	case int:
		return ParamInt
	default:
		return fallback
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}

func RequestSecurityKey(market, symbol, interval string) string {
	market = strings.TrimSpace(strings.ToLower(market))
	symbol = strings.TrimSpace(strings.ToUpper(symbol))
	interval = strings.TrimSpace(strings.ToLower(interval))
	return strings.Join([]string{market, symbol, interval}, "|")
}

func RequestFactorKey(name, interval string) string {
	return strings.Join([]string{name, interval}, "|")
}
