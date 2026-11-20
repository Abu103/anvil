package vm_test

import (
	"io"
	"testing"

	"anvil/bytecode"
	"anvil/lexer"
	"anvil/parser"
	"anvil/vm"
)

const fib32 = `fn fib(n: int) -> int {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}
print(fib(32));
`

func compileForBench(src string) *bytecode.Program {
	toks, err := lexer.Lex(src)
	if err != nil {
		panic(err)
	}
	tree, err := parser.Parse(toks)
	if err != nil {
		panic(err)
	}
	prog, err := bytecode.Compile(tree)
	if err != nil {
		panic(err)
	}
	return prog
}

func BenchmarkFib32VM(b *testing.B) {
	// Compile once; this measures execution, not parsing.
	prog := compileForBench(fib32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vm.Run(prog, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
