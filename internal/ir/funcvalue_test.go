package ir

import (
	"strings"
	"testing"
)

// indirectSample builds the IR for a call through a code address (D-039):
// main takes f's address and calls it, with a context and an argument,
// and prints the two results.
func indirectSample() *Program {
	p := &Program{}
	m := p.NewFunc("main", "p.go", Pos{1, 1})
	f := p.NewFunc("f", "p.go", Pos{2, 1})
	f.Params, f.Results = []Type{Ptr, I64}, []Type{I64, Bool}
	fb := f.Entry()
	f.Param(fb, 0, Pos{2, 2})
	x := f.Param(fb, 1, Pos{2, 3})
	f.Ret(fb, Pos{2, 4}, x, f.BoolConst(fb, true, Pos{2, 5}))
	b := m.Entry()
	code := m.FuncAddr(b, f, Pos{1, 2})
	c := m.CallInd(b, &Sig{Params: []Type{Ptr, I64}, Results: []Type{I64, Bool}}, code, []*Instr{m.TypedConst(b, Ptr, 0, Pos{1, 3}), m.Const(b, 7, Pos{1, 4})}, Pos{1, 5})
	m.Println(b, []*Instr{m.Extract(b, c, 0, Pos{1, 6}), m.Extract(b, c, 1, Pos{1, 7})}, Pos{1, 8})
	m.Ret(b, Pos{1, 9})
	return p
}

func TestPrintIndirect(t *testing.T) {
	got := indirectSample().String()
	for _, want := range []string{
		"v0 = funcaddr f @1:2",
		"v3 = callind func(ptr, i64) (i64, bool), v0, v1, v2 @1:5",
		"v4 = extract 0, v3 @1:6",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("String() lacks %q:\n%s", want, got)
		}
	}
}

func TestVerifyIndirect(t *testing.T) {
	if errs := Verify(indirectSample()); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	main := func(p *Program, i int) *Instr { return p.Funcs[0].Blocks[0].Instrs[i] }
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		{"foreign function", func(p *Program) { main(p, 0).Callee = &Func{Name: "g"} }, "funcaddr of a function not of the program"},
		{"init", func(p *Program) { p.Init = p.Funcs[1]; p.Funcs[1].Name = "init" }, "funcaddr of init"},
		{"funcaddr type", func(p *Program) { main(p, 0).Type = I64 }, "funcaddr has result type i64, want ptr"},
		{"code type", func(p *Program) { main(p, 3).Args[0] = main(p, 2) }, "operand v2 has type i64, want ptr"},
		{"argument type", func(p *Program) { main(p, 3).Args[2] = main(p, 1) }, "operand v1 has type ptr, want i64"},
		{"argument count", func(p *Program) { main(p, 3).Args = main(p, 3).Args[:2] }, "callind has 2 operands, want 3"},
		{"no signature", func(p *Program) { main(p, 3).Sig = nil }, "callind without a signature"},
		{"signature on another operation", func(p *Program) { main(p, 1).Sig = &Sig{} }, "const has a signature"},
		{"extract beyond the results", func(p *Program) { main(p, 4).Index = 2 }, "extract 2 of a call with 2 results"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := indirectSample()
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
