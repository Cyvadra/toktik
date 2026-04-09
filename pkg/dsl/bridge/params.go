package bridge

import (
	"fmt"
	"math"
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

// ParamType describes the data type of a DSL parameter.
type ParamType string

const (
	ParamFloat  ParamType = "float"
	ParamInt    ParamType = "int"
	ParamBool   ParamType = "bool"
	ParamString ParamType = "string"
)

// ParamSchema describes a single DSL strategy parameter extracted from input.*() calls.
type ParamSchema struct {
	// Name is the variable name on the left side of the assignment (e.g., `length`).
	Name string

	// Title is the user-facing label from the title argument (e.g., "EMA Length").
	Title string

	// Type is the parameter's data type.
	Type ParamType

	// Default is the default value (float64, int, bool, or string depending on Type).
	Default interface{}

	// Min is the minimum value (NaN or nil if not specified).
	Min *float64

	// Max is the maximum value (NaN or nil if not specified).
	Max *float64

	// Step is the increment step (NaN or nil if not specified).
	Step *float64

	// Options is a list of allowed values for input.string with options=[].
	Options []string
}

// LookupKey returns the key used for parameter override lookup.
// Prefers Title if non-empty, otherwise falls back to Name.
func (p ParamSchema) LookupKey() string {
	if p.Title != "" {
		return p.Title
	}
	return p.Name
}

// ExtractParams walks the AST and extracts all input declarations as ParamSchema.
func ExtractParams(prog *ast.Program) []ParamSchema {
	if prog == nil {
		return nil
	}
	var params []ParamSchema
	for _, stmt := range prog.Stmts {
		id, ok := stmt.(*ast.InputDecl)
		if !ok {
			continue
		}
		ps := extractInputDecl(id)
		params = append(params, ps)
	}
	return params
}

func extractInputDecl(id *ast.InputDecl) ParamSchema {
	ps := ParamSchema{
		Name: id.Name,
	}

	// Determine type and resolve args by inspecting the call function name.
	// InputDecl.Token.Literal contains the function name (e.g., "input", "input.int").
	funcName := strings.TrimSpace(id.Token.Literal)

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
		// Generic input(): infer type from default value.
		ps.Type = ParamFloat
	}

	// Collect named and positional args.
	args := resolveInputArgs(id.Args, ps.Type)

	// Resolve title.
	if t, ok := args["title"]; ok {
		ps.Title = fmt.Sprint(t)
	}

	// Resolve default value.
	if d, ok := args["defval"]; ok {
		ps.Default = d
	} else {
		ps.Default = defaultForType(ps.Type)
	}

	// For generic input() infer type from default if not set.
	if funcName == "input" || funcName == "" {
		ps.Type = inferType(ps.Default, ps.Type)
	}

	// Resolve numeric constraints.
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

	// Resolve string options.
	if v, ok := args["options"]; ok {
		if arr, aok := v.([]string); aok {
			ps.Options = arr
		}
	}

	return ps
}

// resolveInputArgs extracts named/positional args from an InputDecl's CallArg list.
// Positional order follows PineScript convention:
//
//	input(defval, title, minval, maxval, step)
//	input.bool(defval, title)
//	input.string(defval, title, options)
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
		val := evalLiteralExpr(arg.Value)
		if key == "" {
			// Positional argument.
			if posIdx < len(positionalNames) {
				key = positionalNames[posIdx]
			} else {
				key = fmt.Sprintf("_pos_%d", posIdx)
			}
			posIdx++
		}
		out[key] = val
	}
	return out
}

// evalLiteralExpr extracts a Go value from a literal AST expression.
func evalLiteralExpr(expr ast.Expr) interface{} {
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
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) {
			return 0, false
		}
		return t, true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

// ApplyParams applies parameter overrides from Params to the interpreter's Inputs map.
// It supports mixed types: float64, int, bool (as 0/1), and string (stored as float if numeric).
// String overrides that cannot be converted are stored in InputStrings.
func ApplyParams(ip *runtime.Interpreter, params map[string]interface{}) {
	if len(params) == 0 {
		return
	}
	if ip.Inputs == nil {
		ip.Inputs = make(map[string]float64, len(params))
	}
	if ip.InputStrings == nil {
		ip.InputStrings = make(map[string]string, len(params))
	}

	for key, val := range params {
		switch v := val.(type) {
		case float64:
			ip.Inputs[key] = v
		case int:
			ip.Inputs[key] = float64(v)
		case bool:
			if v {
				ip.Inputs[key] = 1
			} else {
				ip.Inputs[key] = 0
			}
		case string:
			ip.InputStrings[key] = v
		}
	}
}
