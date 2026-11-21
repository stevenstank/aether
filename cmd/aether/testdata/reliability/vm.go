// A stack-based bytecode virtual machine with a compiler from a tiny
// assembly text, labels, calls, and a fixed-size frame stack of arrays.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

type Op uint8

const (
	PUSH Op = iota
	POP
	ADD
	SUB
	MUL
	DIV
	MOD
	DUP
	SWAP
	OVER
	JMP
	JZ
	CALL
	RET
	LOAD
	STORE
	PRINT
	HALT
	LT
)

var names = map[string]Op{
	"push": PUSH, "pop": POP, "add": ADD, "sub": SUB, "mul": MUL, "div": DIV, "mod": MOD,
	"dup": DUP, "swap": SWAP, "over": OVER, "jmp": JMP, "jz": JZ, "call": CALL, "ret": RET,
	"load": LOAD, "store": STORE, "print": PRINT, "halt": HALT, "lt": LT,
}

type Instr struct {
	Op  Op
	Arg int32
}

func (i Instr) String() string {
	for n, o := range names {
		if o == i.Op {
			return fmt.Sprintf("%s %d", n, i.Arg)
		}
	}
	return "?"
}

type frame struct {
	ret    int
	locals [8]int64
}

type VM struct {
	code   []Instr
	stack  [256]int64
	sp     int
	frames [64]frame
	fp     int
	out    strings.Builder
	steps  int
}

type vmError struct {
	pc  int
	msg string
}

func (e vmError) Error() string { return fmt.Sprintf("pc %d: %s", e.pc, e.msg) }

func assemble(src string) ([]Instr, error) {
	labels := map[string]int{}
	var lines [][]string
	for _, l := range strings.Split(src, "\n") {
		if i := strings.IndexByte(l, ';'); i >= 0 {
			l = l[:i]
		}
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		if strings.HasSuffix(f[0], ":") {
			labels[strings.TrimSuffix(f[0], ":")] = len(lines)
			f = f[1:]
			if len(f) == 0 {
				continue
			}
		}
		lines = append(lines, f)
	}
	code := make([]Instr, 0, len(lines))
	for n, f := range lines {
		op, ok := names[f[0]]
		if !ok {
			return nil, fmt.Errorf("line %d: unknown op %q", n, f[0])
		}
		var arg int64
		if len(f) > 1 {
			if t, ok := labels[f[1]]; ok {
				arg = int64(t)
			} else {
				v, err := strconv.ParseInt(f[1], 10, 32)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", n, err)
				}
				arg = v
			}
		}
		code = append(code, Instr{op, int32(arg)})
	}
	return code, nil
}

func (m *VM) push(v int64) {
	m.stack[m.sp] = v
	m.sp++
}

func (m *VM) pop() int64 {
	m.sp--
	return m.stack[m.sp]
}

func (m *VM) Run() (err error) {
	pc := 0
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(interface{ RuntimeError() }); ok {
				_ = re
				err = vmError{pc, fmt.Sprint(r)}
				return
			}
			panic(r)
		}
	}()
	for {
		m.steps++
		in := m.code[pc]
		pc++
		switch in.Op {
		case PUSH:
			m.push(int64(in.Arg))
		case POP:
			m.pop()
		case ADD, SUB, MUL, DIV, MOD, LT:
			b, a := m.pop(), m.pop()
			var r int64
			switch in.Op {
			case ADD:
				r = a + b
			case SUB:
				r = a - b
			case MUL:
				r = a * b
			case DIV:
				r = a / b
			case MOD:
				r = a % b
			case LT:
				if a < b {
					r = 1
				}
			}
			m.push(r)
		case DUP:
			v := m.pop()
			m.push(v)
			m.push(v)
		case SWAP:
			b, a := m.pop(), m.pop()
			m.push(b)
			m.push(a)
		case OVER:
			b, a := m.pop(), m.pop()
			m.push(a)
			m.push(b)
			m.push(a)
		case JMP:
			pc = int(in.Arg)
		case JZ:
			if m.pop() == 0 {
				pc = int(in.Arg)
			}
		case CALL:
			m.fp++
			m.frames[m.fp] = frame{ret: pc}
			pc = int(in.Arg)
		case RET:
			pc = m.frames[m.fp].ret
			m.fp--
		case LOAD:
			m.push(m.frames[m.fp].locals[in.Arg])
		case STORE:
			m.frames[m.fp].locals[in.Arg] = m.pop()
		case PRINT:
			fmt.Fprintf(&m.out, "%d ", m.pop())
		case HALT:
			return nil
		default:
			return vmError{pc - 1, "bad opcode " + strconv.Itoa(int(in.Op))}
		}
	}
}

const fibProg = `
	push 0
	store 0        ; i = 0
loop:
	load 0
	push 25
	lt
	jz done
	load 0
	call fib
	print
	load 0
	push 1
	add
	store 0
	jmp loop
done:
	halt
fib:               ; n -> fib(n), iterative
	store 0
	push 0
	store 1
	push 1
	store 2
floop:
	load 0
	jz fend
	load 1
	load 2
	dup
	store 1
	add
	store 2
	load 0
	push 1
	sub
	store 0
	jmp floop
fend:
	load 1
	ret
`

const factProg = `
	push 15
	call fact
	print
	push 20
	call fact
	print
	halt
fact:          ; recursive
	dup
	push 2
	lt
	jz rec
	pop
	push 1
	ret
rec:
	dup
	push 1
	sub
	call fact
	mul
	ret
`

const badProgs = `push 1
push 0
div
halt
---
push 1
call deep
deep: call deep
---
pop
---
push 3
bogus 4
`

func main() {
	for i, src := range []string{fibProg, factProg} {
		code, err := assemble(src)
		if err != nil {
			fmt.Println("asm:", err)
			continue
		}
		m := &VM{code: code}
		err = m.Run()
		fmt.Printf("prog %d: %d instrs, %d steps, err=%v\n  %s\n", i, len(code), m.steps, err, m.out.String())
	}
	for i, src := range strings.Split(badProgs, "---\n") {
		code, err := assemble(src)
		if err != nil {
			fmt.Println("bad", i, "asm:", err)
			continue
		}
		m := &VM{code: code}
		err = m.Run()
		fmt.Println("bad", i, "err:", err, "sp:", m.sp, "fp:", m.fp)
	}
	code, _ := assemble(factProg)
	fmt.Println(code[0], code[len(code)-1], Instr{Op: 99})
}
