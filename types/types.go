// Package types checks an Anvil syntax tree before any backend runs it.
// Later phases may assume a well-typed program and skip defensive checks.
package types

import (
	"fmt"
	"strings"

	"anvil/ast"
)

// Type is an Anvil type.
type Type int

const (
	// Invalid is the error-recovery type: it stops cascading diagnostics.
	Invalid Type = iota
	Int
	Bool
)

func (t Type) String() string {
	switch t {
	case Int:
		return "int"
	case Bool:
		return "bool"
	default:
		return "invalid"
	}
}

// FuncSig is a function's parameter and return types.
type FuncSig struct {
	Params []Type
	Return Type
}

// Diagnostic is one type error with a source position.
type Diagnostic struct {
	Pos     ast.Position
	Message string
	Source  string // the whole source line containing Pos
}

// Format renders the diagnostic with a caret under the offending token.
func (d Diagnostic) Format() string {
	lineNo := fmt.Sprint(d.Pos.Line)
	pad := strings.Repeat(" ", len(lineNo))
	caret := ""
	if d.Pos.Col > 0 && d.Pos.Col <= len(d.Source)+1 {
		caret = strings.Repeat(" ", d.Pos.Col-1) + "^"
	}
	return fmt.Sprintf("error: %s\n  --> %d:%d\n   %s|\n %s | %s\n   %s| %s",
		d.Message, d.Pos.Line, d.Pos.Col, pad, lineNo, d.Source, pad, caret)
}

type checker struct {
	funcs         map[string]FuncSig
	scopes        []map[string]Type
	diags         []Diagnostic
	lines         []string
	currentReturn Type
}

// Check type-checks prog using src for error excerpts. It collects all the
// errors it can instead of stopping at the first.
func Check(prog *ast.Program, src string) []Diagnostic {
	c := &checker{
		funcs: make(map[string]FuncSig, len(prog.Funcs)),
		lines: strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n"),
	}
	c.push()

	// Register every signature first, so callers may precede callees.
	for _, f := range prog.Funcs {
		if _, dup := c.funcs[f.Name]; dup {
			c.error(f.At, "duplicate function %q", f.Name)
			continue
		}
		sig := FuncSig{Return: convertType(f.ReturnType)}
		for _, p := range f.Params {
			sig.Params = append(sig.Params, convertType(p.Type))
		}
		c.funcs[f.Name] = sig
	}

	for _, f := range prog.Funcs {
		c.checkFunc(f)
	}
	for _, st := range prog.Main {
		c.checkStmt(st, false)
	}
	return c.diags
}

func convertType(s string) Type {
	if s == "bool" {
		return Bool
	}
	return Int
}

func (c *checker) push() { c.scopes = append(c.scopes, make(map[string]Type)) }

func (c *checker) pop() { c.scopes = c.scopes[:len(c.scopes)-1] }

func (c *checker) declare(at ast.Position, name string, typ Type) {
	if _, exists := c.scopes[len(c.scopes)-1][name]; exists {
		c.error(at, "variable %q is already declared in this scope", name)
		return
	}
	c.scopes[len(c.scopes)-1][name] = typ
}

func (c *checker) lookup(name string) (Type, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if t, ok := c.scopes[i][name]; ok {
			return t, true
		}
	}
	return Invalid, false
}

func (c *checker) error(at ast.Position, format string, args ...any) {
	source := ""
	if at.Line > 0 && at.Line <= len(c.lines) {
		source = strings.TrimRight(c.lines[at.Line-1], "\r")
	}
	c.diags = append(c.diags, Diagnostic{
		Pos: at, Message: fmt.Sprintf(format, args...), Source: source,
	})
}

func (c *checker) checkFunc(f *ast.FuncDecl) {
	c.push()
	c.currentReturn = convertType(f.ReturnType)
	for _, p := range f.Params {
		c.declare(p.At, p.Name, convertType(p.Type))
	}
	c.checkStmt(f.Body, true)
	if !terminates(f.Body) {
		c.error(f.At, "function %q may finish without returning", f.Name)
	}
	c.currentReturn = Invalid
	c.pop()
}

// terminates reports whether every path through a statement ends in return.
func terminates(st ast.Stmt) bool {
	switch n := st.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		for _, s := range n.Stmts {
			if terminates(s) {
				return true
			}
		}
	case *ast.IfStmt:
		return n.Else != nil && terminates(n.Then) && terminates(n.Else)
	}
	return false
}

func (c *checker) checkCond(e ast.Expr, context string) {
	t := c.checkExpr(e)
	if t != Bool && t != Invalid {
		c.error(e.Pos(), "%s condition must be bool, got %s", context, t)
	}
}

func (c *checker) checkStmt(st ast.Stmt, inFunc bool) {
	switch n := st.(type) {
	case *ast.LetStmt:
		valueType := c.checkExpr(n.Value)
		declared := convertType(n.Type)
		if valueType != Invalid && valueType != declared {
			c.error(n.Value.Pos(), "cannot assign %s to %s", valueType, declared)
		}
		c.declare(n.At, n.Name, declared)

	case *ast.AssignStmt:
		valueType := c.checkExpr(n.Value)
		nameType, ok := c.lookup(n.Name)
		if !ok {
			c.error(n.At, "assignment to undefined variable %q", n.Name)
			return
		}
		if valueType != Invalid && valueType != nameType {
			c.error(n.Value.Pos(), "cannot assign %s to %s", valueType, nameType)
		}

	case *ast.IfStmt:
		c.checkCond(n.Cond, "if")
		c.checkStmt(n.Then, inFunc)
		if n.Else != nil {
			c.checkStmt(n.Else, inFunc)
		}

	case *ast.WhileStmt:
		c.checkCond(n.Cond, "while")
		c.checkStmt(n.Body, inFunc)

	case *ast.ReturnStmt:
		if !inFunc {
			c.error(n.At, "return outside a function")
		}
		t := c.checkExpr(n.Value)
		if c.currentReturn != Invalid && t != Invalid && t != c.currentReturn {
			c.error(n.Value.Pos(), "returning %s from a function that returns %s", t, c.currentReturn)
		}

	case *ast.PrintStmt:
		t := c.checkExpr(n.Value)
		if t != Int && t != Bool && t != Invalid {
			c.error(n.Value.Pos(), "print needs int or bool, got %s", t)
		}

	case *ast.ExprStmt:
		c.checkExpr(n.Expr)

	case *ast.BlockStmt:
		c.push()
		for _, s := range n.Stmts {
			c.checkStmt(s, inFunc)
		}
		c.pop()
	}
}

func (c *checker) checkExpr(e ast.Expr) Type {
	switch n := e.(type) {
	case *ast.IntLit:
		return Int
	case *ast.BoolLit:
		return Bool

	case *ast.Ident:
		t, ok := c.lookup(n.Name)
		if !ok {
			c.error(n.At, "undefined variable %q", n.Name)
			return Invalid
		}
		return t

	case *ast.UnaryExpr:
		t := c.checkExpr(n.Right)
		switch n.Op {
		case "-":
			if t != Invalid && t != Int {
				c.error(n.At, "unary - needs int, got %s", t)
				return Invalid
			}
			return Int
		case "!":
			if t != Invalid && t != Bool {
				c.error(n.At, "! needs bool, got %s", t)
				return Invalid
			}
			return Bool
		}
		return Invalid

	case *ast.BinaryExpr:
		return c.checkBinary(n)

	case *ast.CallExpr:
		sig, ok := c.funcs[n.Callee]
		if !ok {
			c.error(n.At, "undefined function %q", n.Callee)
			return Invalid
		}
		if len(n.Args) != len(sig.Params) {
			c.error(n.At, "%s: got %d arguments, want %d", n.Callee, len(n.Args), len(sig.Params))
		}
		for i, arg := range n.Args {
			t := c.checkExpr(arg)
			if i < len(sig.Params) && t != Invalid && t != sig.Params[i] {
				c.error(arg.Pos(), "%s: argument %d needs %s, got %s", n.Callee, i, sig.Params[i], t)
			}
		}
		return sig.Return
	}
	return Invalid
}

func (c *checker) checkBinary(n *ast.BinaryExpr) Type {
	left := c.checkExpr(n.Left)
	right := c.checkExpr(n.Right)

	switch n.Op {
	case "+", "-", "*", "/":
		if left != Invalid && left != Int {
			c.error(n.Left.Pos(), "operands of %s must both be int, got %s and %s", n.Op, left, right)
		}
		if right != Invalid && right != Int {
			c.error(n.Right.Pos(), "operands of %s must both be int, got %s and %s", n.Op, left, right)
		}
		if left != Int || right != Int {
			return Invalid
		}
		return Int

	case "<", ">", "<=", ">=":
		if left != Invalid && left != Int || right != Invalid && right != Int {
			c.error(n.At, "operands of %s must both be int, got %s and %s", n.Op, left, right)
		}
		return Bool

	case "==", "!=":
		if left != Invalid && right != Invalid && left != right {
			c.error(n.At, "cannot compare %s with %s", left, right)
		}
		return Bool

	case "&&", "||":
		if left != Invalid && left != Bool || right != Invalid && right != Bool {
			c.error(n.At, "operands of %s must both be bool, got %s and %s", n.Op, left, right)
		}
		return Bool
	}
	return Invalid
}
