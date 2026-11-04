package ir

import (
	"slices"
	"strings"
	"testing"
)

// ssaProg returns a program whose main, in ordinary form, calls f(i64)
// i64, in SSA form, which build fills from an empty entry block. A program
// may mix the two forms (D-025).
func ssaProg(build func(f *Func, p *Instr)) *Program {
	prog := &Program{}
	m := prog.NewFunc("main", "p.go", Pos{1, 6})
	f := prog.NewFunc("f", "p.go", Pos{5, 6})
	f.Params, f.Results, f.SSA = []Type{I64}, []Type{I64}, true
	b := m.Entry()
	m.Println(b, []*Instr{m.Call(b, f, []*Instr{m.Const(b, 7, Pos{2, 4})}, Pos{2, 3})}, Pos{2, 2})
	m.Ret(b, Pos{3, 1})
	build(f, f.Param(f.Entry(), 0, Pos{5, 8}))
	return prog
}

// at is a position for the instructions of the SSA fixtures.
var at = Pos{6, 2}

// The SSA fixtures, all valid. Each takes the parameter p and builds f.
var ssaFixtures = []struct {
	name  string
	build func(f *Func, p *Instr)
}{
	// if p < 0 { r = -p } else { r = p + 1 }; return r + p
	{"diamond", func(f *Func, p *Instr) {
		b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Branch(b0, f.Compare(b0, Lt, p, f.Const(b0, 0, at), at), b1, b2, at)
		neg := f.Neg(b1, p, at)
		f.Jump(b1, b3, at)
		inc := f.Binary(b2, Add, p, f.Const(b2, 1, at), at)
		f.Jump(b2, b3, at)
		r := f.Phi(b3, I64, at)
		r.AddIncoming(Edge{b1, 0}, neg)
		r.AddIncoming(Edge{b2, 0}, inc)
		f.Ret(b3, at, f.Binary(b3, Add, r, p, at))
	}},
	// r := 1; if p < 0 { r = p }; return r. The edge b0.1 is critical: b0
	// has two successors and b2 two predecessors.
	{"critical edge", func(f *Func, p *Instr) {
		b0, b1, b2 := f.Entry(), f.NewBlock(), f.NewBlock()
		one := f.Const(b0, 1, at)
		f.Branch(b0, f.Compare(b0, Lt, p, f.Const(b0, 0, at), at), b1, b2, at)
		f.Jump(b1, b2, at)
		r := f.Phi(b2, I64, at)
		r.AddIncoming(Edge{b0, 1}, one)
		r.AddIncoming(Edge{b1, 0}, p)
		f.Ret(b2, at, r)
	}},
	// i := 0; for i < p { i++ }; return i
	{"loop", func(f *Func, p *Instr) {
		b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		zero := f.Const(b0, 0, at)
		f.Jump(b0, b1, at)
		i := f.Phi(b1, I64, at)
		f.Branch(b1, f.Compare(b1, Lt, i, p, at), b2, b3, at)
		inc := f.Binary(b2, Add, i, f.Const(b2, 1, at), at)
		f.Jump(b2, b1, at)
		i.AddIncoming(Edge{b0, 0}, zero)
		i.AddIncoming(Edge{b2, 0}, inc)
		f.Ret(b3, at, i)
	}},
	// s := 0
	// for i := 0; i < p; i++ {
	//	for j := 0; j < i; {
	//		j++
	//		if j&1 == 1 { continue }
	//		s += j
	//	}
	// }
	// return s
	{"nested loops with continue", func(f *Func, p *Instr) {
		b0 := f.Entry()
		b1, b2, b3, b4, b5, b6 := f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		zero := f.Const(b0, 0, at)
		f.Jump(b0, b1, at)
		i, s := f.Phi(b1, I64, at), f.Phi(b1, I64, at)
		f.Branch(b1, f.Compare(b1, Lt, i, p, at), b2, b6, at)
		j, s1 := f.Phi(b2, I64, at), f.Phi(b2, I64, at)
		f.Branch(b2, f.Compare(b2, Lt, j, i, at), b3, b5, at)
		one := f.Const(b3, 1, at)
		j1 := f.Binary(b3, Add, j, one, at)
		f.Branch(b3, f.Compare(b3, Eq, f.Binary(b3, And, j1, one, at), one, at), b2, b4, at)
		s2 := f.Binary(b4, Add, s1, j1, at)
		f.Jump(b4, b2, at)
		i1 := f.Binary(b5, Add, i, f.Const(b5, 1, at), at)
		f.Jump(b5, b1, at)
		f.Ret(b6, at, s)
		i.AddIncoming(Edge{b0, 0}, zero)
		i.AddIncoming(Edge{b5, 0}, i1)
		s.AddIncoming(Edge{b0, 0}, zero)
		s.AddIncoming(Edge{b5, 0}, s1)
		j.AddIncoming(Edge{b1, 0}, zero)
		j.AddIncoming(Edge{b3, 0}, j1)
		j.AddIncoming(Edge{b4, 0}, j1)
		s1.AddIncoming(Edge{b1, 0}, s)
		s1.AddIncoming(Edge{b3, 0}, s1)
		s1.AddIncoming(Edge{b4, 0}, s2)
	}},
	// switch { case p < 0: r, ok = 10, true; case p < 9: r, ok = 20, true;
	// default: r, ok = p, false }; if ok { return r + p }; return r. The
	// join b5 has three predecessors, and the bool phi feeds a branch.
	{"multiple predecessors", func(f *Func, p *Instr) {
		b0 := f.Entry()
		b1, b2, b3, b4, b5, b6 := f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Branch(b0, f.Compare(b0, Lt, p, f.Const(b0, 0, at), at), b2, b1, at)
		f.Branch(b1, f.Compare(b1, Lt, p, f.Const(b1, 9, at), at), b3, b4, at)
		ten, t1 := f.Const(b2, 10, at), f.BoolConst(b2, true, at)
		f.Jump(b2, b5, at)
		twenty, t2 := f.Const(b3, 20, at), f.BoolConst(b3, true, at)
		f.Jump(b3, b5, at)
		no := f.BoolConst(b4, false, at)
		f.Jump(b4, b5, at)
		r, ok := f.Phi(b5, I64, at), f.Phi(b5, Bool, at)
		r.AddIncoming(Edge{b2, 0}, ten)
		r.AddIncoming(Edge{b3, 0}, twenty)
		r.AddIncoming(Edge{b4, 0}, p)
		ok.AddIncoming(Edge{b2, 0}, t1)
		ok.AddIncoming(Edge{b3, 0}, t2)
		ok.AddIncoming(Edge{b4, 0}, no)
		f.Branch(b5, ok, b6, b6, at) // two edges to b6
		f.Ret(b6, at, f.Binary(b6, Add, r, p, at))
	}},
	// A branch whose two targets are the same block: the phi's value
	// depends on which edge was taken, not only on the predecessor.
	{"duplicate edges", func(f *Func, p *Instr) {
		b0, b1 := f.Entry(), f.NewBlock()
		one := f.Const(b0, 1, at)
		f.Branch(b0, f.Compare(b0, Lt, p, one, at), b1, b1, at)
		r := f.Phi(b1, I64, at)
		r.AddIncoming(Edge{b0, 0}, one)
		r.AddIncoming(Edge{b0, 1}, p)
		f.Ret(b1, at, r)
	}},
}

func fixture(t *testing.T, name string) (*Program, *Func) {
	t.Helper()
	for _, fx := range ssaFixtures {
		if fx.name == name {
			p := ssaProg(fx.build)
			return p, p.Funcs[1]
		}
	}
	t.Fatalf("no fixture %q", name)
	return nil, nil
}

func TestVerifySSAValid(t *testing.T) {
	for _, fx := range ssaFixtures {
		t.Run(fx.name, func(t *testing.T) {
			p := ssaProg(fx.build)
			if errs := Verify(p); len(errs) != 0 {
				t.Fatalf("valid SSA rejected: %v\n%s", errs, p)
			}
			// The same function in ordinary form is rejected: it has phis,
			// and except in "duplicate edges", values used in other blocks.
			p.Funcs[1].SSA = false
			if errs := Verify(p); len(errs) == 0 {
				t.Error("SSA function accepted in ordinary form")
			}
		})
	}
}

func TestPrintSSA(t *testing.T) {
	_, f := fixture(t, "multiple predecessors")
	want := `func f(i64) i64 ssa @p.go:5:6
b0:
  v0 = param 0 @5:8
  v1 = const 0 @6:2
  v2 = lt v0, v1 @6:2
  branch v2, b2, b1 @6:2
b1:
  v3 = const 9 @6:2
  v4 = lt v0, v3 @6:2
  branch v4, b3, b4 @6:2
b2:
  v5 = const 10 @6:2
  v6 = const true @6:2
  jump b5 @6:2
b3:
  v7 = const 20 @6:2
  v8 = const true @6:2
  jump b5 @6:2
b4:
  v9 = const false @6:2
  jump b5 @6:2
b5:
  v10 = phi i64, b2.0: v5, b3.0: v7, b4.0: v0 @6:2
  v11 = phi bool, b2.0: v6, b3.0: v8, b4.0: v9 @6:2
  branch v11, b6, b6 @6:2
b6:
  v12 = add v10, v0 @6:2
  ret v12 @6:2
`
	var b strings.Builder
	f.write(&b)
	if got := b.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	_, f = fixture(t, "duplicate edges")
	if got := f.Blocks[1].Instrs[0].String(); got != "v3 = phi i64, b0.0: v1, b0.1: v0" {
		t.Errorf("duplicate-edge phi printed as %q", got)
	}
	// A malformed phi still prints, marking what is missing.
	phi := &Instr{Op: Phi, Type: I64, ID: 9, Args: []*Instr{nil}, Edges: []Edge{{nil, 0}, {f.Entry(), 1}}}
	if got := phi.String(); got != "v9 = phi i64, b?.0: v?, b0.1: v?" {
		t.Errorf("malformed phi printed as %q", got)
	}
}

// TestPhiBuilder checks that Phi inserts after a block's existing phis,
// even once the block has other instructions.
func TestPhiBuilder(t *testing.T) {
	_, f := fixture(t, "loop")
	b1 := f.Blocks[1]
	extra := f.Phi(b1, Bool, at)
	if b1.Instrs[1] != extra || b1.Instrs[0].Op != Phi || b1.Instrs[2].Op != Lt {
		t.Errorf("phi inserted at the wrong place:\n%v", b1.Instrs)
	}
}

// TestVerifySSARejects breaks one SSA invariant (D-025) at a time in a valid
// fixture and checks that Verify reports it.
func TestVerifySSARejects(t *testing.T) {
	instr := func(f *Func, b, i int) *Instr { return f.Blocks[b].Instrs[i] }
	tests := []struct {
		fixture string
		name    string
		break_  func(p *Program, f *Func)
		want    string
	}{
		{"diamond", "slot", func(_ *Program, f *Func) { f.NewSlot("x", I64, at) },
			"function in SSA form has 1 slots"},
		{"diamond", "store", func(_ *Program, f *Func) {
			s := f.NewSlot("x", I64, at)
			f.Slots = nil
			b := f.Blocks[1]
			b.Instrs = slices.Insert(b.Instrs, 1, &Instr{Op: Store, ID: -1, Args: []*Instr{instr(f, 1, 0)}, Slot: s, Pos: at})
		}, "store in a function in SSA form"},
		{"diamond", "load", func(_ *Program, f *Func) {
			s := f.NewSlot("x", I64, at)
			f.Slots = nil
			b := f.Blocks[1]
			b.Instrs = slices.Insert(b.Instrs, 0, &Instr{Op: Load, Type: I64, ID: 50, Slot: s, Pos: at})
		}, "load in a function in SSA form"},
		{"diamond", "phi in ordinary function", func(p *Program, _ *Func) {
			m := p.Funcs[0]
			b := m.Entry()
			b.Instrs = slices.Insert(b.Instrs, 0, &Instr{Op: Phi, Type: I64, ID: 50, Pos: at})
		}, "phi in a function not in SSA form"},
		{"loop", "edge into entry", func(_ *Program, f *Func) { instr(f, 2, 2).Targets[0] = f.Entry() },
			"edge b2.0 enters the entry block"},
		{"loop", "phi in entry", func(_ *Program, f *Func) {
			b := f.Entry()
			b.Instrs = slices.Insert(b.Instrs, 0, &Instr{Op: Phi, Type: I64, ID: 50, Pos: at})
		}, "phi in the entry block"},
		{"loop", "phi after body", func(_ *Program, f *Func) {
			b := f.Blocks[1]
			b.Instrs[0], b.Instrs[1] = b.Instrs[1], b.Instrs[0]
		}, "phi after a non-phi instruction"},
		{"diamond", "missing edge", func(_ *Program, f *Func) {
			phi := instr(f, 3, 0)
			phi.Edges, phi.Args = phi.Edges[:1], phi.Args[:1]
		}, "no value for incoming edge b2.0"},
		{"diamond", "edge from a non-predecessor", func(_ *Program, f *Func) {
			instr(f, 3, 0).Edges[1] = Edge{f.Entry(), 0}
		}, "edge b0.0 is not an incoming edge of b3"},
		{"critical edge", "edge with the wrong index", func(_ *Program, f *Func) {
			// b0 is a predecessor of b2, but along its target 1, not 0.
			instr(f, 2, 0).Edges[0].Index = 0
		}, "edge b0.0 is not an incoming edge of b2"},
		{"critical edge", "edge index out of range", func(_ *Program, f *Func) {
			instr(f, 2, 0).Edges[1].Index = 1
		}, "edge b1.1 is not an incoming edge of b2"},
		{"critical edge", "edge from a nil block", func(_ *Program, f *Func) {
			instr(f, 2, 0).Edges[1].From = nil
		}, "edge b?.0 is not an incoming edge of b2"},
		{"duplicate edges", "parallel edge given twice", func(_ *Program, f *Func) {
			// The set of predecessor blocks is still {b0}; only the edges
			// show that b0.1 has no value.
			instr(f, 1, 0).Edges[1].Index = 0
		}, "edge b0.0 has more than one value"},
		{"duplicate edges", "parallel edge missing", func(_ *Program, f *Func) {
			phi := instr(f, 1, 0)
			phi.Edges, phi.Args = phi.Edges[1:], phi.Args[1:]
		}, "no value for incoming edge b0.0"},
		{"diamond", "edges out of order", func(_ *Program, f *Func) {
			phi := instr(f, 3, 0)
			slices.Reverse(phi.Edges)
			slices.Reverse(phi.Args)
		}, "incoming edges are not in canonical order"},
		{"diamond", "more values than edges", func(_ *Program, f *Func) {
			phi := instr(f, 3, 0)
			phi.Args = append(phi.Args, phi.Args[0])
		}, "phi has 3 operands, want 2"},
		{"diamond", "value of the wrong type", func(_ *Program, f *Func) {
			instr(f, 3, 0).Args[0] = instr(f, 0, 2) // the bool p < 0
		}, "operand v2 has type bool, want i64"},
		{"diamond", "phi of invalid type", func(_ *Program, f *Func) { instr(f, 3, 0).Type = Tuple },
			"phi has invalid type tuple"},
		{"diamond", "values swapped between edges", func(_ *Program, f *Func) {
			// Each value is defined in the other arm: it is not available
			// along the edge it is given for.
			args := instr(f, 3, 0).Args
			args[0], args[1] = args[1], args[0]
		}, "value v5 for edge b1.0 is defined in b2, which does not dominate b1"},
		{"loop", "value defined after the loop", func(_ *Program, f *Func) {
			// The exit block b3 does not dominate the back edge's source.
			b3 := f.Blocks[3]
			c := f.Const(b3, 5, at)
			b3.Instrs = []*Instr{c, b3.Instrs[0]}
			instr(f, 1, 0).Args[1] = c
		}, "is defined in b3, which does not dominate b2"},
		{"loop", "phi value not an instruction of the function", func(p *Program, f *Func) {
			instr(f, 1, 0).Args[0] = instr(p.Funcs[0], 0, 0)
		}, "value v0 for edge b0.0 is not an instruction of this function"},
		{"diamond", "use not dominated", func(_ *Program, f *Func) {
			instr(f, 3, 1).Args[0] = instr(f, 1, 0) // -p, defined only on one arm
		}, "operand v3 is defined in b1, which does not dominate b3"},
		{"loop", "use in an earlier block", func(_ *Program, f *Func) {
			instr(f, 1, 1).Args[1] = instr(f, 2, 1) // i < i+1, with i+1 defined in the body
		}, "operand v5 is defined in b2, which does not dominate b1"},
		{"diamond", "use before definition in its block", func(_ *Program, f *Func) {
			instr(f, 3, 1).Args[0] = instr(f, 3, 1)
		}, "operand v7 is not defined before its use in this block"},
		{"diamond", "operand from another function", func(p *Program, f *Func) {
			instr(f, 1, 0).Args[0] = instr(p.Funcs[0], 0, 0)
		}, "operand v0 is not an instruction of this function"},
		{"diamond", "instruction in two blocks", func(_ *Program, f *Func) {
			b := f.Blocks[2]
			b.Instrs = slices.Insert(b.Instrs, 0, instr(f, 0, 1))
		}, "instruction appears more than once in the function"},
		{"diamond", "value number in two blocks", func(_ *Program, f *Func) { instr(f, 2, 0).ID = instr(f, 1, 0).ID },
			"value number v3 defined twice"},
		{"diamond", "edges on a non-phi", func(_ *Program, f *Func) { instr(f, 1, 0).Edges = []Edge{{f.Entry(), 0}} },
			"neg has incoming edges"},
		{"diamond", "tuple used outside its block", func(p *Program, f *Func) {
			g := p.NewFunc("g", "p.go", Pos{9, 6})
			g.Results = []Type{I64, I64}
			gb := g.Entry()
			g.Ret(gb, at, g.Const(gb, 1, at), g.Const(gb, 2, at))
			call := &Instr{Op: Call, Type: Tuple, ID: 60, Callee: g, Pos: at}
			b0, b1 := f.Entry(), f.Blocks[1]
			b0.Instrs = slices.Insert(b0.Instrs, 1, call)
			b1.Instrs = slices.Insert(b1.Instrs, 0, &Instr{Op: Extract, Type: I64, ID: 61, Args: []*Instr{call}, Pos: at})
		}, "tuple v60 used outside the block of its call"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, f := fixture(t, tt.fixture)
			tt.break_(p, f)
			errs := Verify(p)
			for _, e := range errs {
				if strings.Contains(e.Error(), tt.want) {
					return
				}
			}
			t.Errorf("no error containing %q; got %v\n%s", tt.want, errs, p)
		})
	}
}

// TestVerifySSAMalformedCFG checks that an SSA function whose CFG cannot be
// analyzed is reported, not a crash.
func TestVerifySSAMalformedCFG(t *testing.T) {
	for _, mutate := range []func(f *Func){
		func(f *Func) { f.Blocks[0].Instrs[3].Targets[0] = nil },
		func(f *Func) { f.Blocks[0].Instrs[3].Targets[1] = &Block{ID: 1} },
		func(f *Func) { f.Blocks[0].Instrs[3] = nil },
		func(f *Func) { f.Blocks[3].Instrs[2].Targets = []*Block{f.Blocks[1]} },
		func(f *Func) { f.Blocks[1].Instrs = nil },
	} {
		p, f := fixture(t, "diamond")
		mutate(f)
		if errs := Verify(p); len(errs) == 0 {
			t.Errorf("malformed CFG accepted:\n%s", p)
		}
	}
}

// TestVerifySSAMutations mutates every operand and every phi edge of every
// fixture, one at a time, and checks Verify's verdict against an oracle
// independent of DomTree: dominance computed from its definition
// (naiveDominates). An operand replaced by another value of its type is
// valid exactly if the value is defined earlier in the same block or in a
// strictly dominating block, or, for a phi, in a block that dominates the
// edge's source. An edge replaced by any other, removed, or swapped with
// its neighbor is never valid.
func TestVerifySSAMutations(t *testing.T) {
	accepted, rejected := 0, 0
	check := func(p *Program, want bool, what string) {
		t.Helper()
		errs := Verify(p)
		if got := len(errs) == 0; got != want {
			t.Fatalf("%s: valid = %v, want %v: %v\n%s", what, got, want, errs, p)
		}
		if want {
			accepted++
		} else {
			rejected++
		}
	}
	for _, fx := range ssaFixtures {
		p := ssaProg(fx.build)
		f := p.Funcs[1]
		dom := naiveDominates(f)
		where := map[*Instr][2]int{} // block and index of each instruction
		var values []*Instr
		for _, b := range f.Blocks {
			for i, in := range b.Instrs {
				where[in] = [2]int{b.ID, i}
				if in.Type != NoType {
					values = append(values, in)
				}
			}
		}
		for _, b := range f.Blocks {
			for i, in := range b.Instrs {
				for j, orig := range in.Args {
					for _, c := range values {
						if c.Type != orig.Type {
							continue
						}
						cb, ci := where[c][0], where[c][1]
						var want bool
						if in.Op == Phi {
							want = dom[cb][in.Edges[j].From.ID]
						} else {
							want = cb == b.ID && ci < i || cb != b.ID && dom[cb][b.ID]
						}
						in.Args[j] = c
						check(p, want, fx.name+": "+in.String())
					}
					in.Args[j] = orig
				}
				if in.Op != Phi {
					continue
				}
				for j, orig := range in.Edges {
					for _, from := range f.Blocks {
						for k := range 2 {
							if e := (Edge{from, k}); e != orig {
								in.Edges[j] = e
								check(p, false, fx.name+": "+in.String())
							}
						}
					}
					in.Edges[j] = orig
					edges, args := in.Edges, in.Args
					in.Edges, in.Args = slices.Delete(slices.Clone(edges), j, j+1), slices.Delete(slices.Clone(args), j, j+1)
					check(p, false, fx.name+": removed: "+in.String())
					if j+1 < len(edges) {
						in.Edges, in.Args = slices.Clone(edges), slices.Clone(args)
						in.Edges[j], in.Edges[j+1] = in.Edges[j+1], in.Edges[j]
						in.Args[j], in.Args[j+1] = in.Args[j+1], in.Args[j]
						check(p, false, fx.name+": swapped: "+in.String())
					}
					in.Edges, in.Args = edges, args
				}
			}
		}
		check(p, true, fx.name+": restored")
	}
	t.Logf("%d mutants accepted, %d rejected, all as the oracle predicts", accepted, rejected)
}
