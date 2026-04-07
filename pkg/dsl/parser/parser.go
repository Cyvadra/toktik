// Package parser turns a token stream into an AST.
//
// It uses Pratt (top-down operator precedence) parsing for expressions and
// recursive descent for statements. Newlines act as statement separators.
package parser

import (
	"fmt"
	"strconv"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/lexer"
	"github.com/Cyvadra/toktik/pkg/dsl/token"
)

// Precedence levels for Pratt parsing.
const (
	_ int = iota
	precOr
	precAnd
	precEquality   // == !=
	precComparison // < > <= >=
	precAddition   // + -
	precMultiply   // * / %
	precUnary      // - not !
	precCall       // () [] .
)

func infixPrec(tt token.Type) int {
	switch tt {
	case token.Or:
		return precOr
	case token.And:
		return precAnd
	case token.EqEq, token.BangEq:
		return precEquality
	case token.Lt, token.Gt, token.LtEq, token.GtEq:
		return precComparison
	case token.Plus, token.Minus:
		return precAddition
	case token.Star, token.Slash, token.Percent:
		return precMultiply
	default:
		return 0
	}
}

// Parser holds state for parsing a token stream.
type Parser struct {
	tokens []token.Token
	pos    int
	errors []string
}

// New creates a parser from source text.
func New(src string) *Parser {
	tokens, _ := lexer.Tokenize(src)
	return &Parser{tokens: tokens}
}

// Parse parses the entire program.
func Parse(src string) (*ast.Program, []string) {
	p := New(src)
	prog := p.parseProgram()
	return prog, p.errors
}

// ---------- helpers ----------

func (p *Parser) cur() token.Token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return token.Token{Type: token.EOF}
}

func (p *Parser) peek() token.Token {
	idx := p.pos + 1
	for idx < len(p.tokens) && p.tokens[idx].Type == token.Newline {
		idx++
	}
	if idx < len(p.tokens) {
		return p.tokens[idx]
	}
	return token.Token{Type: token.EOF}
}

func (p *Parser) peekRaw() token.Token {
	if p.pos+1 < len(p.tokens) {
		return p.tokens[p.pos+1]
	}
	return token.Token{Type: token.EOF}
}

func (p *Parser) advance() token.Token {
	tok := p.cur()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *Parser) expect(tt token.Type) token.Token {
	tok := p.cur()
	if tok.Type != tt {
		p.errorf("expected %s, got %s (%q) at line %d col %d",
			tt, tok.Type, tok.Literal, tok.Line, tok.Col)
		return tok
	}
	p.advance()
	return tok
}

func (p *Parser) errorf(format string, args ...interface{}) {
	p.errors = append(p.errors, fmt.Sprintf(format, args...))
}

func (p *Parser) match(tt token.Type) bool {
	if p.cur().Type == tt {
		p.advance()
		return true
	}
	return false
}

// skipNewlines advances past any newline tokens.
func (p *Parser) skipNewlines() {
	for p.cur().Type == token.Newline {
		p.advance()
	}
}

// ---------- program ----------

func (p *Parser) parseProgram() *ast.Program {
	prog := &ast.Program{}
	p.skipNewlines()
	for p.cur().Type != token.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			prog.Stmts = append(prog.Stmts, stmt)
		}
		// Expect a newline or EOF after each statement.
		if p.cur().Type != token.EOF && p.cur().Type != token.RBrace {
			if !p.match(token.Newline) {
				p.skipNewlines() // try to recover
			}
		}
		p.skipNewlines()
	}
	return prog
}

// ---------- statements ----------

func (p *Parser) parseStatement() ast.Stmt {
	switch p.cur().Type {
	case token.KwStrategy:
		// strategy(...) is a declaration, but strategy.xxx is expression.
		if p.peekRaw().Type == token.LParen {
			return p.parseStrategyDecl()
		}
		return p.parseExprOrAssignStmt()
	case token.KwInput:
		return p.parseInputDecl()
	case token.KwVar, token.KwVarip:
		return p.parseVarDecl(true)
	case token.KwIf:
		return p.parseIfStmt()
	case token.KwFor:
		return p.parseForStmt()
	case token.KwWhile:
		return p.parseWhileStmt()
	case token.KwSwitch:
		return p.parseSwitchStmt()
	case token.KwFn:
		return p.parseFnDecl()
	case token.KwReturn:
		return p.parseReturnStmt()
	case token.KwBreak:
		tok := p.advance()
		return &ast.BreakStmt{Token: tok}
	case token.KwContinue:
		tok := p.advance()
		return &ast.ContinueStmt{Token: tok}
	case token.KwImport:
		return p.parseImportStmt()
	case token.LBracket:
		// Could be tuple assign: [a, b] = expr
		if p.isTupleAssign() {
			return p.parseTupleAssign()
		}
		return p.parseExprOrAssignStmt()
	case token.Ident:
		return p.parseExprOrAssignStmt()
	default:
		return p.parseExprOrAssignStmt()
	}
}

// parseExprOrAssignStmt parses an expression or assignment statement.
func (p *Parser) parseExprOrAssignStmt() ast.Stmt {
	// Check if this is a simple assignment: ident = expr (variable declaration without var)
	if p.cur().Type == token.Ident {
		next := p.peekRaw()
		switch next.Type {
		case token.Eq:
			// ident = expr  → treated as var decl (re-evaluated each bar)
			tok := p.advance()
			p.advance() // skip =
			val := p.parseExpr(0)
			return &ast.VarDecl{
				Token: tok,
				Name:  tok.Literal,
				Value: val,
			}
		case token.ColonEq, token.PlusEq, token.MinusEq, token.StarEq, token.SlashEq, token.PercentEq:
			tok := p.advance()
			op := p.advance()
			val := p.parseExpr(0)
			return &ast.AssignStmt{
				Token: tok,
				Name:  tok.Literal,
				Op:    op.Type,
				Value: val,
			}
		}
	}

	expr := p.parseExpr(0)
	return &ast.ExprStmt{Expression: expr}
}

// ---------- strategy ----------

func (p *Parser) parseStrategyDecl() ast.Stmt {
	tok := p.expect(token.KwStrategy)
	p.expect(token.LParen)
	args := p.parseCallArgs()
	p.expect(token.RParen)
	return &ast.StrategyDecl{Token: tok, Args: args}
}

// ---------- input ----------

func (p *Parser) parseInputDecl() ast.Stmt {
	// input declarations: name = input(...)
	// But sometimes it's just input(...) as a statement — we handle the usual form:
	// x = input(defval, title="X")
	// The "x = " part is handled in parseExprOrAssignStmt, so if we reach here,
	// it means `input` appeared at statement start. Treat as expression.
	tok := p.cur()
	expr := p.parseExpr(0)
	return &ast.ExprStmt{Expression: &ast.CallExpr{
		Token:  tok,
		Callee: &ast.IdentExpr{Token: tok, Name: "input"},
		Args:   extractCallArgs(expr),
	}}
}

func extractCallArgs(e ast.Expr) []ast.CallArg {
	if c, ok := e.(*ast.CallExpr); ok {
		return c.Args
	}
	return nil
}

// ---------- var/varip ----------

func (p *Parser) parseVarDecl(hasQualifier bool) ast.Stmt {
	tok := p.advance() // consume var/varip
	isVarip := tok.Type == token.KwVarip
	isPersist := tok.Type == token.KwVar

	name := p.expect(token.Ident)
	p.expect(token.Eq)
	val := p.parseExpr(0)
	return &ast.VarDecl{
		Token:   tok,
		Name:    name.Literal,
		Varip:   isVarip,
		Persist: isPersist,
		Value:   val,
	}
}

// ---------- if ----------

func (p *Parser) parseIfStmt() ast.Stmt {
	tok := p.expect(token.KwIf)
	cond := p.parseExpr(0)
	body := p.parseBlock()

	var elseIfs []ast.ElseIf
	var elseBlock *ast.Block

	for p.cur().Type == token.KwElse {
		elseTok := p.advance()
		if p.cur().Type == token.KwIf {
			p.advance()
			eifCond := p.parseExpr(0)
			eifBody := p.parseBlock()
			elseIfs = append(elseIfs, ast.ElseIf{
				Token:     elseTok,
				Condition: eifCond,
				Body:      eifBody,
			})
		} else {
			elseBlock = p.parseBlock()
			break
		}
	}

	return &ast.IfStmt{
		Token:     tok,
		Condition: cond,
		Body:      body,
		ElseIfs:   elseIfs,
		Else:      elseBlock,
	}
}

// ---------- for ----------

func (p *Parser) parseForStmt() ast.Stmt {
	tok := p.expect(token.KwFor)

	// for v in collection
	if p.cur().Type == token.Ident && p.peek().Type == token.KwIn {
		name := p.advance()
		p.expect(token.KwIn)
		coll := p.parseExpr(0)
		body := p.parseBlock()
		return &ast.ForInStmt{Token: tok, Var: name.Literal, Collection: coll, Body: body}
	}

	// for i = start to end [by step]
	name := p.expect(token.Ident)
	p.expect(token.Eq)
	start := p.parseExpr(0)
	p.expect(token.KwTo)
	end := p.parseExpr(0)

	var step ast.Expr
	if p.match(token.KwBy) {
		step = p.parseExpr(0)
	}

	body := p.parseBlock()
	return &ast.ForStmt{Token: tok, Var: name.Literal, Start: start, End: end, Step: step, Body: body}
}

// ---------- while ----------

func (p *Parser) parseWhileStmt() ast.Stmt {
	tok := p.expect(token.KwWhile)
	cond := p.parseExpr(0)
	body := p.parseBlock()
	return &ast.WhileStmt{Token: tok, Condition: cond, Body: body}
}

// ---------- switch ----------

func (p *Parser) parseSwitchStmt() ast.Stmt {
	tok := p.expect(token.KwSwitch)

	var tag ast.Expr
	// If next is not newline/brace, parse the tag expression.
	if p.cur().Type != token.Newline && p.cur().Type != token.LBrace {
		tag = p.parseExpr(0)
	}
	p.skipNewlines()

	var cases []ast.SwitchCase
	var defBlock *ast.Block

	// Parse cases until we hit something that's not a valid case start.
	for p.cur().Type != token.EOF && p.cur().Type != token.RBrace {
		p.skipNewlines()
		if p.cur().Type == token.EOF {
			break
		}

		// Check for default
		if p.cur().Type == token.Ident && p.cur().Literal == "default" {
			p.advance()
			p.expect(token.Arrow)
			defBlock = p.parseBlock()
			continue
		}

		// "else =>" as default
		if p.cur().Type == token.KwElse {
			p.advance()
			p.expect(token.Arrow)
			defBlock = p.parseBlock()
			continue
		}

		val := p.parseExpr(0)
		p.expect(token.Arrow)
		body := p.parseBlock()
		cases = append(cases, ast.SwitchCase{Value: val, Body: body})
	}

	return &ast.SwitchStmt{Token: tok, Tag: tag, Cases: cases, Default: defBlock}
}

// ---------- fn ----------

func (p *Parser) parseFnDecl() ast.Stmt {
	tok := p.expect(token.KwFn)
	name := p.expect(token.Ident)
	p.expect(token.LParen)
	params := p.parseFnParams()
	p.expect(token.RParen)
	body := p.parseBlock()
	return &ast.FnDecl{Token: tok, Name: name.Literal, Params: params, Body: body}
}

func (p *Parser) parseFnParams() []ast.FnParam {
	var params []ast.FnParam
	for p.cur().Type != token.RParen && p.cur().Type != token.EOF {
		name := p.expect(token.Ident)
		param := ast.FnParam{Name: name.Literal}
		if p.match(token.Eq) {
			param.Default = p.parseExpr(0)
		}
		params = append(params, param)
		if !p.match(token.Comma) {
			break
		}
	}
	return params
}

// ---------- return ----------

func (p *Parser) parseReturnStmt() ast.Stmt {
	tok := p.expect(token.KwReturn)
	var val ast.Expr
	if p.cur().Type != token.Newline && p.cur().Type != token.EOF && p.cur().Type != token.RBrace {
		val = p.parseExpr(0)
	}
	return &ast.ReturnStmt{Token: tok, Value: val}
}

// ---------- import ----------

func (p *Parser) parseImportStmt() ast.Stmt {
	tok := p.expect(token.KwImport)
	pathTok := p.expect(token.String)
	return &ast.ImportStmt{Token: tok, Path: pathTok.Literal}
}

// ---------- tuple assign ----------

func (p *Parser) isTupleAssign() bool {
	// Look ahead: [ident, ident, ...] =
	saved := p.pos
	defer func() { p.pos = saved }()

	if p.cur().Type != token.LBracket {
		return false
	}
	p.advance()
	depth := 1
	for depth > 0 && p.cur().Type != token.EOF {
		if p.cur().Type == token.LBracket {
			depth++
		} else if p.cur().Type == token.RBracket {
			depth--
		}
		p.advance()
	}
	return p.cur().Type == token.Eq
}

func (p *Parser) parseTupleAssign() ast.Stmt {
	tok := p.expect(token.LBracket)
	var names []string
	for p.cur().Type != token.RBracket && p.cur().Type != token.EOF {
		n := p.expect(token.Ident)
		names = append(names, n.Literal)
		p.match(token.Comma)
	}
	p.expect(token.RBracket)
	p.expect(token.Eq)
	val := p.parseExpr(0)
	return &ast.TupleAssign{Token: tok, Names: names, Value: val}
}

// ---------- block ----------

// parseBlock parses a block: either { stmts } or newline-indented stmts.
// For simplicity, we use braces or treat the next single line / indented lines as the block.
func (p *Parser) parseBlock() *ast.Block {
	p.skipNewlines()
	tok := p.cur()
	block := &ast.Block{Token: tok}

	if p.cur().Type == token.LBrace {
		p.advance()
		p.skipNewlines()
		for p.cur().Type != token.RBrace && p.cur().Type != token.EOF {
			stmt := p.parseStatement()
			if stmt != nil {
				block.Stmts = append(block.Stmts, stmt)
			}
			if p.cur().Type != token.RBrace {
				p.match(token.Newline)
			}
			p.skipNewlines()
		}
		p.expect(token.RBrace)
		return block
	}

	// Single-statement block (no braces, no indentation tracking).
	stmt := p.parseStatement()
	if stmt != nil {
		block.Stmts = append(block.Stmts, stmt)
	}
	return block
}

// ---------- expressions (Pratt parser) ----------

func (p *Parser) parseExpr(minPrec int) ast.Expr {
	left := p.parsePrefix()
	for {
		prec := infixPrec(p.cur().Type)
		if prec <= minPrec {
			// Check for non-arithmetic infix: call, index, dot, ternary.
			switch p.cur().Type {
			case token.LParen:
				if minPrec < precCall {
					left = p.parseCallSuffix(left)
					continue
				}
			case token.LBracket:
				if minPrec < precCall {
					left = p.parseIndexSuffix(left)
					continue
				}
			case token.Dot:
				if minPrec < precCall {
					left = p.parseDotSuffix(left)
					continue
				}
			case token.Question:
				if minPrec < precOr {
					left = p.parseTernarySuffix(left)
					continue
				}
			}
			break
		}
		op := p.advance()
		right := p.parseExpr(prec)
		left = &ast.BinaryExpr{Token: op, Left: left, Op: op.Type, Right: right}
	}
	return left
}

func (p *Parser) parsePrefix() ast.Expr {
	tok := p.cur()
	switch tok.Type {
	case token.Int:
		p.advance()
		v, _ := strconv.ParseFloat(tok.Literal, 64)
		return &ast.NumberLit{Token: tok, Value: v}
	case token.Float:
		p.advance()
		v, _ := strconv.ParseFloat(tok.Literal, 64)
		return &ast.NumberLit{Token: tok, Value: v}
	case token.String:
		p.advance()
		return &ast.StringLit{Token: tok, Value: tok.Literal}
	case token.True:
		p.advance()
		return &ast.BoolLit{Token: tok, Value: true}
	case token.False:
		p.advance()
		return &ast.BoolLit{Token: tok, Value: false}
	case token.Na:
		p.advance()
		return &ast.NaLit{Token: tok}
	case token.Ident:
		p.advance()
		return &ast.IdentExpr{Token: tok, Name: tok.Literal}
	case token.KwStrategy, token.KwInput, token.KwString, token.KwInt,
		token.KwFloat, token.KwBool, token.KwColor, token.KwSeries,
		token.KwSimple, token.KwConst:
		// These keywords can also appear as namespace identifiers in expressions.
		p.advance()
		return &ast.IdentExpr{Token: tok, Name: tok.Literal}
	case token.Minus, token.Not:
		p.advance()
		operand := p.parseExpr(precUnary)
		return &ast.UnaryExpr{Token: tok, Op: tok.Type, Operand: operand}
	case token.LParen:
		return p.parseParenOrLambda()
	case token.LBracket:
		return p.parseArrayLit()
	case token.KwIf:
		// Inline if-expression: if cond then a else b (without block syntax)
		return p.parseIfExpr()
	default:
		p.advance()
		p.errorf("unexpected token %s (%q) at line %d col %d",
			tok.Type, tok.Literal, tok.Line, tok.Col)
		return &ast.NaLit{Token: tok}
	}
}

func (p *Parser) parseParenOrLambda() ast.Expr {
	tok := p.advance() // skip (
	// Empty parens or check if this is a lambda: (params) => expr
	// For now, treat as grouped expression.
	if p.cur().Type == token.RParen {
		p.advance()
		// () => expr  — lambda with no params.
		if p.cur().Type == token.Arrow {
			p.advance()
			body := p.parseExpr(0)
			return &ast.LambdaExpr{Token: tok, Body: body}
		}
		// Empty parens — return NaLit as placeholder.
		return &ast.NaLit{Token: tok}
	}

	// Try to detect lambda: (ident, ident, ...) =>
	if p.isLambdaParams() {
		params := p.parseFnParams()
		p.expect(token.RParen)
		p.expect(token.Arrow)
		body := p.parseExpr(0)
		return &ast.LambdaExpr{Token: tok, Params: params, Body: body}
	}

	expr := p.parseExpr(0)
	p.expect(token.RParen)
	return expr
}

// isLambdaParams checks if position is at: ident[, ident]* ) =>
func (p *Parser) isLambdaParams() bool {
	saved := p.pos
	defer func() { p.pos = saved }()

	for {
		if p.cur().Type != token.Ident {
			return false
		}
		p.advance()
		// Allow default values: ident = expr
		if p.cur().Type == token.Eq {
			// Skip to comma or RParen (rough heuristic).
			p.advance()
			depth := 0
			for p.cur().Type != token.EOF {
				if p.cur().Type == token.LParen {
					depth++
				} else if p.cur().Type == token.RParen {
					if depth == 0 {
						break
					}
					depth--
				} else if p.cur().Type == token.Comma && depth == 0 {
					break
				}
				p.advance()
			}
		}
		if p.cur().Type == token.RParen {
			p.advance()
			return p.cur().Type == token.Arrow
		}
		if p.cur().Type != token.Comma {
			return false
		}
		p.advance()
	}
}

func (p *Parser) parseArrayLit() ast.Expr {
	tok := p.advance() // skip [
	var elems []ast.Expr
	for p.cur().Type != token.RBracket && p.cur().Type != token.EOF {
		elems = append(elems, p.parseExpr(0))
		if !p.match(token.Comma) {
			break
		}
	}
	p.expect(token.RBracket)
	return &ast.ArrayLit{Token: tok, Elements: elems}
}

func (p *Parser) parseIfExpr() ast.Expr {
	// Reuse parseIfStmt and wrap as an expression via a sentinel IdentExpr
	// that the interpreter knows to evaluate. In practice, the top-level
	// parser will parse if as a statement; this path handles inline use.
	stmt := p.parseIfStmt()
	_ = stmt
	tok := stmt.Pos()
	return &ast.IdentExpr{Token: tok, Name: "__if_expr__"}
}

// ---------- infix / postfix suffixes ----------

func (p *Parser) parseCallSuffix(callee ast.Expr) ast.Expr {
	tok := p.advance() // skip (
	args := p.parseCallArgs()
	p.expect(token.RParen)
	return &ast.CallExpr{Token: tok, Callee: callee, Args: args}
}

func (p *Parser) parseCallArgs() []ast.CallArg {
	var args []ast.CallArg
	for p.cur().Type != token.RParen && p.cur().Type != token.EOF {
		p.skipNewlines()
		if p.cur().Type == token.RParen {
			break
		}
		// Named arg: ident = expr
		if p.cur().Type == token.Ident && p.peekRaw().Type == token.Eq {
			name := p.advance()
			p.advance() // skip =
			val := p.parseExpr(0)
			args = append(args, ast.CallArg{Name: name.Literal, Value: val})
		} else {
			val := p.parseExpr(0)
			args = append(args, ast.CallArg{Value: val})
		}
		p.skipNewlines()
		if !p.match(token.Comma) {
			break
		}
	}
	return args
}

func (p *Parser) parseIndexSuffix(left ast.Expr) ast.Expr {
	tok := p.advance() // skip [
	idx := p.parseExpr(0)
	p.expect(token.RBracket)
	return &ast.IndexExpr{Token: tok, Left: left, Index: idx}
}

func (p *Parser) parseDotSuffix(left ast.Expr) ast.Expr {
	tok := p.advance() // skip .
	field := p.expect(token.Ident)
	return &ast.DotExpr{Token: tok, Object: left, Field: field.Literal}
}

func (p *Parser) parseTernarySuffix(cond ast.Expr) ast.Expr {
	tok := p.advance() // skip ?
	then := p.parseExpr(0)
	p.expect(token.Colon)
	els := p.parseExpr(0)
	return &ast.TernaryExpr{Token: tok, Condition: cond, Then: then, Else: els}
}
