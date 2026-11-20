# Anvil demo script (5 minutes)

All commands run from `anvil/` in Debian WSL 2. Have a terminal open and a
projector showing the same window. Build the CLI first:

```sh
go build -o /tmp/anvil ./cmd/anvil
```

---

## 0:00 — What it is (30 s)

> "Anvil is a compiler for a small typed language, written from scratch in Go.
> It has three backends: a tree-walking interpreter, a bytecode VM, and a real
> x86-64 ELF binary. No LLVM, no libc."

Show the pipeline from `README.md` (or draw it):

```
.av -> lexer -> parser -> type checker -> interpreter
                                      -> bytecode -> VM
                                      -> assembly -> as -> ld -> ELF
```

> "Every stage is a working compiler. The interpreter is the oracle the other
> two must match."

## 0:30 — Interpreter (40 s)

```sh
/tmp/anvil run examples/arithmetic.av
/tmp/anvil run examples/factorial.av
/tmp/anvil run examples/fibonacci.av
```

> "Arithmetic with wrapping and truncating division, recursion, and a
> recursive function called from a while loop. The `.expected` files are the
> golden output."

## 1:10 — Bytecode and VM (50 s)

```sh
/tmp/anvil dis examples/fibonacci.av
/tmp/anvil vm examples/fibonacci.av
```

Point at the disassembly:

> "This is the flat bytecode: loads, a constant, a comparison, backpatched
> jumps, and calls. The VM runs this on a value stack with real call frames."

## 2:00 — Type errors (35 s)

```sh
printf 'let x: int = 1 + true;\nprint(x);\n' > /tmp/bad.av
/tmp/anvil run /tmp/bad.av
```

Expected:

```
error: operands of + must both be int, got int and bool
  --> 1:18
    |
 1 | let x: int = 1 + true;
    |                  ^
```

> "The checker runs before every backend, reports a line and a caret, and
> collects several errors at once. Later stages can assume a well-typed tree."

## 2:35 — Native binary (60 s)

```sh
/tmp/anvil build examples/primes.av -o /tmp/primes
file /tmp/primes
/tmp/primes
```

Expected: `ELF 64-bit LSB executable, x86-64, ... statically linked`, then `1229`.

> "That is hand-written assembly, assembled with `as` and linked with `ld`.
> The print routine uses the `write` syscall directly; the binary does not
> call C. Counting primes below ten thousand is about a thousand times slower
> in the interpreter. Adding `--no-as` uses our own encoder and ELF writer
> instead, with no assembler installed."

If asked about `--emit-asm`:

```sh
/tmp/anvil build examples/arithmetic.av -o /tmp/arith --emit-asm | head -30
```

## 3:35 — Benchmarks (40 s)

Show the table from `README.md`:

| Program | Interpreter | VM | Native |
|---|---|---|---|
| `fib(35)` | 7.80 s | 1.91 s | 92.9 ms |
| primes < 10,000 | 40.6 ms | 16.7 ms | 1.08 ms |

> "Five runs each, medians, on two WSL cores. Native includes process startup
> and is still 84 times faster on `fib(35)`."

## 4:15 — The golden test (40 s)

```sh
go test ./integration -run TestThreeWayGolden -v
```

Expected: five subtests pass.

> "This compiles and runs every example through all three backends, compares
> each against its expected file, and then against each other. It is the
> regression gate for every change."

## 4:55 — Close (15 s)

> "Anvil meets the roadmap at every stage: programs run after week two, a
> complete VM compiler after week three, and a native ELF by week four. The
> next optimisation is constant folding and dead-code removal, protected by
> the same golden test."

---

## If asked: why a call convention that is not System V?

> "Anvil never calls C code, so the ABI buys us nothing. Caller pushes
> arguments right to left and cleans up; callee keeps locals below `rbp` and
> parameters at `[rbp+16+8*i]`. It is a dozen instructions of prologue and
> epilogue, and it makes the generated assembly easy to read."

## If asked: how do you handle `MinInt64 / -1`?

> "x86 `idiv` faults and Go panics, so both backends special-case it and return
> `MinInt64`. `print_int` also converts the magnitude with unsigned division,
> so printing the same value does not overflow. It is in `SPEC.md` because
> otherwise the three backends would quietly disagree."
