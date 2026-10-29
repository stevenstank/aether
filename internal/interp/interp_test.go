package interp

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// The tests build IR by hand, so the interpreter is tested independently of
// lowering. Lowering is tested against gc through the interpreter in
// cmd/aether.

var p1 = ir.Pos{Line: 1, Col: 1}

// program returns a main whose body is built by body, then ends in ret.
func program(body func(f *ir.Func, b *ir.Block)) *ir.Program {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	body(f, f.Entry())
	f.Ret(f.Entry(), p1)
	return p
}

func output(t *testing.T, p *ir.Program) string {
	t.Helper()
	var stderr bytes.Buffer
	if code, err := Run(p, &stderr); err != nil || code != 0 {
		t.Fatalf("exit %d, %v", code, err)
	}
	return stderr.String()
}

func TestArithmeticWraps(t *testing.T) {
	const max, min = math.MaxInt64, math.MinInt64
	tests := []struct {
		op   ir.Op
		x, y int64
		want string
	}{
		{ir.Add, max, 1, "-9223372036854775808"},
		{ir.Sub, min, 1, "9223372036854775807"},
		{ir.Mul, 3037000500, 3037000500, "-9223372036709301616"},
		{ir.Mul, max, 2, "-2"},
		{ir.Mul, min, -1, "-9223372036854775808"},
		{ir.Add, -5, 3, "-2"},
		{ir.Sub, 3, 5, "-2"},
		{ir.Mul, -4, 6, "-24"},
	}
	for _, tt := range tests {
		got := output(t, program(func(f *ir.Func, b *ir.Block) {
			v := f.Binary(b, tt.op, f.Const(b, tt.x, p1), f.Const(b, tt.y, p1), p1)
			f.Println(b, []*ir.Instr{v}, p1)
		}))
		if got != tt.want+"\n" {
			t.Errorf("%d %s %d: got %q, want %q", tt.x, tt.op, tt.y, got, tt.want)
		}
	}
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, []*ir.Instr{f.Neg(b, f.Const(b, min, p1), p1), f.Neg(b, f.Const(b, max, p1), p1)}, p1)
	}))
	if want := "-9223372036854775808 -9223372036854775807\n"; got != want {
		t.Errorf("neg: got %q, want %q", got, want)
	}
}

func TestPrintlnFormat(t *testing.T) {
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, nil, p1)
		f.Println(b, []*ir.Instr{f.Const(b, 0, p1)}, p1)
		f.Println(b, []*ir.Instr{f.Const(b, -10, p1), f.Const(b, 100, p1), f.Const(b, math.MinInt64, p1)}, p1)
	}))
	if want := "\n0\n-10 100 -9223372036854775808\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSlots(t *testing.T) {
	// x := 1; y := x; x = 2; println(x, y)
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		x := f.NewSlot("x", ir.I64, p1)
		y := f.NewSlot("y", ir.I64, p1)
		f.Store(b, x, f.Const(b, 1, p1), p1)
		f.Store(b, y, f.Load(b, x, p1), p1)
		f.Store(b, x, f.Const(b, 2, p1), p1)
		f.Println(b, []*ir.Instr{f.Load(b, x, p1), f.Load(b, y, p1)}, p1)
	}))
	if want := "2 1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMalformedNotExecuted(t *testing.T) {
	p := program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, []*ir.Instr{f.Const(b, 1, p1)}, p1)
		f.Load(b, f.NewSlot("x", ir.I64, p1), p1) // load before store
	})
	var stderr bytes.Buffer
	_, err := Run(p, &stderr)
	if err == nil || !strings.Contains(err.Error(), "load of s0 (x) before any store") {
		t.Errorf("got error %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("malformed program produced output %q", stderr.String())
	}
}

type failingWriter struct{ calls int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	return 0, errors.New("write failed")
}

func TestWriteErrorsIgnored(t *testing.T) {
	p := program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, nil, p1)
		f.Println(b, nil, p1)
	})
	w := &failingWriter{}
	if code, err := Run(p, w); err != nil || code != 0 {
		t.Fatalf("write error reported: exit %d, %v", code, err)
	}
	if w.calls != 2 {
		t.Errorf("%d writes, want 2: execution must continue after a failed write", w.calls)
	}
}

func TestCalls(t *testing.T) {
	// func f(a, b, c int) int { return a - b*c }
	// func g(x int) { println(x) }
	// func main() { x := f(1, 2, 3); g(x); println(f(x, x, x)) }
	p := &ir.Program{}
	m := p.NewFunc("main", "p.go", p1)
	f := p.NewFunc("f", "p.go", p1)
	f.Params, f.Results = []ir.Type{ir.I64, ir.I64, ir.I64}, []ir.Type{ir.I64}
	fb := f.Entry()
	a, b, c := f.Param(fb, 0, p1), f.Param(fb, 1, p1), f.Param(fb, 2, p1)
	f.Ret(fb, p1, f.Binary(fb, ir.Sub, a, f.Binary(fb, ir.Mul, b, c, p1), p1))
	g := p.NewFunc("g", "p.go", p1)
	g.Params = []ir.Type{ir.I64}
	g.Println(g.Entry(), []*ir.Instr{g.Param(g.Entry(), 0, p1)}, p1)
	g.Ret(g.Entry(), p1)
	mb := m.Entry()
	x := m.NewSlot("x", ir.I64, p1)
	m.Store(mb, x, m.Call(mb, f, []*ir.Instr{m.Const(mb, 1, p1), m.Const(mb, 2, p1), m.Const(mb, 3, p1)}, p1), p1)
	m.Call(mb, g, []*ir.Instr{m.Load(mb, x, p1)}, p1)
	lx := m.Load(mb, x, p1)
	m.Println(mb, []*ir.Instr{m.Call(mb, f, []*ir.Instr{lx, lx, lx}, p1)}, p1)
	m.Ret(mb, p1)
	if got, want := output(t, p), "-5\n-30\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// recursion returns a program that prints 7, then calls r(0), where
// r(n) = r(n+1) + 1 never returns.
func recursion() *ir.Program {
	p := &ir.Program{}
	m := p.NewFunc("main", "p.go", p1)
	r := p.NewFunc("r", "p.go", p1)
	r.Params, r.Results = []ir.Type{ir.I64}, []ir.Type{ir.I64}
	rb := r.Entry()
	one := r.Const(rb, 1, p1)
	n := r.Binary(rb, ir.Add, r.Param(rb, 0, p1), one, p1)
	r.Ret(rb, p1, r.Binary(rb, ir.Add, r.Call(rb, r, []*ir.Instr{n}, p1), one, p1))
	mb := m.Entry()
	m.Println(mb, []*ir.Instr{m.Const(mb, 7, p1)}, p1)
	m.Println(mb, []*ir.Instr{m.Call(mb, r, []*ir.Instr{m.Const(mb, 0, p1)}, p1)}, p1)
	m.Ret(mb, p1)
	return p
}

// chain returns a program of depth frames: main calls f1, which calls f2,
// and so on; the last prints 1.
func chain(depth int) *ir.Program {
	p := &ir.Program{}
	fs := []*ir.Func{p.NewFunc("main", "p.go", p1)}
	for i := 1; i < depth; i++ {
		fs = append(fs, p.NewFunc(fmt.Sprintf("f%d", i), "p.go", p1))
	}
	for i, f := range fs {
		if i+1 < len(fs) {
			f.Call(f.Entry(), fs[i+1], nil, p1)
		} else {
			f.Println(f.Entry(), []*ir.Instr{f.Const(f.Entry(), 1, p1)}, p1)
		}
		f.Ret(f.Entry(), p1)
	}
	return p
}

// TestStackOverflow checks the depth limit (D-023): MaxDepth frames run,
// one more is a stack overflow, reported after the earlier output with exit
// status 2, as the native runtime does (SEMANTICS.md V-2).
func TestStackOverflow(t *testing.T) {
	tests := []struct {
		name string
		prog *ir.Program
		out  string
		code int
	}{
		{"unbounded recursion", recursion(), "7\n" + stackOverflow, 2},
		{"MaxDepth frames", chain(MaxDepth), "1\n", 0},
		{"MaxDepth+1 frames", chain(MaxDepth + 1), stackOverflow, 2},
	}
	for _, tt := range tests {
		var stderr bytes.Buffer
		code, err := Run(tt.prog, &stderr)
		if err != nil || code != tt.code || stderr.String() != tt.out {
			t.Errorf("%s: exit %d, %v, stderr %q; want exit %d, %q", tt.name, code, err, stderr.String(), tt.code, tt.out)
		}
	}
}

// TestStackOverflowLowGoStack runs unbounded recursion to MaxDepth in a
// subprocess whose Go stack is limited far below what one Go call per IR
// call would need. It is supporting evidence for the frame-stack design
// (D-023), not a proof that the Go stack cannot be exhausted.
func TestStackOverflowLowGoStack(t *testing.T) {
	if os.Getenv("AETHER_INTERP_LOW_STACK") == "1" {
		debug.SetMaxStack(64 << 10)
		var stderr bytes.Buffer
		if code, err := Run(recursion(), &stderr); err != nil || code != 2 || stderr.String() != "7\n"+stackOverflow {
			t.Fatalf("exit %d, %v, stderr %q", code, err, stderr.String())
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStackOverflowLowGoStack$", "-test.count=1")
	cmd.Env = append(os.Environ(), "AETHER_INTERP_LOW_STACK=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subprocess with a 64 KiB Go stack: %v\n%s", err, out)
	}
}
