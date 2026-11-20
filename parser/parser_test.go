package parser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"anvil/ast"
	"anvil/lexer"
)

func parseSource(t *testing.T, src string) *ast.Program {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return prog
}

// printExpr parses print(<src>); and returns the printed expression.
func printExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	prog := parseSource(t, "print("+src+");")
	if len(prog.Main) != 1 {
		t.Fatalf("got %d top-level statements, want 1", len(prog.Main))
	}
	st, ok := prog.Main[0].(*ast.PrintStmt)
	if !ok {
		t.Fatalf("statement is %T, want *ast.PrintStmt", prog.Main[0])
	}
	return st.Value
}

func binary(t *testing.T, e ast.Expr, wantOp string) *ast.BinaryExpr {
	t.Helper()
	b, ok := e.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expression %T is not a BinaryExpr", e)
	}
	if b.Op != wantOp {
		t.Fatalf("operator = %q, want %q", b.Op, wantOp)
	}
	return b
}

func TestParseEmptyProgram(t *testing.T) {
	prog := parseSource(t, "")
	if len(prog.Funcs) != 0 || len(prog.Main) != 0 {
		t.Fatalf("program is not empty: %+v", prog)
	}
}

func TestMultiplyNestedInsideAdd(t *testing.T) {
	add := binary(t, printExpr(t, "1 + 2 * 3"), "+")
	if _, ok := add.Left.(*ast.IntLit); !ok {
		t.Fatalf("add.Left is %T, want IntLit", add.Left)
	}
	mul := binary(t, add.Right, "*")
	if mul.Left.Pos() != (ast.Position{Line: 1, Col: 11}) {
		t.Fatalf("the 2 is at %v, want 1:11", mul.Left.Pos())
	}
}

func TestSubtractIsLeftAssociative(t *testing.T) {
	outer := binary(t, printExpr(t, "a - b - c"), "-")
	inner := binary(t, outer.Left, "-")
	if id, ok := inner.Left.(*ast.Ident); !ok || id.Name != "a" {
		t.Fatalf("leftmost operand = %T %+v, want Ident a", inner.Left, inner.Left)
	}
	if id, ok := outer.Right.(*ast.Ident); !ok || id.Name != "c" {
		t.Fatalf("rightmost operand = %T %+v, want Ident c", outer.Right, outer.Right)
	}
}

func TestComparisonsAreLeftAssociative(t *testing.T) {
	outer := binary(t, printExpr(t, "a < b < c"), "<")
	binary(t, outer.Left, "<")
}

func TestUnaryOperatorsBindTighterThanMultiply(t *testing.T) {
	mul := binary(t, printExpr(t, "-a * !b"), "*")
	u1, ok := mul.Left.(*ast.UnaryExpr)
	if !ok || u1.Op != "-" {
		t.Fatalf("left = %T %+v, want unary -", mul.Left, mul.Left)
	}
	u2, ok := mul.Right.(*ast.UnaryExpr)
	if !ok || u2.Op != "!" {
		t.Fatalf("right = %T %+v, want unary !", mul.Right, mul.Right)
	}
}

func TestFullPrecedenceLadder(t *testing.T) {
	// || < && < == < < < + < *
	or := binary(t, printExpr(t, "1 || 2 && 3 == 4 < 5 + 6 * 7"), "||")
	and := binary(t, or.Right, "&&")
	eq := binary(t, and.Right, "==")
	lt := binary(t, eq.Right, "<")
	add := binary(t, lt.Right, "+")
	binary(t, add.Right, "*")
}

func TestGroupingOverridesPrecedence(t *testing.T) {
	mul := binary(t, printExpr(t, "(1 + 2) * 3"), "*")
	binary(t, mul.Left, "+")
}

func TestCallArgumentsAndPrecedence(t *testing.T) {
	call, ok := printExpr(t, "gcd(a + 1, b * 2)").(*ast.CallExpr)
	if !ok {
		t.Fatalf("expression is %T, want CallExpr", printExpr(t, "gcd(a + 1, b * 2)"))
	}
	if call.Callee != "gcd" || len(call.Args) != 2 {
		t.Fatalf("call = %+v, want gcd with 2 args", call)
	}
	binary(t, call.Args[0], "+")
	binary(t, call.Args[1], "*")
}

func TestFunctionDeclaration(t *testing.T) {
	prog := parseSource(t, "fn add(a: int, b: bool) -> int { return a; }")
	if len(prog.Funcs) != 1 || len(prog.Main) != 0 {
		t.Fatalf("program = %+v", prog)
	}
	f := prog.Funcs[0]
	if f.Name != "add" || f.ReturnType != "int" {
		t.Fatalf("function = %+v", f)
	}
	if len(f.Params) != 2 || f.Params[0].Name != "a" || f.Params[0].Type != "int" ||
		f.Params[1].Name != "b" || f.Params[1].Type != "bool" {
		t.Fatalf("params = %+v", f.Params)
	}
	if len(f.Body.Stmts) != 1 {
		t.Fatalf("body has %d statements", len(f.Body.Stmts))
	}
	if _, ok := f.Body.Stmts[0].(*ast.ReturnStmt); !ok {
		t.Fatalf("body[0] is %T", f.Body.Stmts[0])
	}
}

func TestAllStatementKinds(t *testing.T) {
	src := `let x: int = 1;
x = x + 1;
if (x < 2) { print(x); } else if (x == 2) { print(2); } else { print(3); }
while (x < 10) { x = x + 1; }
print(x);
touch();
`
	prog := parseSource(t, src)
	want := []any{
		&ast.LetStmt{}, &ast.AssignStmt{}, &ast.IfStmt{},
		&ast.WhileStmt{}, &ast.PrintStmt{}, &ast.ExprStmt{},
	}
	if len(prog.Main) != len(want) {
		t.Fatalf("got %d statements, want %d", len(prog.Main), len(want))
	}
	for i := range want {
		if got, wantType := fmt.Sprintf("%T", prog.Main[i]), fmt.Sprintf("%T", want[i]); got != wantType {
			t.Fatalf("statement %d is %s, want %s", i, got, wantType)
		}
	}

	ifStmt := prog.Main[2].(*ast.IfStmt)
	if ifStmt.Else == nil {
		t.Fatal("if statement has no else branch")
	}
	if _, ok := ifStmt.Else.(*ast.IfStmt); !ok {
		t.Fatalf("else branch is %T, want *ast.IfStmt", ifStmt.Else)
	}
}

func TestAllExamplesParse(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "examples", "*.av"))
	if err != nil || len(files) != 5 {
		t.Fatalf("found %d examples: %v", len(files), err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			toks, err := lexer.Lex(string(src))
			if err != nil {
				t.Fatalf("Lex: %v", err)
			}
			if _, err := Parse(toks); err != nil {
				t.Fatalf("Parse: %v", err)
			}
		})
	}
}

func TestMissingSemicolonIsReportedWithPosition(t *testing.T) {
	toks, err := lexer.Lex("let x: int = 1\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(toks)
	if err == nil {
		t.Fatal("accepted a let without a semicolon")
	}
	msg := err.Error()
	if !strings.Contains(msg, "expected ;") || !strings.Contains(msg, "line 2") {
		t.Fatalf("error = %q, want the expected token and line 2", msg)
	}
}

func TestMissingParenIsReportedWithoutPanic(t *testing.T) {
	toks, _ := lexer.Lex("print(1;")
	_, err := Parse(toks)
	if err == nil {
		t.Fatal("accepted print(1;")
	}
	if !strings.Contains(err.Error(), "expected )") {
		t.Fatalf("error = %q", err)
	}
}

func TestMissingExpressionIsReported(t *testing.T) {
	toks, _ := lexer.Lex("print();")
	_, err := Parse(toks)
	if err == nil || !strings.Contains(err.Error(), "expected expression") {
		t.Fatalf("error = %v, want an expected-expression error", err)
	}
}

func TestNodesKeepTheirPositions(t *testing.T) {
	prog := parseSource(t, "let x: int = 1;")
	st := prog.Main[0]
	if st.Pos() != (ast.Position{Line: 1, Col: 1}) {
		t.Fatalf("LetStmt position = %v", st.Pos())
	}
	lit := st.(*ast.LetStmt).Value
	if lit.Pos() != (ast.Position{Line: 1, Col: 14}) {
		t.Fatalf("IntLit position = %v, want 1:14", lit.Pos())
	}
}

func TestDebugPrinter(t *testing.T) {
	prog := parseSource(t, "let x: int = 1 + 2; print(x);")
	var out bytes.Buffer
	ast.Print(&out, prog)

	want := `Program
  LetStmt x int
    BinaryExpr +
      IntLit 1
      IntLit 2
  PrintStmt
    Ident x
`
	if out.String() != want {
		t.Fatalf("printed:\n%s\nwant:\n%s", out.String(), want)
	}
}
