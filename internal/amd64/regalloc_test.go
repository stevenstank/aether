package amd64

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// raFuncs builds SSA functions for the allocator tests, by name. Each
// returns its program, so that Verify can check it, and the function.
var raFuncs = map[string]func() (*ir.Program, *ir.Func){
	// p0 + ... + p5 with every parameter live until its add.
	"straight": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(6)
		b := f.Entry()
		var ps []*ir.Instr
		for i := range 6 {
			ps = append(ps, f.Param(b, i, p1))
		}
		s := ps[0]
		for _, x := range ps[1:] {
			s = f.Binary(b, ir.Add, s, x, p1)
		}
		f.Ret(b, p1, s)
		return p, f
	},
	// for i := 0; i < p0; i++ { t += i }; return t
	"loop": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(1)
		b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		n := f.Param(b0, 0, p1)
		zero, one := f.Const(b0, 0, p1), f.Const(b0, 1, p1)
		f.Jump(b0, b1, p1)
		t, i := f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1)
		f.Branch(b1, f.Compare(b1, ir.Lt, i, n, p1), b2, b3, p1)
		t2 := f.Binary(b2, ir.Add, t, i, p1)
		i2 := f.Binary(b2, ir.Add, i, one, p1)
		f.Jump(b2, b1, p1)
		f.Ret(b3, p1, t)
		t.AddIncoming(ir.Edge{From: b0}, zero)
		t.AddIncoming(ir.Edge{From: b2}, t2)
		i.AddIncoming(ir.Edge{From: b0}, zero)
		i.AddIncoming(ir.Edge{From: b2}, i2)
		return p, f
	},
	// a, b, c = b, a, c+a around a loop: φs that read each other.
	"swap": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(1)
		b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		n := f.Param(b0, 0, p1)
		one, two := f.Const(b0, 1, p1), f.Const(b0, 2, p1)
		f.Jump(b0, b1, p1)
		a, b, c := f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1)
		f.Branch(b1, f.Compare(b1, ir.Lt, c, n, p1), b2, b3, p1)
		c2 := f.Binary(b2, ir.Add, c, a, p1)
		f.Jump(b2, b1, p1)
		f.Ret(b3, p1, f.Binary(b3, ir.Sub, a, b, p1))
		for _, x := range []struct{ phi, init, back *ir.Instr }{{a, one, b}, {b, two, a}, {c, one, c2}} {
			x.phi.AddIncoming(ir.Edge{From: b0}, x.init)
			x.phi.AddIncoming(ir.Edge{From: b2}, x.back)
		}
		return p, f
	},
	// x := p0*p0; y := g(p0); println(x, y); return x + p0 - y
	"call": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(1)
		g := p.NewFunc("g", "p.go", p1)
		g.Params, g.Results, g.SSA = []ir.Type{ir.I64}, []ir.Type{ir.I64}, true
		g.Ret(g.Entry(), p1, g.Param(g.Entry(), 0, p1))
		b := f.Entry()
		x := f.Param(b, 0, p1)
		sq := f.Binary(b, ir.Mul, x, x, p1)
		y := f.Call(b, g, []*ir.Instr{x}, p1)
		f.Println(b, []*ir.Instr{sq, y}, p1)
		f.Ret(b, p1, f.Binary(b, ir.Sub, f.Binary(b, ir.Add, sq, x, p1), y, p1))
		return p, f
	},
	// if p0 < 0 { r = -p0 } else { r = p0 * 2 }; return r + p0
	"diamond": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(1)
		b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		x := f.Param(b0, 0, p1)
		f.Branch(b0, f.Compare(b0, ir.Lt, x, f.Const(b0, 0, p1), p1), b1, b2, p1)
		neg := f.Neg(b1, x, p1)
		f.Jump(b1, b3, p1)
		dbl := f.Binary(b2, ir.Mul, x, f.Const(b2, 2, p1), p1)
		f.Jump(b2, b3, p1)
		r := f.Phi(b3, ir.I64, p1)
		r.AddIncoming(ir.Edge{From: b1}, neg)
		r.AddIncoming(ir.Edge{From: b2}, dbl)
		f.Ret(b3, p1, f.Binary(b3, ir.Add, r, x, p1))
		return p, f
	},
	// return g(p1, -p0, p0+1): values computed for a call's arguments.
	"args": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(2)
		g := p.NewFunc("g", "p.go", p1)
		g.Params, g.Results, g.SSA = []ir.Type{ir.I64, ir.I64, ir.I64}, []ir.Type{ir.I64}, true
		gb := g.Entry()
		u, v, w := g.Param(gb, 0, p1), g.Param(gb, 1, p1), g.Param(gb, 2, p1)
		g.Ret(gb, p1, g.Binary(gb, ir.Sub, g.Binary(gb, ir.Mul, u, v, p1), w, p1))
		b := f.Entry()
		x, y := f.Param(b, 0, p1), f.Param(b, 1, p1)
		neg := f.Neg(b, x, p1)
		inc := f.Binary(b, ir.Add, x, f.Const(b, 1, p1), p1)
		f.Ret(b, p1, f.Call(b, g, []*ir.Instr{y, neg, inc}, p1))
		return p, f
	},
	// for i := 0; i < p0; { j := i + 1; t += i * j; i = j }: i is live
	// after j's definition, so j cannot take i's register.
	"busy": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(1)
		b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		n := f.Param(b0, 0, p1)
		zero, one := f.Const(b0, 0, p1), f.Const(b0, 1, p1)
		f.Jump(b0, b1, p1)
		t, i := f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1)
		f.Branch(b1, f.Compare(b1, ir.Lt, i, n, p1), b2, b3, p1)
		j := f.Binary(b2, ir.Add, i, one, p1)
		t2 := f.Binary(b2, ir.Add, t, f.Binary(b2, ir.Mul, i, j, p1), p1)
		f.Jump(b2, b1, p1)
		f.Ret(b3, p1, t)
		t.AddIncoming(ir.Edge{From: b0}, zero)
		t.AddIncoming(ir.Edge{From: b2}, t2)
		i.AddIncoming(ir.Edge{From: b0}, zero)
		i.AddIncoming(ir.Edge{From: b2}, j)
		return p, f
	},
	// a, b := 0, 1; for k := 0; k < p0; k++ { a, b = b, a+b }; return a
	"fib": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(1)
		b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		n := f.Param(b0, 0, p1)
		zero, one := f.Const(b0, 0, p1), f.Const(b0, 1, p1)
		f.Jump(b0, b1, p1)
		a, b, k := f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1)
		f.Branch(b1, f.Compare(b1, ir.Lt, k, n, p1), b2, b3, p1)
		s := f.Binary(b2, ir.Add, a, b, p1)
		k2 := f.Binary(b2, ir.Add, k, f.Const(b2, 1, p1), p1)
		f.Jump(b2, b1, p1)
		f.Ret(b3, p1, a)
		for _, x := range []struct{ phi, init, back *ir.Instr }{{a, zero, b}, {b, one, s}, {k, zero, k2}} {
			x.phi.AddIncoming(ir.Edge{From: b0}, x.init)
			x.phi.AddIncoming(ir.Edge{From: b2}, x.back)
		}
		return p, f
	},
	// x := p0; for k := 0; k < 8; k++ { if x&1 != 0 { x = x>>1 ^ 5 } else
	// { x >>= 1 } }; return x: a loop around a diamond, as in a CRC.
	"shift": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(1)
		b0, b1, b2, b3, b4, b5, b6 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		x0 := f.Param(b0, 0, p1)
		zero := f.Const(b0, 0, p1)
		f.Jump(b0, b1, p1)
		x, k := f.Phi(b1, ir.I64, p1), f.Phi(b1, ir.I64, p1)
		f.Branch(b1, f.Compare(b1, ir.Lt, k, f.Const(b1, 8, p1), p1), b2, b6, p1)
		low := f.Binary(b2, ir.And, x, f.Const(b2, 1, p1), p1)
		f.Branch(b2, f.Compare(b2, ir.Ne, low, f.Const(b2, 0, p1), p1), b3, b4, p1)
		odd := f.Binary(b3, ir.Xor, f.Shift(b3, ir.Shr, x, f.TypedConst(b3, ir.U64, 1, p1), p1), f.Const(b3, 5, p1), p1)
		f.Jump(b3, b5, p1)
		even := f.Shift(b4, ir.Shr, x, f.TypedConst(b4, ir.U64, 1, p1), p1)
		f.Jump(b4, b5, p1)
		x2 := f.Phi(b5, ir.I64, p1)
		x2.AddIncoming(ir.Edge{From: b3}, odd)
		x2.AddIncoming(ir.Edge{From: b4}, even)
		k2 := f.Binary(b5, ir.Add, k, f.Const(b5, 1, p1), p1)
		f.Jump(b5, b1, p1)
		f.Ret(b6, p1, x)
		x.AddIncoming(ir.Edge{From: b0}, x0)
		x.AddIncoming(ir.Edge{From: b5}, x2)
		k.AddIncoming(ir.Edge{From: b0}, zero)
		k.AddIncoming(ir.Edge{From: b5}, k2)
		return p, f
	},
	// The sum of 15 parameters, added last to first, so all 15 are live
	// at the first add: more than the 11 registers.
	"spill": func() (*ir.Program, *ir.Func) {
		p, f := raFunc(15)
		b := f.Entry()
		var ps []*ir.Instr
		for i := range 15 {
			ps = append(ps, f.Param(b, i, p1))
		}
		// p0 is used three times, so it is not among those spilled.
		s := f.Binary(b, ir.Add, ps[0], ps[0], p1)
		for i := 14; i >= 1; i-- {
			s = f.Binary(b, ir.Add, s, ps[i], p1)
		}
		f.Ret(b, p1, f.Binary(b, ir.Add, s, ps[0], p1))
		return p, f
	},
}

// raArgs are the arguments main calls each test function with, one call
// per list.
var raArgs = map[string][][]int64{
	"straight": {{1, 2, 3, 4, 5, 6}, {-1, 1 << 62, 1 << 62, 1, 0, 9}},
	"loop":     {{0}, {1}, {10}},
	"swap":     {{0}, {1}, {7}},
	"call":     {{3}, {-5}},
	"diamond":  {{-4}, {0}, {9}},
	"args":     {{3, 4}, {-1 << 63, 7}},
	"busy":     {{0}, {1}, {9}},
	"fib":      {{0}, {1}, {50}},
	"shift":    {{0}, {1}, {-12345}},
	"spill":    {{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, {-7, 0, 1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 1 << 40}},
}

// raFunc returns a program with an SSA function f of n int64 parameters
// and one int64 result. Its main is empty until raMain completes it.
func raFunc(n int) (*ir.Program, *ir.Func) {
	p := &ir.Program{}
	p.NewFunc("main", "p.go", p1)
	f := p.NewFunc("f", "p.go", p1)
	f.SSA = true
	for range n {
		f.Params = append(f.Params, ir.I64)
	}
	f.Results = []ir.Type{ir.I64}
	return p, f
}

// raMain completes main: it prints f's result for each argument list of
// the named function, one line each.
func raMain(p *ir.Program, f *ir.Func, name string) {
	m := p.Funcs[0]
	b := m.Entry()
	for _, args := range raArgs[name] {
		var vs []*ir.Instr
		for _, a := range args {
			vs = append(vs, m.Const(b, a, p1))
		}
		m.Println(b, []*ir.Instr{m.Call(b, f, vs, p1)}, p1)
	}
	m.Ret(b, p1)
}

// raBuild builds the named test program and verifies it.
func raBuild(t *testing.T, name string) (*ir.Program, *ir.Func) {
	t.Helper()
	p, f := raFuncs[name]()
	raMain(p, f, name)
	if errs := ir.Verify(p); len(errs) > 0 {
		t.Fatalf("%s: Verify: %v\n%s", name, errs, p)
	}
	return p, f
}

// raAllocate builds the named function, allocates it, and checks the
// allocation.
func raAllocate(t *testing.T, name string) (*ir.Func, map[*ir.Instr]int) {
	t.Helper()
	_, f := raBuild(t, name)
	regs, err := allocate(f)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := checkAllocation(f, regs); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f, regs
}

func TestAllocate(t *testing.T) {
	for _, c := range []struct {
		name  string
		cells []string // values that must keep their cells
		regs  int      // values with a register
	}{
		{"straight", nil, 11},                   // 6 parameters and 5 sums
		{"loop", nil, 8},                        // all
		{"swap", nil, 9},                        // all
		{"call", []string{"v0", "v1", "v2"}, 2}, // p0, x, and y are live across calls
		{"diamond", nil, 8},
		{"spill", []string{"v1", "v2", "v3", "v4"}, 26}, // the four fewest-used, first defined
	} {
		f, regs := raAllocate(t, c.name)
		if len(regs) != c.regs {
			t.Errorf("%s: %d values have registers, want %d:\n%s", c.name, len(regs), c.regs, describe(f, regs))
		}
		for _, v := range c.cells {
			for x := range regs {
				if fmt.Sprintf("v%d", x.ID) == v {
					t.Errorf("%s: %s has register %s, want its cell:\n%s", c.name, v, allocRegs[regs[x]], describe(f, regs))
				}
			}
		}
	}
}

// TestAllocateDeterministic checks that allocation depends only on the
// function.
func TestAllocateDeterministic(t *testing.T) {
	for name := range raFuncs {
		f, want := raAllocate(t, name)
		for range 5 {
			got, err := allocate(f)
			if err != nil {
				t.Fatal(err)
			}
			if describe(f, got) != describe(f, want) {
				t.Fatalf("%s: allocation differs between runs:\n%s\n%s", name, describe(f, got), describe(f, want))
			}
		}
	}
}

func TestAllocateNotSSA(t *testing.T) {
	_, f := raBuild(t, "straight")
	f.SSA = false
	if regs, err := allocate(f); err != nil || len(regs) != 0 {
		t.Errorf("ordinary function allocated: %v %v", regs, err)
	}
}

// describe prints each value's location, in value order.
func describe(f *ir.Func, regs map[*ir.Instr]int) string {
	var b strings.Builder
	for _, blk := range f.Blocks {
		for _, in := range blk.Instrs {
			if in.ID < 0 {
				continue
			}
			loc := "cell"
			if r, ok := regs[in]; ok {
				loc = allocRegs[r]
			}
			fmt.Fprintf(&b, "%s: %s\n", in, loc)
		}
	}
	return b.String()
}

// TestCheckAllocationRejects plants bugs the checker must find.
func TestCheckAllocationRejects(t *testing.T) {
	value := func(f *ir.Func, id int) *ir.Instr {
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if in.ID == id {
					return in
				}
			}
		}
		t.Fatalf("no v%d", id)
		return nil
	}
	for _, c := range []struct {
		name, fn, want string
		plant          func(f *ir.Func, regs map[*ir.Instr]int)
	}{
		{"interfering values share a register", "straight", "reads v",
			func(f *ir.Func, regs map[*ir.Instr]int) { regs[value(f, 1)] = regs[value(f, 0)] }},
		{"value in a register across a call", "call", "which holds no known value",
			func(f *ir.Func, regs map[*ir.Instr]int) { regs[value(f, 0)] = 10 }},
		{"println operand in a register", "call", "has operand",
			func(f *ir.Func, regs map[*ir.Instr]int) { regs[value(f, 1)] = 10 }},
		{"φ shares a register with a value live into its block", "loop", "",
			func(f *ir.Func, regs map[*ir.Instr]int) {
				regs[f.Blocks[1].Instrs[0]] = regs[value(f, 0)]
			}},
		{"register out of range", "loop", "no register",
			func(f *ir.Func, regs map[*ir.Instr]int) { regs[value(f, 0)] = len(allocRegs) }},
		{"value of another function", "loop", "not in a reachable block",
			func(f *ir.Func, regs map[*ir.Instr]int) { regs[&ir.Instr{Op: ir.Const, ID: 3}] = 0 }},
		{"instruction without a value", "loop", "cannot have a register",
			func(f *ir.Func, regs map[*ir.Instr]int) {
				b := f.Blocks[0]
				regs[b.Instrs[len(b.Instrs)-1]] = 0
			}},
	} {
		f, regs := raAllocate(t, c.fn)
		c.plant(f, regs)
		err := checkAllocation(f, regs)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: checkAllocation = %v, want an error containing %q:\n%s", c.name, err, c.want, describe(f, regs))
		}
	}
}

// TestCheckAllocationSweep moves every value with a register to every
// other register, in every test function. The checker, which tracks
// register contents forward, must reject the mutant exactly when the
// value interferes with another value in that register, by liveness: one
// is live after the other's definition (for φs, after all of their
// block's φs).
func TestCheckAllocationSweep(t *testing.T) {
	accepted, rejected := 0, 0
	for name := range raFuncs {
		f, regs := raAllocate(t, name)
		liveOut := liveness(f)
		def := map[*ir.Instr]valueSet{} // the values live after each definition
		for _, b := range f.Blocks {
			after := liveAfter(b, liveOut[b.ID])
			phis := 0
			for phis < len(b.Instrs) && b.Instrs[phis].Op == ir.Phi {
				phis++
			}
			for j, in := range b.Instrs {
				if in.Op == ir.Phi {
					def[in] = after[phis-1]
				} else {
					def[in] = after[j]
				}
			}
		}
		for v, r0 := range regs {
			for r := range allocRegs {
				if r == r0 {
					continue
				}
				interferes := false
				for w, rw := range regs {
					if w != v && rw == r && (def[v][w] || def[w][v]) {
						interferes = true
					}
				}
				regs[v] = r
				err := checkAllocation(f, regs)
				regs[v] = r0
				if (err != nil) != interferes {
					t.Errorf("%s: v%d moved to %s: checker %v, interference %v", name, v.ID, allocRegs[r], err, interferes)
				}
				if err != nil {
					rejected++
				} else {
					accepted++
				}
			}
		}
	}
	if accepted == 0 || rejected == 0 {
		t.Errorf("%d mutants accepted, %d rejected: the sweep must see both", accepted, rejected)
	}
	t.Logf("%d mutants accepted and %d rejected, all as interference predicts", accepted, rejected)
}

// TestAllocateLimit checks that a function over maxAllocValues gets no
// registers, and one at the limit does.
func TestAllocateLimit(t *testing.T) {
	build := func(n int) *ir.Func {
		_, f := raFunc(0)
		b := f.Entry()
		s := f.Const(b, 1, p1)
		for range n - 1 {
			s = f.Neg(b, s, p1)
		}
		f.Ret(b, p1, s)
		return f
	}
	if regs, err := allocate(build(maxAllocValues)); err != nil || len(regs) != maxAllocValues {
		t.Errorf("at the limit: %d registers, %v; want %d", len(regs), err, maxAllocValues)
	}
	if regs, err := allocate(build(maxAllocValues + 1)); err != nil || len(regs) != 0 {
		t.Errorf("over the limit: %d registers, %v; want none", len(regs), err)
	}
}
