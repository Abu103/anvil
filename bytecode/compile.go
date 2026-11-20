package bytecode

import (
	"fmt"

	"anvil/ast"
)

// Compile lowers a type-checked syntax tree to bytecode.
func Compile(tree *ast.Program) (*Program, error) {
	c := &compiler{
		funcs: make(map[string]int, len(tree.Funcs)),
	}
	for i, f := range tree.Funcs {
		c.funcs[f.Name] = i
	}

	funcs := make([]Function, 0, len(tree.Funcs))
	for _, f := range tree.Funcs {
		fn, err := c.compileFunction(f)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
	}

	c.reset()
	c.pushScope()
	for _, st := range tree.Main {
		if err := c.compileStmt(st); err != nil {
			return nil, err
		}
	}
	c.emit(Halt, 0)
	main := Function{Name: "main", Code: c.code, NumLocals: c.numLocals}

	return &Program{Funcs: funcs, Main: main, Constants: c.constants}, nil
}

type compiler struct {
	constants []Value
	funcs     map[string]int // function name -> index in Program.Funcs
	code      []Instr
	scopes    []map[string]int
	numLocals int
}

func (c *compiler) reset() {
	c.code = nil
	c.scopes = nil
	c.numLocals = 0
}

func (c *compiler) pushScope() { c.scopes = append(c.scopes, make(map[string]int)) }

func (c *compiler) popScope() { c.scopes = c.scopes[:len(c.scopes)-1] }

func (c *compiler) define(name string) int {
	slot := c.numLocals
	c.numLocals++
	c.scopes[len(c.scopes)-1][name] = slot
	return slot
}

func (c *compiler) lookup(name string) (int, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if slot, ok := c.scopes[i][name]; ok {
			return slot, true
		}
	}
	return 0, false
}

func (c *compiler) emit(op Op, arg int64) int {
	c.code = append(c.code, Instr{Op: op, Arg: arg})
	return len(c.code) - 1
}

func (c *compiler) patch(index, target int) {
	c.code[index].Arg = int64(target)
}

func (c *compiler) constant(v Value) int64 {
	c.constants = append(c.constants, v)
	return int64(len(c.constants) - 1)
}

func (c *compiler) compileFunction(f *ast.FuncDecl) (Function, error) {
	c.reset()
	c.pushScope()
	for _, p := range f.Params {
		c.define(p.Name) // parameters occupy slots 0, 1, 2, ...
	}
	if err := c.compileStmt(f.Body); err != nil {
		return Function{}, err
	}
	if len(c.code) == 0 || c.code[len(c.code)-1].Op != Ret {
		c.emit(Ret, 0) // defensive: the type checker already proved all paths return
	}
	c.popScope()

	return Function{
		Name:      f.Name,
		Code:      append([]Instr(nil), c.code...),
		Arity:     len(f.Params),
		NumLocals: c.numLocals,
	}, nil
}

func (c *compiler) compileStmt(st ast.Stmt) error {
	switch n := st.(type) {
	case *ast.LetStmt:
		if err := c.compileExpr(n.Value); err != nil {
			return err
		}
		slot := c.define(n.Name)
		c.emit(Store, int64(slot))
		return nil

	case *ast.AssignStmt:
		slot, ok := c.lookup(n.Name)
		if !ok {
			return fmt.Errorf("bytecode: line %d: unknown variable %q", n.At.Line, n.Name)
		}
		if err := c.compileExpr(n.Value); err != nil {
			return err
		}
		c.emit(Store, int64(slot))
		return nil

	case *ast.IfStmt:
		if err := c.compileExpr(n.Cond); err != nil {
			return err
		}
		jumpElse := c.emit(JumpIfFalse, 0)
		if err := c.compileStmt(n.Then); err != nil {
			return err
		}
		jumpEnd := c.emit(Jump, 0) // skip the else branch
		c.patch(jumpElse, len(c.code))
		if n.Else != nil {
			if err := c.compileStmt(n.Else); err != nil {
				return err
			}
		}
		c.patch(jumpEnd, len(c.code))
		return nil

	case *ast.WhileStmt:
		start := len(c.code)
		if err := c.compileExpr(n.Cond); err != nil {
			return err
		}
		exit := c.emit(JumpIfFalse, 0)
		if err := c.compileStmt(n.Body); err != nil {
			return err
		}
		c.emit(Jump, int64(start)) // back edge to the condition
		c.patch(exit, len(c.code))
		return nil

	case *ast.ReturnStmt:
		if err := c.compileExpr(n.Value); err != nil {
			return err
		}
		c.emit(Ret, 0)
		return nil

	case *ast.PrintStmt:
		if err := c.compileExpr(n.Value); err != nil {
			return err
		}
		c.emit(Print, 0)
		return nil

	case *ast.ExprStmt:
		if err := c.compileExpr(n.Expr); err != nil {
			return err
		}
		c.emit(Pop, 0) // a call's return value is unused here
		return nil

	case *ast.BlockStmt:
		c.pushScope()
		for _, s := range n.Stmts {
			if err := c.compileStmt(s); err != nil {
				return err
			}
		}
		c.popScope()
		return nil
	}
	return fmt.Errorf("bytecode: unsupported statement %T", st)
}

func (c *compiler) compileExpr(e ast.Expr) error {
	switch n := e.(type) {
	case *ast.IntLit:
		c.emit(Const, c.constant(Int(n.Value)))
		return nil

	case *ast.BoolLit:
		c.emit(Const, c.constant(Bool(n.Value)))
		return nil

	case *ast.Ident:
		slot, ok := c.lookup(n.Name)
		if !ok {
			return fmt.Errorf("bytecode: line %d: unknown variable %q", n.At.Line, n.Name)
		}
		c.emit(Load, int64(slot))
		return nil

	case *ast.UnaryExpr:
		if err := c.compileExpr(n.Right); err != nil {
			return err
		}
		if n.Op == "-" {
			c.emit(Neg, 0)
		} else {
			c.emit(Not, 0)
		}
		return nil

	case *ast.BinaryExpr:
		return c.compileBinary(n)

	case *ast.CallExpr:
		index, ok := c.funcs[n.Callee]
		if !ok {
			return fmt.Errorf("bytecode: line %d: unknown function %q", n.At.Line, n.Callee)
		}
		for _, arg := range n.Args {
			if err := c.compileExpr(arg); err != nil {
				return err
			}
		}
		c.emit(Call, int64(index))
		return nil
	}
	return fmt.Errorf("bytecode: unsupported expression %T", e)
}

func (c *compiler) compileBinary(n *ast.BinaryExpr) error {
	// && and || need short-circuit jumps and a bool result on the stack.
	if n.Op == "&&" || n.Op == "||" {
		if err := c.compileExpr(n.Left); err != nil {
			return err
		}
		var skip *int
		if n.Op == "&&" {
			i := c.emit(JumpIfFalse, 0)
			skip = &i
		} else {
			i := c.emit(JumpIfTrue, 0)
			skip = &i
		}
		if err := c.compileExpr(n.Right); err != nil {
			return err
		}
		end := c.emit(Jump, 0)
		c.patch(*skip, len(c.code))
		c.emit(Const, c.constant(Bool(n.Op == "||"))) // false for &&, true for ||
		c.patch(end, len(c.code))
		return nil
	}

	if err := c.compileExpr(n.Left); err != nil {
		return err
	}
	if err := c.compileExpr(n.Right); err != nil {
		return err
	}
	op := map[string]Op{
		"+": Add, "-": Sub, "*": Mul, "/": Div,
		"<": Lt, "<=": Le, ">": Gt, ">=": Ge,
		"==": Eq, "!=": Ne,
	}[n.Op]
	if op == 0 {
		return fmt.Errorf("bytecode: line %d: unknown operator %q", n.At.Line, n.Op)
	}
	c.emit(op, 0)
	return nil
}
