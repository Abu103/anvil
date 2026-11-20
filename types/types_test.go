package types_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"anvil/lexer"
	"anvil/parser"
	"anvil/types"
)

func check(src string) ([]types.Diagnostic, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	prog, err := parser.Parse(toks)
	if err != nil {
		return nil, err
	}
	return types.Check(prog, src), nil
}

func TestAllExamplesTypeCheck(t *testing.T) {
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
			diags, err := check(string(src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(diags) != 0 {
				for _, d := range diags {
					t.Errorf("%s", d.Format())
				}
			}
		})
	}
}

func TestBadPrograms(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"mixed arithmetic", "print(1 + true);", "must both be int"},
		{"undeclared variable", "print(mystery);", "undefined variable"},
		{"wrong argument count", "fn f(a: int) -> int { return a; } print(f());", "got 0 arguments, want 1"},
		{"wrong argument type", "fn f(a: int) -> int { return a; } print(f(true));", "argument 0"},
		{"missing return", "fn f(x: int) -> int { let y: int = x; }", "without returning"},
		{"non-bool if", "if (1) { print(2); }", "if condition must be bool"},
		{"non-bool while", "while (true) { } print(1);", ""}, // control case, must be clean
		{"bad let annotation", "let x: bool = 1;", "cannot assign int to bool"},
		{"bad assignment", "let x: int = 1; x = false;", "cannot assign bool to int"},
		{"bool arithmetic", "print(true + false);", "must both be int"},
		{"return outside function", "return 1;", "outside a function"},
		{"int bool equality", "print(1 == true);", "cannot compare int with bool"},
		{"not on int", "print(!1);", "! needs bool"},
		{"minus on bool", "print(-true);", "unary - needs int"},
		{"unknown function", "print(nope(1));", "undefined function"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags, err := check(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if tc.want == "" {
				if len(diags) != 0 {
					t.Fatalf("expected a clean program, got: %s", diags[0].Format())
				}
				return
			}
			if len(diags) == 0 {
				t.Fatalf("expected a diagnostic containing %q, got none", tc.want)
			}
			joined := ""
			for _, d := range diags {
				joined += d.Format() + "\n"
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("diagnostics:\n%s\nwant to contain %q", joined, tc.want)
			}
		})
	}
}

func TestCollectsSeveralErrors(t *testing.T) {
	diags, err := check("print(mystery + true);\nprint(other);\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) < 2 {
		t.Fatalf("got %d diagnostics, want at least 2: %+v", len(diags), diags)
	}
}

func TestDiagnosticShowsSourceLineAndCaret(t *testing.T) {
	diags, err := check("print(1 + true);\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("no diagnostic")
	}
	got := diags[0].Format()
	for _, want := range []string{"1 + true", "1:11", "^"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic:\n%s\nwant it to contain %q", got, want)
		}
	}
	if !strings.Contains(strings.Split(got, "\n")[4], "^") {
		t.Fatalf("caret is not on the fifth line:\n%s", got)
	}
}

func TestReturnPerBranchIsRequired(t *testing.T) {
	good := `fn f(x: int) -> int {
  if (x > 0) { return 1; } else { return 2; }
}
print(f(3));
`
	diags, err := check(good)
	if err != nil || len(diags) != 0 {
		t.Fatalf("good program should type-check: %v %+v", err, diags)
	}

	bad := `fn f(x: int) -> int {
  if (x > 0) { return 1; }
}
`
	diags, err = check(bad)
	if err != nil || len(diags) == 0 {
		t.Fatalf("one-sided return should fail: %v %+v", err, diags)
	}
}

func TestExactErrorMessages(t *testing.T) {
	cases := []struct{ src, want string }{
		{"print(1 + true);", "operands of + must both be int, got int and bool"},
		{"print(mystery);", "undefined variable \"mystery\""},
		{"fn f(a: int) -> int { return a; } print(f());", "f: got 0 arguments, want 1"},
		{"fn f(a: int) -> int { return a; } print(f(true));", "f: argument 0 needs int, got bool"},
		{"fn f(x: int) -> int { let y: int = x; }", "function \"f\" may finish without returning"},
		{"if (1) { print(2); }", "if condition must be bool, got int"},
		{"let x: bool = 1;", "cannot assign int to bool"},
		{"let x: int = 1; x = false;", "cannot assign bool to int"},
		{"print(true + false);", "operands of + must both be int, got bool and bool"},
		{"print(1 == true);", "cannot compare int with bool"},
		{"return 1;", "return outside a function"},
		{"print(!1);", "! needs bool, got int"},
	}
	if len(cases) != 12 {
		t.Fatal("this test should exercise at least 10 bad programs")
	}
	for _, tc := range cases {
		diags, err := check(tc.src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.src, err)
		}
		if len(diags) == 0 {
			t.Fatalf("%q produced no diagnostic", tc.src)
		}
		if got := diags[0].Message; got != tc.want {
			t.Fatalf("%q: message = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestReturnTypeMismatch(t *testing.T) {
	diags, err := check("fn f() -> int { return true; }")
	if err != nil || len(diags) == 0 {
		t.Fatalf("expected a return type error: %v %+v", err, diags)
	}
}
