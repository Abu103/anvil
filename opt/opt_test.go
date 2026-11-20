package opt_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"anvil/ast"
	"anvil/interp"
	"anvil/lexer"
	"anvil/opt"
	"anvil/parser"
)

func optimizeSrc(t *testing.T, src string) (*ast.Program, opt.Stats) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	tree, err := parser.Parse(toks)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	stats := opt.Optimize(tree)
	return tree, stats
}

func printed(t *testing.T, prog *ast.Program) string {
	t.Helper()
	var out bytes.Buffer
	if err := interp.RunAST(prog, &out); err != nil {
		t.Fatalf("RunAST: %v", err)
	}
	return out.String()
}

func TestConstantFoldsArithmetic(t *testing.T) {
	prog, stats := optimizeSrc(t, "print(1 + 2 * 3);")
	if stats.Folded < 2 {
		t.Fatalf("folded %d nodes, want at least the multiply and the add", stats.Folded)
	}
	var b bytes.Buffer
	ast.Print(&b, prog)
	if !strings.Contains(b.String(), "IntLit 7") {
		t.Fatalf("tree was not folded:\n%s", b.String())
	}
	if got := printed(t, prog); got != "7\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestConstantFoldRespectsDivisionByZero(t *testing.T) {
	_, stats := optimizeSrc(t, "print(1 / 0);")
	if stats.Folded != 0 {
		t.Fatalf("folded a division by zero %d times", stats.Folded)
	}
}

func TestConstantFoldMinInt64Division(t *testing.T) {
	prog, _ := optimizeSrc(t, "print((-9223372036854775807 - 1) / -1);")
	if got := printed(t, prog); got != "-9223372036854775808\n" {
		t.Fatalf("output = %q, want wrapping MinInt64", got)
	}
}

func TestDeadCodeAfterReturn(t *testing.T) {
	src := `fn f() -> int {
  return 1;
  print(99);
}
print(f());
`
	prog, stats := optimizeSrc(t, src)
	if stats.Dropped == 0 {
		t.Fatal("no dead statement was removed")
	}
	var b bytes.Buffer
	ast.Print(&b, prog)
	if strings.Contains(b.String(), "99") {
		t.Fatalf("dead print survived:\n%s", b.String())
	}
	if got := printed(t, prog); got != "1\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestIfFalseBranchRemoval(t *testing.T) {
	src := "if (false) { print(1); } else { print(2); }"
	prog, stats := optimizeSrc(t, src)
	if stats.Dropped == 0 {
		t.Fatal("the false branch was not removed")
	}
	if got := printed(t, prog); got != "2\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestIfTrueInlinesWithoutChangingScope(t *testing.T) {
	src := `let x: int = 1;
if (true) { let x: int = 2; print(x); }
print(x);
`
	prog, _ := optimizeSrc(t, src)
	if got := printed(t, prog); got != "2\n1\n" {
		t.Fatalf("scope changed by inlining: output = %q, want 2 then 1", got)
	}
}

func TestWhileFalseIsRemoved(t *testing.T) {
	prog, stats := optimizeSrc(t, "while (false) { print(1); } print(2);")
	if stats.Dropped == 0 {
		t.Fatal("the dead loop was not removed")
	}
	if got := printed(t, prog); got != "2\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestOptimizedExamplesKeepTheirOutput(t *testing.T) {
	for _, name := range []string{"arithmetic", "factorial", "fibonacci", "gcd", "primes"} {
		t.Run(name, func(t *testing.T) {
			src := readExample(t, name)
			toks, err := lexer.Lex(src)
			if err != nil {
				t.Fatal(err)
			}
			tree, err := parser.Parse(toks)
			if err != nil {
				t.Fatal(err)
			}

			var before bytes.Buffer
			if err := interp.RunAST(tree, &before); err != nil {
				t.Fatal(err)
			}
			opt.Optimize(tree)
			var after bytes.Buffer
			if err := interp.RunAST(tree, &after); err != nil {
				t.Fatal(err)
			}
			if before.String() != after.String() {
				t.Fatalf("optimisation changed output:\nbefore:\n%s\nafter:\n%s", before.String(), after.String())
			}
		})
	}
}

func readExample(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "examples", name+".av"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
