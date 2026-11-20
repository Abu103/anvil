package bytecode_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"anvil/bytecode"
	"anvil/lexer"
	"anvil/parser"
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

func TestCompileArithmetic(t *testing.T) {
	prog := compileSrc(t, "print(1 + 2 * 3);")
	want := []bytecode.Instr{
		{Op: bytecode.Const, Arg: 0},
		{Op: bytecode.Const, Arg: 1},
		{Op: bytecode.Const, Arg: 2},
		{Op: bytecode.Mul},
		{Op: bytecode.Add},
		{Op: bytecode.Print},
		{Op: bytecode.Halt},
	}
	if !reflect.DeepEqual(prog.Main.Code, want) {
		t.Fatalf("code = %v, want %v", prog.Main.Code, want)
	}
	wantConsts := []int64{1, 2, 3}
	for i, v := range wantConsts {
		if prog.Constants[i].Int != v {
			t.Fatalf("constant %d = %d, want %d", i, prog.Constants[i].Int, v)
		}
	}
}

func TestSubtractIsLeftAssociativeInBytecode(t *testing.T) {
	prog := compileSrc(t, "let a: int = 1; let b: int = 2; let c: int = 3; print(a - b - c);")
	tail := prog.Main.Code[len(prog.Main.Code)-7 : len(prog.Main.Code)-2]
	want := []bytecode.Instr{
		{Op: bytecode.Load, Arg: 0},
		{Op: bytecode.Load, Arg: 1},
		{Op: bytecode.Sub},
		{Op: bytecode.Load, Arg: 2},
		{Op: bytecode.Sub},
	}
	if !reflect.DeepEqual(tail, want) {
		t.Fatalf("tail = %v, want %v", tail, want)
	}
}

func TestIfElseJumpsArePatched(t *testing.T) {
	prog := compileSrc(t, "if (true) { print(1); } else { print(2); }")
	code := prog.Main.Code
	if code[1].Op != bytecode.JumpIfFalse || code[1].Arg != 5 {
		t.Fatalf("JumpIfFalse = %+v, want target 5", code[1])
	}
	if code[4].Op != bytecode.Jump || code[4].Arg != 7 {
		t.Fatalf("Jump = %+v, want target 7", code[4])
	}
	if code[7].Op != bytecode.Halt {
		t.Fatalf("last instruction = %+v, want Halt", code[7])
	}
}

func TestWhileJumpGoesBackToTheCondition(t *testing.T) {
	prog := compileSrc(t, "let i: int = 0; while (i < 3) { i = i + 1; }")
	code := prog.Main.Code
	if code[5].Op != bytecode.JumpIfFalse || code[5].Arg != 11 {
		t.Fatalf("exit jump = %+v, want target 11", code[5])
	}
	last := code[len(code)-2]
	if last.Op != bytecode.Jump || last.Arg != 2 {
		t.Fatalf("loop back edge = %+v, want target 2 (the condition)", last)
	}
}

func TestFunctionAndCall(t *testing.T) {
	prog := compileSrc(t, "fn add(a: int, b: int) -> int { return a + b; } print(add(1, 2));")
	if len(prog.Funcs) != 1 {
		t.Fatalf("got %d functions", len(prog.Funcs))
	}
	fn := prog.Funcs[0]
	if fn.Name != "add" || fn.Arity != 2 {
		t.Fatalf("function = %+v", fn)
	}
	wantBody := []bytecode.Instr{
		{Op: bytecode.Load, Arg: 0},
		{Op: bytecode.Load, Arg: 1},
		{Op: bytecode.Add},
		{Op: bytecode.Ret},
	}
	if !reflect.DeepEqual(fn.Code, wantBody) {
		t.Fatalf("body = %v, want %v", fn.Code, wantBody)
	}

	wantMain := []bytecode.Instr{
		{Op: bytecode.Const, Arg: 0},
		{Op: bytecode.Const, Arg: 1},
		{Op: bytecode.Call, Arg: 0},
		{Op: bytecode.Print},
		{Op: bytecode.Halt},
	}
	if !reflect.DeepEqual(prog.Main.Code, wantMain) {
		t.Fatalf("main = %v, want %v", prog.Main.Code, wantMain)
	}
}

func TestShortCircuitCompilesJumps(t *testing.T) {
	prog := compileSrc(t, "print(false && (1 / 0 == 0));")
	code := prog.Main.Code
	var jf, div bool
	for _, in := range code {
		if in.Op == bytecode.JumpIfFalse {
			jf = true
		}
		if in.Op == bytecode.Div {
			div = true
		}
	}
	if !jf || !div {
		t.Fatalf("expected both a short-circuit jump and the right-hand Div: %v", code)
	}
}

func TestLocalSlotsAreNumbered(t *testing.T) {
	prog := compileSrc(t, "let a: int = 1; let b: int = 2; print(a + b);")
	if prog.Main.NumLocals != 2 {
		t.Fatalf("NumLocals = %d, want 2", prog.Main.NumLocals)
	}
	if prog.Main.Code[1] != (bytecode.Instr{Op: bytecode.Store, Arg: 0}) ||
		prog.Main.Code[3] != (bytecode.Instr{Op: bytecode.Store, Arg: 1}) {
		t.Fatalf("local stores = %v", prog.Main.Code)
	}
}

func TestAllExamplesCompile(t *testing.T) {
	for _, name := range []string{"arithmetic", "factorial", "fibonacci", "gcd", "primes"} {
		t.Run(name, func(t *testing.T) {
			src := readExample(t, name)
			prog := compileSrc(t, src)
			if len(prog.Main.Code) == 0 {
				t.Fatal("empty main")
			}
		})
	}
}

func TestDisassembler(t *testing.T) {
	prog := compileSrc(t, "fn f(x: int) -> int { return x + 1; } print(f(2));")
	text := bytecode.Disassemble(prog)
	for _, want := range []string{"== f ==", "== main ==", "LOAD", "ADD", "RET", "CALL", "HALT"} {
		if !strings.Contains(text, want) {
			t.Fatalf("disassembly does not contain %q:\n%s", want, text)
		}
	}
}

func readExample(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../examples/" + name + ".av")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
