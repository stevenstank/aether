package amd64

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/ir"
)

// The allocator's adversarial functions, built as lowering would build them
// and promoted, each near maxAllocValues at its largest size.

// nestedLoops is n nested loops, for k := 0; k < 1; k++, around x = x*3 + 1:
// every loop's counter is live across all of the loops inside it.
func nestedLoops(n int) *ir.Func {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	x := f.NewSlot("x", ir.I64, p1)
	b := f.Entry()
	f.Store(b, x, f.Const(b, 1, p1), p1)
	var latches []*ir.Block
	var heads []*ir.Block
	for k := range n {
		i := f.NewSlot(fmt.Sprintf("i%d", k), ir.I64, p1)
		f.Store(b, i, f.Const(b, 0, p1), p1)
		head, body, latch := f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Jump(b, head, p1)
		heads = append(heads, head)
		latches = append(latches, latch)
		// The head's false edge goes to the enclosing loop's latch, added
		// below; the latch increments and jumps back to the head.
		f.Store(latch, i, f.Binary(latch, ir.Add, f.Load(latch, i, p1), f.Const(latch, 1, p1), p1), p1)
		f.Jump(latch, head, p1)
		b = body
	}
	f.Store(b, x, f.Binary(b, ir.Add, f.Binary(b, ir.Mul, f.Load(b, x, p1), f.Const(b, 3, p1), p1), f.Const(b, 1, p1), p1), p1)
	f.Jump(b, latches[n-1], p1)
	exit := f.NewBlock()
	f.Println(exit, []*ir.Instr{f.Load(exit, x, p1)}, p1)
	f.Ret(exit, p1)
	for k, head := range heads {
		out := exit
		if k > 0 {
			out = latches[k-1]
		}
		body := f.Blocks[head.ID+1]
		i := f.Slots[k+1]
		f.Branch(head, f.Compare(head, ir.Lt, f.Load(head, i, p1), f.Const(head, 1, p1), p1), body, out, p1)
	}
	return promoted(p, f)
}

// wideValues is m values computed first and all printed after n diamonds
// that each update y: every value is live across every block.
func wideValues(m, n int) *ir.Func {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	g := p.NewGlobal("g", ir.I64, "p.go", p1)
	y := f.NewSlot("y", ir.I64, p1)
	b := f.Entry()
	var vs []*ir.Instr
	for range m {
		vs = append(vs, f.GLoad(b, g, p1))
	}
	f.Store(b, y, f.Const(b, 0, p1), p1)
	for range n {
		t, e, join := f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Branch(b, f.Compare(b, ir.Lt, f.Load(b, y, p1), vs[0], p1), t, e, p1)
		f.Store(t, y, f.Binary(t, ir.Add, f.Load(t, y, p1), vs[0], p1), p1)
		f.Jump(t, join, p1)
		f.Store(e, y, f.Binary(e, ir.Sub, f.Load(e, y, p1), vs[0], p1), p1)
		f.Jump(e, join, p1)
		b = join
	}
	f.Println(b, append(vs, f.Load(b, y, p1)), p1)
	f.Ret(b, p1)
	return promoted(p, f)
}

func promoted(p *ir.Program, f *ir.Func) *ir.Func {
	if errs := ir.Verify(p); len(errs) > 0 {
		panic(fmt.Sprint(errs[0], "\n", p))
	}
	if err := ir.Promote(f); err != nil {
		panic(err)
	}
	return f
}

// allocCost allocates f with coalescing, the best of runs, and returns the
// time and the bytes allocated, with the number of values and blocks.
func allocCost(t *testing.T, f *ir.Func, runs int) (time.Duration, uint64) {
	t.Helper()
	best, bytes := time.Hour, uint64(0)
	var m0, m1 runtime.MemStats
	for range runs {
		runtime.GC()
		runtime.ReadMemStats(&m0)
		start := time.Now()
		regs, err := allocateExcept(f, nil, true)
		d := time.Since(start)
		runtime.ReadMemStats(&m1)
		if err != nil {
			t.Fatal(err)
		}
		if len(regs) == 0 {
			t.Fatalf("func with %d values got no registers", countValues(f))
		}
		best, bytes = min(best, d), m1.TotalAlloc-m0.TotalAlloc
	}
	return best, bytes
}

// TestAllocateScales allocates the adversarial functions at sizes up to
// maxAllocValues and logs the time and bytes allocated. The bytes must stay
// under 64 MiB at every size.
func TestAllocateScales(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates large functions")
	}
	for _, c := range []struct {
		name string
		f    func(n int) *ir.Func
		ns   []int
	}{
		{"loops", nestedLoops, []int{32, 64, 128, 256, 320}},
		{"wide", func(n int) *ir.Func { return wideValues(n, n) }, []int{64, 128, 256, 512, 600}},
	} {
		for _, n := range c.ns {
			f := c.f(n)
			if countValues(f) > maxAllocValues {
				t.Fatalf("%s %d: %d values, over maxAllocValues", c.name, n, countValues(f))
			}
			d, bytes := allocCost(t, f, 3)
			t.Logf("%s n=%d: %d values, %d blocks: %v, %.1f MiB", c.name, n, countValues(f), len(f.Blocks), d, float64(bytes)/(1<<20))
			if bytes > 64<<20 {
				t.Errorf("%s n=%d: allocation used %.1f MiB, want under 64", c.name, n, float64(bytes)/(1<<20))
			}
		}
	}
}
