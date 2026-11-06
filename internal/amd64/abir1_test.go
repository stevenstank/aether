package amd64

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// r1Copies simulates parallelCopy on moves: every destination must end up with
// its source's old value, and only destinations and %rax may be written.
func r1Copies(t *testing.T, moves []regCopy) {
	t.Helper()
	var b bytes.Buffer
	e := &emitter{b: &b}
	e.parallelCopy(moves)
	dsts := map[string]bool{"%rax": true}
	for _, m := range moves {
		dsts[m.dst] = true
	}
	s := &r1Sim{map[string]string{}, func(d string) bool { return dsts[d] }}
	if err := s.run(parseInstrs(b.Bytes())); err != nil {
		t.Fatalf("%v: %v\n%s", moves, err, b.String())
	}
	for _, m := range moves {
		if got := s.read(m.dst); got != m.src {
			t.Errorf("%v: %s holds %s, not %s\n%s", moves, m.dst, got, m.src, b.String())
		}
	}
}

// TestR1ParallelCopy runs every permutation of the argument registers, the
// identity and every copy cycle among them, also with the other sources a call
// has, and shapes with a shared source and copies into memory, as at entry.
func TestR1ParallelCopy(t *testing.T) {
	regs := argRegs
	n := 0
	var walk func(k int)
	walk = func(k int) {
		if k == len(regs) {
			var moves []regCopy
			for i, r := range regs {
				moves = append(moves, regCopy{argRegs[i], r})
			}
			r1Copies(t, moves)
			n++
			return
		}
		for i := k; i < len(regs); i++ {
			regs[k], regs[i] = regs[i], regs[k]
			walk(k + 1)
			regs[k], regs[i] = regs[i], regs[k]
		}
	}
	walk(0)
	if n != 720 {
		t.Errorf("%d permutations, want 720", n)
	}
	for _, moves := range [][]regCopy{
		{{"%rdi", "-8(%rbp)"}, {"%rsi", "$5"}, {"%r8", "%rbx"}},
		{{"%rdi", "%rsi"}, {"%rsi", "%rdi"}, {"%r8", "%r9"}, {"%r9", "%r8"}},
		{{"%rdi", "%rbx"}, {"%rsi", "%rbx"}, {"%r8", "%rdi"}},
		{{"%rdi", "%rsi"}, {"%r8", "%rsi"}, {"%rsi", "%rdi"}},
		{{"-8(%rbp)", "%rdi"}, {"%rdi", "%rsi"}, {"%rsi", "%rdi"}},
	} {
		r1Copies(t, moves)
	}
}

// r1Func adds a function of n int parameters and the given number of results.
func r1Func(p *ir.Program, name string, n, results int) *ir.Func {
	f := p.NewFunc(name, "p.go", p1)
	for range n {
		f.Params = append(f.Params, ir.I64)
	}
	for range results {
		f.Results = append(f.Results, ir.I64)
	}
	return f
}

// TestR1EntryPrecondition builds functions the IR contract allows and ABI-R1
// cannot compile: each is an error, and what lowering produces is not.
func TestR1EntryPrecondition(t *testing.T) {
	tests := []struct {
		name string
		want string // "" for no error
		body func(f *ir.Func)
	}{
		{"params then stores", "", func(f *ir.Func) {
			b := f.Entry()
			f.Store(b, f.NewSlot("x", ir.I64, p1), f.Param(b, 0, p1), p1)
			f.Store(b, f.NewSlot("y", ir.I64, p1), f.Param(b, 1, p1), p1)
			f.Ret(b, p1)
		}},
		{"code after the last param", "", func(f *ir.Func) {
			b := f.Entry()
			f.Param(b, 0, p1)
			f.Println(b, []*ir.Instr{f.Param(b, 1, p1)}, p1)
			f.Ret(b, p1)
		}},
		{"param after a clobbering call", "runs before the last param", func(f *ir.Func) {
			b := f.Entry()
			f.Println(b, []*ir.Instr{f.Param(b, 0, p1)}, p1)
			f.Println(b, []*ir.Instr{f.Param(b, 1, p1)}, p1)
			f.Ret(b, p1)
		}},
		{"param after arithmetic", "runs before the last param", func(f *ir.Func) {
			b := f.Entry()
			x := f.Binary(b, ir.Add, f.Param(b, 0, p1), f.Const(b, 1, p1), p1)
			f.Println(b, []*ir.Instr{x, f.Param(b, 1, p1)}, p1)
			f.Ret(b, p1)
		}},
		{"store of another value before a param", "runs before the last param", func(f *ir.Func) {
			b := f.Entry()
			f.Store(b, f.NewSlot("x", ir.I64, p1), f.Const(b, 1, p1), p1)
			f.Println(b, []*ir.Instr{f.Param(b, 0, p1)}, p1)
			f.Ret(b, p1)
		}},
		{"entry with a predecessor", "entry block has predecessor", func(f *ir.Func) {
			b, more, done := f.Entry(), f.NewBlock(), f.NewBlock()
			x := f.NewSlot("x", ir.I64, p1)
			f.Store(b, x, f.Param(b, 0, p1), p1)
			f.Jump(b, more, p1)
			f.Branch(more, f.Compare(more, ir.Eq, f.Load(more, x, p1), f.Const(more, 0, p1), p1), b, done, p1)
			f.Ret(done, p1)
		}},
	}
	for _, tt := range tests {
		p := &ir.Program{}
		f := r1Func(p, "f", 2, 0)
		tt.body(f)
		m := p.NewFunc("main", "p.go", p1)
		mb := m.Entry()
		m.Call(mb, f, []*ir.Instr{m.Const(mb, 1, p1), m.Const(mb, 2, p1)}, p1)
		m.Ret(mb, p1)
		_, err := EmitWith(p, EmitOptions{ABIR1: true})
		if tt.want == "" && err != nil {
			t.Errorf("%s: %v", tt.name, err)
		}
		if tt.want != "" && (err == nil || !strings.Contains(err.Error(), "abi-r1: ") || !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: error %v, want one about %q", tt.name, err, tt.want)
		}
		if tt.want != "" {
			// ABI-S0 reads stable memory, so the IR is fine there.
			if _, err := Emit(p); err != nil {
				t.Errorf("%s: ABI-S0: %v", tt.name, err)
			}
		}
	}
}

// TestR1FrameSizes checks when ABI-R1 keeps the outgoing area: a frame drops it
// only if no call of the function passes a word, and otherwise keeps all of it.
func TestR1FrameSizes(t *testing.T) {
	tests := []struct {
		name         string
		params, res  int
		s0Out, r1Out int
	}{
		{"no words", 0, 0, 0, 0},
		{"six arguments and a result", 6, 1, 7, 0},
		{"seven arguments", 7, 0, 7, 7},
		{"seven arguments and a result", 7, 1, 8, 8},
		{"two results", 1, 2, 3, 3},
		{"three results", 0, 3, 3, 3},
	}
	for _, tt := range tests {
		for _, opts := range []EmitOptions{{}, {ABIR1: true}} {
			p := &ir.Program{}
			c := r1Func(p, "c", tt.params, tt.res)
			cb := c.Entry()
			var rs []*ir.Instr
			for range tt.res {
				rs = append(rs, c.Const(cb, 3, p1))
			}
			c.Ret(cb, p1, rs...)
			m := p.NewFunc("main", "p.go", p1)
			mb := m.Entry()
			var args []*ir.Instr
			for i := range tt.params {
				args = append(args, m.Const(mb, int64(i), p1))
			}
			m.Call(mb, c, args, p1)
			m.Ret(mb, p1)
			want := tt.s0Out
			if opts.ABIR1 {
				want = tt.r1Out
			}
			var b bytes.Buffer
			fr, err := emitFunc(&b, 1, m, opts)
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if fr.Out != want {
				t.Errorf("%s: ABIR1 %v: %d outgoing words, want %d", tt.name, opts.ABIR1, fr.Out, want)
			}
		}
	}
}

// TestR1StackBoundary is TestStackBoundary with arguments in registers. leaf has
// a call that never runs and passes no word, so its frame is smaller than under
// ABI-S0, and the smaller size is what the prologue checks. At E = T it runs and
// its println runs the helpers in the reserve below L. At E = T - 16 the
// arguments in registers do not stop the check from reporting the overflow.
func TestR1StackBoundary(t *testing.T) {
	p := &ir.Program{}
	m := p.NewFunc("main", "p.go", p1)
	leaf := r1Func(p, "leaf", 2, 1)
	other := r1Func(p, "other", 1, 1)
	ob := other.Entry()
	other.Ret(ob, p1, other.Param(ob, 0, p1))
	lb, rec, done := leaf.Entry(), leaf.NewBlock(), leaf.NewBlock()
	x, y := leaf.Param(lb, 0, p1), leaf.Param(lb, 1, p1)
	leaf.Println(lb, []*ir.Instr{x, y}, p1)
	leaf.Branch(lb, leaf.Compare(lb, ir.Eq, y, leaf.Const(lb, 0, p1), p1), rec, done, p1)
	leaf.Ret(rec, p1, leaf.Call(rec, other, []*ir.Instr{leaf.Const(rec, 3, p1)}, p1))
	leaf.Ret(done, p1, leaf.Const(done, 4, p1))
	mb := m.Entry()
	m.Call(mb, leaf, []*ir.Instr{m.Const(mb, 1, p1), m.Const(mb, 2, p1)}, p1)
	m.Ret(mb, p1)
	asm, err := EmitWith(p, EmitOptions{ABIR1: true})
	if err != nil {
		t.Fatal(err)
	}
	// x, y, three constants, the comparison, and the call's result, with no
	// outgoing word; ABI-S0 needs two.
	r1, err := NewFrame(0, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	s0, err := NewFrame(0, 7, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Size >= s0.Size {
		t.Fatalf("frame of %d bytes is not smaller than ABI-S0's %d", r1.Size, s0.Size)
	}
	prologue := fmt.Appendf(nil, "main.leaf:\n\tmovq\taether.stacklimit(%%rip), %%rax\n\taddq\t$%d, %%rax\n\tcmpq\t%%rax, %%rsp\n\tjb\taether.stackoverflow\n\tpushq\t%%rbp\n\tmovq\t%%rsp, %%rbp\n\tsubq\t$%d, %%rsp\n", r1.Size+8, r1.Size)
	if !bytes.Contains(asm, prologue) {
		t.Fatalf("leaf's frame is not %d bytes:\n%s", r1.Size, asm)
	}
	compiled := bytes.ReplaceAll(asm, []byte("main.main"), []byte("main.compiled"))
	for _, tt := range []struct {
		name string
		e    int // entry %rsp minus T
		want string
		code int
	}{
		{"E = T", 0, "-5 7\n", 0},
		{"E = T - 16", -16, "fatal error: stack overflow\n", 2},
	} {
		harness := fmt.Sprintf(`	.text
	.globl	main.main
main.main:
	pushq	%%rbp
	movq	%%rsp, %%rbp
	movq	%s(%%rip), %%rsp
	addq	$%d, %%rsp
	movq	$-5, %%rdi
	movq	$7, %%rsi
	movq	$99, %%rax
	call	main.leaf
	movq	%%rbp, %%rsp
	popq	%%rbp
	ret
	.section	.note.GNU-stack,"",@progbits
`, symStackLimit, r1.Size+16+tt.e)
		files := withRuntime(compiled)
		files["harness.s"] = []byte(harness)
		got, code := execute(t, link(t, t.TempDir(), files), nil)
		if got != tt.want || code != tt.code {
			t.Errorf("%s: exit %d, stderr %q; want exit %d, %q", tt.name, code, got, tt.code, tt.want)
		}
	}
}
