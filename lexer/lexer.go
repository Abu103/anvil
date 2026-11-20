// Package lexer turns Anvil source text into tokens with 1-based line and
// column positions. Every later phase uses those positions for error messages.
package lexer

import (
	"fmt"
	"strconv"
	"strings"
)

// TokenKind is the category of a token.
type TokenKind int

const (
	EOF TokenKind = iota
	NUMBER
	IDENT

	// Keywords.
	FN
	LET
	IF
	ELSE
	WHILE
	RETURN
	PRINT
	TRUE
	FALSE
	INT
	BOOL

	// Operators.
	PLUS
	MINUS
	STAR
	SLASH
	EQEQ
	NEQ
	LT
	GT
	LE
	GE
	ANDAND
	OROR
	BANG
	EQUAL

	// Punctuation.
	LPAREN
	RPAREN
	LBRACE
	RBRACE
	COMMA
	SEMICOLON
	COLON
	ARROW
)

var keywords = map[string]TokenKind{
	"fn": FN, "let": LET, "if": IF, "else": ELSE, "while": WHILE,
	"return": RETURN, "print": PRINT, "true": TRUE, "false": FALSE,
	"int": INT, "bool": BOOL,
}

var kindNames = map[TokenKind]string{
	EOF: "EOF", NUMBER: "NUMBER", IDENT: "IDENT",
	FN: "fn", LET: "let", IF: "if", ELSE: "else", WHILE: "while",
	RETURN: "return", PRINT: "print", TRUE: "true", FALSE: "false",
	INT: "int", BOOL: "bool",
	PLUS: "+", MINUS: "-", STAR: "*", SLASH: "/",
	EQEQ: "==", NEQ: "!=", LT: "<", GT: ">", LE: "<=", GE: ">=",
	ANDAND: "&&", OROR: "||", BANG: "!", EQUAL: "=",
	LPAREN: "(", RPAREN: ")", LBRACE: "{", RBRACE: "}",
	COMMA: ",", SEMICOLON: ";", COLON: ":", ARROW: "->",
}

// String returns the token's spelling, used in parser and type errors.
func (k TokenKind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("TokenKind(%d)", int(k))
}

// Token is one lexical item. Text is the exact source spelling; EOF has ""
// as its text.
type Token struct {
	Kind TokenKind
	Text string
	Line int
	Col  int
}

// Lex scans src into tokens, ending with a single EOF token. It reports the
// first bad character or out-of-range integer with a position.
func Lex(src string) ([]Token, error) {
	l := &scanner{src: src, line: 1, col: 1}
	return l.scan()
}

type scanner struct {
	src       string
	pos       int // byte offset into src
	line, col int
}

func (s *scanner) peek() byte {
	if s.pos >= len(s.src) {
		return 0
	}
	return s.src[s.pos]
}

func (s *scanner) peekNext() byte {
	if s.pos+1 >= len(s.src) {
		return 0
	}
	return s.src[s.pos+1]
}

func (s *scanner) advance() byte {
	ch := s.src[s.pos]
	s.pos++
	if ch == '\n' {
		s.line++
		s.col = 1
	} else {
		s.col++
	}
	return ch
}

func (s *scanner) at() (int, int) { return s.line, s.col }

func (s *scanner) errorf(line, col int, format string, args ...any) error {
	return fmt.Errorf("lexer: line %d, column %d: %s", line, col, fmt.Sprintf(format, args...))
}

func (s *scanner) scan() ([]Token, error) {
	var toks []Token
	for s.pos < len(s.src) {
		ch := s.peek()

		switch {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n':
			s.advance()

		case ch == '/' && s.peekNext() == '/':
			for s.pos < len(s.src) && s.peek() != '\n' {
				s.advance()
			}

		case isDigit(ch):
			line, col := s.at()
			var text strings.Builder
			for isDigit(s.peek()) {
				text.WriteByte(s.advance())
			}
			lit := text.String()
			if _, err := strconv.ParseInt(lit, 10, 64); err != nil {
				return nil, s.errorf(line, col, "integer %s does not fit in int64", lit)
			}
			toks = append(toks, Token{NUMBER, lit, line, col})

		case isIdentStart(ch):
			line, col := s.at()
			var text strings.Builder
			for isIdentPart(s.peek()) {
				text.WriteByte(s.advance())
			}
			lit := text.String()
			kind, ok := keywords[lit]
			if !ok {
				kind = IDENT
			}
			toks = append(toks, Token{kind, lit, line, col})

		default:
			line, col := s.at()
			kind, ok := s.operator()
			if !ok {
				return nil, s.errorf(line, col, "unexpected character %q", string(ch))
			}
			toks = append(toks, Token{kind, s.src[s.pos-operatorWidth(kind) : s.pos], line, col})
		}
	}

	line, col := s.at()
	return append(toks, Token{EOF, "", line, col}), nil
}

// operator consumes a one- or two-character operator and reports how many
// bytes it consumed through operatorWidth.
func (s *scanner) operator() (TokenKind, bool) {
	pairs := []struct {
		text string
		kind TokenKind
	}{
		{"==", EQEQ}, {"!=", NEQ}, {"<=", LE}, {">=", GE},
		{"&&", ANDAND}, {"||", OROR}, {"->", ARROW},
	}
	for _, p := range pairs {
		if s.peek() == p.text[0] && s.peekNext() == p.text[1] {
			s.advance()
			s.advance()
			return p.kind, true
		}
	}

	var kind TokenKind
	switch s.peek() {
	case '+':
		kind = PLUS
	case '-':
		kind = MINUS
	case '*':
		kind = STAR
	case '/':
		kind = SLASH
	case '<':
		kind = LT
	case '>':
		kind = GT
	case '!':
		kind = BANG
	case '=':
		kind = EQUAL
	case '(':
		kind = LPAREN
	case ')':
		kind = RPAREN
	case '{':
		kind = LBRACE
	case '}':
		kind = RBRACE
	case ',':
		kind = COMMA
	case ';':
		kind = SEMICOLON
	case ':':
		kind = COLON
	default:
		return 0, false
	}
	s.advance()
	return kind, true
}

func operatorWidth(k TokenKind) int {
	switch k {
	case EQEQ, NEQ, LE, GE, ANDAND, OROR, ARROW:
		return 2
	default:
		return 1
	}
}

func isDigit(ch byte) bool { return ch >= '0' && ch <= '9' }

func isIdentStart(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isIdentPart(ch byte) bool { return isIdentStart(ch) || isDigit(ch) }
