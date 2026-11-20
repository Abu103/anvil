package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func invoke(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunExample(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "fibonacci.av")
	want, err := os.ReadFile(filepath.Join("..", "..", "examples", "fibonacci.expected"))
	if err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := invoke("run", path)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != string(want) {
		t.Fatalf("output:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestRunNeedsAFile(t *testing.T) {
	if code, _, stderr := invoke("run"); code != 2 || !strings.Contains(stderr, "usage") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestRunMissingFileFails(t *testing.T) {
	code, _, stderr := invoke("run", filepath.Join(t.TempDir(), "nope.av"))
	if code != 1 || stderr == "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestRunReportsSourceErrors(t *testing.T) {
	// Type checking arrives in step 5, so use a syntax error for now.
	path := filepath.Join(t.TempDir(), "bad.av")
	if err := os.WriteFile(path, []byte("print(1 /);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := invoke("run", path)
	if code != 1 || !strings.Contains(stderr, "parser:") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestDisassembleExample(t *testing.T) {
	code, stdout, stderr := invoke("dis", filepath.Join("..", "..", "examples", "fibonacci.av"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"== fib ==", "== main ==", "CALL", "HALT"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("disassembly does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestDisNeedsAFile(t *testing.T) {
	if code, _, stderr := invoke("dis"); code != 2 || !strings.Contains(stderr, "usage") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestVMCommandMatchesGoldenFile(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "examples", "fibonacci.expected"))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := invoke("vm", filepath.Join("..", "..", "examples", "fibonacci.av"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != string(want) {
		t.Fatalf("output = %q, want %q", stdout, want)
	}
}

func TestUnknownCommand(t *testing.T) {
	if code, _, stderr := invoke("frobnicate"); code != 2 || !strings.Contains(stderr, "unknown command") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}
