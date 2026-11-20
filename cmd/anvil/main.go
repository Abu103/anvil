// Command anvil compiles and runs Anvil programs.
//
//	anvil run FILE    execute with the tree-walking interpreter
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"anvil/bytecode"
	"anvil/codegen"
	"anvil/interp"
	"anvil/lexer"
	"anvil/parser"
	"anvil/types"
	"anvil/vm"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command and returns an exit code: 0 success, 1 a program or
// file error, 2 bad usage.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "run":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: anvil run FILE")
			return 2
		}
		src, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "anvil:", err)
			return 1
		}
		if err := interp.Run(string(src), stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0

	case "dis":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: anvil dis FILE")
			return 2
		}
		src, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "anvil:", err)
			return 1
		}
		prog, err := compileSource(string(src))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprint(stdout, bytecode.Disassemble(prog))
		return 0

	case "vm":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: anvil vm FILE")
			return 2
		}
		src, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "anvil:", err)
			return 1
		}
		prog, err := compileSource(string(src))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := vm.Run(prog, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0

	case "build":
		return buildCommand(args[1:], stdout, stderr)

	default:
		fmt.Fprintf(stderr, "anvil: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: anvil <command> [args]

commands:
  run FILE    execute FILE with the tree-walking interpreter
  vm FILE     execute FILE on the bytecode VM
  dis FILE    print the bytecode for each function
  build FILE -o OUT [--emit-asm] [--no-opt] [--no-as]   produce a native x86-64 Linux binary`)
}

// buildCommand assembles and links a native binary. Flags are accepted before
// or after the filename, so the roadmap's `anvil build file.av -o out` works.
func buildCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "output executable (required)")
	emitAsm := fs.Bool("emit-asm", false, "print the generated assembly")
	noOpt := fs.Bool("no-opt", false, "disable constant folding and dead-code removal")
	directELF := fs.Bool("no-as", false, "write the ELF directly, without as/ld")

	if err := fs.Parse(reorderBuildFlags(args)); err != nil {
		return 2
	}
	if *output == "" || fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: anvil build FILE -o OUT [--emit-asm]")
		return 2
	}

	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "anvil:", err)
		return 1
	}
	var asmOut io.Writer
	if *emitAsm {
		asmOut = stdout
	}
	if err := codegen.BuildWithOptions(string(src), *output, codegen.Options{
		Assembly:  asmOut,
		NoOpt:     *noOpt,
		DirectELF: *directELF,
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// reorderBuildFlags moves known flags in front of the positional file name.
func reorderBuildFlags(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 < len(args) {
				flags = append(flags, args[i], args[i+1])
				i++
			} else {
				flags = append(flags, args[i])
			}
		case "--emit-asm", "-emit-asm":
			flags = append(flags, "-emit-asm")
		case "--no-opt", "-no-opt":
			flags = append(flags, "-no-opt")
		case "--no-as", "-no-as":
			flags = append(flags, "-no-as")
		default:
			rest = append(rest, args[i])
		}
	}
	return append(flags, rest...)
}

// compileSource lexes, parses, type-checks and compiles to bytecode.
func compileSource(src string) (*bytecode.Program, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	tree, err := parser.Parse(toks)
	if err != nil {
		return nil, err
	}
	if diags := types.Check(tree, src); len(diags) > 0 {
		var b strings.Builder
		b.WriteString("anvil: type errors:\n")
		for _, d := range diags {
			b.WriteString(d.Format())
			b.WriteByte('\n')
		}
		return nil, errors.New(b.String())
	}
	return bytecode.Compile(tree)
}
