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
		{"operand of wrong type", func(p *Program) { instr(p, 0).Type = Type(9) }, "operand v0 has type Type(9), want i64"},
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
		{"two results", func(p *Program) { p.Funcs[1].Results = []Type{I64, I64} }, "func f: 2 results; at most 1 is supported"},
		{"parameter type", func(p *Program) { p.Funcs[1].Params[0] = NoType }, "func f: parameter 0 has invalid type none"},
		{"result type", func(p *Program) { p.Funcs[1].Results[0] = Type(9) }, "func f: result 0 has invalid type Type(9)"},
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
