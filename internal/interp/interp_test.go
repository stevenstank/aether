package interp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

// countdown builds: n := start; for { println(n, n > 0, !(n > 0)); if
// !(n > 0) { break }; n-- }. With start < 0 it never ends.
func countdown(start int64, loop bool) *ir.Program {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	n := f.NewSlot("n", ir.I64, p1)
	b0, head, body, exit := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	f.Store(b0, n, f.Const(b0, start, p1), p1)
	f.Jump(b0, head, p1)
	pos := f.Compare(head, ir.Gt, f.Load(head, n, p1), f.Const(head, 0, p1), p1)
	f.Println(head, []*ir.Instr{f.Load(head, n, p1), pos, f.Not(head, pos, p1)}, p1)
	if loop {
		pos = f.Compare(head, ir.Ne, f.Load(head, n, p1), f.Const(head, 0, p1), p1)
	}
	f.Branch(head, pos, body, exit, p1)
	f.Store(body, n, f.Binary(body, ir.Sub, f.Load(body, n, p1), f.Const(body, 1, p1), p1), p1)
	f.Jump(body, head, p1)
	f.Ret(exit, p1)
	return p
}

func TestControlFlow(t *testing.T) {
	want := "2 true false\n1 true false\n0 false true\n"
	if got := output(t, countdown(2, false)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComparisons(t *testing.T) {
	edges := []int64{math.MinInt64, -1, 0, 1, math.MaxInt64}
	var want strings.Builder
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		for _, x := range edges {
			for _, y := range edges {
				var args []*ir.Instr
				for _, op := range []ir.Op{ir.Eq, ir.Ne, ir.Lt, ir.Le, ir.Gt, ir.Ge} {
					args = append(args, f.Compare(b, op, f.Const(b, x, p1), f.Const(b, y, p1), p1))
				}
				f.Println(b, args, p1)
				fmt.Fprintln(&want, x == y, x != y, x < y, x <= y, x > y, x >= y)
			}
		}
		for _, x := range []bool{false, true} {
			for _, y := range []bool{false, true} {
				bx, by := f.BoolConst(b, x, p1), f.BoolConst(b, y, p1)
				f.Println(b, []*ir.Instr{f.Compare(b, ir.Eq, bx, by, p1), f.Compare(b, ir.Ne, bx, by, p1), f.Not(b, bx, p1)}, p1)
				fmt.Fprintln(&want, x == y, x != y, !x)
			}
		}
	}))
	if got != want.String() {
		t.Errorf("got:\n%s\nwant:\n%s", got, want.String())
	}
}

// TestStepBudget checks that a program that never ends stops with ErrSteps,
// and that the budget does not affect a program that ends within it.
func TestStepBudget(t *testing.T) {
	var stderr bytes.Buffer
	if code, err := RunSteps(countdown(-1, true), &stderr, 1000); !errors.Is(err, ErrSteps) || code != 0 {
		t.Errorf("endless loop: exit %d, %v; want ErrSteps", code, err)
	}
	stderr.Reset()
	if code, err := RunSteps(countdown(2, false), &stderr, 1000); err != nil || code != 0 || stderr.String() != "2 true false\n1 true false\n0 false true\n" {
		t.Errorf("bounded loop: exit %d, %v, %q", code, err, stderr.String())
	}
}

// TestGlobalsAndInit checks that globals start at zero, that Init runs
// before main, and that stores in one function are seen by the others:
//
//	var x, z int
//	var b uint8
//	func double() { x *= 2; b = 255 }
//	// Init: x = 5; double()
//	func main() { println(x, b, z); x++; println(x) }
func TestGlobalsAndInit(t *testing.T) {
	p := &ir.Program{}
	x := p.NewGlobal("x", ir.I64, "p.go", p1)
	z := p.NewGlobal("z", ir.I64, "p.go", p1)
	b := p.NewGlobal("b", ir.U8, "p.go", p1)
	m := p.NewFunc("main", "p.go", p1)
	d := p.NewFunc("double", "p.go", p1)
	db := d.Entry()
	d.GStore(db, x, d.Binary(db, ir.Mul, d.GLoad(db, x, p1), d.Const(db, 2, p1), p1), p1)
	d.GStore(db, b, d.TypedConst(db, ir.U8, 255, p1), p1)
	d.Ret(db, p1)
	p.Init = p.NewFunc("init", "p.go", p1)
	ib := p.Init.Entry()
	p.Init.GStore(ib, x, p.Init.Const(ib, 5, p1), p1)
	p.Init.Call(ib, d, nil, p1)
	p.Init.Ret(ib, p1)
	mb := m.Entry()
	m.Println(mb, []*ir.Instr{m.GLoad(mb, x, p1), m.GLoad(mb, b, p1), m.GLoad(mb, z, p1)}, p1)
	m.GStore(mb, x, m.Binary(mb, ir.Add, m.GLoad(mb, x, p1), m.Const(mb, 1, p1), p1), p1)
	m.Println(mb, []*ir.Instr{m.GLoad(mb, x, p1)}, p1)
	m.Ret(mb, p1)
	if got, want := output(t, p), "10 255 0\n11\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// The step budget covers Init and main together: Init executes 11
	// instructions, double's included, and main 11.
	if _, err := RunSteps(p, &bytes.Buffer{}, 22); err != nil {
		t.Errorf("budget of 22: %v", err)
	}
	if _, err := RunSteps(p, &bytes.Buffer{}, 21); !errors.Is(err, ErrSteps) {
		t.Errorf("budget of 21: %v, want ErrSteps", err)
	}
}

// TestPanicInInit checks that a panic in Init ends the program before main
// runs.
func TestPanicInInit(t *testing.T) {
	p := program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, []*ir.Instr{f.Const(b, 2, p1)}, p1)
	})
	p.Init = p.NewFunc("init", "p.go", p1)
	ib := p.Init.Entry()
	p.Init.Print(ib, []*ir.Instr{p.Init.Const(ib, 1, p1)}, p1)
	p.Init.Panic(ib, ir.PanicDivide, p1)
	var stderr bytes.Buffer
	code, err := Run(p, &stderr)
	if want := "1panic: runtime error: integer divide by zero\n"; err != nil || code != 2 || stderr.String() != want {
		t.Errorf("exit %d, %v, stderr %q; want exit 2, %q", code, err, stderr.String(), want)
	}
}

// indirect returns a program that stores f's code address in a global
// memory object, after which it stores 1, loads the address again, and
// calls it with that object as the context and 20; f returns its argument
// plus the word after the code address in its context. sig is the call's
// signature.
func indirect(sig *ir.Sig) *ir.Program {
	p := &ir.Program{}
	m := p.NewFunc("main", "p.go", p1)
	f := p.NewFunc("f", "p.go", p1)
	f.Params, f.Results = []ir.Type{ir.Ptr, ir.I64}, []ir.Type{ir.I64}
	fb := f.Entry()
	ctx, x := f.Param(fb, 0, p1), f.Param(fb, 1, p1)
	one := f.LdM(fb, ir.I64, ctx, f.Const(fb, 8, p1), p1)
	f.Ret(fb, p1, f.Binary(fb, ir.Add, x, one, p1))
	g := p.NewMem("g", 16, 8, "p.go", p1)
	mb := m.Entry()
	a := m.Addr(mb, g, p1)
	m.StM(mb, a, m.Const(mb, 0, p1), m.FuncAddr(mb, f, p1), p1)
	m.StM(mb, a, m.Const(mb, 8, p1), m.Const(mb, 1, p1), p1)
	code := m.LdM(mb, ir.Ptr, a, m.Const(mb, 0, p1), p1)
	m.Println(mb, []*ir.Instr{m.CallInd(mb, sig, code, []*ir.Instr{a, m.Const(mb, 20, p1)}, p1)}, p1)
	m.Ret(mb, p1)
	return p
}

// TestIndirectCalls checks CallInd (D-039): a call through a code address
// that went through memory, and the interpreter's refusal of a call whose
// signature is not the callee's or of an address that is not code.
func TestIndirectCalls(t *testing.T) {
	sig := &ir.Sig{Params: []ir.Type{ir.Ptr, ir.I64}, Results: []ir.Type{ir.I64}}
	if got, want := output(t, indirect(sig)), "21\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A signature other than f's: valid IR, but not a call the
	// interpreter will make.
	bad := indirect(&ir.Sig{Params: []ir.Type{ir.Ptr, ir.I64}, Results: []ir.Type{ir.Bool}})
	if _, err := Run(bad, io.Discard); err == nil || !strings.Contains(err.Error(), "another signature") {
		t.Errorf("signature mismatch: err = %v", err)
	}
	// The address of a memory object is not code.
	notCode := indirect(sig)
	main := notCode.Funcs[0].Entry().Instrs
	for _, in := range main {
		if in.Op == ir.CallInd {
			in.Args[0] = main[0] // the addr
		}
	}
	if _, err := Run(notCode, io.Discard); err == nil || !strings.Contains(err.Error(), "is not a code address") {
		t.Errorf("not code: err = %v", err)
	}
}

// readProgram reads from fd 0 into an object of 8 bytes three times, and
// from the closed fd 3, printing each result and the first two bytes read
// (increment 11.8).
func readProgram() *ir.Program {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	o := f.Alloc(b, f.Const(b, 8, p1), p1)
	for _, fd := range []int64{0, 0, 0, 3} {
		n := f.Sys(b, ir.SysRead, ir.I64, p1, f.Const(b, fd, p1), o, f.Const(b, 8, p1))
		f.Println(b, []*ir.Instr{n, f.LoadB(b, o, f.Const(b, 0, p1), p1), f.LoadB(b, o, f.Const(b, 1, p1), p1)}, p1)
	}
	f.Ret(b, p1)
	return p
}

// TestSysRead checks SysRead: the run's stdin, read once, then at its end,
// a closed descriptor, no stdin, and a read into a read-only object.
func TestSysRead(t *testing.T) {
	var stderr bytes.Buffer
	code, err := RunOptions(readProgram(), &stderr, Options{Stdin: strings.NewReader("hey")})
	if want := "3 104 101\n0 104 101\n0 104 101\n-9 104 101\n"; err != nil || code != 0 || stderr.String() != want {
		t.Errorf("exit %d, %v, stderr %q; want %q", code, err, stderr.String(), want)
	}
	stderr.Reset()
	if code, err := Run(readProgram(), &stderr); err != nil || code != 0 || stderr.String() != "0 0 0\n0 0 0\n0 0 0\n-9 0 0\n" {
		t.Errorf("no stdin: exit %d, %v, stderr %q", code, err, stderr.String())
	}
	p := &ir.Program{}
	s := p.Intern("const")
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	f.Sys(b, ir.SysRead, ir.I64, p1, f.Const(b, 0, p1), f.Str(b, s, p1), f.Const(b, 5, p1))
	f.Ret(b, p1)
	if _, err := RunOptions(p, io.Discard, Options{Stdin: strings.NewReader("x")}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("read into a string's bytes: err = %v", err)
	}
}

// TestSys checks the system calls (D-049, increment 11.6): writes to the
// run's stdout and stderr and to a closed descriptor, the arguments, and an
// exit in Init, which ends the run before main with its status modulo 256.
func TestSys(t *testing.T) {
	p := &ir.Program{}
	hello := p.Intern("hello")
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	{
		s := f.Str(b, hello, p1)
		for _, fd := range []int64{1, 2, 3} {
			n := f.Sys(b, ir.SysWrite, ir.I64, p1, f.Const(b, fd, p1), s, f.Const(b, 5, p1))
			f.Println(b, []*ir.Instr{n}, p1)
		}
		f.Sys(b, ir.SysGC, ir.NoType, p1)
		i := f.Const(b, 1, p1)
		f.Println(b, []*ir.Instr{f.Sys(b, ir.SysArgc, ir.I64, p1), f.Sys(b, ir.SysArgLen, ir.I64, p1, i),
			f.LoadB(b, f.Sys(b, ir.SysArgPtr, ir.Ptr, p1, i), f.Const(b, 1, p1), p1)}, p1)
		f.Sys(b, ir.SysExit, ir.NoType, p1, f.Const(b, 300, p1))
		f.Println(b, nil, p1) // not reached
		f.Ret(b, p1)
	}
	var stdout, stderr bytes.Buffer
	code, err := RunOptions(p, &stderr, Options{Stdout: &stdout, Args: []string{"prog", "xyz"}})
	if want := "5\nhello5\n-9\n2 3 121\n"; err != nil || code != 44 || stdout.String() != "hello" || stderr.String() != want {
		t.Errorf("exit %d, %v, stdout %q, stderr %q; want exit 44, \"hello\", %q", code, err, stdout.String(), stderr.String(), want)
	}
	// Without options, stdout is discarded and there is one argument.
	stderr.Reset()
	if code, err := Run(p, &stderr); err == nil || code != 0 || !strings.Contains(err.Error(), "argument 1 of 1") {
		t.Errorf("exit %d, %v; want an error for argument 1 of 1", code, err)
	}

	p.Init = p.NewFunc("init", "p.go", p1)
	ib := p.Init.Entry()
	p.Init.Sys(ib, ir.SysExit, ir.NoType, p1, p.Init.Const(ib, 0, p1))
	p.Init.Ret(ib, p1)
	stderr.Reset()
	if code, err := Run(p, &stderr); err != nil || code != 0 || stderr.Len() != 0 {
		t.Errorf("exit in Init: exit %d, %v, stderr %q; want exit 0 and main not run", code, err, stderr.String())
	}
}

// TestSysEnvAndCall checks the environment's system calls and SysCall
// (D-051): close of the run's stdout, which a later write then fails, and of
// a descriptor never open, and the error of a system call the interpreter
// does not make.
func TestSysEnvAndCall(t *testing.T) {
	build := func(trap int64) *ir.Program {
		p := &ir.Program{}
		f := p.NewFunc("main", "p.go", p1)
		b := f.Entry()
		i := f.Const(b, 1, p1)
		f.Println(b, []*ir.Instr{f.Sys(b, ir.SysEnvc, ir.I64, p1), f.Sys(b, ir.SysEnvLen, ir.I64, p1, i),
			f.LoadB(b, f.Sys(b, ir.SysEnvPtr, ir.Ptr, p1, i), f.Const(b, 2, p1), p1)}, p1)
		w := func(v int64) *ir.Instr { return f.TypedConst(b, ir.U64, v, p1) }
		for _, fd := range []int64{1, 7} {
			f.Println(b, []*ir.Instr{f.Sys(b, ir.SysCall, ir.I64, p1, w(trap), w(fd), w(0), w(0), w(0), w(0), w(0))}, p1)
		}
		s := f.Str(b, p.Intern("x"), p1)
		f.Println(b, []*ir.Instr{f.Sys(b, ir.SysWrite, ir.I64, p1, f.Const(b, 1, p1), s, f.Const(b, 1, p1))}, p1)
		f.Ret(b, p1)
		return p
	}
	var stdout, stderr bytes.Buffer
	code, err := RunOptions(build(3), &stderr, Options{Stdout: &stdout, Env: []string{"A=1", "BC=23"}})
	if want := "2 5 61\n0\n-9\n-9\n"; err != nil || code != 0 || stdout.Len() != 0 || stderr.String() != want {
		t.Errorf("exit %d, %v, stdout %q, stderr %q; want %q", code, err, stdout.String(), stderr.String(), want)
	}
	if _, err := RunOptions(build(3), io.Discard, Options{}); err == nil || !strings.Contains(err.Error(), "environment string 1 of 0") {
		t.Errorf("no environment: err = %v", err)
	}
	if _, err := RunOptions(build(39), io.Discard, Options{Env: []string{"A=1", "B=2"}}); err == nil || !strings.Contains(err.Error(), "not emulated") {
		t.Errorf("getpid: err = %v", err)
	}
}

// TestTicks checks SysTicks in the interpreter (D-058): nanoseconds of
// the monotonic clock, which grow.
func TestTicks(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	t0 := f.Sys(b, ir.SysTicks, ir.I64, p1)
	for range 1000 {
		f.Sys(b, ir.SysRand, ir.U64, p1)
	}
	t1 := f.Sys(b, ir.SysTicks, ir.I64, p1)
	f.Println(b, []*ir.Instr{f.Compare(b, ir.Gt, t1, t0, p1), f.Compare(b, ir.Ge, t0, f.Const(b, 0, p1), p1)}, p1)
	f.Ret(b, p1)
	var stderr bytes.Buffer
	if code, err := RunOptions(p, &stderr, Options{}); err != nil || code != 0 || stderr.String() != "true true\n" {
		t.Errorf("exit %d, %v, %q", code, err, stderr.String())
	}
}
