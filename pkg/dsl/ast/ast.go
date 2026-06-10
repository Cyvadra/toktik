// Package ast defines the abstract syntax tree nodes for the Toktik DSL.
package ast

import "github.com/Cyvadra/toktik/pkg/dsl/token"

// Node is the common interface for all AST nodes.
type Node interface {
	Pos() token.Token
	nodeTag()
}

// Expr nodes produce a value.
type Expr interface {
	Node
	exprTag()
}

// Stmt nodes perform an action.
type Stmt interface {
	Node
	stmtTag()
}

// Program is the root of every script.
type Program struct {
	Stmts []Stmt
}

func (p *Program) Pos() token.Token {
	if len(p.Stmts) > 0 {
		return p.Stmts[0].Pos()
	}
	return token.Token{}
}
func (*Program) nodeTag() {}
func (*Program) stmtTag() {}

// StrategyDecl: strategy("name", overlay=true, ...), indicator(...), or library(...)
type StrategyDecl struct {
	Token token.Token
	Kind  string
	Args  []CallArg
}

// InputDecl: x = input(defval, title="X", ...)
type InputDecl struct {
	Token token.Token
	Name  string
	Args  []CallArg
}

// VarDecl: var x = expr | varip x = expr | x = expr
type VarDecl struct {
	Token    token.Token
	Name     string
	TypeHint string
	Varip    bool
	Persist  bool
	Value    Expr
}

// AssignStmt: x := expr | x += expr | ...
type AssignStmt struct {
	Token token.Token
	Name  string
	Op    token.Type
	Value Expr
}

// IndexAssignStmt: arr[i] = expr
type IndexAssignStmt struct {
	Token token.Token
	Left  Expr // the object being indexed (e.g. arr)
	Index Expr // the index expression
	Value Expr // the value to assign
}

// TupleAssign: [a, b] = func(...)
type TupleAssign struct {
	Token token.Token
	Names []string
	Value Expr
}

// ExprStmt wraps an expression used as a statement.
type ExprStmt struct {
	Expression Expr
}

// IfStmt: if cond { body } else if cond2 { body } else { body }
type IfStmt struct {
	Token     token.Token
	Condition Expr
	Body      *Block
	ElseIfs   []ElseIf
	Else      *Block
}

// ElseIf holds one "else if" branch.
type ElseIf struct {
	Token     token.Token
	Condition Expr
	Body      *Block
}

// ForStmt: for i = start to end by step { body }
type ForStmt struct {
	Token token.Token
	Var   string
	Start Expr
	End   Expr
	Step  Expr
	Body  *Block
}

// ForInStmt: for v in collection { body }
type ForInStmt struct {
	Token      token.Token
	Var        string
	Collection Expr
	Body       *Block
}

// WhileStmt: while cond { body }
type WhileStmt struct {
	Token     token.Token
	Condition Expr
	Body      *Block
}

// SwitchStmt: switch expr { case val => body ... }
type SwitchStmt struct {
	Token   token.Token
	Tag     Expr
	Cases   []SwitchCase
	Default *Block
}

// SwitchCase: value => body
type SwitchCase struct {
	Value Expr
	Body  *Block
}

// FnDecl: fn name(params) { body }
type FnDecl struct {
	Token  token.Token
	Name   string
	Params []FnParam
	Body   *Block
}

// FnParam is a function parameter with optional default value.
type FnParam struct {
	Name    string
	Default Expr
}

// ReturnStmt: return expr
type ReturnStmt struct {
	Token token.Token
	Value Expr
}

// BreakStmt: break
type BreakStmt struct {
	Token token.Token
}

// ContinueStmt: continue
type ContinueStmt struct {
	Token token.Token
}

// ImportStmt: import "module"
type ImportStmt struct {
	Token token.Token
	Path  string
}

// Block is a list of statements.
type Block struct {
	Token token.Token
	Stmts []Stmt
}

// NumberLit: 42, 3.14, 6.02e23
type NumberLit struct {
	Token token.Token
	Value float64
}

// StringLit: "hello"
type StringLit struct {
	Token token.Token
	Value string
}

// BoolLit: true, false
type BoolLit struct {
	Token token.Token
	Value bool
}

// NaLit: na
type NaLit struct {
	Token token.Token
}

// IdentExpr: variable or namespace reference
type IdentExpr struct {
	Token token.Token
	Name  string
}

// BinaryExpr: left op right
type BinaryExpr struct {
	Token token.Token
	Left  Expr
	Op    token.Type
	Right Expr
}

// UnaryExpr: op operand
type UnaryExpr struct {
	Token   token.Token
	Op      token.Type
	Operand Expr
}

// CallExpr: callee(args...)
type CallExpr struct {
	Token  token.Token
	Callee Expr
	Args   []CallArg
}

// CallArg is a positional or named argument.
type CallArg struct {
	Name  string
	Value Expr
}

// DotExpr: object.field
type DotExpr struct {
	Token  token.Token
	Object Expr
	Field  string
}

// IndexExpr: expr[index]
type IndexExpr struct {
	Token token.Token
	Left  Expr
	Index Expr
}

// TernaryExpr: cond ? then : else
type TernaryExpr struct {
	Token     token.Token
	Condition Expr
	Then      Expr
	Else      Expr
}

// ArrayLit: [expr, expr, ...]
type ArrayLit struct {
	Token    token.Token
	Elements []Expr
}

// LambdaExpr: (params) => expr
type LambdaExpr struct {
	Token  token.Token
	Params []FnParam
	Body   Expr
}

// Pos implementations
func (n *StrategyDecl) Pos() token.Token    { return n.Token }
func (n *InputDecl) Pos() token.Token       { return n.Token }
func (n *VarDecl) Pos() token.Token         { return n.Token }
func (n *AssignStmt) Pos() token.Token      { return n.Token }
func (n *IndexAssignStmt) Pos() token.Token { return n.Token }
func (n *TupleAssign) Pos() token.Token     { return n.Token }
func (n *ExprStmt) Pos() token.Token        { return n.Expression.Pos() }
func (n *IfStmt) Pos() token.Token          { return n.Token }
func (n *ForStmt) Pos() token.Token         { return n.Token }
func (n *ForInStmt) Pos() token.Token       { return n.Token }
func (n *WhileStmt) Pos() token.Token       { return n.Token }
func (n *SwitchStmt) Pos() token.Token      { return n.Token }
func (n *FnDecl) Pos() token.Token          { return n.Token }
func (n *ReturnStmt) Pos() token.Token      { return n.Token }
func (n *BreakStmt) Pos() token.Token       { return n.Token }
func (n *ContinueStmt) Pos() token.Token    { return n.Token }
func (n *ImportStmt) Pos() token.Token      { return n.Token }
func (n *Block) Pos() token.Token           { return n.Token }
func (n *NumberLit) Pos() token.Token       { return n.Token }
func (n *StringLit) Pos() token.Token       { return n.Token }
func (n *BoolLit) Pos() token.Token         { return n.Token }
func (n *NaLit) Pos() token.Token           { return n.Token }
func (n *IdentExpr) Pos() token.Token       { return n.Token }
func (n *BinaryExpr) Pos() token.Token      { return n.Token }
func (n *UnaryExpr) Pos() token.Token       { return n.Token }
func (n *CallExpr) Pos() token.Token        { return n.Token }
func (n *DotExpr) Pos() token.Token         { return n.Token }
func (n *IndexExpr) Pos() token.Token       { return n.Token }
func (n *TernaryExpr) Pos() token.Token     { return n.Token }
func (n *ArrayLit) Pos() token.Token        { return n.Token }
func (n *LambdaExpr) Pos() token.Token      { return n.Token }

// nodeTag implementations
func (*StrategyDecl) nodeTag()    {}
func (*InputDecl) nodeTag()       {}
func (*VarDecl) nodeTag()         {}
func (*AssignStmt) nodeTag()      {}
func (*IndexAssignStmt) nodeTag() {}
func (*TupleAssign) nodeTag()     {}
func (*ExprStmt) nodeTag()        {}
func (*IfStmt) nodeTag()          {}
func (*ForStmt) nodeTag()         {}
func (*ForInStmt) nodeTag()       {}
func (*WhileStmt) nodeTag()       {}
func (*SwitchStmt) nodeTag()      {}
func (*FnDecl) nodeTag()          {}
func (*ReturnStmt) nodeTag()      {}
func (*BreakStmt) nodeTag()       {}
func (*ContinueStmt) nodeTag()    {}
func (*ImportStmt) nodeTag()      {}
func (*Block) nodeTag()           {}
func (*NumberLit) nodeTag()       {}
func (*StringLit) nodeTag()       {}
func (*BoolLit) nodeTag()         {}
func (*NaLit) nodeTag()           {}
func (*IdentExpr) nodeTag()       {}
func (*BinaryExpr) nodeTag()      {}
func (*UnaryExpr) nodeTag()       {}
func (*CallExpr) nodeTag()        {}
func (*DotExpr) nodeTag()         {}
func (*IndexExpr) nodeTag()       {}
func (*TernaryExpr) nodeTag()     {}
func (*ArrayLit) nodeTag()        {}
func (*LambdaExpr) nodeTag()      {}

// stmtTag implementations
func (*StrategyDecl) stmtTag()    {}
func (*InputDecl) stmtTag()       {}
func (*VarDecl) stmtTag()         {}
func (*AssignStmt) stmtTag()      {}
func (*IndexAssignStmt) stmtTag() {}
func (*TupleAssign) stmtTag()     {}
func (*ExprStmt) stmtTag()        {}
func (*IfStmt) stmtTag()          {}
func (*ForStmt) stmtTag()         {}
func (*ForInStmt) stmtTag()       {}
func (*WhileStmt) stmtTag()       {}
func (*SwitchStmt) stmtTag()      {}
func (*FnDecl) stmtTag()          {}
func (*ReturnStmt) stmtTag()      {}
func (*BreakStmt) stmtTag()       {}
func (*ContinueStmt) stmtTag()    {}
func (*ImportStmt) stmtTag()      {}
func (*Block) stmtTag()           {}

// exprTag implementations
func (*NumberLit) exprTag()   {}
func (*StringLit) exprTag()   {}
func (*BoolLit) exprTag()     {}
func (*NaLit) exprTag()       {}
func (*IdentExpr) exprTag()   {}
func (*BinaryExpr) exprTag()  {}
func (*UnaryExpr) exprTag()   {}
func (*CallExpr) exprTag()    {}
func (*DotExpr) exprTag()     {}
func (*IndexExpr) exprTag()   {}
func (*TernaryExpr) exprTag() {}
func (*ArrayLit) exprTag()    {}
func (*LambdaExpr) exprTag()  {}
