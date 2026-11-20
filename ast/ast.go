// Package ast defines the syntax tree shared by the parser, type checker and
// all three backends. Every node keeps the position of its first token.
package ast

import (
	"fmt"
	"io"
	"strings"
)

// Position is a 1-based line and column in the source file.
type Position struct {
	Line int
	Col  int
}

func (p Position) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Expr is an expression node.
type Expr interface {
	Pos() Position
	exprNode()
}

// Stmt is a statement node.
type Stmt interface {
	Pos() Position
	stmtNode()
}

// Program holds all function declarations and the top-level statements that
// form the implicit main.
type Program struct {
	Funcs []*FuncDecl
	Main  []Stmt
}

// Expressions.

type IntLit struct {
	At    Position
	Value int64
	Text  string
}

type BoolLit struct {
	At    Position
	Value bool
}

type Ident struct {
	At   Position
	Name string
}

type UnaryExpr struct {
	At    Position
	Op    string
	Right Expr
}

type BinaryExpr struct {
	At    Position
	Op    string
	Left  Expr
	Right Expr
}

type CallExpr struct {
	At     Position
	Callee string
	Args   []Expr
}

// Statements.

type LetStmt struct {
	At    Position
	Name  string
	Type  string
	Value Expr
}

type AssignStmt struct {
	At    Position
	Name  string
	Value Expr
}

type IfStmt struct {
	At   Position
	Cond Expr
	Then *BlockStmt
	Else Stmt // *BlockStmt or *IfStmt; nil when there is no else
}

type WhileStmt struct {
	At   Position
	Cond Expr
	Body *BlockStmt
}

type ReturnStmt struct {
	At    Position
	Value Expr
}

type PrintStmt struct {
	At    Position
	Value Expr
}

type ExprStmt struct {
	At   Position
	Expr Expr // normally a CallExpr
}

type BlockStmt struct {
	At    Position
	Stmts []Stmt
}

// Declarations.

type Param struct {
	At   Position
	Name string
	Type string
}

type FuncDecl struct {
	At         Position
	Name       string
	Params     []Param
	ReturnType string
	Body       *BlockStmt
}

func (n *IntLit) Pos() Position     { return n.At }
func (n *BoolLit) Pos() Position    { return n.At }
func (n *Ident) Pos() Position      { return n.At }
func (n *UnaryExpr) Pos() Position  { return n.At }
func (n *BinaryExpr) Pos() Position { return n.At }
func (n *CallExpr) Pos() Position   { return n.At }

func (n *LetStmt) Pos() Position    { return n.At }
func (n *AssignStmt) Pos() Position { return n.At }
func (n *IfStmt) Pos() Position     { return n.At }
func (n *WhileStmt) Pos() Position  { return n.At }
func (n *ReturnStmt) Pos() Position { return n.At }
func (n *PrintStmt) Pos() Position  { return n.At }
func (n *ExprStmt) Pos() Position   { return n.At }
func (n *BlockStmt) Pos() Position  { return n.At }
func (n *FuncDecl) Pos() Position   { return n.At }
func (n *Param) Pos() Position      { return n.At }

func (*IntLit) exprNode()     {}
func (*BoolLit) exprNode()    {}
func (*Ident) exprNode()      {}
func (*UnaryExpr) exprNode()  {}
func (*BinaryExpr) exprNode() {}
func (*CallExpr) exprNode()   {}

func (*LetStmt) stmtNode()    {}
func (*AssignStmt) stmtNode() {}
func (*IfStmt) stmtNode()     {}
func (*WhileStmt) stmtNode()  {}
func (*ReturnStmt) stmtNode() {}
func (*PrintStmt) stmtNode()  {}
func (*ExprStmt) stmtNode()   {}
func (*BlockStmt) stmtNode()  {}

// Print writes an indented, text form of a program. It is the debugging tool
// used to inspect a parse tree before any backend runs.
func Print(w io.Writer, node any) {
	printNode(w, 0, node)
}

func printNode(w io.Writer, depth int, node any) {
	pad := strings.Repeat("  ", depth)
	switch n := node.(type) {
	case *Program:
		fmt.Fprintln(w, "Program")
		for _, f := range n.Funcs {
			printNode(w, 1, f)
		}
		for _, s := range n.Main {
			printNode(w, 1, s)
		}
	case *FuncDecl:
		fmt.Fprintf(w, "%sFuncDecl %s -> %s\n", pad, n.Name, n.ReturnType)
		for _, p := range n.Params {
			printNode(w, depth+1, &p)
		}
		printNode(w, depth+1, n.Body)
	case *Param:
		fmt.Fprintf(w, "%sParam %s %s\n", pad, n.Name, n.Type)
	case *BlockStmt:
		fmt.Fprintln(w, pad+"BlockStmt")
		for _, s := range n.Stmts {
			printNode(w, depth+1, s)
		}
	case *LetStmt:
		fmt.Fprintf(w, "%sLetStmt %s %s\n", pad, n.Name, n.Type)
		printNode(w, depth+1, n.Value)
	case *AssignStmt:
		fmt.Fprintf(w, "%sAssignStmt %s\n", pad, n.Name)
		printNode(w, depth+1, n.Value)
	case *IfStmt:
		fmt.Fprintln(w, pad+"IfStmt")
		printNode(w, depth+1, n.Cond)
		printNode(w, depth+1, n.Then)
		if n.Else != nil {
			printNode(w, depth+1, n.Else)
		}
	case *WhileStmt:
		fmt.Fprintln(w, pad+"WhileStmt")
		printNode(w, depth+1, n.Cond)
		printNode(w, depth+1, n.Body)
	case *ReturnStmt:
		fmt.Fprintln(w, pad+"ReturnStmt")
		printNode(w, depth+1, n.Value)
	case *PrintStmt:
		fmt.Fprintln(w, pad+"PrintStmt")
		printNode(w, depth+1, n.Value)
	case *ExprStmt:
		fmt.Fprintln(w, pad+"ExprStmt")
		printNode(w, depth+1, n.Expr)
	case *IntLit:
		fmt.Fprintf(w, "%sIntLit %d\n", pad, n.Value)
	case *BoolLit:
		fmt.Fprintf(w, "%sBoolLit %t\n", pad, n.Value)
	case *Ident:
		fmt.Fprintf(w, "%sIdent %s\n", pad, n.Name)
	case *UnaryExpr:
		fmt.Fprintf(w, "%sUnaryExpr %s\n", pad, n.Op)
		printNode(w, depth+1, n.Right)
	case *BinaryExpr:
		fmt.Fprintf(w, "%sBinaryExpr %s\n", pad, n.Op)
		printNode(w, depth+1, n.Left)
		printNode(w, depth+1, n.Right)
	case *CallExpr:
		fmt.Fprintf(w, "%sCallExpr %s\n", pad, n.Callee)
		for _, a := range n.Args {
			printNode(w, depth+1, a)
		}
	default:
		fmt.Fprintf(w, "%s%T\n", pad, node)
	}
}
