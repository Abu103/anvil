# Anvil

A compiler for a small typed language, written from scratch in Go. It has three
backends that all produce identical output on every example: a tree-walking
interpreter, a stack bytecode VM, and a native x86-64 Linux ELF binary with no
LLVM and no libc.

**Three-way golden test:** all five example programs produce byte-identical
output on the interpreter, the VM and the native binary. The native binary is a
statically linked ELF produced by hand-written assembly plus `as` and `ld`.

## Requirements

- Linux x86-64 (or WSL 2 with Debian) for the native backend.
- Go 1.22 or newer.
- GNU `as` and `ld` (Debian: `apt install binutils`).

On Debian WSL: `sudo apt install golang-go binutils file`.

## Build and run

```sh
cd anvil
go test ./...

# Backend 1: tree-walking interpreter
go run ./cmd/anvil run examples/fibonacci.av

# Backend 2: bytecode VM
go run ./cmd/anvil vm examples/fibonacci.av
go run ./cmd/anvil dis examples/fibonacci.av        # readable bytecode

# Backend 3: native binary
go run ./cmd/anvil build examples/fibonacci.av -o fib
./fib
file fib      # ELF 64-bit LSB executable, x86-64, statically linked

# Same binary without as/ld: Anvil writes the machine code and ELF header
go run ./cmd/anvil build --no-as examples/fibonacci.av -o fib-direct
./fib-direct
```

`anvil build FILE -o OUT [--emit-asm] [--no-opt] [--no-as]` accepts flags
before or after the file name. `--emit-asm` prints assembly;
`--no-opt` disables constant folding and dead-code removal for A/B benchmarks;
`--no-as` uses the built-in encoder and ELF writer instead of `as`/`ld`.

## Pipeline

```
  examples/*.av
        |
     lexer  ---------> tokens with line + column
        |
   parser (Pratt) ---> AST (ast.Program)
        |
  type checker ------> diagnostics with source line and caret
        |
        +---> interpreter -----------------------------> output
        |
        +---> bytecode compiler -> stack VM -----------> output
        |
        +---> codegen -> .s -> as -> .o -> ld -> ELF ---> output
```

Every stage is independently testable, and each of the three backends is a
complete, demoable compiler. The interpreter is the oracle: a backend that
disagrees with it on any example has a bug.

## Language reference

### Types and values

`int` is signed 64-bit and **wraps** on overflow. `bool` is `true` or `false`.
There is no implicit conversion: `1 == true` is a type error. Print uses
decimal for ints and `true`/`false` for bools.

`/` truncates toward zero (`-7 / 2 == -3`). Division by zero prints
`anvil: division by zero` and exits 1 on all three backends. There is no `%`
operator; the remaining examples compute `n - (n/d)*d`. To keep the backends in
agreement, `MinInt64 / -1` is defined to be `MinInt64` instead of faulting.

### Grammar

```ebnf
program   = { fnDecl | stmt } ;
fnDecl    = "fn" IDENT "(" [IDENT ":" type {"," IDENT ":" type}] ")"
            "->" type block ;
type      = "int" | "bool" ;
block     = "{" { stmt } "}" ;
stmt      = "let" IDENT ":" type "=" expr ";"
          | IDENT "=" expr ";"
          | "if" "(" expr ")" block ["else" (block | ifStmt)]
          | "while" "(" expr ")" block
          | "return" expr ";" | "print" "(" expr ")" ";"
          | call ";" | block ;
expr      = or ;
or        = and {"||" and} ;
and       = eq {"&&" eq} ;
eq        = cmp {("==" | "!=") cmp} ;
cmp       = add {("<" | ">" | "<=" | ">=") add} ;
add       = mul {("+" | "-") mul} ;
mul       = unary {("*" | "/") unary} ;
unary     = ("-" | "!") unary | primary ;
primary   = INT | "true" | "false" | call | IDENT | "(" expr ")" ;
call      = IDENT "(" [expr {"," expr}] ")" ;
```

`//` starts a comment. All binary operators are left-associative. The full
specification, including scoping and error rules, is in `SPEC.md`.

Top-level statements form an implicit `main`. Top-level `let` variables are
**not** visible inside functions; a function sees only its parameters and its
own block-scoped locals. Forward references to functions are allowed. Every
function declares a return type and every path must return.

### Example error

```
$ anvil run bad.av
anvil: type errors:
error: operands of + must both be int, got int and bool
  --> 1:18
    |
 1 | let x: int = 1 + true;
    |                  ^
```

## Benchmarks

`go test ./integration -run '^$' -bench . -benchtime=1x -count=5`, median of 5
runs, Debian WSL on an i5-1135G7 (2 cores). Native timings include process
startup.

| Program | Interpreter | VM | Native | VM vs interp | Native vs interp |
|---|---|---|---|---|---|
| `fib(35)` | 7.80 s | 1.91 s | 92.9 ms | 4.1x | 84x |
| primes < 10,000 | 40.6 ms | 16.7 ms | 1.08 ms | 2.4x | 38x |

Optimisation A/B on the native backend, median of 5: `fib(35)` went from
73.8 ms to 72.8 ms; primes went from 0.68 ms to 0.89 ms. Both differences are
inside this machine's noise. The value of constant folding and dead-code
removal here is a smaller, cleaner assembly listing, not a measurable speedup;
the report says so rather than claiming a win that was not observed.

The VM wins because it avoids rebuilding AST environments and Go values on
every operation. The native backend wins again because it is one flat sequence
of machine instructions with no interpreter loop.

## Verification

```sh
go test ./...                 # all packages, including the three-way golden test
go test ./codegen -v          # assembles and executes native test programs
```

- Lexer: 15 cases, exact token positions, overflow and bad characters.
- Parser: precedence, associativity, all statement kinds, all five examples.
- Type checker: 15 negative programs, 12 exact error messages, caret output.
- Interpreter and VM: golden outputs plus recursion, short circuit, division
  by zero, deep recursion and clean stack overflow.
- Native: expressions, int64 edges, functions, nested calls, recursion,
  control flow, short circuit, ELF verification, all five golden examples.

## Native backend notes

- Registers: each expression leaves its result in `rax`; `rbx` holds the right
  operand; temporaries go on the machine stack.
- Caller convention: arguments pushed right to left, `call`, then caller adds
  `8 * argc` to `rsp`. Callee prologue is `push rbp; mov rbp,rsp; sub rsp,N`.
  Argument `i` is at `[rbp+16+8*i]`, local `n` at `[rbp-8*n]`, result in `rax`.
- User functions are emitted as `anvil_fn_<name>` so they cannot collide with
  the runtime routines or `_start`.
- `print_int` converts the magnitude using unsigned division, so it handles
  `MinInt64` without overflowing during negation.
- `anvil/elfgen`: the built-in assembler and ELF writer. It encodes the
  instruction subset that `codegen` emits, resolves labels in two passes, and
  writes an `ET_EXEC` ELF64 file with one `PT_LOAD` segment. Verified by
  building and running all five examples with no assembler installed.
- Constant folding handles literal arithmetic, comparisons, logic and unary
  operators, but never folds division by zero. Dead-code removal drops
  statements after a return, `while false`, and `if false` branches; an
  `if true` branch is replaced by its block, preserving the block's scope.


## Limits and future work

- The compiler assumes a well-typed AST; the CLI always type-checks first.
- No arrays, strings or closures; integers are the only value type besides bool.
- Bytecode has a small amount of dead code after `return` (visible in `dis`);
  constant folding and dead-code removal are stretch goals.
- **The built-in ELF writer understands only what codegen emits.** It is not a
  general assembler, but it builds all five examples with no `as`/`ld`.
- Constant folding and dead-code removal are intentionally conservative; they
  improved code size more than runtime on these examples.
- Cross-platform: `go test ./...` passes on Windows Go 1.27 (native tests skip)
  and on Debian WSL Go 1.24.4 with `as`/`ld` 2.44.
- Benchmarks: Cinder-style caveat — numbers are medians of five runs on one
  machine, not a guarantee.
