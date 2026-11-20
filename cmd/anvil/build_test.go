package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildAndRunBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native backend targets x86-64 Linux")
	}
	bin := filepath.Join(t.TempDir(), "fib")
	code, _, stderr := invoke("build", filepath.Join("..", "..", "examples", "fibonacci.av"), "-o", bin)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "examples", "fibonacci.expected"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil || string(out) != string(want) {
		t.Fatalf("binary output = %q, err = %v, want %q", out, err, want)
	}
}

func TestBuildEmitASM(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native backend targets x86-64 Linux")
	}
	bin := filepath.Join(t.TempDir(), "arith")
	code, stdout, stderr := invoke("build", "--emit-asm", "-o", bin,
		filepath.Join("..", "..", "examples", "arithmetic.av"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, ".intel_syntax noprefix") || !strings.Contains(stdout, "anvil_print_int:") {
		t.Fatalf("--emit-asm did not print the assembly:\n%s", stdout)
	}
}

func TestBuildNoOptChangesAssembly(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native backend targets x86-64 Linux")
	}
	bin := filepath.Join(t.TempDir(), "arith")
	file := filepath.Join("..", "..", "examples", "arithmetic.av")

	_, optAsm, optErr := invoke("build", "--emit-asm", "-o", bin, file)
	if optErr != "" {
		t.Fatalf("optimised build: %s", optErr)
	}
	_, rawAsm, rawErr := invoke("build", "--emit-asm", "--no-opt", "-o", bin, file)
	if rawErr != "" {
		t.Fatalf("unoptimised build: %s", rawErr)
	}
	if optAsm == rawAsm {
		t.Fatal("--no-opt produced identical assembly")
	}
}

func TestBuildWithoutAssembler(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native backend targets x86-64 Linux")
	}
	bin := filepath.Join(t.TempDir(), "fib-noas")
	code, _, stderr := invoke("build", "--no-as",
		filepath.Join("..", "..", "examples", "fibonacci.av"), "-o", bin)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "examples", "fibonacci.expected"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil || string(out) != string(want) {
		t.Fatalf("binary output = %q, err = %v, want %q", out, err, want)
	}
}

func TestBuildNoAsNegativeNumbers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native backend targets x86-64 Linux")
	}
	path := filepath.Join(t.TempDir(), "negative.av")
	src := "print(-7);\nprint(2 - 3 - 4);\nprint(-9223372036854775807 - 1);\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "negative")
	code, _, stderr := invoke("build", "--no-as", path, "-o", bin)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("running binary: %v\n%s", err, out)
	}
	want := "-7\n-5\n-9223372036854775808\n"
	if string(out) != want {
		t.Fatalf("direct ELF output = %q, want %q", out, want)
	}
}

func TestBuildNeedsOutputPath(t *testing.T) {
	code, _, stderr := invoke("build", "examples/arith.av")
	if code != 2 || !strings.Contains(stderr, "-o") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}
