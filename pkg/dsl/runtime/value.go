package runtime

import (
	"fmt"
	"math"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
)

// Tag discriminates runtime value types.
type Tag int

const (
	TagNa Tag = iota
	TagFloat
	TagBool
	TagString
	TagSeries
	TagArray
	TagFn
	TagExpr
)

// Value is the universal runtime type.
type Value struct {
	tag    Tag
	fval   float64
	bval   bool
	sval   string
	series *Series
	array  []Value
	fn     *Fn
	expr   ast.Expr
	obj    interface{} // opaque Go object (e.g. OptionsChain, OptionContract)
}

func NaVal() Value                { return Value{tag: TagNa} }
func FloatVal(f float64) Value    { return Value{tag: TagFloat, fval: f} }
func BoolVal(b bool) Value        { return Value{tag: TagBool, bval: b} }
func StringVal(s string) Value    { return Value{tag: TagString, sval: s} }
func SeriesVal(s *Series) Value   { return Value{tag: TagSeries, series: s} }
func ArrayVal(vs []Value) Value   { return Value{tag: TagArray, array: vs} }
func FnVal(fn *Fn) Value          { return Value{tag: TagFn, fn: fn} }
func ExprVal(expr ast.Expr) Value { return Value{tag: TagExpr, expr: expr} }
func ObjVal(o interface{}) Value  { return Value{tag: TagArray, obj: o} }

func (v Value) Tag() Tag   { return v.tag }
func (v Value) IsNa() bool { return v.tag == TagNa }

func (v Value) Float() float64 {
	switch v.tag {
	case TagFloat:
		return v.fval
	case TagBool:
		if v.bval {
			return 1
		}
		return 0
	case TagSeries:
		if v.series != nil {
			return v.series.Current()
		}
	}
	return math.NaN()
}

func (v Value) Bool() bool {
	switch v.tag {
	case TagBool:
		return v.bval
	case TagFloat:
		return v.fval != 0 && !math.IsNaN(v.fval)
	case TagString:
		return v.sval != ""
	case TagNa:
		return false
	case TagSeries:
		if v.series != nil {
			c := v.series.Current()
			return !math.IsNaN(c) && c != 0
		}
	case TagArray:
		return len(v.array) > 0
	}
	return false
}

func (v Value) String() string {
	switch v.tag {
	case TagNa:
		return "na"
	case TagFloat:
		return fmt.Sprintf("%g", v.fval)
	case TagBool:
		if v.bval {
			return "true"
		}
		return "false"
	case TagString:
		return v.sval
	case TagSeries:
		return fmt.Sprintf("series(%g)", v.series.Current())
	case TagArray:
		return fmt.Sprintf("array[%d]", len(v.array))
	case TagFn:
		return fmt.Sprintf("fn(%s)", v.fn.Name)
	case TagExpr:
		return "expr"
	default:
		return "?"
	}
}

func (v Value) Str() string        { return v.sval }
func (v Value) SeriesPtr() *Series { return v.series }
func (v Value) Array() []Value     { return v.array }
func (v Value) FnPtr() *Fn         { return v.fn }
func (v Value) Expr() ast.Expr     { return v.expr }
func (v Value) Obj() interface{}   { return v.obj }
