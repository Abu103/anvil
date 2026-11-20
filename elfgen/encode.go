// Package elfgen is a tiny assembler and ELF writer for the assembly produced
// by codegen. It lets Anvil build a native binary with no `as` or `ld`
// installed. It understands only the instructions Anvil emits; it is not a
// general-purpose assembler.
package elfgen

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	baseAddr   = 0x400000
	loadOffset = 0x1000
	textAddr   = baseAddr + loadOffset
)

type line struct {
	label    string
	mnemonic string
	operands []string
}

type program struct {
	text       []line
	data       []byte
	dataLabels map[string]int
	dataBase   int
	labels     map[string]int
}

// Build assembles asm and writes a minimal static ELF executable to outPath.
func Build(asm, outPath string) error {
	p, err := parse(asm)
	if err != nil {
		return err
	}
	if err := p.measure(); err != nil {
		return err
	}
	image, err := p.encode()
	if err != nil {
		return err
	}
	entry := textAddr + p.labels["_start"]
	if _, ok := p.labels["_start"]; !ok {
		return fmt.Errorf("elfgen: no _start label")
	}
	return writeELF(outPath, image, uint64(entry))
}

func parse(asm string) (*program, error) {
	textPart, dataPart := asm, ""
	if i := strings.Index(asm, ".section .rodata"); i >= 0 {
		textPart, dataPart = asm[:i], asm[i+len(".section .rodata"):]
	}

	p := &program{dataLabels: make(map[string]int)}
	for _, raw := range strings.Split(textPart, "\n") {
		s := strings.TrimSpace(raw)
		if s == "" || s == ".text" || strings.HasPrefix(s, ".intel_syntax") || strings.HasPrefix(s, ".globl") {
			continue
		}
		if strings.HasSuffix(s, ":") {
			p.text = append(p.text, line{label: strings.TrimSuffix(s, ":")})
			continue
		}
		mn, ops := splitInsn(s)
		p.text = append(p.text, line{mnemonic: mn, operands: ops})
	}

	for _, raw := range strings.Split(dataPart, "\n") {
		s := strings.TrimSpace(raw)
		if strings.HasSuffix(s, ":") {
			p.dataLabels[strings.TrimSuffix(s, ":")] = len(p.data)
			continue
		}
		if strings.HasPrefix(s, ".ascii ") {
			lit := strings.TrimSpace(strings.TrimPrefix(s, ".ascii "))
			v, err := strconv.Unquote(lit)
			if err != nil {
				return nil, fmt.Errorf("elfgen: bad string %s: %w", lit, err)
			}
			p.data = append(p.data, []byte(v)...)
		}
	}
	return p, nil
}

func splitInsn(s string) (string, []string) {
	parts := strings.SplitN(s, " ", 2)
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts[0], strings.Split(parts[1], ", ")
}

func (p *program) measure() error {
	p.labels = make(map[string]int)
	off := 0
	for _, ln := range p.text {
		if ln.label != "" {
			p.labels[ln.label] = off
			continue
		}
		code, err := p.encodeLine(ln, off)
		if err != nil {
			return err
		}
		off += len(code)
	}
	p.dataBase = (off + 7) &^ 7
	return nil
}

func (p *program) encode() ([]byte, error) {
	image := make([]byte, p.dataBase+len(p.data))
	off := 0
	for _, ln := range p.text {
		if ln.label != "" {
			continue
		}
		code, err := p.encodeLine(ln, off)
		if err != nil {
			return nil, err
		}
		copy(image[off:], code)
		off += len(code)
	}
	copy(image[p.dataBase:], p.data)
	return image, nil
}

func num(s string) int64 {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") {
		u, _ := strconv.ParseUint(s[2:], 16, 64)
		return int64(u)
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func disp(operand string) int8 {
	inner := strings.Trim(operand, "[]")
	inner = strings.TrimPrefix(inner, "rbp")
	if inner == "" {
		return 0
	}
	return int8(num(inner))
}

func imm32(b []byte, v int64) []byte {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], uint32(int32(v)))
	return append(b, t[:]...)
}

func imm64(b []byte, v uint64) []byte {
	var t [8]byte
	binary.LittleEndian.PutUint64(t[:], v)
	return append(b, t[:]...)
}

func rel32(b []byte, from, to int) []byte {
	return imm32(b, int64(to-(from+4)))
}

func (p *program) labelAddr(name string) int {
	if v, ok := p.labels[name]; ok {
		return textAddr + v
	}
	return textAddr // fallback during measurement
}

func (p *program) dataAddr(name string) int {
	if v, ok := p.dataLabels[name]; ok {
		return textAddr + p.dataBase + v
	}
	return textAddr + p.dataBase
}

// encodeLine encodes one instruction. During measure, label targets are
// absent; every branch uses a fixed length, so the size is still correct.
func (p *program) encodeLine(ln line, at int) ([]byte, error) {
	m, o := ln.mnemonic, ln.operands
	b := []byte{}
	addr := textAddr + at

	switch m {
	case "push":
		if o[0] == "rbp" {
			return []byte{0x55}, nil
		}
		if o[0] == "rax" {
			return []byte{0x50}, nil
		}
	case "pop":
		if o[0] == "rbp" {
			return []byte{0x5D}, nil
		}
		if o[0] == "rax" {
			return []byte{0x58}, nil
		}
	case "ret":
		return []byte{0xC3}, nil
	case "syscall":
		return []byte{0x0F, 0x05}, nil
	case "cqo":
		return []byte{0x48, 0x99}, nil
	case "neg":
		if o[0] == "rax" {
			return []byte{0x48, 0xF7, 0xD8}, nil
		}
	case "idiv":
		if o[0] == "rbx" {
			return []byte{0x48, 0xF7, 0xFB}, nil
		}
	case "xor":
		if o[0] == "rdx" && o[1] == "rdx" {
			return []byte{0x48, 0x31, 0xD2}, nil
		}
	case "div":
		if o[0] == "r9" {
			return []byte{0x49, 0xF7, 0xF1}, nil
		}
	case "inc":
		if o[0] == "rcx" {
			return []byte{0x48, 0xFF, 0xC1}, nil
		}
	case "dec":
		if o[0] == "rsi" {
			return []byte{0x48, 0xFF, 0xCE}, nil
		}

	case "mov":
		if len(o) == 2 {
			dst, src := o[0], o[1]
			switch {
			case dst == "rbp" && src == "rsp":
				return []byte{0x48, 0x89, 0xE5}, nil
			case dst == "rsp" && src == "rbp":
				return []byte{0x48, 0x89, 0xEC}, nil
			case dst == "rbx" && src == "rax":
				return []byte{0x48, 0x89, 0xC3}, nil
			case dst == "rdx" && src == "rcx":
				return []byte{0x48, 0x89, 0xCA}, nil
			case dst == "rax" && strings.HasPrefix(src, "[rbp"):
				d := disp(src)
				if d == int8(int16(d)) {
					return []byte{0x48, 0x8B, 0x45, byte(d)}, nil
				}
			case strings.HasPrefix(dst, "[rbp") && src == "rax":
				d := disp(dst)
				return []byte{0x48, 0x89, 0x45, byte(d)}, nil
			case dst == "[rsi]" && src == "dl":
				return []byte{0x88, 0x16}, nil
			case dst == "rax":
				b := imm32([]byte{0xB8}, num(src))
				return b, nil
			case dst == "rdi":
				b := imm32([]byte{0x48, 0xC7, 0xC7}, num(src))
				return b, nil
			case dst == "rdx":
				b := imm32([]byte{0x48, 0xC7, 0xC2}, num(src))
				return b, nil
			case dst == "rcx":
				b := imm32([]byte{0x48, 0xC7, 0xC1}, num(src))
				return b, nil
			case dst == "r9":
				b := imm32([]byte{0x49, 0xC7, 0xC1}, num(src))
				return b, nil
			case dst == "r8b":
				return []byte{0x41, 0xB0, byte(num(src))}, nil
			}
		}
		if len(o) == 2 && strings.HasPrefix(o[0], "byte ptr ") {
			loc := strings.TrimPrefix(o[0], "byte ptr ")
			v := byte(num(o[1]))
			if strings.HasPrefix(loc, "[rbp") {
				return []byte{0xC6, 0x45, byte(disp(loc)), v}, nil
			}
			return []byte{0xC6, 0x06, v}, nil
		}

	case "movabs":
		if len(o) == 2 {
			if o[0] == "rax" {
				return imm64([]byte{0x48, 0xB8}, uint64(num(o[1]))), nil
			}
			if o[0] == "rdx" {
				return imm64([]byte{0x48, 0xBA}, uint64(num(o[1]))), nil
			}
		}

	case "sub", "add":
		if len(o) == 2 && o[0] == "rsp" {
			op := byte(0xEC)
			if m == "add" {
				op = 0xC4
			}
			v := num(o[1])
			if v >= -128 && v <= 127 {
				return []byte{0x48, 0x83, op, byte(v)}, nil
			}
			return imm32([]byte{0x48, 0x81, op}, v), nil
		}
		if m == "add" && o[0] == "rax" && o[1] == "rbx" {
			return []byte{0x48, 0x01, 0xD8}, nil
		}
		if m == "sub" && o[0] == "rax" && o[1] == "rbx" {
			return []byte{0x48, 0x29, 0xD8}, nil
		}
		if m == "add" && o[0] == "dl" {
			return []byte{0x80, 0xC2, byte(num(o[1]))}, nil
		}

	case "imul":
		if o[0] == "rax" && o[1] == "rbx" {
			return []byte{0x48, 0x0F, 0xAF, 0xC3}, nil
		}

	case "cmp":
		if len(o) == 2 {
			switch {
			case o[0] == "rax" && o[1] == "rbx":
				return []byte{0x48, 0x39, 0xD8}, nil
			case o[0] == "rax" && o[1] == "rdx":
				return []byte{0x48, 0x39, 0xD0}, nil
			case o[0] == "rax":
				return []byte{0x48, 0x83, 0xF8, byte(num(o[1]))}, nil
			case o[0] == "rbx":
				return []byte{0x48, 0x83, 0xFB, byte(num(o[1]))}, nil
			case o[0] == "r8b":
				return []byte{0x41, 0x80, 0xF8, byte(num(o[1]))}, nil
			}
		}

	case "test":
		if o[0] == "rax" && o[1] == "rax" {
			return []byte{0x48, 0x85, 0xC0}, nil
		}

	case "setl", "setle", "setg", "setge", "sete", "setne":
		if o[0] == "al" {
			op := map[string]byte{
				"setl": 0x9C, "setle": 0x9E, "setg": 0x9F,
				"setge": 0x9D, "sete": 0x94, "setne": 0x95,
			}[m]
			return []byte{0x0F, op, 0xC0}, nil
		}

	case "movzx":
		if o[0] == "rax" && o[1] == "al" {
			return []byte{0x48, 0x0F, 0xB6, 0xC0}, nil
		}

	case "lea":
		if len(o) == 2 && o[0] == "rsi" {
			if strings.HasPrefix(o[1], "[rbp") {
				return []byte{0x48, 0x8D, 0x75, byte(disp(o[1]))}, nil
			}
			if strings.HasPrefix(o[1], "[rip+") {
				name := strings.TrimSuffix(strings.TrimPrefix(o[1], "[rip+"), "]")
				return rel32([]byte{0x48, 0x8D, 0x35}, addr+3, p.dataAddr(name)), nil
			}
		}

	case "jmp", "je", "jne", "jz", "jnz", "jns", "call":
		target := p.labelAddr(o[0])
		switch m {
		case "jmp":
			return rel32([]byte{0xE9}, addr+1, target), nil
		case "call":
			return rel32([]byte{0xE8}, addr+1, target), nil
		case "je", "jz":
			return rel32([]byte{0x0F, 0x84}, addr+2, target), nil
		case "jne", "jnz":
			return rel32([]byte{0x0F, 0x85}, addr+2, target), nil
		case "jns":
			return rel32([]byte{0x0F, 0x89}, addr+2, target), nil
		}
	}
	return b, fmt.Errorf("elfgen: cannot encode %q at 0x%x", ln.lineText(), at)
}

func (l line) lineText() string {
	if l.label != "" {
		return l.label
	}
	return l.mnemonic + " " + strings.Join(l.operands, ", ")
}
