// Package codegen emits GNU assembler (Intel syntax) for x86-64 Linux. It has
// no libc dependency: printing and process exit are raw syscalls.
package codegen

import (
	"fmt"
	"strings"

	"anvil/ast"
	"anvil/types"
)

// Generate returns the assembly text for a well-typed program.
func Generate(prog *ast.Program) (string, error) {
	g := &generator{
		funcs:       make(map[string]funcInfo, len(prog.Funcs)),
		funcLabels:  make(map[string]string, len(prog.Funcs)),
		funcReturns: make(map[string]string, len(prog.Funcs)),
	}
	for _, f := range prog.Funcs {
		g.funcs[f.Name] = funcInfo{arity: len(f.Params), ret: typesFromString(f.ReturnType)}
		// User functions are prefixed so they cannot collide with the runtime
		// routines or with _start.
		g.funcLabels[f.Name] = "anvil_fn_" + f.Name
	}

	for _, f := range prog.Funcs {
		if err := g.genFunction(f); err != nil {
			return "", err
		}
	}
	if err := g.genMain(prog.Main); err != nil {
		return "", err
	}
	g.genRuntime()

	var out strings.Builder
	out.WriteString(".intel_syntax noprefix\n")
	out.WriteString(".globl _start\n")
	out.WriteString(".text\n\n")
	out.WriteString(g.text.String())
	out.WriteString("\n.section .rodata\n")
	out.WriteString(g.data.String())
	return out.String(), nil
}

type funcInfo struct {
	arity int
	ret   types.Type
}

type varInfo struct {
	// offset is bytes relative to rbp: negative for locals, 16+8*i for the
	// i-th parameter.
	offset int
	typ    types.Type
}

type generator struct {
	text        strings.Builder
	data        strings.Builder
	labels      int
	locals      int
	scopes      []map[string]varInfo
	funcs       map[string]funcInfo
	funcLabels  map[string]string
	funcReturns map[string]string
	currentFunc string
}

func typesFromString(s string) types.Type {
	if s == "bool" {
		return types.Bool
	}
	return types.Int
}

func (g *generator) emit(format string, args ...any) {
	fmt.Fprintf(&g.text, format+"\n", args...)
}

func (g *generator) label(prefix string) string {
	g.labels++
	return fmt.Sprintf(".L%s_%d", prefix, g.labels)
}

func (g *generator) pushScope() { g.scopes = append(g.scopes, make(map[string]varInfo)) }

func (g *generator) popScope() { g.scopes = g.scopes[:len(g.scopes)-1] }

func (g *generator) define(name string, v varInfo) {
	g.scopes[len(g.scopes)-1][name] = v
}

func (g *generator) lookup(name string) (varInfo, bool) {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if v, ok := g.scopes[i][name]; ok {
			return v, true
		}
	}
	return varInfo{}, false
}

// genMain emits _start with the top-level statements as its body.
func (g *generator) genMain(stmts []ast.Stmt) error {
	g.pushScope()
	g.locals = 0
	g.currentFunc = "main"

	// Generate the body into a separate buffer so the prologue can name the
	// correct stack size, and so earlier function bodies are never overwritten.
	saved := g.text
	g.text = strings.Builder{}
	for _, st := range stmts {
		if err := g.genStmt(st); err != nil {
			g.text = saved
			return err
		}
	}
	body := g.text.String()
	g.text = saved

	g.emit("_start:")
	g.emit("push rbp")
	g.emit("mov rbp, rsp")
	g.emit("sub rsp, %d", stackBytes(g.locals))
	g.text.WriteString(body)
	// exit(0)
	g.emit("mov rdi, 0")
	g.emit("mov rax, 60")
	g.emit("syscall")
	g.popScope()
	return nil
}

// genFunction emits one function with a simple stack convention: the caller
// pushes arguments right to left and cleans them up; the result comes back in
// rax. After the prologue, argument i is at [rbp+16+8*i] because [rbp+8] is the
// return address and [rbp] is the saved rbp.
func (g *generator) genFunction(f *ast.FuncDecl) error {
	g.pushScope()
	g.locals = 0
	g.currentFunc = f.Name
	epilogue := g.label("epilogue")
	g.funcReturns[f.Name] = epilogue

	for i, p := range f.Params {
		g.define(p.Name, varInfo{offset: 16 + 8*i, typ: typesFromString(p.Type)})
	}

	saved := g.text
	g.text = strings.Builder{}
	for _, st := range f.Body.Stmts {
		if err := g.genStmt(st); err != nil {
			g.text = saved
			return err
		}
	}
	body := g.text.String()
	g.text = saved

	label := g.funcLabels[f.Name]
	g.emit(".globl %s", label)
	g.emit("%s:", label)
	g.emit("push rbp")
	g.emit("mov rbp, rsp")
	g.emit("sub rsp, %d", stackBytes(g.locals))
	g.text.WriteString(body)
	g.emit("%s:", epilogue)
	g.emit("mov rsp, rbp")
	g.emit("pop rbp")
	g.emit("ret")
	g.popScope()
	return nil
}

func stackBytes(locals int) int {
	n := 8 * locals
	if n%16 != 0 {
		n += 8
	}
	return n
}

func (g *generator) genStmt(st ast.Stmt) error {
	switch n := st.(type) {
	case *ast.LetStmt:
		if err := g.genExpr(n.Value); err != nil {
			return err
		}
		g.locals++
		offset := -8 * g.locals
		g.emit("mov [rbp%+d], rax", offset)
		g.define(n.Name, varInfo{offset: offset, typ: typesFromString(n.Type)})
		return nil

	case *ast.AssignStmt:
		v, ok := g.lookup(n.Name)
		if !ok {
			return fmt.Errorf("codegen: line %d: unknown variable %q", n.At.Line, n.Name)
		}
		if err := g.genExpr(n.Value); err != nil {
			return err
		}
		g.emit("mov [rbp%+d], rax", v.offset)
		return nil

	case *ast.PrintStmt:
		if err := g.genExpr(n.Value); err != nil {
			return err
		}
		if g.typeOf(n.Value) == types.Bool {
			g.emit("call anvil_print_bool")
		} else {
			g.emit("call anvil_print_int")
		}
		return nil

	case *ast.ExprStmt:
		return g.genExpr(n.Expr)

	case *ast.ReturnStmt:
		if err := g.genExpr(n.Value); err != nil {
			return err
		}
		label, ok := g.funcReturns[g.currentFunc]
		if !ok {
			return fmt.Errorf("codegen: line %d: return outside a function", n.At.Line)
		}
		g.emit("jmp %s", label)
		return nil

	case *ast.BlockStmt:
		g.pushScope()
		for _, st := range n.Stmts {
			if err := g.genStmt(st); err != nil {
				g.popScope()
				return err
			}
		}
		g.popScope()
		return nil

	case *ast.IfStmt:
		if err := g.genExpr(n.Cond); err != nil {
			return err
		}
		elseLabel, endLabel := g.label("else"), g.label("endif")
		g.emit("cmp rax, 0")
		g.emit("je %s", elseLabel)
		if err := g.genStmt(n.Then); err != nil {
			return err
		}
		g.emit("jmp %s", endLabel)
		g.emit("%s:", elseLabel)
		if n.Else != nil {
			if err := g.genStmt(n.Else); err != nil {
				return err
			}
		}
		g.emit("%s:", endLabel)
		return nil

	case *ast.WhileStmt:
		start, end := g.label("while"), g.label("endwhile")
		g.emit("%s:", start)
		if err := g.genExpr(n.Cond); err != nil {
			return err
		}
		g.emit("cmp rax, 0")
		g.emit("je %s", end)
		if err := g.genStmt(n.Body); err != nil {
			return err
		}
		g.emit("jmp %s", start)
		g.emit("%s:", end)
		return nil
	}
	return fmt.Errorf("codegen: unsupported statement %T", st)
}

func (g *generator) loadImm(v int64) {
	if v >= -2147483648 && v <= 2147483647 {
		g.emit("mov rax, %d", v)
	} else {
		g.emit("movabs rax, 0x%x", uint64(v))
	}
}

// genExpr leaves its result in rax. Temporaries are pushed on the machine
// stack, so nested expressions compose without allocating registers.
func (g *generator) genExpr(e ast.Expr) error {
	switch n := e.(type) {
	case *ast.IntLit:
		g.loadImm(n.Value)
		return nil

	case *ast.BoolLit:
		if n.Value {
			g.emit("mov rax, 1")
		} else {
			g.emit("mov rax, 0")
		}
		return nil

	case *ast.Ident:
		v, ok := g.lookup(n.Name)
		if !ok {
			return fmt.Errorf("codegen: line %d: unknown variable %q", n.At.Line, n.Name)
		}
		g.emit("mov rax, [rbp%+d]", v.offset)
		return nil

	case *ast.UnaryExpr:
		if err := g.genExpr(n.Right); err != nil {
			return err
		}
		if n.Op == "-" {
			g.emit("neg rax")
		} else {
			g.emit("cmp rax, 0")
			g.emit("sete al")
			g.emit("movzx rax, al")
		}
		return nil

	case *ast.BinaryExpr:
		return g.genBinary(n)

	case *ast.CallExpr:
		label, ok := g.funcLabels[n.Callee]
		if !ok {
			return fmt.Errorf("codegen: line %d: unknown function %q", n.At.Line, n.Callee)
		}
		// Push right to left, so the first argument sits at [rbp+16] in the
		// callee. Each argument's own temporaries are balanced before the push.
		for i := len(n.Args) - 1; i >= 0; i-- {
			if err := g.genExpr(n.Args[i]); err != nil {
				return err
			}
			g.emit("push rax")
		}
		g.emit("call %s", label)
		if len(n.Args) > 0 {
			g.emit("add rsp, %d", 8*len(n.Args)) // caller cleans up
		}
		return nil
	}
	return fmt.Errorf("codegen: unsupported expression %T", e)
}

func (g *generator) genBinary(n *ast.BinaryExpr) error {
	// Short circuit: the right side is only compiled/executed when needed.
	if n.Op == "&&" || n.Op == "||" {
		if err := g.genExpr(n.Left); err != nil {
			return err
		}
		short, end := g.label("short"), g.label("logic_end")
		g.emit("cmp rax, 0")
		if n.Op == "&&" {
			g.emit("je %s", short)
		} else {
			g.emit("jne %s", short)
		}
		if err := g.genExpr(n.Right); err != nil {
			return err
		}
		g.emit("jmp %s", end)
		g.emit("%s:", short)
		if n.Op == "&&" {
			g.emit("mov rax, 0")
		} else {
			g.emit("mov rax, 1")
		}
		g.emit("%s:", end)
		return nil
	}

	if err := g.genExpr(n.Left); err != nil {
		return err
	}
	g.emit("push rax")
	if err := g.genExpr(n.Right); err != nil {
		return err
	}
	g.emit("mov rbx, rax")
	g.emit("pop rax")

	switch n.Op {
	case "+":
		g.emit("add rax, rbx")
	case "-":
		g.emit("sub rax, rbx")
	case "*":
		g.emit("imul rax, rbx")
	case "/":
		done, skip := g.label("div_done"), g.label("div_skip")
		g.emit("cmp rbx, 0")
		g.emit("je anvil_divzero")
		g.emit("cmp rbx, -1")
		g.emit("jne %s", skip)
		g.emit("movabs rdx, 0x8000000000000000")
		g.emit("cmp rax, rdx")
		g.emit("jne %s", skip)
		g.emit("movabs rax, 0x8000000000000000")
		g.emit("jmp %s", done)
		g.emit("%s:", skip)
		g.emit("cqo")
		g.emit("idiv rbx")
		g.emit("%s:", done)
	case "<":
		g.emit("cmp rax, rbx")
		g.emit("setl al")
		g.emit("movzx rax, al")
	case "<=":
		g.emit("cmp rax, rbx")
		g.emit("setle al")
		g.emit("movzx rax, al")
	case ">":
		g.emit("cmp rax, rbx")
		g.emit("setg al")
		g.emit("movzx rax, al")
	case ">=":
		g.emit("cmp rax, rbx")
		g.emit("setge al")
		g.emit("movzx rax, al")
	case "==":
		g.emit("cmp rax, rbx")
		g.emit("sete al")
		g.emit("movzx rax, al")
	case "!=":
		g.emit("cmp rax, rbx")
		g.emit("setne al")
		g.emit("movzx rax, al")
	default:
		return fmt.Errorf("codegen: line %d: unsupported operator %q", n.At.Line, n.Op)
	}
	return nil
}

// typeOf is deliberately small: the program is already type-checked, so this
// only needs to rediscover the type of an expression to pick a print routine.
func (g *generator) typeOf(e ast.Expr) types.Type {
	switch n := e.(type) {
	case *ast.IntLit:
		return types.Int
	case *ast.BoolLit:
		return types.Bool
	case *ast.Ident:
		if v, ok := g.lookup(n.Name); ok {
			return v.typ
		}
	case *ast.UnaryExpr:
		if n.Op == "!" {
			return types.Bool
		}
		return types.Int
	case *ast.BinaryExpr:
		switch n.Op {
		case "<", "<=", ">", ">=", "==", "!=", "&&", "||":
			return types.Bool
		}
		return types.Int
	case *ast.CallExpr:
		if f, ok := g.funcs[n.Callee]; ok {
			return f.ret
		}
	}
	return types.Int
}

// genRuntime appends the hand-written print and error routines. Each copies
// arguments it may clobber and returns with the value stack balanced.
func (g *generator) genRuntime() {
	pos, conv, loop, write :=
		g.label("pi_pos"), g.label("pi_conv"), g.label("pi_loop"), g.label("pi_write")
	falseLabel, boolWrite := g.label("pb_false"), g.label("pb_write")

	g.emit("")
	g.emit("anvil_print_int:")
	g.emit("push rbp")
	g.emit("mov rbp, rsp")
	g.emit("sub rsp, 56")
	g.emit("test rax, rax")
	g.emit("jns %s", pos)
	g.emit("mov r8b, 1")
	g.emit("neg rax")
	g.emit("jmp %s", conv)
	g.emit("%s:", pos)
	g.emit("mov r8b, 0")
	g.emit("%s:", conv)
	g.emit("lea rsi, [rbp-24]")
	g.emit("mov rcx, 0")
	g.emit("mov r9, 10")
	g.emit("%s:", loop)
	g.emit("xor rdx, rdx")
	g.emit("div r9")
	g.emit("add dl, 48")
	g.emit("dec rsi")
	g.emit("mov [rsi], dl")
	g.emit("inc rcx")
	g.emit("test rax, rax")
	g.emit("jnz %s", loop)
	g.emit("cmp r8b, 0")
	g.emit("jz %s", write)
	g.emit("dec rsi")
	g.emit("mov byte ptr [rsi], 45")
	g.emit("inc rcx")
	g.emit("%s:", write)
	g.emit("mov rdx, rcx")
	g.emit("mov rdi, 1")
	g.emit("mov rax, 1")
	g.emit("syscall")
	g.emit("mov byte ptr [rbp-8], 10")
	g.emit("lea rsi, [rbp-8]")
	g.emit("mov rdx, 1")
	g.emit("mov rdi, 1")
	g.emit("mov rax, 1")
	g.emit("syscall")
	g.emit("mov rsp, rbp")
	g.emit("pop rbp")
	g.emit("ret")

	g.emit("")
	g.emit("anvil_print_bool:")
	g.emit("test rax, rax")
	g.emit("jz %s", falseLabel)
	g.emit("mov rdx, 5")
	g.emit("lea rsi, [rip+anvil_true]")
	g.emit("jmp %s", boolWrite)
	g.emit("%s:", falseLabel)
	g.emit("mov rdx, 6")
	g.emit("lea rsi, [rip+anvil_false]")
	g.emit("%s:", boolWrite)
	g.emit("mov rdi, 1")
	g.emit("mov rax, 1")
	g.emit("syscall")
	g.emit("ret")

	g.emit("")
	g.emit("anvil_divzero:")
	g.emit("mov rdx, 24")
	g.emit("lea rsi, [rip+anvil_divmsg]")
	g.emit("mov rdi, 2")
	g.emit("mov rax, 1")
	g.emit("syscall")
	g.emit("mov rdi, 1")
	g.emit("mov rax, 60")
	g.emit("syscall")

	g.data.WriteString("anvil_true:\n  .ascii \"true\\n\"\n")
	g.data.WriteString("anvil_false:\n  .ascii \"false\\n\"\n")
	g.data.WriteString("anvil_divmsg:\n  .ascii \"anvil: division by zero\\n\"\n")
}
