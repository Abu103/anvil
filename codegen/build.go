package codegen

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"anvil/elfgen"
	"anvil/lexer"
	"anvil/opt"
	"anvil/parser"
	"anvil/types"
)

// Options control native compilation.
type Options struct {
	// Assembly receives the generated assembly text when non-nil.
	Assembly io.Writer
	// NoOpt disables constant folding and dead-code removal, for A/B benchmarks.
	NoOpt bool
	// DirectELF writes machine code and the ELF header itself, with no as/ld.
	DirectELF bool
}

// Build compiles src into a native x86-64 Linux executable at outPath.
func Build(src, outPath string, emitAsm io.Writer) error {
	return BuildWithOptions(src, outPath, Options{Assembly: emitAsm})
}

// BuildWithOptions is Build with the optimisation switch exposed.
func BuildWithOptions(src, outPath string, opts Options) error {
	toks, err := lexer.Lex(src)
	if err != nil {
		return err
	}
	tree, err := parser.Parse(toks)
	if err != nil {
		return err
	}
	if diags := types.Check(tree, src); len(diags) > 0 {
		var b strings.Builder
		b.WriteString("anvil: type errors:\n")
		for _, d := range diags {
			b.WriteString(d.Format())
			b.WriteByte('\n')
		}
		return errors.New(b.String())
	}
	if !opts.NoOpt {
		opt.Optimize(tree)
	}
	asm, err := Generate(tree)
	if err != nil {
		return err
	}
	if opts.Assembly != nil {
		if _, err := io.WriteString(opts.Assembly, asm); err != nil {
			return err
		}
	}

	if opts.DirectELF {
		return elfgen.Build(asm, outPath)
	}

	dir, err := os.MkdirTemp("", "anvil-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	sPath := filepath.Join(dir, "out.s")
	oPath := filepath.Join(dir, "out.o")
	if err := os.WriteFile(sPath, []byte(asm), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("as", "-o", oPath, sPath).CombinedOutput(); err != nil {
		return fmt.Errorf("anvil: as failed: %v\n%s", err, out)
	}
	if out, err := exec.Command("ld", "-o", outPath, oPath).CombinedOutput(); err != nil {
		return fmt.Errorf("anvil: ld failed: %v\n%s", err, out)
	}
	return nil
}
