package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"anvil/codegen"
	"anvil/lexer"
	"anvil/parser"
)

func gen(t *testing.T, src string) string {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	tree, err := parser.Parse(toks)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	asm, err := codegen.Generate(tree)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return asm
}

func requireAll(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("assembly does not contain %q:\n%s", want, text)
		}
	}
}

// assembleAndRun checks the assembly with the real toolchain. Only available
// on Linux x86-64 (the WSL image used for Anvil).
func assembleAndRun(t *testing.T, asm string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("native backend targets x86-64 Linux")
	}
	if _, err := exec.LookPath("as"); err != nil {
		t.Skip("GNU as not installed")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "test.s")
	obj := filepath.Join(dir, "test.o")
	bin := filepath.Join(dir, "test")
	if err := os.WriteFile(src, []byte(asm), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("as", "-o", obj, src).CombinedOutput(); err != nil {
		t.Fatalf("as: %v\n%s", err, out)
	}
	if out, err := exec.Command("ld", "-o", bin, obj).CombinedOutput(); err != nil {
		t.Fatalf("ld: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("running binary: %v\n%s", err, out)
	}
	return string(out)
}

func TestGenerateHasStartAndExitSyscall(t *testing.T) {
	asm := gen(t, "let x: int = 1; print(x);")
	requireAll(t, asm,
		".intel_syntax noprefix", ".globl _start", "_start:",
		"mov rax, 60", "syscall",
	)
}

func TestGenerateExpressionLeavesResultInRAX(t *testing.T) {
	asm := gen(t, "print(1 + 2 * 3);")
	requireAll(t, asm, "push rax", "mov rbx, rax", "pop rax", "imul rax, rbx", "add rax, rbx")
}

func TestGenerateLocalSlots(t *testing.T) {
	asm := gen(t, "let a: int = 1; let b: int = 2; print(a + b);")
	requireAll(t, asm, "[rbp-8]", "[rbp-16]", "sub rsp, 16")
}

func TestGeneratePrintIntRoutine(t *testing.T) {
	asm := gen(t, "print(-1);")
	requireAll(t, asm, "anvil_print_int", "neg rax", "div r9", "mov rax, 1", "syscall")
}

func TestAssembleAndRunExpressions(t *testing.T) {
	asm := gen(t, "print(1 + 2 * 3); print(42); print(-7);")
	if got, want := assembleAndRun(t, asm), "7\n42\n-7\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestAssembleAndRunInt64Edges(t *testing.T) {
	asm := gen(t, "print(9223372036854775807); print(0);")
	if got, want := assembleAndRun(t, asm), "9223372036854775807\n0\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestGenerateFunctionFrame(t *testing.T) {
	asm := gen(t, "fn f(a: int, b: int) -> int { let c: int = a + b; return c; } print(f(1, 2));")
	requireAll(t, asm,
		"anvil_fn_f:", "push rbp", "mov rbp, rsp",
		"[rbp+16]", "[rbp+24]", "[rbp-8]",
		"mov rsp, rbp", "ret", "call anvil_fn_f", "add rsp, 16",
	)
}

func TestAssembleAndRunFunctionCall(t *testing.T) {
	asm := gen(t, "fn add(a: int, b: int) -> int { let c: int = a + b; return c; } print(add(20, 22));")
	if got, want := assembleAndRun(t, asm), "42\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestAssembleAndRunNestedCalls(t *testing.T) {
	src := `fn inc(x: int) -> int { return x + 1; }
fn twice(x: int) -> int { return inc(inc(x)); }
print(twice(5));
`
	if got, want := assembleAndRun(t, gen(t, src)), "7\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestAssembleAndRunDeepParameterList(t *testing.T) {
	src := `fn sum(a: int, b: int, c: int, d: int, e: int) -> int {
  return a + b + c + d + e;
}
print(sum(1, 2, 3, 4, 5));
`
	if got, want := assembleAndRun(t, gen(t, src)), "15\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestGenerateControlFlowJumps(t *testing.T) {
	asm := gen(t, "if (1 < 2) { print(1); } else { print(2); } while (true) { print(3); }")
	requireAll(t, asm, "cmp rax, rbx", "setl al", "cmp rax, 0", "je .Lelse", "jmp .Lendif", "je .Lendwhile", "jmp .Lwhile")
}

func TestAssembleAndRunFactorial(t *testing.T) {
	src := `fn fact(n: int) -> int {
  if (n <= 1) { return 1; }
  return n * fact(n - 1);
}
print(fact(10));
print(fact(20));
`
	want := "3628800\n2432902008176640000\n"
	if got := assembleAndRun(t, gen(t, src)); got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestAssembleAndRunFibonacci(t *testing.T) {
	src := `fn fib(n: int) -> int {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}
print(fib(10));
`
	if got, want := assembleAndRun(t, gen(t, src)), "55\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestAssembleAndRunPrimes(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "examples", "primes.av"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := assembleAndRun(t, gen(t, string(src))), "1229\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestAssembleAndRunShortCircuit(t *testing.T) {
	asm := gen(t, "print(false && (1 / 0 == 0)); print(true || (1 / 0 == 0));")
	if got, want := assembleAndRun(t, asm), "false\ntrue\n"; got != want {
		t.Fatalf("native output = %q, want %q", got, want)
	}
}

func TestBuildProducesELF(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native backend targets x86-64 Linux")
	}
	bin := filepath.Join(t.TempDir(), "arith")
	if err := codegen.Build("print(1 + 2 * 3);", bin, nil); err != nil {
		t.Fatalf("Build: %v", err)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil || string(out) != "7\n" {
		t.Fatalf("binary output = %q, err = %v", out, err)
	}
	fileOut, err := exec.Command("file", bin).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fileOut), "ELF") {
		t.Fatalf("file says %q, want ELF", fileOut)
	}
}
