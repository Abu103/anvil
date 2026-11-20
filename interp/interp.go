// Package interp executes an Anvil program by walking the syntax tree. It is
// the reference implementation: the bytecode VM and the native backend must
// match its output on every example.
package interp

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"anvil/ast"
	"anvil/lexer"
	"anvil/parser"
	"anvil/types"
)

// Value is an Anvil int or bool.
type Value struct {
	isBool bool
	i      int64
	b      bool
}

// Int makes an int value.
func Int(v int64) Value { return Value{i: v} }

// Bool makes a bool value.
func Bool(v bool) Value { return Value{isBool: true, b: v} }

// IsBool reports whether the value is a bool.
func (v Value) IsBool() bool { return v.isBool }

// IntValue returns the int payload (zero for a bool).
func (v Value) IntValue() int64 { return v.i }

// BoolValue returns the bool payload (false for an int).
func (v Value) BoolValue() bool { return v.b }

func (v Value) String() string {
	if v.isBool {
		return strconv.FormatBool(v.b)
	}
	return strconv.FormatInt(v.i, 10)
}

// ErrDivisionByZero is raised by the interpreter at runtime.
var ErrDivisionByZero = errors.New("anvil: division by zero")

// Env is one lexical scope.
type Env struct {
	vars   map[string]Value
	parent *Env
}

// NewEnv creates an empty root scope.
func NewEnv() *Env { return &Env{vars: make(map[string]Value)} }

func (e *Env) child() *Env { return &Env{vars: make(map[string]Value), parent: e} }

func (e *Env) define(name string, v Value) { e.vars[name] = v }

func (e *Env) get(name string) (Value, bool) {
	for scope := e; scope != nil; scope = scope.parent {
		if v, ok := scope.vars[name]; ok {
			return v, true
		}
	}
	return Value{}, false
}

func (e *Env) set(name string, v Value) bool {
	for scope := e; scope != nil; scope = scope.parent {
		if _, ok := scope.vars[name]; ok {
			scope.vars[name] = v
			return true
		}
	}
	return false
}

type interpreter struct {
	funcs map[string]*ast.FuncDecl
	out   io.Writer
}

// returnSignal is used as an internal error so a return from deep inside a
// block can unwind to the function boundary. It never escapes Run.
type returnSignal struct{ value Value }

func (r *returnSignal) Error() string { return "return" }

// Run lexes, parses and executes src, writing print output to out.
func Run(src string, out io.Writer) error {
	toks, err := lexer.Lex(src)
	if err != nil {
		return err
	}
	prog, err := parser.Parse(toks)
	if err != nil {
		return err
	}
	if diags := types.Check(prog, src); len(diags) > 0 {
		var b strings.Builder
		b.WriteString("anvil: type errors:\n")
		for _, d := range diags {
			b.WriteString(d.Format())
			b.WriteByte('\n')
		}
		return errors.New(b.String())
	}
	return RunAST(prog, out)
}

// RunAST executes an already parsed (and optionally optimised) program.
func RunAST(prog *ast.Program, out io.Writer) error {
	i := &interpreter{
		funcs: make(map[string]*ast.FuncDecl, len(prog.Funcs)),
		out:   out,
	}
	for _, f := range prog.Funcs {
		i.funcs[f.Name] = f
	}

	// Top-level statements form the implicit main. Their scope is not visible
	// inside functions (SPEC section 1).
	env := NewEnv()
	for _, st := range prog.Main {
		if err := i.exec(st, env); err != nil {
			return err
		}
	}
	return nil
}

func (i *interpreter) call(f *ast.FuncDecl, args []Value) (Value, error) {
	if len(args) != len(f.Params) {
		return Value{}, i.at(f.At, "%s: got %d arguments, want %d", f.Name, len(args), len(f.Params))
	}
	// No parent: a function sees only its parameters and its own locals.
	env := NewEnv()
	for n, p := range f.Params {
		env.define(p.Name, args[n])
	}

	err := i.exec(f.Body, env)
	if err != nil {
		var ret *returnSignal
		if errors.As(err, &ret) {
			return ret.value, nil
		}
		return Value{}, err
	}
	return Value{}, i.at(f.At, "%s: reached the end without returning", f.Name)
}

func (i *interpreter) at(p ast.Position, format string, args ...any) error {
	return fmt.Errorf("anvil: line %d, column %d: %s", p.Line, p.Col, fmt.Sprintf(format, args...))
}

func (i *interpreter) exec(st ast.Stmt, env *Env) error {
	switch n := st.(type) {
	case *ast.LetStmt:
		v, err := i.eval(n.Value, env)
		if err != nil {
			return err
		}
		env.define(n.Name, v)
		return nil

	case *ast.AssignStmt:
		v, err := i.eval(n.Value, env)
		if err != nil {
			return err
		}
		if !env.set(n.Name, v) {
			return i.at(n.At, "assignment to undefined variable %q", n.Name)
		}
		return nil

	case *ast.IfStmt:
		cond, err := i.eval(n.Cond, env)
		if err != nil {
			return err
		}
		if !cond.isBool {
			return i.at(n.Cond.Pos(), "if condition must be bool")
		}
		if cond.b {
			return i.exec(n.Then, env)
		}
		if n.Else != nil {
			return i.exec(n.Else, env)
		}
		return nil

	case *ast.WhileStmt:
		for {
			cond, err := i.eval(n.Cond, env)
			if err != nil {
				return err
			}
			if !cond.isBool {
				return i.at(n.Cond.Pos(), "while condition must be bool")
			}
			if !cond.b {
				return nil
			}
			// A fresh scope per iteration, so a let in the body is new each time.
			if err := i.exec(n.Body, env); err != nil {
				return err
			}
		}

	case *ast.ReturnStmt:
		v, err := i.eval(n.Value, env)
		if err != nil {
			return err
		}
		return &returnSignal{value: v}

	case *ast.PrintStmt:
		v, err := i.eval(n.Value, env)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(i.out, v.String())
		return err

	case *ast.ExprStmt:
		_, err := i.eval(n.Expr, env)
		return err

	case *ast.BlockStmt:
		inner := env.child()
		for _, s := range n.Stmts {
			if err := i.exec(s, inner); err != nil {
				return err
			}
		}
		return nil

	default:
		return i.at(st.Pos(), "unknown statement %T", st)
	}
}

func (i *interpreter) eval(e ast.Expr, env *Env) (Value, error) {
	switch n := e.(type) {
	case *ast.IntLit:
		return Int(n.Value), nil

	case *ast.BoolLit:
		return Bool(n.Value), nil

	case *ast.Ident:
		v, ok := env.get(n.Name)
		if !ok {
			return Value{}, i.at(n.At, "undefined variable %q", n.Name)
		}
		return v, nil

	case *ast.UnaryExpr:
		v, err := i.eval(n.Right, env)
		if err != nil {
			return Value{}, err
		}
		switch n.Op {
		case "-":
			if v.isBool {
				return Value{}, i.at(n.At, "unary - needs int")
			}
			return Int(-v.i), nil // Go int64 arithmetic wraps, as the SPEC requires
		case "!":
			if !v.isBool {
				return Value{}, i.at(n.At, "! needs bool")
			}
			return Bool(!v.b), nil
		}
		return Value{}, i.at(n.At, "unknown unary operator %q", n.Op)

	case *ast.BinaryExpr:
		return i.evalBinary(n, env)

	case *ast.CallExpr:
		f, ok := i.funcs[n.Callee]
		if !ok {
			return Value{}, i.at(n.At, "undefined function %q", n.Callee)
		}
		args := make([]Value, 0, len(n.Args))
		for _, a := range n.Args {
			v, err := i.eval(a, env)
			if err != nil {
				return Value{}, err
			}
			args = append(args, v)
		}
		return i.call(f, args)

	default:
		return Value{}, i.at(e.Pos(), "unknown expression %T", e)
	}
}

func (i *interpreter) evalBinary(n *ast.BinaryExpr, env *Env) (Value, error) {
	// Short circuit before evaluating the right side.
	if n.Op == "&&" || n.Op == "||" {
		left, err := i.eval(n.Left, env)
		if err != nil {
			return Value{}, err
		}
		if !left.isBool {
			return Value{}, i.at(n.At, "%s needs bool operands", n.Op)
		}
		if n.Op == "&&" && !left.b {
			return Bool(false), nil
		}
		if n.Op == "||" && left.b {
			return Bool(true), nil
		}
		right, err := i.eval(n.Right, env)
		if err != nil {
			return Value{}, err
		}
		if !right.isBool {
			return Value{}, i.at(n.At, "%s needs bool operands", n.Op)
		}
		return Bool(right.b), nil
	}

	left, err := i.eval(n.Left, env)
	if err != nil {
		return Value{}, err
	}
	right, err := i.eval(n.Right, env)
	if err != nil {
		return Value{}, err
	}

	switch n.Op {
	case "+", "-", "*", "/":
		if left.isBool || right.isBool {
			return Value{}, i.at(n.At, "operands of %s must both be int", n.Op)
		}
		switch n.Op {
		case "+":
			return Int(left.i + right.i), nil
		case "-":
			return Int(left.i - right.i), nil
		case "*":
			return Int(left.i * right.i), nil
		default:
			q, err := divInt(left.i, right.i)
			if err != nil {
				return Value{}, i.at(n.At, "%v", err)
			}
			return Int(q), nil
		}

	case "<", ">", "<=", ">=":
		if left.isBool || right.isBool {
			return Value{}, i.at(n.At, "operands of %s must both be int", n.Op)
		}
		switch n.Op {
		case "<":
			return Bool(left.i < right.i), nil
		case ">":
			return Bool(left.i > right.i), nil
		case "<=":
			return Bool(left.i <= right.i), nil
		default:
			return Bool(left.i >= right.i), nil
		}

	case "==", "!=":
		if left.isBool != right.isBool {
			return Value{}, i.at(n.At, "cannot compare int with bool")
		}
		equal := left.isBool && left.b == right.b || !left.isBool && left.i == right.i
		if n.Op == "!=" {
			equal = !equal
		}
		return Bool(equal), nil
	}

	return Value{}, i.at(n.At, "unknown binary operator %q", n.Op)
}

// divInt implements the SPEC: truncation toward zero and MinInt64 / -1 wraps.
func divInt(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivisionByZero
	}
	if a == math.MinInt64 && b == -1 {
		return math.MinInt64, nil
	}
	return a / b, nil
}
