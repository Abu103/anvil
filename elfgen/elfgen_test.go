package elfgen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"anvil/codegen"
	"anvil/elfgen"
	"anvil/lexer"
	"anvil/parser"
)

func TestBuildAllExamplesWithoutAssembler(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the direct ELF writer targets x86-64 Linux")
	}

	files, err := filepath.Glob(filepath.Join("..", "examples", "*.av"))
	if err != nil || len(files) != 5 {
		t.Fatalf("found %d examples: %v", len(files), err)
	}
	dir := t.TempDir()
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			toks, err := lexer.Lex(string(src))
			if err != nil {
				t.Fatal(err)
			}
			tree, err := parser.Parse(toks)
			if err != nil {
				t.Fatal(err)
			}
			asm, err := codegen.Generate(tree)
			if err != nil {
				t.Fatal(err)
			}

			bin := filepath.Join(dir, filepath.Base(file[:len(file)-3])+"-elf")
			if err := elfgen.Build(asm, bin); err != nil {
				t.Fatalf("elfgen.Build: %v", err)
			}
			want, err := os.ReadFile(file[:len(file)-3] + ".expected")
			if err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin).CombinedOutput()
			if err != nil {
				t.Fatalf("running %s: %v\n%s", bin, err, out)
			}
			if string(out) != string(want) {
				t.Fatalf("output:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

func TestEMittedFileIsELF(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the direct ELF writer targets x86-64 Linux")
	}
	if _, err := exec.LookPath("file"); err != nil {
		t.Skip("file(1) not installed")
	}
	bin := filepath.Join(t.TempDir(), "prog")
	asm := ".intel_syntax noprefix\n.globl _start\n.text\n_start:\n  mov rdi, 0\n  mov rax, 60\n  syscall\n"
	if err := elfgen.Build(asm, bin); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("file", bin).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) == "" || !containsELF(string(out)) {
		t.Fatalf("file says %q", out)
	}
}

func containsELF(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if s[i:i+3] == "ELF" {
			return true
		}
	}
	return false
}
