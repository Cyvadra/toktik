package bridge

import (
	"github.com/Cyvadra/toktik/pkg/dsl/analysis"
	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

type ParamType = analysis.ParamType

const (
	ParamFloat  = analysis.ParamFloat
	ParamInt    = analysis.ParamInt
	ParamBool   = analysis.ParamBool
	ParamString = analysis.ParamString
)

type ParamSchema = analysis.ParamSchema

func ExtractParams(prog *ast.Program) []ParamSchema {
	return analysis.ExtractParams(prog)
}

func ApplyParams(ip *runtime.Interpreter, params map[string]interface{}) {
	if ip == nil || len(params) == 0 {
		return
	}
	if ip.Inputs == nil {
		ip.Inputs = make(map[string]float64, len(params))
	}
	for key, raw := range params {
		switch value := raw.(type) {
		case float64:
			ip.Inputs[key] = value
		case float32:
			ip.Inputs[key] = float64(value)
		case int:
			ip.Inputs[key] = float64(value)
		case int32:
			ip.Inputs[key] = float64(value)
		case int64:
			ip.Inputs[key] = float64(value)
		case bool:
			if value {
				ip.Inputs[key] = 1
			} else {
				ip.Inputs[key] = 0
			}
		case string:
			if ip.InputStrings == nil {
				ip.InputStrings = make(map[string]string)
			}
			ip.InputStrings[key] = value
		}
	}
}
