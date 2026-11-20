package ir

import (
	"strings"
	"testing"
)

// goProgram is main starting a goroutine that runs g, switching to it, and
// g ending and resuming main (D-048).
func goProgram() *Program {
	p := &Program{}
	pos := Pos{1, 1}
	main := p.NewFunc("main", "p.go", pos)
	g := p.NewFunc("g", "p.go", pos)
	b := main.Entry()
	h := main.GoNew(b, g, pos)
	main.GoSwitch(b, h, pos)
	main.Ret(b, pos)
	gb := g.Entry()
	g.GoExit(gb, g.Const(gb, 0, pos), pos)
	return p
}

// TestGoroutineOps checks that the verifier accepts the goroutine
// operations, prints them, and rejects their misuse.
func TestGoroutineOps(t *testing.T) {
	p := goProgram()
	if errs := Verify(p); len(errs) != 0 {
		t.Fatalf("rejected: %v", errs)
	}
	text := p.String()
	for _, want := range []string{"v0 = gonew g @", "goswitch v0 @", "goexit v0 @"} {
		if !strings.Contains(text, want) {
			t.Errorf("printed IR lacks %q:\n%s", want, text)
		}
	}
	if !GoExit.IsTerminator() || GoSwitch.IsTerminator() || GoNew.IsTerminator() {
		t.Error("only goexit is a terminator")
	}
	tests := []struct {
		name   string
		break_ func(p *Program)
		want   string
	}{
		{"gonew with parameters", func(p *Program) { p.Funcs[1].Params = []Type{I64} }, "gonew of g, which has parameters or results"},
		{"gonew with results", func(p *Program) { p.Funcs[1].Results = []Type{I64} }, "gonew of g, which has parameters or results"},
		{"gonew of init", func(p *Program) { p.Init = p.Funcs[1] }, "gonew of init"},
		{"gonew of no function", func(p *Program) { p.Funcs[0].Blocks[0].Instrs[0].Callee = nil }, "gonew of a function not of the program"},
		{"goswitch of a bool", func(p *Program) {
			p.Funcs[0].Blocks[0].Instrs[0].Type = Bool
		}, "operand v0 has type bool, want i64"},
		{"goexit before the end", func(p *Program) {
			b := p.Funcs[1].Blocks[0]
			b.Instrs = append(b.Instrs, &Instr{Op: Ret, ID: -1, Pos: Pos{1, 1}})
		}, "terminator before the end of the block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := goProgram()
			tt.break_(p)
			errs := Verify(p)
			for _, err := range errs {
				if strings.Contains(err.Error(), tt.want) {
					return
				}
			}
			t.Errorf("errors %v, want one containing %q", errs, tt.want)
		})
	}
}

// TestSysOps checks that the verifier accepts the system calls (D-049,
// increment 11.6), prints them, and rejects their misuse.
func TestSysOps(t *testing.T) {
	build := func() *Program {
		p := &Program{}
		pos := Pos{1, 1}
		f := p.NewFunc("main", "p.go", pos)
		b := f.Entry()
		n := f.Sys(b, SysArgc, I64, pos)
		f.Sys(b, SysWrite, I64, pos, f.Const(b, 2, pos), f.Sys(b, SysArgPtr, Ptr, pos, n), f.Sys(b, SysArgLen, I64, pos, n))
		f.Sys(b, SysGC, NoType, pos)
		f.Sys(b, SysExit, NoType, pos, n)
		f.Ret(b, pos)
		return p
	}
	p := build()
	if errs := Verify(p); len(errs) != 0 {
		t.Fatalf("rejected: %v", errs)
	}
	text := p.String()
	for _, want := range []string{"v0 = sys argc @", "v4 = sys write, v1, v2, v3 @", "sys gc @", "sys exit, v0 @"} {
		if !strings.Contains(text, want) {
			t.Errorf("printed IR lacks %q:\n%s", want, text)
		}
	}
	tests := []struct {
		name   string
		break_ func(in []*Instr)
		want   string
	}{
		{"unknown kind", func(in []*Instr) { in[0].Index = len(SysNames) }, "sys of unknown kind 6"},
		{"argc of a ptr", func(in []*Instr) { in[0].Type = Ptr }, "has type ptr, want i64"},
		{"write of a non-ptr", func(in []*Instr) { in[4].Args[1] = in[1] }, "operand v1 has type i64, want ptr"},
		{"exit with no status", func(in []*Instr) { in[6].Args = nil }, "has 0 operands, want 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := build()
			tt.break_(p.Funcs[0].Blocks[0].Instrs)
			errs := Verify(p)
			for _, err := range errs {
				if strings.Contains(err.Error(), tt.want) {
					return
				}
			}
			t.Errorf("errors %v, want one containing %q", errs, tt.want)
		})
	}
}
