// Package lexer tokenises Toktik DSL source text.
package lexer

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Cyvadra/toktik/pkg/dsl/token"
)

// Lexer produces tokens from source text.
type Lexer struct {
	src   []byte
	pos   int  // current byte offset
	line  int  // 1-based line number
	col   int  // 1-based column
	ch    rune // current character (0 at EOF)
	width int  // byte width of ch
}

// New creates a lexer for the given source. The source must be valid UTF-8.
func New(src string) *Lexer {
	l := &Lexer{src: []byte(src), line: 1, col: 0}
	l.advance()
	return l
}

// Tokenize returns all tokens from the source. The last token is always EOF.
func Tokenize(src string) ([]token.Token, error) {
	l := New(src)
	var tokens []token.Token
	for {
		tok := l.Next()
		tokens = append(tokens, tok)
		if tok.Type == token.EOF {
			break
		}
		if tok.Type == token.Illegal {
			return tokens, fmt.Errorf("line %d col %d: illegal token %q", tok.Line, tok.Col, tok.Literal)
		}
	}
	return tokens, nil
}

// Next returns the next token.
func (l *Lexer) Next() token.Token {
	l.skipWhitespaceAndComments()

	line, col := l.line, l.col

	if l.ch == 0 {
		return token.Token{Type: token.EOF, Literal: "", Line: line, Col: col}
	}

	// Newlines are statement separators.
	if l.ch == '\n' {
		l.advance()
		return token.Token{Type: token.Newline, Literal: "\\n", Line: line, Col: col}
	}

	// String literals.
	if l.ch == '"' || l.ch == '\'' {
		return l.readString(line, col)
	}

	// Numbers.
	if l.ch >= '0' && l.ch <= '9' {
		return l.readNumber(line, col)
	}

	// Identifiers / keywords.
	if isIdentStart(l.ch) {
		return l.readIdent(line, col)
	}

	// Two-char operators first, then single-char.
	if tok, ok := l.tryTwoChar(line, col); ok {
		return tok
	}
	return l.readSingleChar(line, col)
}

// advance moves to the next character.
func (l *Lexer) advance() {
	if l.pos >= len(l.src) {
		l.ch = 0
		l.width = 0
		return
	}
	r, w := utf8.DecodeRune(l.src[l.pos:])
	if l.ch == '\n' {
		l.line++
		l.col = 0
	}
	l.ch = r
	l.width = w
	l.pos += w
	l.col++
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRune(l.src[l.pos:])
	return r
}

// skipWhitespaceAndComments skips spaces, tabs, carriage returns, and comments.
// Newlines are NOT skipped here because they serve as statement separators.
func (l *Lexer) skipWhitespaceAndComments() {
	for {
		// Skip non-newline whitespace.
		for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
			l.advance()
		}
		// Line continuation: backslash immediately before newline.
		if l.ch == '\\' && l.peek() == '\n' {
			l.advance() // skip backslash
			l.advance() // skip newline
			continue
		}
		// Single-line comment: // ... until end of line.
		if l.ch == '/' && l.peek() == '/' {
			for l.ch != '\n' && l.ch != 0 {
				l.advance()
			}
			continue
		}
		// Block comment: /* ... */ (not nested).
		if l.ch == '/' && l.peek() == '*' {
			l.advance() // skip /
			l.advance() // skip *
			for {
				if l.ch == 0 {
					break
				}
				if l.ch == '*' && l.peek() == '/' {
					l.advance() // skip *
					l.advance() // skip /
					break
				}
				l.advance()
			}
			continue
		}
		break
	}
}

func (l *Lexer) readString(line, col int) token.Token {
	quote := l.ch
	l.advance() // skip opening quote
	var b strings.Builder
	for l.ch != quote {
		if l.ch == 0 || l.ch == '\n' {
			return token.Token{Type: token.Illegal, Literal: b.String(), Line: line, Col: col}
		}
		if l.ch == '\\' {
			l.advance()
			switch l.ch {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case '\'':
				b.WriteByte('\'')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteByte('\\')
				b.WriteRune(l.ch)
			}
		} else {
			b.WriteRune(l.ch)
		}
		l.advance()
	}
	l.advance() // skip closing quote
	return token.Token{Type: token.String, Literal: b.String(), Line: line, Col: col}
}

func (l *Lexer) readNumber(line, col int) token.Token {
	var b strings.Builder
	isFloat := false
	for l.ch >= '0' && l.ch <= '9' {
		b.WriteRune(l.ch)
		l.advance()
	}
	if l.ch == '.' && l.peek() >= '0' && l.peek() <= '9' {
		isFloat = true
		b.WriteRune(l.ch)
		l.advance()
		for l.ch >= '0' && l.ch <= '9' {
			b.WriteRune(l.ch)
			l.advance()
		}
	}
	// Scientific notation: e/E
	if l.ch == 'e' || l.ch == 'E' {
		isFloat = true
		b.WriteRune(l.ch)
		l.advance()
		if l.ch == '+' || l.ch == '-' {
			b.WriteRune(l.ch)
			l.advance()
		}
		for l.ch >= '0' && l.ch <= '9' {
			b.WriteRune(l.ch)
			l.advance()
		}
	}
	// Trailing dot with no digit after makes it a float: 3.
	if !isFloat && l.ch == '.' && !(l.peek() >= '0' && l.peek() <= '9') {
		isFloat = true
		b.WriteRune(l.ch)
		l.advance()
	}
	tt := token.Int
	if isFloat {
		tt = token.Float
	}
	return token.Token{Type: tt, Literal: b.String(), Line: line, Col: col}
}

func (l *Lexer) readIdent(line, col int) token.Token {
	var b strings.Builder
	for isIdentPart(l.ch) {
		b.WriteRune(l.ch)
		l.advance()
	}
	lit := b.String()
	return token.Token{Type: token.LookupIdent(lit), Literal: lit, Line: line, Col: col}
}

func (l *Lexer) tryTwoChar(line, col int) (token.Token, bool) {
	mk := func(tt token.Type, lit string) (token.Token, bool) {
		l.advance()
		l.advance()
		return token.Token{Type: tt, Literal: lit, Line: line, Col: col}, true
	}
	p := l.peek()
	switch l.ch {
	case '=':
		if p == '=' {
			return mk(token.EqEq, "==")
		}
		if p == '>' {
			return mk(token.Arrow, "=>")
		}
	case '!':
		if p == '=' {
			return mk(token.BangEq, "!=")
		}
	case '<':
		if p == '=' {
			return mk(token.LtEq, "<=")
		}
	case '>':
		if p == '=' {
			return mk(token.GtEq, ">=")
		}
	case ':':
		if p == '=' {
			return mk(token.ColonEq, ":=")
		}
	case '+':
		if p == '+' {
			return mk(token.PlusPlus, "++")
		}
		if p == '=' {
			return mk(token.PlusEq, "+=")
		}
	case '-':
		if p == '-' {
			return mk(token.MinusMinus, "--")
		}
		if p == '=' {
			return mk(token.MinusEq, "-=")
		}
	case '*':
		if p == '=' {
			return mk(token.StarEq, "*=")
		}
	case '/':
		if p == '=' {
			return mk(token.SlashEq, "/=")
		}
	case '%':
		if p == '=' {
			return mk(token.PercentEq, "%=")
		}
	}
	return token.Token{}, false
}

func (l *Lexer) readSingleChar(line, col int) token.Token {
	ch := l.ch
	l.advance()
	switch ch {
	case '+':
		return token.Token{Type: token.Plus, Literal: "+", Line: line, Col: col}
	case '-':
		return token.Token{Type: token.Minus, Literal: "-", Line: line, Col: col}
	case '*':
		return token.Token{Type: token.Star, Literal: "*", Line: line, Col: col}
	case '/':
		return token.Token{Type: token.Slash, Literal: "/", Line: line, Col: col}
	case '%':
		return token.Token{Type: token.Percent, Literal: "%", Line: line, Col: col}
	case '=':
		return token.Token{Type: token.Eq, Literal: "=", Line: line, Col: col}
	case '<':
		return token.Token{Type: token.Lt, Literal: "<", Line: line, Col: col}
	case '>':
		return token.Token{Type: token.Gt, Literal: ">", Line: line, Col: col}
	case '(':
		return token.Token{Type: token.LParen, Literal: "(", Line: line, Col: col}
	case ')':
		return token.Token{Type: token.RParen, Literal: ")", Line: line, Col: col}
	case '[':
		return token.Token{Type: token.LBracket, Literal: "[", Line: line, Col: col}
	case ']':
		return token.Token{Type: token.RBracket, Literal: "]", Line: line, Col: col}
	case '{':
		return token.Token{Type: token.LBrace, Literal: "{", Line: line, Col: col}
	case '}':
		return token.Token{Type: token.RBrace, Literal: "}", Line: line, Col: col}
	case ',':
		return token.Token{Type: token.Comma, Literal: ",", Line: line, Col: col}
	case '.':
		return token.Token{Type: token.Dot, Literal: ".", Line: line, Col: col}
	case '?':
		return token.Token{Type: token.Question, Literal: "?", Line: line, Col: col}
	case ':':
		return token.Token{Type: token.Colon, Literal: ":", Line: line, Col: col}
	case '!':
		return token.Token{Type: token.Not, Literal: "!", Line: line, Col: col}
	}
	return token.Token{Type: token.Illegal, Literal: string(ch), Line: line, Col: col}
}

func isIdentStart(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch)
}

func isIdentPart(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch) || unicode.IsDigit(ch)
}
