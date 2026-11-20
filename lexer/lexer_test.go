package lexer

import (
	"strings"
	"testing"
)

func TestLexEmptyInputProducesOnlyEOF(t *testing.T) {
	toks, err := Lex("")
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	if len(toks) != 1 || toks[0].Kind != EOF {
		t.Fatalf("tokens = %+v, want a single EOF", toks)
	}
	if toks[0].Line != 1 || toks[0].Col != 1 {
		t.Fatalf("EOF at %d:%d, want 1:1", toks[0].Line, toks[0].Col)
	}
}

func TestLexSingleOperatorsAndPunctuation(t *testing.T) {
	cases := []struct {
		src  string
		kind TokenKind
	}{
		{"+", PLUS}, {"-", MINUS}, {"*", STAR}, {"/", SLASH},
		{"<", LT}, {">", GT}, {"=", EQUAL}, {"!", BANG},
		{"(", LPAREN}, {")", RPAREN}, {"{", LBRACE}, {"}", RBRACE},
		{",", COMMA}, {";", SEMICOLON}, {":", COLON},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			toks, err := Lex(tc.src)
			if err != nil {
				t.Fatalf("Lex: %v", err)
			}
			if len(toks) != 2 {
				t.Fatalf("got %d tokens, want 2: %+v", len(toks), toks)
			}
			if toks[0].Kind != tc.kind {
				t.Fatalf("kind = %v, want %v", toks[0].Kind, tc.kind)
			}
			if toks[0].Text != tc.src {
				t.Fatalf("text = %q, want %q", toks[0].Text, tc.src)
			}
		})
	}
}

func TestLexTwoCharacterOperators(t *testing.T) {
	cases := []struct {
		src  string
		kind TokenKind
	}{
		{"==", EQEQ}, {"!=", NEQ}, {"<=", LE}, {">=", GE},
		{"&&", ANDAND}, {"||", OROR}, {"->", ARROW},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			toks, err := Lex(tc.src)
			if err != nil {
				t.Fatalf("Lex: %v", err)
			}
			if len(toks) != 2 || toks[0].Kind != tc.kind || toks[0].Text != tc.src {
				t.Fatalf("tokens = %+v, want single %v(%q)", toks, tc.kind, tc.src)
			}
		})
	}
}

func TestLexKeywords(t *testing.T) {
	words := []string{"fn", "let", "if", "else", "while", "return", "print", "true", "false", "int", "bool"}
	want := []TokenKind{FN, LET, IF, ELSE, WHILE, RETURN, PRINT, TRUE, FALSE, INT, BOOL}

	src := strings.Join(words, " ")
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	if len(toks) != len(words)+1 {
		t.Fatalf("got %d tokens, want %d", len(toks), len(words)+1)
	}
	for i := range words {
		if toks[i].Kind != want[i] || toks[i].Text != words[i] {
			t.Fatalf("token %d = (%v,%q), want (%v,%q)", i, toks[i].Kind, toks[i].Text, want[i], words[i])
		}
	}
}

func TestLexIdentifierVariants(t *testing.T) {
	toks, err := Lex("_x9 alpha123 fnOrd FN")
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	wantText := []string{"_x9", "alpha123", "fnOrd", "FN"}
	for i, w := range wantText {
		if toks[i].Kind != IDENT || toks[i].Text != w {
			t.Fatalf("token %d = (%v,%q), want IDENT(%q)", i, toks[i].Kind, toks[i].Text, w)
		}
	}
}

func TestLexFunctionSignatureWithPositions(t *testing.T) {
	src := "fn f(x: int) -> int { return x + 1; }"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}

	want := []Token{
		{FN, "fn", 1, 1}, {IDENT, "f", 1, 4}, {LPAREN, "(", 1, 5},
		{IDENT, "x", 1, 6}, {COLON, ":", 1, 7}, {INT, "int", 1, 9},
		{RPAREN, ")", 1, 12}, {ARROW, "->", 1, 14}, {INT, "int", 1, 17},
		{LBRACE, "{", 1, 21}, {RETURN, "return", 1, 23}, {IDENT, "x", 1, 30},
		{PLUS, "+", 1, 32}, {NUMBER, "1", 1, 34}, {SEMICOLON, ";", 1, 35},
		{RBRACE, "}", 1, 37}, {EOF, "", 1, len(src) + 1},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Fatalf("token %d = %+v, want %+v", i, toks[i], want[i])
		}
	}
}

func TestLexTracksLineAndColumn(t *testing.T) {
	src := "let x: int = 1;\n\n  print(x);\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}

	// The two print tokens and its parenthesis are on line 3.
	var printTok Token
	for _, tok := range toks {
		if tok.Kind == PRINT {
			printTok = tok
		}
	}
	if printTok.Line != 3 || printTok.Col != 3 {
		t.Fatalf("print at %d:%d, want 3:3", printTok.Line, printTok.Col)
	}
	if toks[len(toks)-1].Line != 4 {
		t.Fatalf("EOF on line %d, want 4", toks[len(toks)-1].Line)
	}
}

func TestLexSkipsCommentsAndWhitespace(t *testing.T) {
	src := "// leading comment\nlet x: int = 1; // trailing\n\tprint(x);\r\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	var kinds []TokenKind
	for _, tok := range toks {
		kinds = append(kinds, tok.Kind)
	}
	want := []TokenKind{LET, IDENT, COLON, INT, EQUAL, NUMBER, SEMICOLON, PRINT, LPAREN, IDENT, RPAREN, SEMICOLON, EOF}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
}

func TestLexNegativeNumberIsMinusThenNumber(t *testing.T) {
	// The spec leaves sign handling to the parser, so `a -1` and `a - 1`
	// tokenize identically.
	for _, src := range []string{"-1", "a -1", "a - 1"} {
		toks, err := Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", src, err)
		}
		found := false
		for i := 0; i+1 < len(toks); i++ {
			if toks[i].Kind == MINUS && toks[i+1].Kind == NUMBER && toks[i+1].Text == "1" {
				found = true
			}
		}
		if src == "-1" {
			found = toks[0].Kind == MINUS && toks[1].Kind == NUMBER
		}
		if !found {
			t.Fatalf("Lex(%q) = %+v, want a MINUS followed by NUMBER(1)", src, toks)
		}
	}
}

func TestLexIntegerBounds(t *testing.T) {
	for _, src := range []string{"0", "9223372036854775807"} {
		if _, err := Lex(src); err != nil {
			t.Fatalf("Lex(%q): %v", src, err)
		}
	}
	if _, err := Lex("9223372036854775808"); err == nil {
		t.Fatal("accepted an integer larger than int64")
	}
}

func TestLexReportsUnknownCharacterWithPosition(t *testing.T) {
	_, err := Lex("let x = 1;\nprint(@x);\n")
	if err == nil {
		t.Fatal("Lex accepted '@'")
	}
	msg := err.Error()
	for _, want := range []string{"@", "2", "7"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
}

func TestLexReportsLoneAmpersand(t *testing.T) {
	if _, err := Lex("true & false"); err == nil {
		t.Fatal("Lex accepted a single '&'")
	}
}

func TestTokenKindString(t *testing.T) {
	if got := EQEQ.String(); got != "==" {
		t.Fatalf("EQEQ.String() = %q, want %q", got, "==")
	}
	if got := EOF.String(); got != "EOF" {
		t.Fatalf("EOF.String() = %q, want %q", got, "EOF")
	}
}
