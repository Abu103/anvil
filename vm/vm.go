// Package vm executes Anvil bytecode on a stack machine. It is the safe,
// submittable compiler backend: every example must match the interpreter.
package vm

import (
	"errors"
	"fmt"
	"io"
	"math"

	"anvil/bytecode"
)

const (
	initialStack = 1024
	maxStack     = 1 << 20
	maxFrames    = 1 << 16
)

type frame struct {
	function *bytecode.Function
	ip       int
	// base is the index in the value stack where this call's arguments (and
	// therefore its local slots) begin.
	base int
}

// VM holds the value stack, call frames and the output sink.
type VM struct {
	prog   *bytecode.Program
	stack  []bytecode.Value // pre-sized; sp indexes the next free slot
	sp     int
	frames []frame
	out    io.Writer
	err    error
}

// Run executes prog, writing print output to out.
func Run(prog *bytecode.Program, out io.Writer) error {
	v := &VM{
		prog:  prog,
		stack: make([]bytecode.Value, initialStack),
		out:   out,
	}
	v.frames = append(v.frames, frame{function: &prog.Main})
	if err := v.reserve(prog.Main.NumLocals); err != nil {
		return err
	}
	v.sp = prog.Main.NumLocals
	return v.loop()
}

func (v *VM) reserve(need int) error {
	if need > maxStack {
		return errors.New("vm: stack overflow")
	}
	if need <= len(v.stack) {
		return nil
	}
	size := len(v.stack)
	for size < need {
		size *= 2
	}
	if size > maxStack {
		size = maxStack
	}
	v.stack = append(v.stack, make([]bytecode.Value, size-len(v.stack))...)
	return nil
}

func (v *VM) push(val bytecode.Value) {
	if v.err != nil {
		return
	}
	if v.sp >= maxStack {
		v.err = errors.New("vm: stack overflow")
		return
	}
	if v.sp >= len(v.stack) {
		if err := v.reserve(v.sp + 1); err != nil {
			v.err = err
			return
		}
	}
	v.stack[v.sp] = val
	v.sp++
}

func (v *VM) pop() bytecode.Value {
	if v.sp <= 0 {
		v.err = errors.New("vm: stack underflow")
		return bytecode.Value{}
	}
	v.sp--
	return v.stack[v.sp]
}

func (v *VM) loop() error {
	for {
		if len(v.frames) == 0 {
			return nil
		}
		f := &v.frames[len(v.frames)-1]
		if f.ip >= len(f.function.Code) {
			return fmt.Errorf("vm: %s ran past the end of its code", f.function.Name)
		}

		in := f.function.Code[f.ip]
		f.ip++

		switch in.Op {
		case bytecode.Const:
			if in.Arg < 0 || in.Arg >= int64(len(v.prog.Constants)) {
				return fmt.Errorf("vm: bad constant index %d", in.Arg)
			}
			v.push(v.prog.Constants[in.Arg])

		case bytecode.Load:
			slot := f.base + int(in.Arg)
			if slot < 0 || slot >= v.sp {
				return fmt.Errorf("vm: bad local slot %d", in.Arg)
			}
			v.push(v.stack[slot])

		case bytecode.Store:
			slot := f.base + int(in.Arg)
			if slot < 0 || slot >= len(v.stack) {
				return fmt.Errorf("vm: bad local slot %d", in.Arg)
			}
			v.stack[slot] = v.pop()

		case bytecode.Add:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Int(a.Int + b.Int))
		case bytecode.Sub:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Int(a.Int - b.Int))
		case bytecode.Mul:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Int(a.Int * b.Int))
		case bytecode.Div:
			b, a := v.pop(), v.pop()
			q, err := divInt(a.Int, b.Int)
			if err != nil {
				return errors.New("vm: division by zero")
			}
			v.push(bytecode.Int(q))
		case bytecode.Neg:
			a := v.pop()
			v.push(bytecode.Int(-a.Int))
		case bytecode.Not:
			a := v.pop()
			v.push(bytecode.Bool(!a.Bool))

		case bytecode.Lt:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Bool(a.Int < b.Int))
		case bytecode.Le:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Bool(a.Int <= b.Int))
		case bytecode.Gt:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Bool(a.Int > b.Int))
		case bytecode.Ge:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Bool(a.Int >= b.Int))
		case bytecode.Eq:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Bool(sameValue(a, b)))
		case bytecode.Ne:
			b, a := v.pop(), v.pop()
			v.push(bytecode.Bool(!sameValue(a, b)))

		case bytecode.Jump:
			f.ip = int(in.Arg)
		case bytecode.JumpIfFalse:
			cond := v.pop()
			if !truthy(cond) {
				f.ip = int(in.Arg)
			}
		case bytecode.JumpIfTrue:
			cond := v.pop()
			if truthy(cond) {
				f.ip = int(in.Arg)
			}

		case bytecode.Call:
			callee := &v.prog.Funcs[in.Arg]
			if len(v.frames) >= maxFrames {
				return errors.New("vm: call stack overflow")
			}
			base := v.sp - callee.Arity
			if base < 0 {
				return errors.New("vm: bad call: missing arguments")
			}
			if err := v.reserve(base + callee.NumLocals); err != nil {
				return err
			}
			// Slots beyond the arguments must not contain stale values from a
			// previous call.
			for i := callee.Arity; i < callee.NumLocals; i++ {
				v.stack[base+i] = bytecode.Value{}
			}
			v.sp = base + callee.NumLocals
			v.frames = append(v.frames, frame{function: callee, base: base})

		case bytecode.Ret:
			ret := v.pop()
			done := v.frames[len(v.frames)-1]
			v.frames = v.frames[:len(v.frames)-1]
			if len(v.frames) == 0 {
				return nil
			}
			// Rewind to where this call's arguments started, then leave the
			// return value there for the caller.
			v.sp = done.base
			v.push(ret)

		case bytecode.Print:
			val := v.pop()
			if _, err := fmt.Fprintln(v.out, val.String()); err != nil {
				return err
			}
		case bytecode.Pop:
			v.pop()
		case bytecode.Halt:
			return nil

		default:
			return fmt.Errorf("vm: unknown opcode %s", in.Op)
		}

		if v.err != nil {
			return v.err
		}
	}
}

func truthy(v bytecode.Value) bool {
	if v.IsBool {
		return v.Bool
	}
	return v.Int != 0
}

func sameValue(a, b bytecode.Value) bool {
	if a.IsBool != b.IsBool {
		return false
	}
	if a.IsBool {
		return a.Bool == b.Bool
	}
	return a.Int == b.Int
}

// divInt implements the SPEC: truncation toward zero, MinInt64 / -1 wraps.
func divInt(a, b int64) (int64, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	if a == math.MinInt64 && b == -1 {
		return math.MinInt64, nil
	}
	return a / b, nil
}
