package parser

import (
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/ast"
)

func TestParseSimpleAssign(t *testing.T) {
	prog, errs := Parse("x = 42")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(prog.Stmts))
	}
	vd, ok := prog.Stmts[0].(*ast.VarDecl)
	if !ok {
		t.Fatalf("expected VarDecl, got %T", prog.Stmts[0])
	}
	if vd.Name != "x" {
		t.Errorf("expected name x, got %s", vd.Name)
	}
	num, ok := vd.Value.(*ast.NumberLit)
	if !ok {
		t.Fatalf("expected NumberLit, got %T", vd.Value)
	}
	if num.Value != 42 {
		t.Errorf("expected 42, got %f", num.Value)
	}
}

func TestParseBinaryExpr(t *testing.T) {
	prog, errs := Parse("x = 1 + 2 * 3")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	vd := prog.Stmts[0].(*ast.VarDecl)
	// Should be 1 + (2 * 3) due to precedence.
	bin, ok := vd.Value.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", vd.Value)
	}
	left, ok := bin.Left.(*ast.NumberLit)
	if !ok || left.Value != 1 {
		t.Errorf("expected left=1, got %T", bin.Left)
	}
	right, ok := bin.Right.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected right=BinaryExpr, got %T", bin.Right)
	}
	if right.Left.(*ast.NumberLit).Value != 2 || right.Right.(*ast.NumberLit).Value != 3 {
		t.Error("right subtree incorrect")
	}
}

func TestParseCallExpr(t *testing.T) {
	prog, errs := Parse(`ta.sma(close, 14)`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	es := prog.Stmts[0].(*ast.ExprStmt)
	call, ok := es.Expression.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", es.Expression)
	}
	if len(call.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(call.Args))
	}
}

func TestParseStrategyDecl(t *testing.T) {
	prog, errs := Parse(`strategy("My Strategy", overlay=true)`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	sd, ok := prog.Stmts[0].(*ast.StrategyDecl)
	if !ok {
		t.Fatalf("expected StrategyDecl, got %T", prog.Stmts[0])
	}
	if len(sd.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(sd.Args))
	}
	if sd.Args[1].Name != "overlay" {
		t.Errorf("expected named arg overlay, got %q", sd.Args[1].Name)
	}
}

func TestParseIfStmt(t *testing.T) {
	src := "if x > 0 {\n  y = 1\n}"
	prog, errs := Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(prog.Stmts) < 1 {
		t.Fatal("no statements")
	}
	_, ok := prog.Stmts[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", prog.Stmts[0])
	}
}

func TestParseForStmt(t *testing.T) {
	src := "for i = 0 to 10 by 2 {\n  x = i\n}"
	prog, errs := Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	fs, ok := prog.Stmts[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", prog.Stmts[0])
	}
	if fs.Var != "i" {
		t.Errorf("expected var i, got %s", fs.Var)
	}
}

func TestParseFnDecl(t *testing.T) {
	src := "fn add(a, b) {\n  return a + b\n}"
	prog, errs := Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	fd, ok := prog.Stmts[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected FnDecl, got %T", prog.Stmts[0])
	}
	if fd.Name != "add" {
		t.Errorf("expected fn name add, got %s", fd.Name)
	}
	if len(fd.Params) != 2 {
		t.Errorf("expected 2 params, got %d", len(fd.Params))
	}
}

func TestParseDotAccess(t *testing.T) {
	prog, errs := Parse("x = strategy.long")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	vd := prog.Stmts[0].(*ast.VarDecl)
	dot, ok := vd.Value.(*ast.DotExpr)
	if !ok {
		t.Fatalf("expected DotExpr, got %T", vd.Value)
	}
	if dot.Field != "long" {
		t.Errorf("expected field long, got %s", dot.Field)
	}
}

func TestParseIndex(t *testing.T) {
	prog, errs := Parse("x = close[1]")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	vd := prog.Stmts[0].(*ast.VarDecl)
	idx, ok := vd.Value.(*ast.IndexExpr)
	if !ok {
		t.Fatalf("expected IndexExpr, got %T", vd.Value)
	}
	ident := idx.Left.(*ast.IdentExpr)
	if ident.Name != "close" {
		t.Errorf("expected close, got %s", ident.Name)
	}
}

func TestParseTernary(t *testing.T) {
	prog, errs := Parse("x = a > 0 ? 1 : 0")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	vd := prog.Stmts[0].(*ast.VarDecl)
	_, ok := vd.Value.(*ast.TernaryExpr)
	if !ok {
		t.Fatalf("expected TernaryExpr, got %T", vd.Value)
	}
}

func TestParseVarDecl(t *testing.T) {
	prog, errs := Parse("var sum = 0")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	vd := prog.Stmts[0].(*ast.VarDecl)
	if !vd.Persist {
		t.Error("expected Persist=true")
	}
	if vd.Name != "sum" {
		t.Errorf("expected name sum, got %s", vd.Name)
	}
}
