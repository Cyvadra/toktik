package analysis

import (
	"math"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
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
	ExpressionMode bool
	Expression     ast.Expr
}

type RequestTier string

const (
	RequestTierStatic         RequestTier = "static"
	RequestTierSemiDynamic    RequestTier = "semi_dynamic"
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
	m := Manifest{
		StrategyName: ExtractStrategyName(prog),
		Inputs:       ExtractParams(prog),
	}
	if prog == nil {
		return m
	}
	for _, stmt := range prog.Stmts {
		if sd, ok := stmt.(*ast.StrategyDecl); ok {
			extractStrategyMetadata(sd, &m.Metadata)
		}
		walkStmt(stmt, &m)
	}
	m.Metadata.Requests = append([]RequestSpec(nil), m.Requests...)
	return m
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

func walkStmt(stmt ast.Stmt, m *Manifest) {
	if stmt == nil || m == nil {
		return
	}
	switch node := stmt.(type) {
	case *ast.StrategyDecl:
		for _, arg := range node.Args {
			walkExpr(arg.Value, m)
		}
	case *ast.InputDecl:
		for _, arg := range node.Args {
			walkExpr(arg.Value, m)
		}
	case *ast.VarDecl:
		walkExpr(node.Value, m)
	case *ast.AssignStmt:
		walkExpr(node.Value, m)
	case *ast.IndexAssignStmt:
		walkExpr(node.Left, m)
		walkExpr(node.Index, m)
		walkExpr(node.Value, m)
	case *ast.TupleAssign:
		walkExpr(node.Value, m)
	case *ast.ExprStmt:
		walkExpr(node.Expression, m)
	case *ast.IfStmt:
		walkExpr(node.Condition, m)
		walkBlock(node.Body, m)
		for _, branch := range node.ElseIfs {
			walkExpr(branch.Condition, m)
			walkBlock(branch.Body, m)
		}
		walkBlock(node.Else, m)
	case *ast.ForStmt:
		walkExpr(node.Start, m)
		walkExpr(node.End, m)
		walkExpr(node.Step, m)
		walkBlock(node.Body, m)
	case *ast.ForInStmt:
		walkExpr(node.Collection, m)
		walkBlock(node.Body, m)
	case *ast.WhileStmt:
		walkExpr(node.Condition, m)
		walkBlock(node.Body, m)
	case *ast.SwitchStmt:
		walkExpr(node.Tag, m)
		for _, switchCase := range node.Cases {
			walkExpr(switchCase.Value, m)
			walkBlock(switchCase.Body, m)
		}
		walkBlock(node.Default, m)
	case *ast.FnDecl:
		for _, param := range node.Params {
			walkExpr(param.Default, m)
		}
		walkBlock(node.Body, m)
	case *ast.ReturnStmt:
		walkExpr(node.Value, m)
	case *ast.Block:
		walkBlock(node, m)
	}
}

func walkBlock(block *ast.Block, m *Manifest) {
	if block == nil {
		return
	}
	for _, stmt := range block.Stmts {
		walkStmt(stmt, m)
	}
}

func walkExpr(expr ast.Expr, m *Manifest) {
	if expr == nil || m == nil {
		return
	}
	switch node := expr.(type) {
	case *ast.BinaryExpr:
		walkExpr(node.Left, m)
		walkExpr(node.Right, m)
	case *ast.UnaryExpr:
		walkExpr(node.Operand, m)
	case *ast.CallExpr:
		name := qualifiedName(node.Callee)
		if isOptionsReference(name) {
			m.UsesOptions = true
		}
		if isRegularTradeCall(name) {
			m.UsesRegularOrders = true
		}
		if spec, ok := parseRequestSpec(node); ok {
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
		}
		walkExpr(node.Callee, m)
		for _, arg := range node.Args {
			walkExpr(arg.Value, m)
		}
	case *ast.DotExpr:
		name := qualifiedName(node)
		if isOptionsReference(name) {
			m.UsesOptions = true
		}
		walkExpr(node.Object, m)
	case *ast.IndexExpr:
		walkExpr(node.Left, m)
		walkExpr(node.Index, m)
	case *ast.TernaryExpr:
		walkExpr(node.Condition, m)
		walkExpr(node.Then, m)
		walkExpr(node.Else, m)
	case *ast.ArrayLit:
		for _, element := range node.Elements {
			walkExpr(element, m)
		}
	case *ast.LambdaExpr:
		walkExpr(node.Body, m)
	}
}

func parseRequestSpec(call *ast.CallExpr) (RequestSpec, bool) {
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
		value := literalString(expr)
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
		return RequestSpec{Kind: "option_chain", Market: market, Symbol: symbol, Key: backtest.ChainLookupKey(market, symbol), Tier: RequestTierStatic}, true
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
	default:
		return RequestSpec{}, false
	}
}

func requestDiagnosticFunction(kind string) string {
	switch kind {
	case "security":
		return "request.security"
	case "factor":
		return "request.factor"
	case "fundamental":
		return "request.fundamental"
	case "option_chain":
		return "options.chain"
	default:
		return "request"
	}
}

func RequestFundamentalKey(market, symbol, factor, mode string) string {
	market = strings.TrimSpace(strings.ToLower(market))
	symbol = strings.TrimSpace(strings.ToUpper(symbol))
	factor = strings.TrimSpace(strings.ToLower(factor))
	mode = strings.TrimSpace(strings.ToLower(mode))
	return market + "|" + symbol + "|" + factor + "|" + mode
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
	return strings.Join([]string{market, symbol, interval}, "|")
}

func RequestFactorKey(name, interval string) string {
	return strings.Join([]string{name, interval}, "|")
}
