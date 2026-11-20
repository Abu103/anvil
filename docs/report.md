# Anvil: a from-scratch compiler with three backends

**BCS753 Project-I — project report**

## Abstract

Anvil compiles a small statically typed language to three targets: a
tree-walking interpreter, a stack virtual machine, and a native x86-64 Linux
ELF executable. The language supports signed 64-bit integers, booleans,
functions with recursion, `let`, assignment, `if`/`else`, `while`, `return` and
`print`. The compiler has no external dependencies beyond the Go standard
library; the native backend emits GNU assembler text and calls `as` and `ld`,
with no LLVM and no C runtime. All five example programs produce byte-identical
output on all three backends. On `fib(35)` the medians are 7.80 s
(interpreter), 1.91 s (VM) and 92.9 ms (native binary), a factor of 84 between
the first and the last.

## 1. Goals and scope

The project had two competing requirements: always have something submittable,
and end with a real binary. The roadmap solved this by making each stage a
complete compiler: the interpreter runs real programs, the VM is a faster and
safer backend, and native code is an additive final stage. Cut rules remove the
native backend first, never the tests or the report.

The non-goals are deliberate. There are no arrays, strings, closures,
exceptions or modules. Each of those would add a memory layout decision that
has to be repeated in three backends. Keeping the language tiny made it
possible to finish all three.

## 2. Language design

A compiler is only as clear as the language it accepts, so `SPEC.md` was
written before any code. The grammar is EBNF, precedence is a fixed table, and
the integer rules are written down where the backends could disagree.

The language has exactly two types. `int` is a signed 64-bit integer with
wrapping overflow, and `bool` has `true` and `false`. There is no implicit
conversion, so `1 + true` and `1 == true` are errors. The precedence table,
from loosest to tightest, is `||`, `&&`, `== !=`, `< > <= >=`, `+ -`, `* /`,
unary `- !`, call. All binary operators are left-associative.

Three details were specified explicitly because they are where backends
diverge:

1. **Division truncates toward zero.** `-7 / 2` is `-3`, not `-4`. Go, C and
   x86 `idiv` agree here, but a Python-based reference would not.
2. **`MinInt64 / -1` wraps to `MinInt64`.** x86 `idiv` raises a fault for this
   case and Go panics on int64 overflow, so both backends check it explicitly.
3. **Short circuit is semantic.** `false && (1/0 == 0)` must not evaluate the
   division. This is tested on all three backends.

There is no `%` operator. The two programs that need remainders (`gcd` and
`primes`) compute `n - (n/d)*d`, which works because division truncates and the
operands are positive. Adding `%` would have meant a new opcode and a new x86
sequence for one convenience.

Scoping is deliberately restricted: top-level `let` belongs to the implicit
`main` and is invisible inside functions. A function sees its parameters and
its own block-scoped locals. This removes the need for global storage from all
three backends at the cost of a slightly less convenient language.

## 3. Architecture

### 3.1 Lexer and parser

The lexer scans bytes and emits tokens carrying 1-based line and column. It
skips whitespace and `//` comments, recognises maximal-munch operators (`==`,
`->`, `&&`), and rejects an integer outside int64 with a position. Line and
column were stored from day one; adding them later would have meant touching
every later file.

The parser uses recursive descent for statements and a Pratt expression parser
with a binding-power table. Precedence is therefore one table rather than six
near-identical functions. Left associativity comes from parsing the right
operand with the operator's own power, so an equal-power operator is not
consumed. Errors are collected with a position and never panic; a recovery
routine skips to the next statement boundary so several errors can be reported
in one run.

### 3.2 Type checker

The checker runs before any backend. It maintains a stack of scopes, a table
of function signatures registered before bodies are checked (so forward
references work), and a list of diagnostics. It checks operand types,
conditions, argument counts and types, assignments, and that every path of a
non-void function returns. Diagnostics include the source line and a caret,
and several are collected per run.

The all-paths-return rule is enforced by a small structural analysis: a block
terminates if any of its statements does, and an `if` terminates only if both
branches do. Without this check a native function can fall off the end and
return whatever is in `rax`, which is exactly the kind of bug that is hard to
see in a demo.

### 3.3 Interpreter

The tree walker is the oracle. Values are an `int64`/`bool` union; each scope
is a map with a parent pointer, blocks get fresh scopes, and function calls get
a scope with no parent, matching the language rule. `return` unwinds through
blocks with an internal sentinel error. The recursion test runs to depth
10,000, which Go's growable stacks handle.

### 3.4 Bytecode compiler and VM

The bytecode is a stack machine: instructions such as `Const`, `Load`, `Add`
and `Call` operate on a value stack. Each function compiles to its own
instruction slice with a constant pool and numbered local slots. `if` and
`while` use jumps with backpatching: the compiler emits a placeholder, records
its index, and writes the real target once the body is emitted. The
disassembler prints every function with offsets and was used throughout
debugging.

The VM keeps a pre-sized value stack and an explicit stack pointer, with call
frames storing the instruction pointer and a base index. A call uses the
arguments already on the stack as the callee's parameter slots; `Ret` rewinds
the stack pointer to the call's base and leaves the result there. Infinite
recursion produces a clean error instead of a Go panic.

### 3.5 Native code generator

The native backend walks the AST directly. Every expression leaves its value
in `rax`. For a binary operation it evaluates the left operand, pushes `rax`,
evaluates the right operand, moves it into `rbx`, pops the left operand back
into `rax`, and applies the operation. This is simple and composes for nested
expressions at the cost of extra stack traffic, which the stretch goal of
register allocation would remove.

Comparisons use `setcc` followed by `movzx rax, al`, turning the boolean into
0 or 1. Short circuiting uses a conditional jump and a constant result on the
skipped path. `if` and `while` use `cmp rax, 0` plus `je`/`jmp` with labels from
a counter, so duplicate names cannot occur.

`print` is a hand-written runtime routine. `anvil_print_int` computes the
magnitude into an unsigned value, divides by ten repeatedly into a stack
buffer, inserts a minus sign if needed, and issues two `write` syscalls (digits
and newline). Using unsigned division for the magnitude is what makes
`MinInt64` print correctly. `anvil_print_bool` selects one of two constant
strings. Division by zero jumps to a routine that writes a message to stderr
and exits with status 1. `ld` links the result as a static ELF with `_start`
as the entry point.

### 3.6 Calling convention

The convention is intentionally not the System V ABI, because Anvil never
calls C code. The caller pushes arguments right to left, executes `call`, and
adds `8 * argc` to `rsp` afterwards. The callee pushes `rbp`, sets `rbp = rsp`,
and subtracts space for locals. After that, parameter `i` is at
`[rbp+16+8*i]` (the saved `rbp` and return address occupy the first 16 bytes)
and local `n` is at `[rbp-8*n]`. `return` evaluates into `rax` and jumps to a
per-function epilogue label that restores `rsp`, pops `rbp` and returns. User
functions are emitted as `anvil_fn_<name>` to avoid clashes with `_start` and
the runtime routines.

## 4. Correctness strategy

The golden test is the centre of the project. For each of the five examples it
runs the interpreter, the VM and a freshly built native binary, compares all
three outputs to the `.expected` file, and then compares them to each other.
It runs on every `go test ./...`.

Supporting tests pin the boundaries: lexer positions and overflow; parser
precedence, associativity and all five example files; type checker errors with
exact messages; interpreter and VM short circuit, division by zero and deep
recursion; native arithmetic at int64 edges, nested calls, five-parameter
frames, recursion, control flow and short circuit. `anvil build` is checked by
running `file` on the output and asserting that it is ELF.

This strategy found two real bugs during development: a short-circuit branch
that pushed the wrong boolean constant, and a type checker that did not compare
`return` expressions with the declared return type. Both were caught by tests
before the three-way golden test was written, which is why the golden test is
the final check rather than the first.

## 5. Evaluation

Environment: Debian WSL 2 on an i5-1135G7 with two cores, Go 1.24.4, binutils
2.44. Each benchmark was run five times with `-benchtime=1x`; the median is
reported. Native timings include process startup.

| Program | Interpreter | VM | Native | VM speedup | Native speedup |
|---|---|---|---|---|---|
| `fib(35)` | 7.80 s | 1.91 s | 92.9 ms | 4.1x | 84x |
| primes < 10,000 | 40.6 ms | 16.7 ms | 1.08 ms | 2.4x | 38x |

An earlier measurement of `fib(32)` gave 1.85 s for the tree walker and 0.385 s
for the VM, a factor of 4.8. The larger input widens the gap because the VM's
per-call overhead grows more slowly than the interpreter's allocation and type
switching.

The bootstrap cost of the native path is about a millisecond of assembler and
linker time, plus process startup in the benchmark. For programs that run for
tens of milliseconds or more, the native output is decisively faster. For a
single small expression the three backends are indistinguishable to a human.

## 6. Limitations

- **The built-in ELF writer is not a general assembler.** `--no-as` encodes
  exactly the instruction subset `codegen` emits and builds all five examples
  with no `as`/`ld`, but it would need extending for new instructions.
- **No register allocation.** Every binary operation pushes and pops `rax`.
  The generated code is correct and simple but does more memory traffic than a
  production compiler would.
- **The optimisations are conservative.** Constant folding and dead-code
  removal improved code size more than runtime on the measured examples.
- **Cross-platform scope.** The pure-Go stages pass on Windows Go 1.27; native
  tests skip there because the target is x86-64 Linux. Cinder, by contrast,
  passes its full suite on both platforms.
- **No arrays or strings.** Output is integers and booleans only.
- **Recursive native calls use the machine stack** with no overflow check
  beyond the operating system's guard page, while the VM checks explicitly.
- **Compilation is in-process but single-shot.** There is no incremental
  compilation or caching.
- **The three-way golden test skips native output off Linux**, which is the
  documented target platform.

## 7. The stretch work, and what remains

The stretch items were attempted after steps 1–11 were green.

**Constant folding and dead-code removal.** `anvil/opt` walks the AST, folds
literal arithmetic, comparisons, logic and unary operators, never folds
division by zero, drops statements after a return, removes `while false` and
`if false` branches, and replaces `if true` with its block. Keeping the block
wrapper matters: inlining its statements would change the scope of a `let`
declared in the branch, a bug caught by a dedicated test. Output equality on
all five examples is tested before and after optimisation.

The measured effect is small. Median native time for `fib(35)` moved from
73.8 ms to 72.8 ms and primes moved from 0.68 ms to 0.89 ms, both inside the
noise of this machine. The benefit is a cleaner listing, not speed, and the
report does not claim otherwise. `--no-opt` exposes the switch for A/B runs.

**A direct ELF writer.** `anvil/elfgen` parses the assembly that `codegen`
emits, encodes the instruction subset in two passes (labels first, then bytes),
and writes an `ET_EXEC` ELF64 file with a single `PT_LOAD` segment. `anvil build
--no-as` uses it; all five examples build and run correctly with no assembler
installed, and a binary was verified to run with an empty environment. A bug
during this work is worth recording: relative branches were initially one byte
off because the displacement helper already subtracted the instruction length,
and the caller subtracted it again. The five-example golden test caught it as a
segmentation fault rather than a wrong answer, which is why it was fixed from
the disassembly instead of by guessing.

A second bug appeared after the first golden run: `B8 imm32` writes EAX and
zero-extends into RAX, so negative constants printed as large positive numbers.
The `as`/`ld` path was correct because GNU `as` picks a sign-extending encoding
for the same source. The fix was `48 C7 C0 imm32`, pinned by a dedicated CLI
test and by a new `--no-as` leg in the golden test. Both bugs make the same
point: test the exact path the user runs, and compare against an independent
oracle instead of trusting a check that does not reach that path.

Remaining future work:

1. A tiny register allocator for expression temporaries, replacing push/pop
   pairs and re-running the benchmarks.
2. A `%` operator, once its interaction with negative operands is specified.
3. Arrays with a fixed layout in a stack frame or data section.
4. Extending `elfgen` beyond the current instruction subset if codegen grows.

## 8. Appendices

### Appendix A: the bytecode instruction set

| Opcode | Meaning |
|---|---|
| `Const k` | push constant `k` onto the value stack |
| `Load n` | push local slot `n` |
| `Store n` | pop a value into local slot `n` |
| `Add Sub Mul Div` | pop two values, push the result |
| `Lt Le Gt Ge Eq Ne` | pop two values, push a 0/1 bool |
| `Neg Not` | unary arithmetic and logical negation |
| `Jump t` | set the instruction pointer to `t` |
| `JumpIfFalse t` / `JumpIfTrue t` | pop a condition and branch |
| `Call f` | call function `f`; its arguments are already on the stack |
| `Ret` | return to the caller with `rax`-equivalent top-of-stack |
| `Print` / `Pop` | print and discard the top of stack |
| `Halt` | stop the VM |

Control flow is compiled with backpatching. For `if`, the compiler emits
`JumpIfFalse` with a placeholder, compiles the then-branch, emits a `Jump` over
the else-branch, patches the first jump to the else label, compiles the
else-branch, and patches the second jump to the end. `while` records the start
index, emits the condition and an exit jump, compiles the body, emits a back
edge to the start, and patches the exit.

### Appendix B: a worked native example

`print(1 + 2 * 3);` compiles to this shape (labels shortened):

```
_start:
  push rbp
  mov rbp, rsp
  sub rsp, 0
  mov rax, 1
  push rax
  mov rax, 2
  push rax
  mov rax, 3
  mov rbx, rax
  pop rax
  imul rax, rbx
  mov rbx, rax
  pop rax
  add rax, rbx
  call anvil_print_int
  mov rdi, 0
  mov rax, 60
  syscall
```

`anvil_print_int` divides the magnitude by ten repeatedly, stores digits in a
stack buffer, prepends a minus sign when needed, and issues two `write`
syscalls. The complete generated file also contains `.intel_syntax noprefix`,
`.globl _start`, the `.rodata` strings, and the division-by-zero routine.

### Appendix C: decision log

| Decision | Reason |
|---|---|
| Write `SPEC.md` before code | Three backends must agree on division, overflow and short circuit |
| No `%` operator | Remainders are expressible with `/` and `*`; one less backend case |
| Top-level `let` invisible in functions | No global storage in any backend |
| User functions emitted as `anvil_fn_*` | Prevents clashes with `_start` and runtime routines |
| Own calling convention | Anvil never calls C, so the System V ABI buys nothing |
| `RWMutex`-style scopes in native codegen | Nested blocks can shadow names without slot reuse bugs |
| Tree walker as the oracle | A backend disagreeing with it is demonstrably wrong |
| Golden test before the final report | Makes later changes safe to attempt |

### Appendix D: commands used for verification

```
cd anvil
go test ./... -count=1                       # all packages, incl. three-way golden
go test ./integration -run TestThreeWayGolden -v
go run ./cmd/anvil run   examples/fibonacci.av
go run ./cmd/anvil vm    examples/fibonacci.av
go run ./cmd/anvil dis   examples/fibonacci.av
go run ./cmd/anvil build examples/primes.av -o /tmp/primes
file /tmp/primes
go test ./integration -run '^$' -bench . -benchtime=1x -count=5
```

On Windows, `go test ./...` also passes; the native tests are skipped because
the x86-64 Linux backend cannot run there.

### Appendix E: one program through every stage

The factorial example makes a good walkthrough because it touches every phase.

| Stage | Representation |
|---|---|
| Source | `fn factorial(n: int) -> int { if (n <= 1) { return 1; } return n * factorial(n - 1); }` |
| Tokens | `fn factorial ( n : int ) -> int { if ( n <= 1 ) { return 1 ; } ... }` |
| AST | `FuncDecl factorial` → `IfStmt` → `ReturnStmt`, then `ReturnStmt(BinaryExpr *)` |
| Types | `n: int`, condition `bool`, both returns `int`, call arity 1 checked |
| Bytecode | `Load 0; Const 1; Le; JumpIfFalse; Const 1; Ret; ... Call 0; Mul; Ret` |
| Native | `push rbp; mov rbp,rsp; sub rsp,8; mov rax,[rbp+16]; ... call anvil_fn_factorial; imul rax,rbx; ... ret` |
| Output | `3628800`, `2432902008176640000` on all three backends |

Two things are worth noticing. First, the AST still contains the `if` as a tree
while the bytecode already has flat jumps: that is the point of lowering.
Second, the native version has no idea what a type is; all type information was
consumed by the checker and by the choice of print routine. That separation is
why adding a new backend does not disturb the earlier ones.

### Lessons learned

The most useful habit was to keep an executable oracle. When the VM printed
`true` where the interpreter printed `false`, the golden test identified the
exact example and line; the short-circuit bug was then a five-minute fix.
Without the oracle, the same bug would have been a confusing native-only
symptom.

The second was to write down arbitrary decisions. `MinInt64 / -1` took one
line in `SPEC.md` and prevented three different behaviours. The same applies
to top-level scope and print formatting. A specification is not documentation
for its own sake; it is the place where independent implementations meet.

### Appendix F: reproducibility and threats to validity

Every reported number can be reproduced from a clean checkout. `go.mod`
declares `go 1.22`; the suite passes with Go 1.24.4 in WSL and Go 1.27 on
Windows. The toolchain used for the benchmark numbers was GNU binutils 2.44 and
WSL 2 with a Debian root filesystem on an i5-1135G7 with two cores. `go test ./... -count=1` runs every test from
scratch, including the native tests that assemble, link and execute binaries
in temporary directories. The benchmarks use `-count=5 -benchtime=1x` and the
medians are given in section 5; a single run on a busier machine can differ by
a factor of two on the small primes program, which is why five runs matter.

Three threats to validity are worth stating. First, native timings include
process startup while interpreter and VM timings do not. This is how a user
actually invokes each backend, but it inflates the native denominator for very
short programs. The primes program runs in about a millisecond natively, so
startup is a large fraction of that number; the 38x speedup should be read as
an order of magnitude, not a precise ratio.

Second, the golden programs are small and integer-only. They exercise
recursion, loops, short circuit and sign handling, but they do not stress heap
allocation, large call frames or deep expression nesting. A compiler bug in a
code path the examples never reach would not be caught. The negative type
tests cover many more paths than the positive golden set, so the checker is
better exercised than the backends.

Third, stack overflow in the native backend is not handled by Anvil. Deep
recursion eventually reaches the operating system's guard page and the process
dies from a signal, whereas the VM prints a clean error. This is a real
difference between the backends, not just a benchmark artefact, and it is
listed as a limitation in section 6.

Finally, the source Markdown remains the canonical report; the PDF is
generated by `tools/md2pdf.py` and can be regenerated after any edit. That
keeps the numbers in the document tied to the numbers in the README.

## 9. Conclusion

Anvil meets the roadmap's definition of done at every stage: step 4 ran real
programs, step 7 produced a complete VM compiler, and step 10 produced a real
statically linked x86-64 ELF with no LLVM and no libc. The design decision that
paid off most was writing the language specification first, especially the
division and overflow rules, because those are the places where three
independently written backends would otherwise disagree. The decision that
paid off second most was the three-way golden test, which makes every later
optimisation safe to attempt.

## References

1. R. Nystrom, *Crafting Interpreters* (online book), chapters on scanning,
   parsing, evaluation and bytecode virtual machines.
2. M. matklad, *Simple but Powerful Pratt Parsing*.
3. W. Toomey, *A Compiler Writing Journey* (online series).
4. E. Bendersky, *A Type Checker in Go* and related compiler posts.
5. The Go Authors, package documentation for `go/ast`, `os/exec`, `testing`.
6. Intel Corporation, *Intel 64 and IA-32 Architectures Software Developer's
   Manual*, Intel syntax and instruction reference.
7. GNU Binutils documentation, *GNU as* and *GNU ld* manuals.
