package vm_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"anvil/bytecode"
	"anvil/interp"
	"anvil/lexer"
	"anvil/parser"
	"anvil/vm"
)

func compileSrc(t *testing.T, src string) *bytecode.Program {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	tree, err := parser.Parse(toks)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	prog, err := bytecode.Compile(tree)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return prog
}

func run(t *testing.T, src string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := vm.Run(compileSrc(t, src), &out)
	return out.String(), err
}

func TestVMAritmeticAndPrint(t *testing.T) {
	got, err := run(t, "print(1 + 2 * 3); print(true && false); print(-7 / 2);")
	if err != nil {
		t.Fatal(err)
	}
	if want := "7\nfalse\n-3\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestVMLocalsAndAssignment(t *testing.T) {
	got, err := run(t, "let x: int = 1; x = x + 4; print(x);")
	if err != nil {
		t.Fatal(err)
	}
	if got != "5\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestVMCallsRecursionAndFrames(t *testing.T) {
	src := `fn fact(n: int) -> int {
  if (n <= 1) { return 1; }
  return n * fact(n - 1);
}
print(fact(10));
`
	got, err := run(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if got != "3628800\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestVMShortCircuitSkipsDivisionByZero(t *testing.T) {
	got, err := run(t, "print(false && (1 / 0 == 0)); print(true || (1 / 0 == 0));")
	if err != nil {
		t.Fatalf("short circuit failed: %v", err)
	}
	if got != "false\ntrue\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestVMReportsDivisionByZero(t *testing.T) {
	_, err := run(t, "print(1 / 0);")
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("error = %v, want division by zero", err)
	}
}

func TestVMStackOverflowIsACleanError(t *testing.T) {
	_, err := run(t, "fn f(x: int) -> int { return f(x); } print(f(1));")
	if err == nil || !strings.Contains(err.Error(), "stack") {
		t.Fatalf("error = %v, want a stack overflow error", err)
	}
}

func TestVMMatchesInterpreterOnGoldenExamples(t *testing.T) {
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
			want, err := os.ReadFile(strings.TrimSuffix(file, ".av") + ".expected")
			if err != nil {
				t.Fatal(err)
			}

			var vmOut bytes.Buffer
			if err := vm.Run(compileSrc(t, string(src)), &vmOut); err != nil {
				t.Fatalf("VM: %v", err)
			}
			if !bytes.Equal(vmOut.Bytes(), want) {
				t.Fatalf("VM output:\n%s\nwant:\n%s", vmOut.String(), want)
			}

			var refOut bytes.Buffer
			if err := interp.Run(string(src), &refOut); err != nil {
				t.Fatalf("interpreter: %v", err)
			}
			if !bytes.Equal(refOut.Bytes(), vmOut.Bytes()) {
				t.Fatalf("VM and interpreter disagree:\nVM:\n%s\ninterp:\n%s", vmOut.String(), refOut.String())
			}
		})
	}
}
