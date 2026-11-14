package amd64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// heapProgram exercises the heap (D-010, D-035): objects allocated by a
// callee and used after it returns, interior pointers made by padd at
// constant and run-time offsets, pointers stored in objects and loaded back
// (a linked list of 10000 nodes, with values live across each allocation),
// pointer comparisons, zero-size allocations, and sizes that are not
// multiples of 8, whose neighbors must not overlap.
func heapProgram() *ir.Program {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)

	// node(next, v) allocates {next *node; v int64} and returns it.
	node := p.NewFunc("node", "p.go", p1)
	node.Params = []ir.Type{ir.Ptr, ir.I64}
	node.Results = []ir.Type{ir.Ptr}
	{
		b := node.Entry()
		next, v := node.Param(b, 0, p1), node.Param(b, 1, p1)
		o := node.Alloc(b, node.Const(b, 16, p1), p1)
		node.StM(b, o, node.Const(b, 0, p1), next, p1)
		node.StM(b, node.PAdd(b, o, node.Const(b, 8, p1), p1), node.Const(b, 0, p1), v, p1)
		node.Ret(b, p1, o)
	}

	// sum(list) walks the list and returns the sum of its values and its
	// length.
	sum := p.NewFunc("sum", "p.go", p1)
	sum.Params = []ir.Type{ir.Ptr}
	sum.Results = []ir.Type{ir.I64, ir.I64}
	{
		cur, acc, n := sum.NewSlot("cur", ir.Ptr, p1), sum.NewSlot("acc", ir.I64, p1), sum.NewSlot("n", ir.I64, p1)
		b0, head, body, exit := sum.Entry(), sum.NewBlock(), sum.NewBlock(), sum.NewBlock()
		sum.Store(b0, cur, sum.Param(b0, 0, p1), p1)
		sum.Store(b0, acc, sum.Const(b0, 0, p1), p1)
		sum.Store(b0, n, sum.Const(b0, 0, p1), p1)
		sum.Jump(b0, head, p1)
		sum.Branch(head, sum.Compare(head, ir.Ne, sum.Load(head, cur, p1), sum.TypedConst(head, ir.Ptr, 0, p1), p1), body, exit, p1)
		c := sum.Load(body, cur, p1)
		sum.Store(body, acc, sum.Binary(body, ir.Add, sum.Load(body, acc, p1), sum.LdM(body, ir.I64, c, sum.Const(body, 8, p1), p1), p1), p1)
		sum.Store(body, n, sum.Binary(body, ir.Add, sum.Load(body, n, p1), sum.Const(body, 1, p1), p1), p1)
		sum.Store(body, cur, sum.LdM(body, ir.Ptr, c, sum.Const(body, 0, p1), p1), p1)
		sum.Jump(body, head, p1)
		sum.Ret(exit, p1, sum.Load(exit, acc, p1), sum.Load(exit, n, p1))
	}

	b0 := main.Entry()
	list := main.NewSlot("list", ir.Ptr, p1)
	i := main.NewSlot("i", ir.I64, p1)
	main.Store(b0, list, main.TypedConst(b0, ir.Ptr, 0, p1), p1)
	main.Store(b0, i, main.Const(b0, 0, p1), p1)
	head, body, done := main.NewBlock(), main.NewBlock(), main.NewBlock()
	main.Jump(b0, head, p1)
	main.Branch(head, main.Compare(head, ir.Lt, main.Load(head, i, p1), main.Const(head, 10000, p1), p1), body, done, p1)
	iv := main.Load(body, i, p1)
	// i*3 is live across the call, and so across the allocation in it.
	k := main.Binary(body, ir.Mul, iv, main.Const(body, 3, p1), p1)
	n := main.Call(body, node, []*ir.Instr{main.Load(body, list, p1), k}, p1)
	main.Store(body, list, n, p1)
	// i+1 as 3i - 2i + 1, which uses k after the call.
	next := main.Binary(body, ir.Sub, k, main.Binary(body, ir.Add, iv, iv, p1), p1)
	main.Store(body, i, main.Binary(body, ir.Add, next, main.Const(body, 1, p1), p1), p1)
	main.Jump(body, head, p1)

	b := done
	c := func(v int64) *ir.Instr { return main.Const(b, v, p1) }
	s := main.Call(b, sum, []*ir.Instr{main.Load(b, list, p1)}, p1)
	main.Println(b, []*ir.Instr{main.Extract(b, s, 0, p1), main.Extract(b, s, 1, p1)}, p1)

	// Objects of 1, 3, 13, and 8 bytes, each filled with its own byte: an
	// overlap would show in the sums.
	var objs []*ir.Instr
	for j, size := range []int64{1, 3, 13, 8} {
		o := main.Alloc(b, c(size), p1)
		for off := int64(0); off < size; off++ {
			main.StM(b, o, c(off), main.TypedConst(b, ir.U8, int64(16*(j+1)), p1), p1)
		}
		objs = append(objs, o)
	}
	var loads []*ir.Instr
	for j, size := range []int64{1, 3, 13, 8} {
		loads = append(loads, main.LdM(b, ir.U8, objs[j], c(0), p1), main.LdM(b, ir.U8, objs[j], c(size-1), p1))
	}
	main.Println(b, loads, p1)

	// Interior pointers at a run-time offset, and comparisons.
	o := main.Alloc(b, c(64), p1)
	at := main.PAdd(b, o, main.Binary(b, ir.Mul, main.Load(b, i, p1), c(0), p1), p1) // o + 0, computed
	at40 := main.PAdd(b, at, c(40), p1)
	main.StM(b, at40, c(-8), main.TypedConst(b, ir.I32, -5, p1), p1)
	z1, z2 := main.Alloc(b, c(0), p1), main.Alloc(b, c(0), p1)
	main.Println(b, []*ir.Instr{
		main.LdM(b, ir.I32, o, c(32), p1),
		main.LdM(b, ir.I64, o, c(56), p1),
		main.Compare(b, ir.Eq, at, o, p1),
		main.Compare(b, ir.Ne, at40, o, p1),
		main.Compare(b, ir.Eq, main.PAdd(b, at40, c(-40), p1), o, p1),
		main.Compare(b, ir.Eq, z1, z2, p1),
		main.Compare(b, ir.Eq, objs[0], objs[1], p1),
	}, p1)
	main.Ret(b, p1)
	return p
}

// TestNativeHeap runs heapProgram in the interpreter and natively with
// every backend variant, and requires the same output, worked out by hand.
func TestNativeHeap(t *testing.T) {
	var want bytes.Buffer
	if code, err := interp.Run(heapProgram(), &want); err != nil || code != 0 {
		t.Fatalf("interpreter: exit %d, %v", code, err)
	}
	// The values 3i for i < 10000 sum to 3 * 9999 * 10000 / 2.
	if w := "149985000 10000\n16 16 32 32 48 48 64 64\n-5 0 true true true true false\n"; want.String() != w {
		t.Fatalf("interpreter output %q, want %q", want.String(), w)
	}
	for _, v := range memVariants {
		prog := heapProgram()
		if v.promote {
			for _, f := range prog.Funcs {
				if err := ir.Promote(f); err != nil {
					t.Fatal(err)
				}
			}
		}
		asm, err := EmitWith(prog, v.opts)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		if got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || got != want.String() {
			t.Errorf("%s: exit %d, output %q, want %q", v.name, code, got, want.String())
		}
	}
}

// TestNativeHeapLimits checks the nil panic and the end of the heap
// natively: 64 GiB is reserved, not touched, so allocations up to it
// succeed at once and the next fails with gc's fatal error.
func TestNativeHeapLimits(t *testing.T) {
	const heap = 1 << 36 // interp.HeapLimit, aether.alloc's reservation
	if interp.HeapLimit != heap {
		t.Fatalf("interp.HeapLimit %d, want %d", interp.HeapLimit, heap)
	}
	for _, tt := range []struct {
		name  string
		sizes []int64
		want  string
		code  int
	}{
		{"fits exactly", []int64{heap/2 - 3, heap / 2}, "1\n2\n", 0},
		{"8 bytes over", []int64{heap / 2, heap/2 + 1}, "1\nfatal error: runtime: out of memory\n", 2},
		{"near 2^64", []int64{-1}, "fatal error: runtime: out of memory\n", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &ir.Program{}
			f := p.NewFunc("main", "p.go", p1)
			b := f.Entry()
			for k, n := range tt.sizes {
				o := f.Alloc(b, f.Const(b, n, p1), p1)
				f.StM(b, o, f.Const(b, 0, p1), f.TypedConst(b, ir.U8, 1, p1), p1) // the first page only
				f.Println(b, []*ir.Instr{f.Const(b, int64(k+1), p1)}, p1)
			}
			f.Ret(b, p1)
			asm, err := Emit(p)
			if err != nil {
				t.Fatal(err)
			}
			if got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != tt.code || got != tt.want {
				t.Errorf("exit %d, %q; want exit %d, %q", code, got, tt.code, tt.want)
			}
		})
	}
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	f.Print(b, []*ir.Instr{f.Const(b, 7, p1)}, p1)
	f.Panic(b, ir.PanicNil, p1)
	asm, err := Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "7panic: runtime error: invalid memory address or nil pointer dereference\n"
	if got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 2 || got != want {
		t.Errorf("nil panic: exit %d, %q", code, got)
	}
	if !strings.HasPrefix(ir.PanicMessages[ir.PanicNil], "invalid memory address") {
		t.Error("PanicNil's message")
	}
}
