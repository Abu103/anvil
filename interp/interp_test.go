package interp_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"anvil/interp"
)

func runSource(t *testing.T, src string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := interp.Run(src, &out)
	return out.String(), err
}

func runOK(t *testing.T, src string) string {
	t.Helper()
	out, err := runSource(t, src)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out
}

func TestPrintIntAndBool(t *testing.T) {
	got := runOK(t, "print(42); print(-7); print(true); print(1 < 2);")
	if want := "42\n-7\ntrue\ntrue\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestArithmeticPrecedenceAndTruncation(t *testing.T) {
	got := runOK(t, "print(1 + 2 * 3); print((1 + 2) * 3); print(-7 / 2); print(7 / -2); print(2 - 3 - 4);")
	if want := "7\n9\n-3\n-3\n-5\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestWrappingAndMinInt64Division(t *testing.T) {
	got := runOK(t, "print(9223372036854775807 + 1); print(-9223372036854775807 - 2);")
	if want := "-9223372036854775808\n9223372036854775807\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	// SPEC: MinInt64 / -1 must wrap, not trap.
	got = runOK(t, "let m: int = -9223372036854775807 - 1; print(m / -1);")
	if got != "-9223372036854775808\n" {
		t.Fatalf("MinInt64 / -1 = %q", got)
	}
}

func TestDivisionByZero(t *testing.T) {
	_, err := runSource(t, "print(1 / 0);")
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("error = %v, want division by zero", err)
	}
}

func TestVariablesAssignmentAndScopes(t *testing.T) {
	got := runOK(t, `let x: int = 1;
x = x + 4;
if (true) {
  let x: int = 99;
  print(x);
}
print(x);
`)
	if want := "99\n5\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestIfElseIfElse(t *testing.T) {
	got := runOK(t, `let x: int = 2;
if (x == 1) { print(1); } else if (x == 2) { print(2); } else { print(3); }
if (x != 2) { print(9); } else { print(8); }
`)
	if want := "2\n8\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestWhileLoop(t *testing.T) {
	got := runOK(t, "let i: int = 0; while (i < 5) { print(i); i = i + 1; }")
	if want := "0\n1\n2\n3\n4\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestShortCircuitAvoidsDivisionByZero(t *testing.T) {
	got := runOK(t, "print(false && (1 / 0 == 0)); print(true || (1 / 0 == 0));")
	if want := "false\ntrue\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRecursion(t *testing.T) {
	src := `fn fact(n: int) -> int {
  if (n <= 1) { return 1; }
  return n * fact(n - 1);
}
print(fact(10));
`
	if got := runOK(t, src); got != "3628800\n" {
		t.Fatalf("factorial = %q", got)
	}
}

func TestFunctionLocalsDoNotLeakIntoCallers(t *testing.T) {
	src := `fn f(n: int) -> int {
  let inner: int = n + 1;
  return inner;
}
print(f(1));
`
	if got := runOK(t, src); got != "2\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestDeepRecursionTenThousand(t *testing.T) {
	src := `fn count(n: int) -> int {
  if (n == 0) { return 0; }
  return count(n - 1);
}
print(count(10000));
`
	if got := runOK(t, src); got != "0\n" {
		t.Fatalf("deep recursion output = %q", got)
	}
}

func TestTypeErrorsAreRejectedBeforeRunning(t *testing.T) {
	out, err := runSource(t, "print(1); print(1 + true);")
	if err == nil || !strings.Contains(err.Error(), "must both be int") {
		t.Fatalf("error = %v, want a type error", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want nothing: checking must happen before execution", out)
	}
}

func TestGoldenExamples(t *testing.T) {
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
			var out bytes.Buffer
			if err := interp.Run(string(src), &out); err != nil {
				t.Fatalf("Run: %v", err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(file, ".av") + ".expected")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("output:\n%s\nwant:\n%s", out.String(), want)
			}
		})
	}
}
