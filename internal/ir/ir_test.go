package ir

import (
	"strings"
	"testing"
)

// sample builds the IR for
//
//	func main() {
//		x := 6
//		y := -x * 7
//		println(x, y+1)
//	}
func sample() *Program {
	p := &Program{}
	f := p.NewFunc("main", "p.go", Pos{3, 6})
	b := f.Entry()
	x := f.NewSlot("x", I64, Pos{4, 2})
	y := f.NewSlot("y", I64, Pos{5, 2})
	f.Store(b, x, f.Const(b, 6, Pos{4, 7}), Pos{4, 2})
	nx := f.Neg(b, f.Load(b, x, Pos{5, 8}), Pos{5, 7})
	f.Store(b, y, f.Binary(b, Mul, nx, f.Const(b, 7, Pos{5, 11}), Pos{5, 9}), Pos{5, 2})
	lx := f.Load(b, x, Pos{6, 10})
	ly := f.Load(b, y, Pos{6, 13})
	sum := f.Binary(b, Add, ly, f.Const(b, 1, Pos{6, 15}), Pos{6, 14})
	f.Println(b, []*Instr{lx, sum}, Pos{6, 2})
	f.Ret(b, Pos{7, 1})
	return p
}

func TestPrint(t *testing.T) {
	want := `func main @p.go:3:6
  slot s0 x i64 @4:2
  slot s1 y i64 @5:2
b0:
  v0 = const 6 @4:7
  store s0, v0 @4:2
  v1 = load s0 @5:8
  v2 = neg v1 @5:7
  v3 = const 7 @5:11
  v4 = mul v2, v3 @5:9
  store s1, v4 @5:2
  v5 = load s0 @6:10
  v6 = load s1 @6:13
  v7 = const 1 @6:15
  v8 = add v6, v7 @6:14
  println v5, v8 @6:2
  ret @7:1
`
	if got := sample().String(); got != want {
		t.Errorf("String():\n%s\nwant:\n%s", got, want)
	}
	if sample().String() != sample().String() {
		t.Error("printing is not deterministic")
	}
}

func TestVerifyValid(t *testing.T) {
	if errs := Verify(sample()); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	p := &Program{}
	f := p.NewFunc("main", "p.go", Pos{1, 1})
	f.Println(f.Entry(), nil, Pos{2, 2}) // println() with no arguments
	f.Ret(f.Entry(), Pos{3, 1})
	if errs := Verify(p); len(errs) != 0 {
		t.Fatalf("println() rejected: %v", errs)
	}
}

// TestVerifyRejects breaks one invariant at a time in an otherwise valid
// program and checks that Verify reports it, naming the right component.
func TestVerifyRejects(t *testing.T) {
	instr := func(p *Program, i int) *Instr { return p.Funcs[0].Blocks[0].Instrs[i] }
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		{"missing entry point", func(p *Program) { p.Funcs[0].Name = "f" }, "func main: missing entry point"},
		{"duplicate function", func(p *Program) { p.Funcs = append(p.Funcs, p.Funcs[0]) }, "func main: duplicate function name"},
		{"no blocks", func(p *Program) { p.Funcs[0].Blocks = nil }, "func main: no blocks"},
		{"empty block", func(p *Program) { p.Funcs[0].Blocks[0].Instrs = nil }, "b0: empty block: missing terminator"},
		{"missing terminator", func(p *Program) {
			b := p.Funcs[0].Blocks[0]
			b.Instrs = b.Instrs[:len(b.Instrs)-1]
		}, "b0: instr 11 (println v5, v8): missing terminator"},
		{"instruction after terminator", func(p *Program) {
			b := p.Funcs[0].Blocks[0]
			b.Instrs = append(b.Instrs, &Instr{Op: Ret, ID: -1, Pos: Pos{8, 1}})
		}, "b0: instr 12 (ret): terminator before the end of the block"},
		{"unreachable block", func(p *Program) {
			p.Funcs[0].Blocks = append(p.Funcs[0].Blocks, &Block{ID: 1, Instrs: []*Instr{{Op: Ret, ID: -1, Pos: Pos{8, 1}}}})
		}, "b1: unreachable block"},
		{"wrong block ID", func(p *Program) { p.Funcs[0].Blocks[0].ID = 3 }, "b0: block has ID 3"},
		{"undefined operand", func(p *Program) {
			instr(p, 3).Args[0] = &Instr{Op: Const, Type: I64, ID: 99, Pos: Pos{1, 1}}
		}, "instr 3 (v2 = neg v99): operand v99 is not defined earlier"},
		{"use before definition", func(p *Program) { instr(p, 3).Args[0] = instr(p, 4) }, "instr 3 (v2 = neg v3): operand v3 is not defined earlier"},
		{"operand without a value", func(p *Program) { instr(p, 3).Args[0] = instr(p, 1) }, "operand v-1 is not defined earlier"},
		{"nil operand", func(p *Program) { instr(p, 3).Args[0] = nil }, "instr 3 (v2 = neg v?): nil operand"},
		{"wrong operand count", func(p *Program) { instr(p, 5).Args = instr(p, 5).Args[:1] }, "instr 5 (v4 = mul v2): mul has 1 operands, want 2"},
		{"wrong result type", func(p *Program) { instr(p, 2).Type = NoType }, "load has result type none, want i64"},
		{"result type on store", func(p *Program) { instr(p, 1).Type = I64 }, "store has result type i64, want none"},
		{"duplicate value number", func(p *Program) { instr(p, 4).ID = 2 }, "value number v2 defined twice"},
		{"operand of wrong type", func(p *Program) { instr(p, 0).Type = Type(99) }, "operand v0 has type Type(99), want i64"},
		{"invalid operation", func(p *Program) { instr(p, 2).Op = Op(77) }, "instr 2 (v1 = Op(77)): invalid operation Op(77)"},
		{"load before store", func(p *Program) { p.Funcs[0].Blocks[0].Instrs[1] = instr(p, 0) }, "load of s0 (x) before any store to it"},
		{"foreign slot", func(p *Program) { instr(p, 2).Slot = &Slot{ID: 0, Name: "x", Type: I64, Pos: Pos{1, 1}} }, "instr 2 (v1 = load s0): slot operand is not a slot of this function"},
		{"missing slot", func(p *Program) { instr(p, 1).Slot = nil }, "instr 1 (store s?, v0): slot operand is not a slot of this function"},
		{"slot on arithmetic", func(p *Program) { instr(p, 3).Slot = p.Funcs[0].Slots[0] }, "neg has a slot operand"},
		{"constant on load", func(p *Program) { instr(p, 2).Const = 5 }, "load has a constant operand"},
		{"slot type", func(p *Program) { p.Funcs[0].Slots[1].Type = NoType }, "slot s1 has invalid type none"},
		{"slot ID", func(p *Program) { p.Funcs[0].Slots[1].ID = 0 }, "slot 1 has ID 0"},
		{"println of non-value", func(p *Program) { instr(p, 11).Args[0] = instr(p, 6) }, "instr 11 (println v-1, v8): operand v-1 is not defined earlier"},
		{"instruction position", func(p *Program) { instr(p, 4).Pos = Pos{} }, "instr 4 (v3 = const 7): missing source position"},
		{"slot position", func(p *Program) { p.Funcs[0].Slots[0].Pos = Pos{} }, "slot s0 has no source position"},
		{"function position", func(p *Program) { p.Funcs[0].File = "" }, "func main: missing source position"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sample()
			tt.break_(p)
			errs := Verify(p)
			var msgs []string
			for _, e := range errs {
				msgs = append(msgs, e.Error())
			}
			all := strings.Join(msgs, "\n")
			if !strings.Contains(all, tt.want) {
				t.Errorf("errors do not mention %q:\n%s", tt.want, all)
			}
		})
	}
}

// callSample builds the IR for
//
//	func main() { println(f(6)) }
//	func f(n int) int { return f(n) }
func callSample() *Program {
	p := &Program{}
	m := p.NewFunc("main", "p.go", Pos{3, 6})
	f := p.NewFunc("f", "p.go", Pos{4, 6})
	f.Params, f.Results = []Type{I64}, []Type{I64}
	fb := f.Entry()
	f.Ret(fb, Pos{4, 21}, f.Call(fb, f, []*Instr{f.Param(fb, 0, Pos{4, 8})}, Pos{4, 29}))
	mb := m.Entry()
	m.Println(mb, []*Instr{m.Call(mb, f, []*Instr{m.Const(mb, 6, Pos{3, 23})}, Pos{3, 21})}, Pos{3, 15})
	m.Ret(mb, Pos{3, 28})
	return p
}

func TestPrintCalls(t *testing.T) {
	want := `func main @p.go:3:6
b0:
  v0 = const 6 @3:23
  v1 = call f, v0 @3:21
  println v1 @3:15
  ret @3:28

func f(i64) i64 @p.go:4:6
b0:
  v0 = param 0 @4:8
  v1 = call f, v0 @4:29
  ret v1 @4:21
`
	if got := callSample().String(); got != want {
		t.Errorf("String():\n%s\nwant:\n%s", got, want)
	}
}

// TestVerifyCalls checks invariants 6 and 7 of D-023 as far as increment 4.2
// introduces them.
func TestVerifyCalls(t *testing.T) {
	if errs := Verify(callSample()); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	main := func(p *Program, i int) *Instr { return p.Funcs[0].Blocks[0].Instrs[i] }
	f := func(p *Program, i int) *Instr { return p.Funcs[1].Blocks[0].Instrs[i] }
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		{"main with a parameter", func(p *Program) { p.Funcs[0].Params = []Type{I64} }, "func main: main has parameters or results"},
		{"parameter type", func(p *Program) { p.Funcs[1].Params[0] = NoType }, "func f: parameter 0 has invalid type none"},
		{"result type", func(p *Program) { p.Funcs[1].Results[0] = Type(99) }, "func f: result 0 has invalid type Type(99)"},
		{"param index", func(p *Program) { f(p, 0).Index = 1 }, "param 1 of a function with 1 parameters"},
		{"param type", func(p *Program) { f(p, 0).Type = NoType }, "param has result type none, want i64"},
		{"param outside entry", func(p *Program) {
			p.Funcs[1].Blocks = append(p.Funcs[1].Blocks, &Block{ID: 1, Instrs: []*Instr{{Op: Param, Type: I64, ID: 5, Pos: Pos{1, 1}}, {Op: Ret, ID: -1, Pos: Pos{1, 1}}}})
		}, "b1: instr 0 (v5 = param 0): param outside the entry block"},
		{"index on another operation", func(p *Program) { main(p, 0).Index = 2 }, "const has a parameter index"},
		{"foreign callee", func(p *Program) { main(p, 1).Callee = &Func{Name: "g"} }, "instr 1 (v1 = call g, v0): callee is not a function of the program"},
		{"missing callee", func(p *Program) { main(p, 1).Callee = nil }, "callee is not a function of the program"},
		{"callee on another operation", func(p *Program) { main(p, 0).Callee = p.Funcs[1] }, "const has a callee"},
		{"argument count", func(p *Program) { main(p, 1).Args = nil }, "call has 0 operands, want 1"},
		{"call result type", func(p *Program) { main(p, 1).Type = NoType }, "call has result type none, want i64"},
		{"missing result", func(p *Program) { f(p, 2).Args = nil }, "func f: b0: instr 2 (ret): ret has 0 operands, want 1"},
		{"result from main", func(p *Program) { main(p, 3).Args = []*Instr{main(p, 0)} }, "func main: b0: instr 3 (ret v0): ret has 1 operands, want 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := callSample()
			tt.break_(p)
			var msgs []string
			for _, e := range Verify(p) {
				msgs = append(msgs, e.Error())
			}
			if all := strings.Join(msgs, "\n"); !strings.Contains(all, tt.want) {
				t.Errorf("errors do not mention %q:\n%s", tt.want, all)
			}
		})
	}
}

// loop builds the IR for
//
//	func main() {
//		i := 0          // b0
//		for i < 3 {     // b1
//			i++     // b2
//		}
//		println(i, true) // b3
//	}
func loop() *Program {
	p := &Program{}
	f := p.NewFunc("main", "p.go", Pos{1, 6})
	i := f.NewSlot("i", I64, Pos{2, 2})
	b0, b1, b2, b3 := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	f.Store(b0, i, f.Const(b0, 0, Pos{2, 7}), Pos{2, 2})
	f.Jump(b0, b1, Pos{3, 2})
	f.Branch(b1, f.Compare(b1, Lt, f.Load(b1, i, Pos{3, 6}), f.Const(b1, 3, Pos{3, 10}), Pos{3, 8}), b2, b3, Pos{3, 2})
	f.Store(b2, i, f.Binary(b2, Add, f.Load(b2, i, Pos{4, 3}), f.Const(b2, 1, Pos{4, 4}), Pos{4, 4}), Pos{4, 3})
	f.Jump(b2, b1, Pos{5, 2})
	f.Println(b3, []*Instr{f.Load(b3, i, Pos{6, 10}), f.BoolConst(b3, true, Pos{6, 13})}, Pos{6, 2})
	f.Ret(b3, Pos{7, 1})
	return p
}

func TestPrintBlocks(t *testing.T) {
	want := `func main @p.go:1:6
  slot s0 i i64 @2:2
b0:
  v0 = const 0 @2:7
  store s0, v0 @2:2
  jump b1 @3:2
b1:
  v1 = load s0 @3:6
  v2 = const 3 @3:10
  v3 = lt v1, v2 @3:8
  branch v3, b2, b3 @3:2
b2:
  v4 = load s0 @4:3
  v5 = const 1 @4:4
  v6 = add v4, v5 @4:4
  store s0, v6 @4:3
  jump b1 @5:2
b3:
  v7 = load s0 @6:10
  v8 = const true @6:13
  println v7, v8 @6:2
  ret @7:1
`
	if got := loop().String(); got != want {
		t.Errorf("String():\n%s\nwant:\n%s", got, want)
	}
}

// TestVerifyControlFlow checks D-023 invariants 1-5. The loop header reads
// a slot stored before the loop, which only the greatest fixed point of the
// definite-assignment dataflow accepts.
func TestVerifyControlFlow(t *testing.T) {
	if errs := Verify(loop()); len(errs) != 0 {
		t.Fatalf("valid loop rejected: %v", errs)
	}
	at := func(p *Program, b, i int) *Instr { return p.Funcs[0].Blocks[b].Instrs[i] }
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		// A slot stored on only one path before a load: in the loop, i is
		// first stored in b2, so the load in b1 (reached from b0) and the
		// one in b3 have a path without a store.
		{"store on one path only", func(p *Program) { p.Funcs[0].Blocks[0].Instrs = p.Funcs[0].Blocks[0].Instrs[2:] },
			"b1: instr 0 (v1 = load s0): load of s0 (i) before any store to it on every path"},
		{"store only inside the loop body, load after", func(p *Program) {
			f := p.Funcs[0]
			j := f.NewSlot("j", I64, Pos{1, 1})
			b2 := f.Blocks[2]
			b2.Instrs = append([]*Instr{{Op: Store, ID: -1, Slot: j, Args: []*Instr{at(p, 2, 1)}, Pos: Pos{1, 1}}}, b2.Instrs...)
			b2.Instrs[0], b2.Instrs[2] = b2.Instrs[2], b2.Instrs[0] // after v5 is defined
			f.Blocks[3].Instrs = append([]*Instr{{Op: Load, Type: I64, ID: 20, Slot: j, Pos: Pos{1, 1}}}, f.Blocks[3].Instrs...)
		}, "b3: instr 0 (v20 = load s1): load of s1 (j) before any store to it on every path"},
		{"unreachable block", func(p *Program) { at(p, 0, 2).Targets[0] = p.Funcs[0].Blocks[3] }, "b1: unreachable block"},
		{"foreign target", func(p *Program) { at(p, 2, 4).Targets[0] = &Block{ID: 1} }, "b2: instr 4 (jump b1): target is not a block of this function"},
		{"missing target", func(p *Program) { at(p, 1, 3).Targets = at(p, 1, 3).Targets[:1] }, "branch has 1 targets, want 2"},
		{"target on another operation", func(p *Program) { at(p, 3, 2).Targets = []*Block{p.Funcs[0].Blocks[0]} }, "println has 1 targets, want 0"},
		{"jump not last", func(p *Program) {
			b := p.Funcs[0].Blocks[0]
			b.Instrs = append(b.Instrs, &Instr{Op: Ret, ID: -1, Pos: Pos{1, 1}})
		}, "b0: instr 2 (jump b1): terminator before the end of the block"},
		{"branch on an integer", func(p *Program) { at(p, 1, 3).Args[0] = at(p, 1, 1) }, "instr 3 (branch v2, b2, b3): operand v2 has type i64, want bool"},
		{"lt on bools", func(p *Program) {
			b3 := p.Funcs[0].Blocks[3]
			lt := &Instr{Op: Lt, Type: Bool, ID: 21, Args: []*Instr{b3.Instrs[1], b3.Instrs[1]}, Pos: Pos{1, 1}}
			b3.Instrs = append(b3.Instrs[:2], append([]*Instr{lt}, b3.Instrs[2:]...)...)
		}, "instr 2 (v21 = lt v8, v8): operand v8 has type bool, want i64"},
		{"eq of mixed types", func(p *Program) {
			b3 := p.Funcs[0].Blocks[3]
			eq := &Instr{Op: Eq, Type: Bool, ID: 21, Args: []*Instr{b3.Instrs[1], b3.Instrs[0]}, Pos: Pos{1, 1}}
			b3.Instrs = append(b3.Instrs[:2], append([]*Instr{eq}, b3.Instrs[2:]...)...)
		}, "instr 2 (v21 = eq v8, v7): operand v7 has type i64, want bool"},
		{"not of an integer", func(p *Program) {
			b3 := p.Funcs[0].Blocks[3]
			not := &Instr{Op: Not, Type: Bool, ID: 21, Args: []*Instr{b3.Instrs[0]}, Pos: Pos{1, 1}}
			b3.Instrs = append(b3.Instrs[:1], append([]*Instr{not}, b3.Instrs[1:]...)...)
		}, "instr 1 (v21 = not v7): operand v7 has type i64, want bool"},
		{"comparison result type", func(p *Program) { at(p, 1, 2).Type = I64 }, "lt has result type i64, want bool"},
		{"bool constant out of range", func(p *Program) { at(p, 3, 1).Const = 2 }, "bool constant 2 is not 0 or 1"},
		{"store of the wrong type", func(p *Program) { at(p, 0, 0).Type = Bool }, "instr 1 (store s0, v0): operand v0 has type bool, want i64"},
		{"value across blocks", func(p *Program) { at(p, 3, 2).Args[0] = at(p, 1, 0) }, "b3: instr 2 (println v1, v8): operand v1 is not defined earlier in this block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := loop()
			tt.break_(p)
			var msgs []string
			for _, e := range Verify(p) {
				msgs = append(msgs, e.Error())
			}
			if all := strings.Join(msgs, "\n"); !strings.Contains(all, tt.want) {
				t.Errorf("errors do not mention %q:\n%s", tt.want, all)
			}
		})
	}
}

// tupleSample builds the IR for
//
//	func main() { a, b := f(); println(a, b) }
//	func f() (int, bool) { return 1, true }
func tupleSample() *Program {
	p := &Program{}
	m := p.NewFunc("main", "p.go", Pos{1, 6})
	f := p.NewFunc("f", "p.go", Pos{2, 6})
	f.Results = []Type{I64, Bool}
	fb := f.Entry()
	f.Ret(fb, Pos{2, 24}, f.Const(fb, 1, Pos{2, 31}), f.BoolConst(fb, true, Pos{2, 34}))
	mb := m.Entry()
	t := m.Call(mb, f, nil, Pos{1, 23})
	m.Println(mb, []*Instr{m.Extract(mb, t, 0, Pos{1, 23}), m.Extract(mb, t, 1, Pos{1, 23})}, Pos{1, 28})
	m.Ret(mb, Pos{1, 43})
	return p
}

func TestPrintTuple(t *testing.T) {
	want := `func main @p.go:1:6
b0:
  v0 = call f @1:23
  v1 = extract 0, v0 @1:23
  v2 = extract 1, v0 @1:23
  println v1, v2 @1:28
  ret @1:43

func f() (i64, bool) @p.go:2:6
b0:
  v0 = const 1 @2:31
  v1 = const true @2:34
  ret v0, v1 @2:24
`
	if got := tupleSample().String(); got != want {
		t.Errorf("String():\n%s\nwant:\n%s", got, want)
	}
}

// TestVerifyTuples checks the tuple-storage rule of D-023 (increment 4.3).
func TestVerifyTuples(t *testing.T) {
	if errs := Verify(tupleSample()); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	main := func(p *Program, i int) *Instr { return p.Funcs[0].Blocks[0].Instrs[i] }
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		{"index out of range", func(p *Program) { main(p, 2).Index = 2 }, "extract 2 of a call with 2 results"},
		{"wrong result type", func(p *Program) { main(p, 2).Type = I64 }, "extract has result type i64, want bool"},
		{"extract of a value", func(p *Program) { main(p, 2).Args[0] = main(p, 1) }, "extract operand is not a call"},
		{"tuple used by println", func(p *Program) { main(p, 3).Args[0] = main(p, 0) }, "tuple v0 used by println; only extract may use a tuple"},
		{"call type", func(p *Program) { main(p, 0).Type = I64 }, "call has result type i64, want tuple"},
		{"too few results", func(p *Program) {
			r := p.Funcs[1].Blocks[0].Instrs[2]
			r.Args = r.Args[:1]
		}, "ret has 1 operands, want 2"},
		{"result of the wrong type", func(p *Program) {
			r := p.Funcs[1].Blocks[0].Instrs[2]
			r.Args[0], r.Args[1] = r.Args[1], r.Args[0]
		}, "operand v1 has type bool, want i64"},
		{"extract in another block", func(p *Program) {
			m := p.Funcs[0]
			b1 := m.NewBlock()
			b0 := m.Blocks[0]
			rest := b0.Instrs[1:]
			b0.Instrs = []*Instr{b0.Instrs[0], {Op: Jump, ID: -1, Targets: []*Block{b1}, Pos: Pos{1, 1}}}
			b1.Instrs = rest
		}, "b1: instr 0 (v1 = extract 0, v0): operand v0 is not defined earlier in this block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tupleSample()
			tt.break_(p)
			var msgs []string
			for _, e := range Verify(p) {
				msgs = append(msgs, e.Error())
			}
			if all := strings.Join(msgs, "\n"); !strings.Contains(all, tt.want) {
				t.Errorf("errors do not mention %q:\n%s", tt.want, all)
			}
		})
	}
}

// sizedSample builds main { x := u8(255); println(int8(x) + -1, ^x &^ 1) }.
func sizedSample() *Program {
	p := &Program{}
	f := p.NewFunc("main", "p.go", Pos{1, 1})
	b := f.Entry()
	x := f.TypedConst(b, U8, 255, Pos{2, 2})
	sum := f.Binary(b, Add, f.Conv(b, I8, x, Pos{3, 3}), f.TypedConst(b, I8, -1, Pos{3, 4}), Pos{3, 5})
	f.Print(b, []*Instr{sum, f.Binary(b, AndNot, f.Com(b, x, Pos{4, 4}), f.TypedConst(b, U8, 1, Pos{4, 6}), Pos{4, 5})}, Pos{3, 1})
	f.Ret(b, Pos{5, 1})
	return p
}

func TestPrintSized(t *testing.T) {
	want := `func main @p.go:1:1
b0:
  v0 = const u8 255 @2:2
  v1 = conv i8, v0 @3:3
  v2 = const i8 -1 @3:4
  v3 = add v1, v2 @3:5
  v4 = com v0 @4:4
  v5 = const u8 1 @4:6
  v6 = andnot v4, v5 @4:5
  print v3, v6 @3:1
  ret @5:1
`
	if got := sizedSample().String(); got != want {
		t.Errorf("String():\n%s\nwant:\n%s", got, want)
	}
}

// TestVerifySized checks the type rules and constant ranges of the sized
// integer types (D-023 invariants 3 and 5).
func TestVerifySized(t *testing.T) {
	if errs := Verify(sizedSample()); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	at := func(p *Program, i int) *Instr { return p.Funcs[0].Blocks[0].Instrs[i] }
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		{"u8 out of range", func(p *Program) { at(p, 0).Const = 256 }, "u8 constant 256 is not in canonical form"},
		{"i8 not sign-extended", func(p *Program) { at(p, 2).Const = 255 }, "i8 constant 255 is not in canonical form"},
		{"mixed operand types", func(p *Program) { at(p, 3).Args[1] = at(p, 0) }, "instr 3 (v3 = add v1, v0): operand v0 has type u8, want i8"},
		{"result type", func(p *Program) { at(p, 3).Type = I64 }, "add has result type i64, want i8"},
		{"conv to bool", func(p *Program) { at(p, 1).Type = Bool }, "conv to bool, not an integer type"},
		{"conv of a bool", func(p *Program) { at(p, 1).Args[0] = &Instr{Op: Const, Type: Bool, ID: 0} }, "operand v0 has type bool, want i64"},
		{"com of a bool", func(p *Program) { at(p, 4).Type, at(p, 4).Args[0].Type = Bool, Bool }, "com has result type bool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sizedSample()
			tt.break_(p)
			var msgs []string
			for _, e := range Verify(p) {
				msgs = append(msgs, e.Error())
			}
			if all := strings.Join(msgs, "\n"); !strings.Contains(all, tt.want) {
				t.Errorf("errors do not mention %q:\n%s", tt.want, all)
			}
		})
	}
	for _, tt := range []struct {
		t    Type
		v    int64
		want int64
	}{{I8, 128, -128}, {I8, -129, 127}, {U8, -1, 255}, {I16, 1 << 15, -1 << 15}, {U16, 1 << 16, 0}, {I32, 1 << 31, -1 << 31}, {U32, -1, 1<<32 - 1}, {U64, -1, -1}, {I64, 5, 5}} {
		if got := tt.t.Canonical(tt.v); got != tt.want {
			t.Errorf("%s.Canonical(%d) = %d, want %d", tt.t, tt.v, got, tt.want)
		}
	}
}

// TestVerifyDivisionAndShifts checks the types of div, rem, and the shifts,
// and the closed set of panics (D-023 invariant 9).
func TestVerifyDivisionAndShifts(t *testing.T) {
	build := func(count Type, kind int) *Program {
		p := &Program{}
		f := p.NewFunc("main", "p.go", Pos{1, 1})
		b0, b1 := f.Entry(), f.NewBlock()
		x := f.TypedConst(b0, I32, -7, Pos{1, 1})
		c := f.TypedConst(b0, count, 3, Pos{1, 1})
		f.Println(b0, []*Instr{f.Binary(b0, Div, x, x, Pos{1, 1}), f.Binary(b0, Rem, x, x, Pos{1, 1}), f.Shift(b0, Shl, x, c, Pos{1, 1}), f.Shift(b0, Shr, x, c, Pos{1, 1})}, Pos{1, 1})
		f.Jump(b0, b1, Pos{1, 1})
		f.Panic(b1, kind, Pos{1, 1})
		return p
	}
	if errs := Verify(build(U8, PanicShift)); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	want := "func main @p.go:1:1\nb0:\n  v0 = const i32 -7 @1:1\n  v1 = const u8 3 @1:1\n  v2 = div v0, v0 @1:1\n  v3 = rem v0, v0 @1:1\n  v4 = shl v0, v1 @1:1\n  v5 = shr v0, v1 @1:1\n  println v2, v3, v4, v5 @1:1\n  jump b1 @1:1\nb1:\n  panic shift @1:1\n"
	if got := build(U8, PanicShift).String(); got != want {
		t.Errorf("String():\n%s\nwant:\n%s", got, want)
	}
	for _, tt := range []struct {
		p    *Program
		want string
	}{
		{build(I8, PanicDivide), "operand v1 has type i8, want u64"},
		{build(U64, 2), "panic of unknown kind 2"},
		{build(U64, -1), "panic of unknown kind -1"},
	} {
		var msgs []string
		for _, e := range Verify(tt.p) {
			msgs = append(msgs, e.Error())
		}
		if all := strings.Join(msgs, "\n"); !strings.Contains(all, tt.want) {
			t.Errorf("errors do not mention %q:\n%s", tt.want, all)
		}
	}
}
