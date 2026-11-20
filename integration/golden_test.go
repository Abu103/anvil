// Package integration holds the three-way golden test: interpreter, VM and
// native binary must agree with each other and with the .expected files.
package integration_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"anvil/bytecode"
	"anvil/codegen"
	"anvil/interp"
	"anvil/lexer"
	"anvil/parser"
	"anvil/types"
	"anvil/vm"
)

func compileProgram(src string) (*bytecode.Program, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	tree, err := parser.Parse(toks)
	if err != nil {
		return nil, err
	}
	if diags := types.Check(tree, src); len(diags) > 0 {
		return nil, errors.New(diags[0].Format())
	}
	return bytecode.Compile(tree)
}

func exampleFiles(t testing.TB) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "examples", "*.av"))
	if err != nil || len(files) != 5 {
		t.Fatalf("found %d examples: %v", len(files), err)
	}
	return files
}

func TestThreeWayGolden(t *testing.T) {
	binDir := t.TempDir()
	for _, file := range exampleFiles(t) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(file[:len(file)-3] + ".expected")
			if err != nil {
				t.Fatal(err)
			}

			var treeOut bytes.Buffer
			if err := interp.Run(string(src), &treeOut); err != nil {
				t.Fatalf("interpreter: %v", err)
			}
			prog, err := compileProgram(string(src))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			var vmOut bytes.Buffer
			if err := vm.Run(prog, &vmOut); err != nil {
				t.Fatalf("VM: %v", err)
			}
			if !bytes.Equal(treeOut.Bytes(), vmOut.Bytes()) {
				t.Fatalf("interpreter and VM disagree:\ninterp:\n%s\nvm:\n%s", treeOut.String(), vmOut.String())
			}
			if !bytes.Equal(treeOut.Bytes(), want) {
				t.Fatalf("output:\n%s\nwant:\n%s", treeOut.String(), want)
			}

			if runtime.GOOS != "linux" {
				t.Skip("native backend targets x86-64 Linux")
			}
			bin := filepath.Join(binDir, filepath.Base(file[:len(file)-3]))
			if err := codegen.Build(string(src), bin, nil); err != nil {
				t.Fatalf("native build: %v", err)
			}
			out, err := exec.Command(bin).Output()
			if err != nil {
				t.Fatalf("running native binary: %v", err)
			}
			if !bytes.Equal(out, want) {
				t.Fatalf("native output:\n%s\nwant:\n%s", out, want)
			}
			if !bytes.Equal(out, treeOut.Bytes()) {
				t.Fatalf("native and interpreter disagree:\nnative:\n%s\ninterp:\n%s", out, treeOut.Bytes())
			}
		})
	}
}

// Benchmarks. Each backend runs the same programs; the native backend includes
// process startup, which is honest for how a user invokes it.
const fib35 = `fn fib(n: int) -> int {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}
print(fib(35));
`

func benchCompile(src string) *bytecode.Program {
	prog, err := compileProgram(src)
	if err != nil {
		panic(err)
	}
	return prog
}

func benchNative(b *testing.B, src string, noOpt bool) {
	bin := filepath.Join(b.TempDir(), "prog")
	if err := codegen.BuildWithOptions(src, bin, codegen.Options{NoOpt: noOpt}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := exec.Command(bin)
		cmd.Stdout = io.Discard
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFib35Interpreter(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if err := interp.Run(fib35, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFib35VM(b *testing.B) {
	prog := benchCompile(fib35)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vm.Run(prog, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFib35Native(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("native backend targets x86-64 Linux")
	}
	benchNative(b, fib35, false)
}

func BenchmarkFib35NativeNoOpt(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("native backend targets x86-64 Linux")
	}
	benchNative(b, fib35, true)
}

func BenchmarkPrimesInterpreter(b *testing.B) {
	src, err := os.ReadFile(filepath.Join("..", "examples", "primes.av"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		if err := interp.Run(string(src), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrimesVM(b *testing.B) {
	src, err := os.ReadFile(filepath.Join("..", "examples", "primes.av"))
	if err != nil {
		b.Fatal(err)
	}
	prog := benchCompile(string(src))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vm.Run(prog, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrimesNative(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("native backend targets x86-64 Linux")
	}
	src, err := os.ReadFile(filepath.Join("..", "examples", "primes.av"))
	if err != nil {
		b.Fatal(err)
	}
	benchNative(b, string(src), false)
}

func BenchmarkPrimesNativeNoOpt(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("native backend targets x86-64 Linux")
	}
	src, err := os.ReadFile(filepath.Join("..", "examples", "primes.av"))
	if err != nil {
		b.Fatal(err)
	}
	benchNative(b, string(src), true)
}
