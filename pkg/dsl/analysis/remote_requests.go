package analysis

import (
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
)

// This file centralizes AST-level discovery of "remote" request.factor /
// request.fundamental calls nested inside a request.security(...) expression
// argument, plus the top-level variable-alias expansion needed to see
// through them (e.g. `iv = request.factor(...)` then
// `request.security(..., iv)`). It previously existed as a second,
// independent AST walker inside pkg/dsl/bridge that duplicated the
// predicates and literal-extraction helpers already defined in this
// package; unifying it here means there is exactly one place that knows how
// to find request.* calls, and bridge only consumes the result.
//
// CollectAliasExprs/ExpandExpr must stay pure functions over an *ast.Expr
// (no bridge/runtime state) because bridge also uses ExpandExpr at
// evaluation time, once per bar, to build the live expression tree it hands
// to the interpreter — that usage cannot be precomputed at analysis time.

// CollectAliasExprs collects top-level, non-persist, non-varip variable
// declarations as a name->expression alias table. It is used to see through
// simple local aliases when resolving a request.security(...) expression
// argument (both for Init-time dependency discovery and per-bar evaluation).
func CollectAliasExprs(prog *ast.Program) map[string]ast.Expr {
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

// ExpandExpr recursively substitutes identifiers that resolve to a top-level
// alias (from CollectAliasExprs) with the aliased expression, so downstream
// analysis/evaluation sees the real expression rather than an opaque name.
func ExpandExpr(expr ast.Expr, aliasExprs map[string]ast.Expr) ast.Expr {
	return expandExpr(expr, aliasExprs, make(map[string]bool))
}

func expandExpr(expr ast.Expr, aliasExprs map[string]ast.Expr, stack map[string]bool) ast.Expr {
	if expr == nil || len(aliasExprs) == 0 {
		return expr
	}
	switch node := expr.(type) {
	case *ast.IdentExpr:
		name := strings.TrimSpace(node.Name)
		if name == "" || stack[name] {
			return expr
		}
		alias, ok := aliasExprs[name]
		if !ok {
			return expr
		}
		stack[name] = true
		resolved := expandExpr(alias, aliasExprs, stack)
		delete(stack, name)
		return resolved
	case *ast.BinaryExpr:
		copy := *node
		copy.Left = expandExpr(node.Left, aliasExprs, stack)
		copy.Right = expandExpr(node.Right, aliasExprs, stack)
		return &copy
	case *ast.UnaryExpr:
		copy := *node
		copy.Operand = expandExpr(node.Operand, aliasExprs, stack)
		return &copy
	case *ast.CallExpr:
		copy := *node
		copy.Callee = expandExpr(node.Callee, aliasExprs, stack)
		copy.Args = make([]ast.CallArg, len(node.Args))
		for i, arg := range node.Args {
			copy.Args[i] = ast.CallArg{Name: arg.Name, Value: expandExpr(arg.Value, aliasExprs, stack)}
		}
		return &copy
	case *ast.DotExpr:
		copy := *node
		copy.Object = expandExpr(node.Object, aliasExprs, stack)
		return &copy
	case *ast.IndexExpr:
		copy := *node
		copy.Left = expandExpr(node.Left, aliasExprs, stack)
		copy.Index = expandExpr(node.Index, aliasExprs, stack)
		return &copy
	case *ast.TernaryExpr:
		copy := *node
		copy.Condition = expandExpr(node.Condition, aliasExprs, stack)
		copy.Then = expandExpr(node.Then, aliasExprs, stack)
		copy.Else = expandExpr(node.Else, aliasExprs, stack)
		return &copy
	case *ast.ArrayLit:
		copy := *node
		copy.Elements = make([]ast.Expr, len(node.Elements))
		for i, element := range node.Elements {
			copy.Elements[i] = expandExpr(element, aliasExprs, stack)
		}
		return &copy
	case *ast.LambdaExpr:
		copy := *node
		copy.Body = expandExpr(node.Body, aliasExprs, stack)
		return &copy
	default:
		return expr
	}
}

// CollectRemoteFactorRequests walks an already alias-expanded expression
// (typically the field argument of request.security(...)) and returns a
// RequestSpec for every nested request.factor/request.fundamental call it
// finds, so the caller can preload the corresponding factor references
// before the expression is evaluated per-bar.
func CollectRemoteFactorRequests(expr ast.Expr) []RequestSpec {
	var out []RequestSpec
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
			if IsRequestFactorCall(n) {
				name := positionalStringArg(n, "name", 0)
				interval := positionalStringArg(n, "interval", 1)
				field := positionalStringArg(n, "field", 2)
				if name != "" && interval != "" && field != "" {
					out = append(out, RequestSpec{Kind: "factor", Name: name, Interval: interval, Field: field, Key: RequestFactorKey(name, interval)})
				}
			} else if IsRequestFundamentalCall(n) {
				market := positionalStringArg(n, "market", 0)
				symbol := positionalStringArg(n, "symbol", 1)
				factor := positionalStringArg(n, "factor", 2)
				mode := positionalStringArg(n, "mode", 3)
				if mode == "" {
					mode = "filled"
				}
				if factor != "" {
					out = append(out, RequestSpec{Kind: "fundamental", Market: market, Symbol: symbol, Name: factor, Interval: "primary", Mode: mode, Field: "value"})
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

// IsRequestSecurityCall reports whether call is request.security(...).
func IsRequestSecurityCall(call *ast.CallExpr) bool {
	return isRequestCall(call, "security")
}

// IsRequestFactorCall reports whether call is request.factor(...).
func IsRequestFactorCall(call *ast.CallExpr) bool {
	return isRequestCall(call, "factor")
}

// IsRequestFundamentalCall reports whether call is request.fundamental(...).
func IsRequestFundamentalCall(call *ast.CallExpr) bool {
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

// RemoteFactorKey builds the lookup key used to associate a nested
// request.factor/request.fundamental call (found inside a request.security
// expression) with the backtest.FactorRef registered for it. The key is
// scoped by the parent security's key so the same factor name/interval can
// be requested independently for different remote securities.
func RemoteFactorKey(securityKey string, spec RequestSpec) string {
	return strings.Join([]string{
		securityKey,
		strings.TrimSpace(strings.ToLower(spec.Market)),
		strings.TrimSpace(strings.ToUpper(spec.Symbol)),
		strings.TrimSpace(strings.ToLower(spec.Name)),
		strings.TrimSpace(strings.ToLower(spec.Interval)),
		strings.TrimSpace(strings.ToLower(spec.Mode)),
	}, "|")
}
