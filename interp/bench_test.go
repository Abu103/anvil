package interp_test

import (
	"io"
	"testing"

	"anvil/interp"
)

const fib32 = `fn fib(n: int) -> int {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}
print(fib(32));
`

func BenchmarkFib32TreeWalker(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if err := interp.Run(fib32, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
