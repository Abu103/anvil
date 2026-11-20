// Package opt applies two safe AST optimisations before code generation:
// constant folding and removal of unreachable statements. Both are guarded by
// the three-way golden test and by tests that compare interpreted output before
// and after optimisation.
package opt

import (
	"anvil/ast"
	"math"
)

// Stats counts the changes made.
type Stats struct {
	Folded  int
	Dropped int
}

// Optimize rewrites prog in place.
func Optimize(prog *ast.Program) Stats {
	s := Stats{}
	for i := range prog.Funcs {
		optimizeFunc(prog.Funcs[i], &s)
	}
	prog.Main = optimizeStmts(prog.Main, &s)
	return s
}

type constant struct {
	isBool bool
	i      int64
	b      bool
}

func constOf(e ast.Expr) (constant, bool) {
	switch n := e.(type) {
	case *ast.IntLit:
		return constant{i: n.Value}, true
	case *ast.BoolLit:
		return constant{isBool: true, b: n.Value}, true
	}
	return constant{}, false
}

func optimizeFunc(f *ast.FuncDecl, s *Stats) {
	f.Body.Stmts = optimizeStmts(f.Body.Stmts, s)
}

func optimizeExpr(e ast.Expr, s *Stats) ast.Expr {
	switch n := e.(type) {
	case *ast.UnaryExpr:
		n.Right = optimizeExpr(n.Right, s)
		if c, ok := constOf(n.Right); ok {
			switch n.Op {
			case "-":
				if !c.isBool {
					s.Folded++
					return &ast.IntLit{At: n.At, Value: -c.i}
				}
			case "!":
				if c.isBool {
					s.Folded++
					return &ast.BoolLit{At: n.At, Value: !c.b}
				}
			}
		}
		return n

	case *ast.BinaryExpr:
		n.Left = optimizeExpr(n.Left, s)
		n.Right = optimizeExpr(n.Right, s)
		if folded, ok := foldBinary(n); ok {
			s.Folded++
			return folded
		}
		return n

	case *ast.CallExpr:
		for i := range n.Args {
			n.Args[i] = optimizeExpr(n.Args[i], s)
		}
		return n
	}
	return e
}

func foldBinary(n *ast.BinaryExpr) (ast.Expr, bool) {
	a, okA := constOf(n.Left)
	b, okB := constOf(n.Right)
	if !okA || !okB {
		return nil, false
	}

	switch n.Op {
	case "+", "-", "*", "/":
		if a.isBool || b.isBool {
			return nil, false
		}
		if n.Op == "/" && b.i == 0 {
			return nil, false // a runtime error must stay a runtime error
		}
		var v int64
		switch n.Op {
		case "+":
			v = a.i + b.i
		case "-":
			v = a.i - b.i
		case "*":
			v = a.i * b.i
		case "/":
			if a.i == math.MinInt64 && b.i == -1 {
				v = math.MinInt64 // SPEC rule; Go would panic
			} else {
				v = a.i / b.i
			}
		}
		return &ast.IntLit{At: n.At, Value: v}, true

	case "<", ">", "<=", ">=":
		if a.isBool || b.isBool {
			return nil, false
		}
		var v bool
		switch n.Op {
		case "<":
			v = a.i < b.i
		case ">":
			v = a.i > b.i
		case "<=":
			v = a.i <= b.i
		case ">=":
			v = a.i >= b.i
		}
		return &ast.BoolLit{At: n.At, Value: v}, true

	case "==", "!=":
		if a.isBool != b.isBool {
			return nil, false
		}
		equal := a.isBool && a.b == b.b || !a.isBool && a.i == b.i
		if n.Op == "!=" {
			equal = !equal
		}
		return &ast.BoolLit{At: n.At, Value: equal}, true

	case "&&", "||":
		if !a.isBool || !b.isBool {
			return nil, false
		}
		v := a.b && b.b
		if n.Op == "||" {
			v = a.b || b.b
		}
		return &ast.BoolLit{At: n.At, Value: v}, true
	}
	return nil, false
}

func optimizeStmts(list []ast.Stmt, s *Stats) []ast.Stmt {
	out := make([]ast.Stmt, 0, len(list))
	for i, st := range list {
		st = optimizeStmt(st, s)
		if st == nil {
			continue
		}
		out = append(out, st)
		if alwaysReturns(st) {
			s.Dropped += len(list) - i - 1
			break
		}
	}
	return out
}

func optimizeStmt(st ast.Stmt, s *Stats) ast.Stmt {
	switch n := st.(type) {
	case *ast.LetStmt:
		n.Value = optimizeExpr(n.Value, s)
		return n
	case *ast.AssignStmt:
		n.Value = optimizeExpr(n.Value, s)
		return n
	case *ast.PrintStmt:
		n.Value = optimizeExpr(n.Value, s)
		return n
	case *ast.ExprStmt:
		n.Expr = optimizeExpr(n.Expr, s)
		return n
	case *ast.ReturnStmt:
		n.Value = optimizeExpr(n.Value, s)
		return n

	case *ast.BlockStmt:
		n.Stmts = optimizeStmts(n.Stmts, s)
		return n

	case *ast.WhileStmt:
		n.Cond = optimizeExpr(n.Cond, s)
		if c, ok := constOf(n.Cond); ok {
			falsy := c.isBool && !c.b || !c.isBool && c.i == 0
			if falsy {
				s.Dropped++
				return nil // while false never executes
			}
		}
		n.Body = optimizeStmt(n.Body, s).(*ast.BlockStmt)
		return n

	case *ast.IfStmt:
		n.Cond = optimizeExpr(n.Cond, s)
		if c, ok := constOf(n.Cond); ok && c.isBool {
			s.Dropped++
			if c.b {
				// Keep the block wrapper: inlining its statements would change
				// the scope of a `let` declared in the branch.
				return optimizeStmt(n.Then, s)
			}
			if n.Else != nil {
				return optimizeStmt(n.Else, s)
			}
			return nil
		}
		n.Then = optimizeStmt(n.Then, s).(*ast.BlockStmt)
		if n.Else != nil {
			n.Else = optimizeStmt(n.Else, s)
		}
		return n
	}
	return st
}

// alwaysReturns reports whether control cannot continue after st.
func alwaysReturns(st ast.Stmt) bool {
	switch n := st.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		for _, s := range n.Stmts {
			if alwaysReturns(s) {
				return true
			}
		}
	case *ast.IfStmt:
		return n.Else != nil && alwaysReturns(n.Then) && alwaysReturns(n.Else)
	}
	return false
}
