package ir

import (
	"strings"
	"testing"
)

// memSample builds a program with a global and a local memory object, an
// address of each, and a load and store of each memory type's width.
func memSample() *Program {
	p := &Program{}
	g := p.NewMem("a", 24, 8, "p.go", Pos{2, 5})
	f := p.NewFunc("main", "p.go", Pos{3, 6})
	b := f.Entry()
	m := f.NewMem("s", 16, 4, Pos{4, 2})
	pg := f.Addr(b, g, Pos{5, 2})
	pm := f.Addr(b, m, Pos{6, 2})
	off := f.Const(b, 8, Pos{6, 3})
	x := f.LdM(b, I64, pg, off, Pos{6, 4})
	f.StM(b, pm, off, f.Conv(b, I32, x, Pos{6, 5}), Pos{6, 6})
	y := f.LdM(b, I32, pm, off, Pos{6, 7})
	f.StM(b, pg, f.Const(b, 16, Pos{7, 1}), f.Str(b, p.Intern("hi"), Pos{7, 2}), Pos{7, 3})
	f.Println(b, []*Instr{y}, Pos{8, 2})
	f.Ret(b, Pos{9, 1})
	return p
}

func TestPrintMem(t *testing.T) {
	want := `string s0 "hi"
mem gm0 a 24 align 8 @p.go:2:5

func main @p.go:3:6
  mem m0 s 16 align 4 @4:2
b0:
  v0 = addr gm0 @5:2
  v1 = addr m0 @6:2
  v2 = const 8 @6:3
  v3 = ldm i64, v0, v2 @6:4
  v4 = conv i32, v3 @6:5
  stm v1, v2, v4 @6:6
  v5 = ldm i32, v1, v2 @6:7
  v6 = const 16 @7:1
  v7 = str s0 @7:2
  stm v0, v6, v7 @7:3
  println v5 @8:2
  ret @9:1
`
	if got := memSample().String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMemSize(t *testing.T) {
	want := map[Type]int64{Bool: 1, I8: 1, U8: 1, I16: 2, U16: 2, I32: 4, U32: 4, F32: 4, I64: 8, U64: 8, F64: 8, Ptr: 8, NoType: 0, Tuple: 0}
	for ty, n := range want {
		if got := ty.MemSize(); got != n {
			t.Errorf("%s.MemSize() = %d, want %d", ty, got, n)
		}
	}
}

// TestVerifyMem checks the memory invariants of D-034: addr names a memory
// object of the function or the program, ldm and stm take a ptr and an i64
// offset and a type with a memory form, and the objects are well formed.
func TestVerifyMem(t *testing.T) {
	if errs := Verify(memSample()); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	in := func(p *Program, i int) *Instr { return p.Funcs[0].Blocks[0].Instrs[i] }
	other := &Mem{ID: 0, Name: "x", Size: 8, Align: 8, Pos: Pos{1, 1}}
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		{"addr of a foreign mem", func(p *Program) { in(p, 1).Mem = other }, "addr operand is not a memory object of this function or the program"},
		{"addr without mem", func(p *Program) { in(p, 1).Mem = nil }, "addr operand is not a memory object"},
		{"addr result type", func(p *Program) { in(p, 0).Type = I64 }, "addr has result type i64, want ptr"},
		{"mem on another op", func(p *Program) { in(p, 2).Mem = p.Funcs[0].Mems[0] }, "const has a memory object operand"},
		{"ldm of an integer address", func(p *Program) { in(p, 3).Args[0] = in(p, 2) }, "operand v2 has type i64, want ptr"},
		{"ldm offset type", func(p *Program) { in(p, 3).Args[1] = in(p, 0) }, "operand v0 has type ptr, want i64"},
		{"ldm of a tuple type", func(p *Program) { in(p, 3).Type = NoType; in(p, 3).ID = -1 }, "ldm of none, which has no memory form"},
		{"ldm operand count", func(p *Program) { in(p, 3).Args = in(p, 3).Args[:1] }, "ldm has 1 operands, want 2"},
		{"stm result", func(p *Program) { in(p, 5).Type, in(p, 5).ID = I64, 99 }, "stm has result type i64, want none"},
		{"stm address type", func(p *Program) { in(p, 5).Args[0] = in(p, 2) }, "operand v2 has type i64, want ptr"},
		{"stm operand count", func(p *Program) { in(p, 5).Args = in(p, 5).Args[:2] }, "stm has 2 operands, want 3"},
		{"bad alignment", func(p *Program) { p.Funcs[0].Mems[0].Align = 3 }, "mem m0 has alignment 3"},
		{"negative size", func(p *Program) { p.Mems[0].Size = -1 }, "global mem gm0 has size -1"},
		{"too large", func(p *Program) { p.Funcs[0].Mems[0].Size = MaxMemSize + 1 }, "mem m0 has size 1073741825"},
		{"wrong ID", func(p *Program) { p.Funcs[0].Mems[0].ID = 2 }, "mem m0 has ID 2"},
		{"local flagged global", func(p *Program) { p.Funcs[0].Mems[0].Global = true }, "mem m0 has the wrong Global flag"},
		{"global without file", func(p *Program) { p.Mems[0].File = "" }, "global mem gm0 has no source file"},
		{"local without position", func(p *Program) { p.Funcs[0].Mems[0].Pos = Pos{} }, "mem m0 has no source position"},
		{"nil global mem", func(p *Program) { p.Mems = append(p.Mems, nil) }, "global mem gm1 is nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := memSample()
			tt.break_(p)
			var msgs []string
			for _, e := range Verify(p) {
				msgs = append(msgs, e.Error())
			}
			if all := strings.Join(msgs, "\n"); !strings.Contains(all, tt.want) {
				t.Errorf("errors:\n%s\nwant one containing %q", all, tt.want)
			}
		})
	}
}
