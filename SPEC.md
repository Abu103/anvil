# Anvil language specification

Anvil is a small statically typed language. This document is the contract: the
lexer, parser, type checker and all three backends must agree with it. Where a
decision is arbitrary (division of `MinInt64` by `-1`, say) it is written down
here so the backends cannot quietly disagree.

## 1. Program shape

A program is a sequence of top-level items. A top-level item is either a
function declaration or a statement.

```ebnf
program   = { topLevel } ;
topLevel  = funDecl | stmt ;
```

Statements at the top level form an implicit `main` and run in order at
startup. Function declarations may appear before or after their callers;
forward references are allowed.

**Top-level `let` is a local of that implicit main.** It is not visible inside
any function. A function sees only its parameters and the `let`s it declares.
There are no globals, no closures, no arrays and no strings.

## 2. Grammar

```ebnf
funDecl    = "fn" IDENT "(" [ params ] ")" "->" type block ;
params     = param { "," param } ;
param      = IDENT ":" type ;
type       = "int" | "bool" ;
block      = "{" { stmt } "}" ;

stmt       = letStmt | assignStmt | ifStmt | whileStmt | returnStmt
           | printStmt | callStmt | block ;

letStmt    = "let" IDENT ":" type "=" expr ";" ;
assignStmt = IDENT "=" expr ";" ;
ifStmt     = "if" "(" expr ")" block [ "else" ( ifStmt | block ) ] ;
whileStmt  = "while" "(" expr ")" block ;
returnStmt = "return" expr ";" ;
printStmt  = "print" "(" expr ")" ";" ;
callStmt   = call ";" ;

expr       = or ;
or         = and { "||" and } ;
and        = equality { "&&" equality } ;
equality   = comparison { ( "==" | "!=" ) comparison } ;
comparison = additive { ( "<" | ">" | "<=" | ">=" ) additive } ;
additive   = multiplicative { ( "+" | "-" ) multiplicative } ;
multiplicative = unary { ( "*" | "/" ) unary } ;
unary      = ( "-" | "!" ) unary | primary ;
primary    = INT | "true" | "false" | call | IDENT | "(" expr ")" ;
call       = IDENT "(" [ args ] ")" ;
args       = expr { "," expr } ;
```

Precedence, loosest first: `||`, `&&`, `== !=`, `< > <= >=`, `+ -`, `* /`,
unary `- !`, call, parentheses. All binary operators are left-associative.

`if` and `while` conditions require parentheses. `else if` chains without
extra braces.

## 3. Lexical rules

- Keywords: `fn let if else while return print true false int bool`.
- Identifiers: letter or `_`, then letters, digits or `_`. `print` and the type
  names are reserved and cannot be used as identifiers.
- Integers: decimal digits, no sign (unary `-` handles negatives, so `a -1` and
  `a - 1` are the same). Must fit in a signed 64-bit integer.
- Comments: `//` to end of line. No block comments.
- Whitespace: spaces, tabs, CR, LF. `;` is required after every statement.
- Every token carries line and column, 1-based.

## 4. Values and types

Two types: `int` (signed 64-bit, two's complement, **wrapping** on overflow) and
`bool` (`true`, `false`).

- Arithmetic `+ - * /` take `int` and return `int`. There is no `%`; the
  remainder is `n - (n / d) * d`, which the examples use deliberately.
- `/` truncates toward zero: `-7 / 2 == -3`, `7 / -2 == -3`.
- `MinInt64 / -1 == MinInt64`. x86 `idiv` would fault; the backends must special
  case it so all three agree.
- Division by zero is a runtime error: print `anvil: division by zero` to
  stderr and exit with status 1, from every backend.
- Comparisons `< > <= >=` take `int` and return `bool`.
- `== !=` take two values of the **same** type (int/int or bool/bool) and return
  `bool`. There is no implicit conversion anywhere: `1 == true` is a type error.
- `&& ||` take `bool` and return `bool`, short-circuiting left to right.
- `!` takes `bool`; unary `-` takes `int`.

## 5. Statements and scoping

- `let` introduces a variable in the current block. A `let` in an inner block
  may shadow an outer name of any type.
- Assignment `x = e;` writes an existing variable, checking that the types
  match. Assigning to an undeclared name is an error.
- `if`/`while` bodies are blocks and open a new scope. A `let` declared in a
  loop body is fresh on every iteration.
- `return` always carries a value. Every function declares a return type, and
  the type checker requires **every path** to return.
- `print(e)` writes one line: `int` in decimal with an optional `-`, `bool` as
  `true` or `false`, then `\n`.
- Calling a function with the wrong number or types of arguments is an error.
- Recursion is supported and unbounded except by the stack.

## 6. Errors

Compile-time errors are reported with line, column, a source line and a caret
under the offending token, and the compiler collects several before giving up:

```
error: operands of + must both be int, got int and bool
  --> examples/bad.av:3:15
   |
 3 |   let x: int = 1 + true;
   |                   ^
```

Runtime errors (division by zero) print one line and exit 1. No exceptions, no
unwinding.

## 7. Example programs

`examples/` holds five programs and their exact expected output:

| Program | What it exercises |
|---|---|
| `arithmetic.av` | precedence, associativity, truncating division, wrapping, bool logic |
| `factorial.av` | recursion, `if`, int64-sized results |
| `fibonacci.av` | recursion plus a top-level `while` loop |
| `gcd.av` | `while`, block scoping, remainder without `%` |
| `primes.av` | nested loops through a function call, bool return, 1229 primes below 10000 |

Every `.av` file has a sibling `.expected` file with the exact bytes the
program must print. Those files are the golden tests for the interpreter, the
VM and the native backend alike.
