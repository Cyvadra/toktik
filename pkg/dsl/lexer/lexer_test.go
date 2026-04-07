package lexer

import (
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/token"
)

func TestBasicTokens(t *testing.T) {
	src := `x = 42 + 3.14`
	tokens, err := Tokenize(src)
	if err != nil {
		t.Fatal(err)
	}
	expected := []token.Type{
		token.Ident, token.Eq, token.Int, token.Plus, token.Float, token.EOF,
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token[%d]: expected %s, got %s (%q)", i, tt, tokens[i].Type, tokens[i].Literal)
		}
	}
}

func TestKeywords(t *testing.T) {
	src := `if else for while fn return var varip strategy input`
	tokens, _ := Tokenize(src)
	expected := []token.Type{
		token.KwIf, token.KwElse, token.KwFor, token.KwWhile,
		token.KwFn, token.KwReturn, token.KwVar, token.KwVarip,
		token.KwStrategy, token.KwInput, token.EOF,
	}
	for i, tt := range expected {
		if i >= len(tokens) {
			t.Fatalf("not enough tokens")
		}
		if tokens[i].Type != tt {
			t.Errorf("token[%d]: expected %s, got %s", i, tt, tokens[i].Type)
		}
	}
}

func TestOperators(t *testing.T) {
	src := `:= == != <= >= => += -= *= /= %=`
	tokens, _ := Tokenize(src)
	expected := []token.Type{
		token.ColonEq, token.EqEq, token.BangEq, token.LtEq, token.GtEq,
		token.Arrow, token.PlusEq, token.MinusEq, token.StarEq, token.SlashEq,
		token.PercentEq, token.EOF,
	}
	for i, tt := range expected {
		if i >= len(tokens) {
			t.Fatalf("not enough tokens")
		}
		if tokens[i].Type != tt {
			t.Errorf("token[%d]: expected %s, got %s (%q)", i, tt, tokens[i].Type, tokens[i].Literal)
		}
	}
}

func TestStringLiterals(t *testing.T) {
	src := `"hello" 'world' "escaped\n\t"`
	tokens, _ := Tokenize(src)
	if tokens[0].Type != token.String || tokens[0].Literal != "hello" {
		t.Errorf("expected string hello, got %s %q", tokens[0].Type, tokens[0].Literal)
	}
	if tokens[1].Type != token.String || tokens[1].Literal != "world" {
		t.Errorf("expected string world, got %s %q", tokens[1].Type, tokens[1].Literal)
	}
	if tokens[2].Type != token.String || tokens[2].Literal != "escaped\n\t" {
		t.Errorf("expected escaped string, got %q", tokens[2].Literal)
	}
}

func TestComments(t *testing.T) {
	src := "x = 1 // comment\ny = 2"
	tokens, _ := Tokenize(src)
	// x = 1 \n y = 2 EOF
	types := make([]token.Type, len(tokens))
	for i, tok := range tokens {
		types[i] = tok.Type
	}
	expected := []token.Type{
		token.Ident, token.Eq, token.Int, token.Newline,
		token.Ident, token.Eq, token.Int, token.EOF,
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), types)
	}
}

func TestScientificNotation(t *testing.T) {
	src := `6.02e23 1E10`
	tokens, _ := Tokenize(src)
	if tokens[0].Type != token.Float || tokens[0].Literal != "6.02e23" {
		t.Errorf("expected float 6.02e23, got %s %q", tokens[0].Type, tokens[0].Literal)
	}
	if tokens[1].Type != token.Float || tokens[1].Literal != "1E10" {
		t.Errorf("expected float 1E10, got %s %q", tokens[1].Type, tokens[1].Literal)
	}
}

func TestNewlineAsSeparator(t *testing.T) {
	src := "a\nb\nc"
	tokens, _ := Tokenize(src)
	expected := []token.Type{
		token.Ident, token.Newline, token.Ident, token.Newline, token.Ident, token.EOF,
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
}

func TestLineContinuation(t *testing.T) {
	// Backslash before newline should merge lines.
	src := "a + \\\nb"
	tokens, _ := Tokenize(src)
	expected := []token.Type{
		token.Ident, token.Plus, token.Ident, token.EOF,
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
}
