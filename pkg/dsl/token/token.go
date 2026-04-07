// Package token defines the token types for the Toktik strategy DSL.
//
// The DSL is inspired by TradingView's Pine Script and WorldQuant Alpha
// expressions, extended with first-class options trading support.
package token

// Type is the category of a lexical token.
type Type int

const (
	// Special
	Illegal Type = iota
	EOF
	Newline // significant line terminator (statement separator)

	// Literals
	Int    // 42
	Float  // 3.14, 6.02e23
	String // "hello", 'world'
	Ident  // variable / function name
	True   // true
	False  // false
	Na     // na

	// Operators – arithmetic
	Plus      // +
	Minus     // -
	Star      // *
	Slash     // /
	Percent   // %
	PlusEq    // +=
	MinusEq   // -=
	StarEq    // *=
	SlashEq   // /=
	PercentEq // %=

	// Operators – comparison
	EqEq   // ==
	BangEq // !=
	Lt     // <
	Gt     // >
	LtEq   // <=
	GtEq   // >=

	// Operators – logical
	And // and
	Or  // or
	Not // not

	// Operators – assignment
	Eq      // =
	ColonEq // :=

	// Operators – conditional
	Question // ?
	Colon    // :

	// Operators – history referencing
	LBracket // [
	RBracket // ]

	// Delimiters
	LParen // (
	RParen // )
	LBrace // {  (currently unused but reserved)
	RBrace // }
	Comma  // ,
	Dot    // .
	Arrow  // =>

	// Keywords
	KwStrategy // strategy
	KwInput    // input
	KwVar      // var
	KwVarip    // varip
	KwIf       // if
	KwElse     // else
	KwFor      // for
	KwWhile    // while
	KwSwitch   // switch
	KwFn       // fn
	KwReturn   // return
	KwImport   // import
	KwIn       // in
	KwTo       // to
	KwBy       // by
	KwBreak    // break
	KwContinue // continue

	// Type keywords (optional annotations)
	KwInt    // int
	KwFloat  // float
	KwBool   // bool
	KwString // string
	KwColor  // color
	KwSeries // series
	KwSimple // simple
	KwConst  // const
)

// Token represents a single lexical token with source position.
type Token struct {
	Type    Type
	Literal string
	Line    int
	Col     int
}

// String returns a human-readable description of the token type.
func (t Type) String() string {
	if int(t) < len(typeNames) {
		return typeNames[t]
	}
	return "UNKNOWN"
}

var typeNames = [...]string{
	Illegal: "ILLEGAL",
	EOF:     "EOF",
	Newline: "NEWLINE",
	Int:     "INT",
	Float:   "FLOAT",
	String:  "STRING",
	Ident:   "IDENT",
	True:    "TRUE",
	False:   "FALSE",
	Na:      "NA",

	Plus:      "+",
	Minus:     "-",
	Star:      "*",
	Slash:     "/",
	Percent:   "%",
	PlusEq:    "+=",
	MinusEq:   "-=",
	StarEq:    "*=",
	SlashEq:   "/=",
	PercentEq: "%=",

	EqEq:   "==",
	BangEq: "!=",
	Lt:     "<",
	Gt:     ">",
	LtEq:   "<=",
	GtEq:   ">=",

	And: "and",
	Or:  "or",
	Not: "not",

	Eq:      "=",
	ColonEq: ":=",

	Question: "?",
	Colon:    ":",

	LBracket: "[",
	RBracket: "]",
	LParen:   "(",
	RParen:   ")",
	LBrace:   "{",
	RBrace:   "}",
	Comma:    ",",
	Dot:      ".",
	Arrow:    "=>",

	KwStrategy: "strategy",
	KwInput:    "input",
	KwVar:      "var",
	KwVarip:    "varip",
	KwIf:       "if",
	KwElse:     "else",
	KwFor:      "for",
	KwWhile:    "while",
	KwSwitch:   "switch",
	KwFn:       "fn",
	KwReturn:   "return",
	KwImport:   "import",
	KwIn:       "in",
	KwTo:       "to",
	KwBy:       "by",
	KwBreak:    "break",
	KwContinue: "continue",

	KwInt:    "int",
	KwFloat:  "float",
	KwBool:   "bool",
	KwString: "string",
	KwColor:  "color",
	KwSeries: "series",
	KwSimple: "simple",
	KwConst:  "const",
}

// keywords maps textual identifiers to keyword token types.
var keywords = map[string]Type{
	"strategy": KwStrategy,
	"input":    KwInput,
	"var":      KwVar,
	"varip":    KwVarip,
	"if":       KwIf,
	"else":     KwElse,
	"for":      KwFor,
	"while":    KwWhile,
	"switch":   KwSwitch,
	"fn":       KwFn,
	"return":   KwReturn,
	"import":   KwImport,
	"in":       KwIn,
	"to":       KwTo,
	"by":       KwBy,
	"break":    KwBreak,
	"continue": KwContinue,

	"true":  True,
	"false": False,
	"na":    Na,

	"and": And,
	"or":  Or,
	"not": Not,

	"int":    KwInt,
	"float":  KwFloat,
	"bool":   KwBool,
	"string": KwString,
	"color":  KwColor,
	"series": KwSeries,
	"simple": KwSimple,
	"const":  KwConst,
}

// LookupIdent returns the keyword Type if ident is a reserved word,
// otherwise returns Ident.
func LookupIdent(ident string) Type {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return Ident
}
