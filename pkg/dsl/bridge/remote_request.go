package bridge

import (
	"math"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/analysis"
	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

// EvalSpecialForm implements runtime.SpecialFormBridge for request.security's
// expression mode. The fourth argument remains an AST template and is evaluated
// with field/factor reads redirected to the requested security context.
func (b *barContextBridge) EvalSpecialForm(ip *runtime.Interpreter, call *ast.CallExpr, scope *runtime.Scope) (runtime.Value, bool) {
	if b == nil || b.ds == nil || call == nil || !isRequestSecurityCall(call) {
		return runtime.Value{}, false
	}
	args := evalCallArgs(ip, call, scope, []string{"market", "symbol", "interval", "field"})
	if len(args) < 4 || args[0] == nil || args[1] == nil || args[2] == nil || args[3] == nil {
		return runtime.NaVal(), true
	}
	marketValue := ip.EvalExpression(args[0], scope)
	symbolValue := ip.EvalExpression(args[1], scope)
	intervalValue := ip.EvalExpression(args[2], scope)
	market := strings.TrimSpace(marketValue.Str())
	symbol := strings.TrimSpace(symbolValue.Str())
	interval := strings.TrimSpace(intervalValue.Str())
	fieldExpr := args[3]
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
	expr := b.ds.resolveRemoteExpr(fieldExpr)
	previous := ip.Bridge
	ip.Bridge = &remoteContextBridge{parent: b, securityRef: ref, securityKey: key}
	restore := bindRemoteFields(ip, scope, key, b.ctx.Security(ref))
	defer func() {
		restore()
		ip.Bridge = previous
	}()
	value := ip.EvalReadOnlyExpression(expr, scope)
	return ip.CaptureSeries("request.security."+key+".__expr", value.Float()), true
}

func evalCallArgs(ip *runtime.Interpreter, call *ast.CallExpr, scope *runtime.Scope, params []string) []ast.Expr {
	out := make([]ast.Expr, len(params))
	paramIdx := make(map[string]int, len(params))
	for i, param := range params {
		paramIdx[param] = i
	}
	pos := 0
	for _, arg := range call.Args {
		if arg.Name != "" {
			if idx, ok := paramIdx[arg.Name]; ok {
				out[idx] = arg.Value
			}
			continue
		}
		if pos < len(out) {
			out[pos] = arg.Value
		}
		pos++
	}
	return out
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
func (r *remoteContextBridge) Buy(qty float64)                       {}
func (r *remoteContextBridge) Sell(qty float64)                      {}
func (r *remoteContextBridge) EntryLong(id string, qty float64)      {}
func (r *remoteContextBridge) EntryShort(id string, qty float64)     {}
func (r *remoteContextBridge) ExitLong(id string)                    {}
func (r *remoteContextBridge) ExitShort(id string)                   {}
func (r *remoteContextBridge) PositionSize() float64                 { return r.parent.PositionSize() }
func (r *remoteContextBridge) PositionAvgPrice() float64             { return r.parent.PositionAvgPrice() }
func (r *remoteContextBridge) Equity() float64                       { return r.parent.Equity() }
func (r *remoteContextBridge) Cash() float64                         { return r.parent.Cash() }
func (r *remoteContextBridge) Ind(name string) float64               { return r.Field(name) }
func (r *remoteContextBridge) IndAt(name string, offset int) float64 { return r.FieldAt(name, offset) }

func (r *remoteContextBridge) EvalSpecialForm(ip *runtime.Interpreter, call *ast.CallExpr, scope *runtime.Scope) (runtime.Value, bool) {
	if call == nil {
		return runtime.Value{}, false
	}
	if isRequestFactorCall(call) {
		return r.evalRemoteFactor(ip, call, scope)
	}
	if isRequestFundamentalCall(call) {
		return r.evalRemoteFundamental(ip, call, scope)
	}
	return runtime.Value{}, false
}

func (r *remoteContextBridge) evalRemoteFactor(ip *runtime.Interpreter, call *ast.CallExpr, scope *runtime.Scope) (runtime.Value, bool) {
	if len(call.Args) < 3 || r.parent == nil || r.parent.ds == nil {
		return runtime.NaVal(), true
	}
	args := evalCallArgs(ip, call, scope, []string{"name", "interval", "field"})
	if len(args) < 3 || args[0] == nil || args[1] == nil || args[2] == nil {
		return runtime.NaVal(), true
	}
	name := strings.TrimSpace(ip.EvalExpression(args[0], scope).Str())
	interval := strings.TrimSpace(ip.EvalExpression(args[1], scope).Str())
	field := strings.TrimSpace(ip.EvalExpression(args[2], scope).Str())
	ref, ok := r.parent.ds.remoteFacRefs[remoteFactorKey(r.securityKey, requestSpec{Name: name, Interval: interval})]
	if !ok {
		return runtime.NaVal(), true
	}
	value := r.parent.ctx.Factor(ref).Field(field)
	return ip.CaptureSeries("request.factor."+r.securityKey+"."+analysis.RequestFactorKey(name, interval)+"."+field, value), true
}

func (r *remoteContextBridge) evalRemoteFundamental(ip *runtime.Interpreter, call *ast.CallExpr, scope *runtime.Scope) (runtime.Value, bool) {
	if len(call.Args) < 3 || r.parent == nil || r.parent.ds == nil {
		return runtime.NaVal(), true
	}
	args := evalCallArgs(ip, call, scope, []string{"market", "symbol", "factor", "mode"})
	if len(args) < 3 || args[2] == nil {
		return runtime.NaVal(), true
	}
	market := ""
	if args[0] != nil {
		market = strings.TrimSpace(ip.EvalExpression(args[0], scope).Str())
	}
	symbol := ""
	if args[1] != nil {
		symbol = strings.TrimSpace(ip.EvalExpression(args[1], scope).Str())
	}
	factor := strings.TrimSpace(ip.EvalExpression(args[2], scope).Str())
	mode := "filled"
	if len(args) >= 4 && args[3] != nil {
		mode = strings.TrimSpace(ip.EvalExpression(args[3], scope).Str())
	}
	if factor == "" {
		return runtime.NaVal(), true
	}
	ref, ok := r.parent.ds.remoteFacRefs[remoteFactorKey(r.securityKey, requestSpec{Market: market, Symbol: symbol, Name: factor, Interval: "primary", Mode: mode})]
	if !ok {
		return runtime.NaVal(), true
	}
	value := r.parent.ctx.Factor(ref).Field("value")
	return ip.CaptureSeries("request.fundamental."+r.securityKey+"."+factor+"."+mode+".value", value), true
}
