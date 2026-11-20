// Package bytecode is Anvil's stack-machine intermediate representation.
// It is closer to machine code than the syntax tree and is the input to the VM.
package bytecode

import (
	"fmt"
	"strconv"
	"strings"
)

// Op is one stack-machine opcode.
type Op byte

const (
	Const Op = iota
	Load
	Store
	Add
	Sub
	Mul
	Div
	Lt
	Le
	Gt
	Ge
	Eq
	Ne
	Neg
	Not
	Jump
	JumpIfFalse
	JumpIfTrue
	Call
	Ret
	Print
	Pop
	Halt
)

var opNames = map[Op]string{
	Const: "const", Load: "load", Store: "store",
	Add: "add", Sub: "sub", Mul: "mul", Div: "div",
	Lt: "lt", Le: "le", Gt: "gt", Ge: "ge", Eq: "eq", Ne: "ne",
	Neg: "neg", Not: "not",
	Jump: "jump", JumpIfFalse: "jump_if_false", JumpIfTrue: "jump_if_true",
	Call: "call", Ret: "ret", Print: "print", Pop: "pop", Halt: "halt",
}

func (o Op) String() string {
	if s, ok := opNames[o]; ok {
		return s
	}
	return fmt.Sprintf("op(%d)", byte(o))
}

// Instr is one instruction. Arg is a constant index, local slot, function index
// or jump target depending on the opcode.
type Instr struct {
	Op  Op
	Arg int64
}

// Value is a runtime value in the constant pool and the VM.
type Value struct {
	IsBool bool
	Int    int64
	Bool   bool
}

// Int makes an int value.
func Int(v int64) Value { return Value{Int: v} }

// Bool makes a bool value.
func Bool(v bool) Value { return Value{IsBool: true, Bool: v} }

func (v Value) String() string {
	if v.IsBool {
		return strconv.FormatBool(v.Bool)
	}
	return strconv.FormatInt(v.Int, 10)
}

// Function is the code for one Anvil function.
type Function struct {
	Name      string
	Code      []Instr
	Arity     int
	NumLocals int
}

// Program is a compiled Anvil program: functions plus the implicit main.
type Program struct {
	Funcs     []Function
	Main      Function
	Constants []Value
}

// Disassemble prints a readable listing of every function. It is the tool used
// most often when debugging a backend.
func Disassemble(p *Program) string {
	var b strings.Builder
	write := func(f *Function) {
		fmt.Fprintf(&b, "== %s ==\n", f.Name)
		for i, in := range f.Code {
			name := strings.ToUpper(in.Op.String())
			switch in.Op {
			case Const:
				if in.Arg >= 0 && in.Arg < int64(len(p.Constants)) {
					fmt.Fprintf(&b, "%04d  %-14s %d (%s)\n", i, name, in.Arg, p.Constants[in.Arg])
				} else {
					fmt.Fprintf(&b, "%04d  %-14s %d (BAD INDEX)\n", i, name, in.Arg)
				}
			case Load, Store, Call:
				fmt.Fprintf(&b, "%04d  %-14s %d\n", i, name, in.Arg)
			case Jump, JumpIfFalse, JumpIfTrue:
				fmt.Fprintf(&b, "%04d  %-14s -> %d\n", i, name, in.Arg)
			default:
				fmt.Fprintf(&b, "%04d  %s\n", i, name)
			}
		}
	}
	for i := range p.Funcs {
		write(&p.Funcs[i])
	}
	write(&p.Main)
	return b.String()
}
