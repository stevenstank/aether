package interp

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// TestMemoryRoundTrip stores a value of each memory type in a local object
// and loads it back: in canonical form, with only the type's width kept, so
// that a narrower load of the same bytes reads their low part.
func TestMemoryRoundTrip(t *testing.T) {
	tests := []struct {
		t    ir.Type
		v    int64
		want string
	}{
		{ir.I8, -128, "-128"},
		{ir.U8, 255, "255"},
		{ir.I16, -32768, "-32768"},
		{ir.U16, 65535, "65535"},
		{ir.I32, math.MinInt32, "-2147483648"},
		{ir.U32, math.MaxUint32, "4294967295"},
		{ir.I64, math.MinInt64, "-9223372036854775808"},
		{ir.U64, -1, "18446744073709551615"},
		{ir.Bool, 1, "true"},
		{ir.F64, int64(math.Float64bits(-0.1)), "-0.1"},
		{ir.F32, int64(math.Float32bits(1.5)), "1.5"},
	}
	for _, tt := range tests {
		got := output(t, program(func(f *ir.Func, b *ir.Block) {
			m := f.NewMem("m", 16, 8, p1)
			p, off := f.Addr(b, m, p1), f.Const(b, 8, p1)
			f.StM(b, p, off, f.TypedConst(b, tt.t, tt.v, p1), p1)
			f.Println(b, []*ir.Instr{f.LdM(b, tt.t, f.Addr(b, m, p1), off, p1)}, p1)
		}))
		if got != tt.want+"\n" {
			t.Errorf("%s %d: got %q, want %q", tt.t, tt.v, got, tt.want)
		}
	}
	// Little-endian: the low byte of a stored i64 is at its offset.
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		m := f.NewMem("m", 8, 8, p1)
		p := f.Addr(b, m, p1)
		f.StM(b, p, f.Const(b, 0, p1), f.Const(b, -2, p1), p1)
		f.Println(b, []*ir.Instr{
			f.LdM(b, ir.I8, p, f.Const(b, 0, p1), p1),
			f.LdM(b, ir.U8, p, f.Const(b, 7, p1), p1),
			f.LdM(b, ir.U32, p, f.Const(b, 4, p1), p1),
		}, p1)
	}))
	if want := "-2 255 4294967295\n"; got != want {
		t.Errorf("byte order: got %q, want %q", got, want)
	}
}

// TestMemoryStrings stores a string's words in memory and prints them
// loaded back.
func TestMemoryStrings(t *testing.T) {
	got := output(t, withStrings(func(f *ir.Func, b *ir.Block) {
		m := f.NewMem("s", 16, 8, p1)
		p, zero, eight := f.Addr(b, m, p1), f.Const(b, 0, p1), f.Const(b, 8, p1)
		f.StM(b, p, zero, f.Str(b, 0, p1), p1)
		f.StM(b, p, eight, f.Const(b, 3, p1), p1)
		f.Println(b, []*ir.Instr{f.LdM(b, ir.Ptr, p, zero, p1), f.LdM(b, ir.I64, p, eight, p1)}, p1)
	}, "abc"))
	if got != "abc\n" {
		t.Errorf("got %q", got)
	}
}

// TestGlobalMemory checks that global memory objects start as zero bytes,
// keep their contents across calls and from Init to main, and are shared by
// every function.
func TestGlobalMemory(t *testing.T) {
	p := &ir.Program{}
	g := p.NewMem("a", 16, 8, "p.go", p1)
	main := p.NewFunc("main", "p.go", p1)
	set := p.NewFunc("set", "p.go", p1)
	init := p.NewFunc("init", "p.go", p1)
	p.Init = init
	b := init.Entry()
	init.StM(b, init.Addr(b, g, p1), init.Const(b, 0, p1), init.Const(b, 7, p1), p1)
	init.Ret(b, p1)
	b = set.Entry()
	set.StM(b, set.Addr(b, g, p1), set.Const(b, 8, p1), set.TypedConst(b, ir.U16, 9, p1), p1)
	set.Ret(b, p1)
	b = main.Entry()
	pre := main.LdM(b, ir.I64, main.Addr(b, g, p1), main.Const(b, 8, p1), p1)
	main.Call(b, set, nil, p1)
	a := main.Addr(b, g, p1)
	main.Println(b, []*ir.Instr{pre, main.LdM(b, ir.I64, a, main.Const(b, 0, p1), p1), main.LdM(b, ir.U16, a, main.Const(b, 8, p1), p1)}, p1)
	main.Ret(b, p1)
	if got := output(t, p); got != "0 7 9\n" {
		t.Errorf("got %q", got)
	}
}

// TestLocalMemoryPerCall checks that each call has its own instance of a
// local object: a recursive function stores its depth in its object before
// the recursive call and prints it after.
func TestLocalMemoryPerCall(t *testing.T) {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	r := p.NewFunc("r", "p.go", p1)
	r.Params = []ir.Type{ir.I64}
	m := r.NewMem("m", 8, 8, p1)
	entry, rec, done := r.Entry(), r.NewBlock(), r.NewBlock()
	n := r.Param(entry, 0, p1)
	zero := r.Const(entry, 0, p1)
	r.StM(entry, r.Addr(entry, m, p1), zero, n, p1)
	r.Branch(entry, r.Compare(entry, ir.Gt, n, zero, p1), rec, done, p1)
	nn := r.LdM(rec, ir.I64, r.Addr(rec, m, p1), r.Const(rec, 0, p1), p1)
	r.Call(rec, r, []*ir.Instr{r.Binary(rec, ir.Sub, nn, r.Const(rec, 1, p1), p1)}, p1)
	r.Jump(rec, done, p1)
	r.Println(done, []*ir.Instr{r.LdM(done, ir.I64, r.Addr(done, m, p1), r.Const(done, 0, p1), p1)}, p1)
	r.Ret(done, p1)
	b := main.Entry()
	main.Call(b, r, []*ir.Instr{main.Const(b, 3, p1)}, p1)
	main.Ret(b, p1)
	if got := output(t, p); got != "0\n1\n2\n3\n" {
		t.Errorf("got %q", got)
	}
}

// TestMemoryPreconditions checks that the interpreter reports, rather than
// executes, an access that breaks a precondition of ldm or stm.
func TestMemoryPreconditions(t *testing.T) {
	tests := []struct {
		name string
		body func(f *ir.Func, b *ir.Block, p *ir.Instr)
		want string
	}{
		{"unwritten", func(f *ir.Func, b *ir.Block, p *ir.Instr) {
			f.StM(b, p, f.Const(b, 0, p1), f.TypedConst(b, ir.U8, 1, p1), p1)
			f.LdM(b, ir.U16, p, f.Const(b, 0, p1), p1)
		}, "read of byte 1, never written"},
		{"past the end", func(f *ir.Func, b *ir.Block, p *ir.Instr) {
			f.StM(b, p, f.Const(b, 16, p1), f.Const(b, 0, p1), p1)
		}, "i64 at offset 16 of an object of 16 bytes"},
		{"negative", func(f *ir.Func, b *ir.Block, p *ir.Instr) {
			f.StM(b, p, f.Const(b, -8, p1), f.Const(b, 0, p1), p1)
		}, "i64 at offset -8"},
		{"misaligned", func(f *ir.Func, b *ir.Block, p *ir.Instr) {
			f.StM(b, p, f.Const(b, 2, p1), f.TypedConst(b, ir.I32, 0, p1), p1)
		}, "i32 at misaligned offset 2"},
		{"string address", func(f *ir.Func, b *ir.Block, p *ir.Instr) {
			f.LdM(b, ir.U8, f.Str(b, 0, p1), f.Const(b, 0, p1), p1)
		}, "address 0x1 is not a memory object in existence"},
		{"bool byte", func(f *ir.Func, b *ir.Block, p *ir.Instr) {
			f.StM(b, p, f.Const(b, 0, p1), f.TypedConst(b, ir.U8, 2, p1), p1)
			f.LdM(b, ir.Bool, p, f.Const(b, 0, p1), p1)
		}, "bool byte 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := withStrings(func(f *ir.Func, b *ir.Block) {
				tt.body(f, b, f.Addr(b, f.NewMem("m", 16, 8, p1), p1))
			}, "x")
			var stderr bytes.Buffer
			_, err := Run(p, &stderr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

// TestMemoryAfterReturn checks that an object ends with its call: an
// address of it returned to the caller no longer reads.
func TestMemoryAfterReturn(t *testing.T) {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	f := p.NewFunc("f", "p.go", p1)
	f.Results = []ir.Type{ir.Ptr}
	b := f.Entry()
	a := f.Addr(b, f.NewMem("m", 8, 8, p1), p1)
	f.StM(b, a, f.Const(b, 0, p1), f.Const(b, 1, p1), p1)
	f.Ret(b, p1, a)
	b = main.Entry()
	main.LdM(b, ir.I64, main.Call(b, f, nil, p1), main.Const(b, 0, p1), p1)
	main.Ret(b, p1)
	if _, err := Run(p, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not a memory object in existence") {
		t.Errorf("got %v", err)
	}
}

// TestMemoryOverflow checks that local objects beyond MaxMemory are a stack
// overflow, as on the native stack, and that a returned call's objects no
// longer count.
func TestMemoryOverflow(t *testing.T) {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	f := p.NewFunc("f", "p.go", p1)
	b := f.Entry()
	f.Addr(b, f.NewMem("m", MaxMemory/2, 8, p1), p1)
	f.Ret(b, p1)
	b = main.Entry()
	for range 4 {
		main.Call(b, f, nil, p1) // each returns, so they fit
	}
	main.Println(b, []*ir.Instr{main.Const(b, 1, p1)}, p1)
	main.Addr(b, main.NewMem("big", MaxMemory/2+8, 8, p1), p1)
	main.Call(b, f, nil, p1) // this one does not fit
	main.Ret(b, p1)
	var stderr bytes.Buffer
	code, err := Run(p, &stderr)
	if err != nil || code != 2 || stderr.String() != "1\n"+stackOverflow {
		t.Errorf("exit %d, %v, stderr %q", code, err, stderr.String())
	}
}

// withStrings is program with the given strings.
func withStrings(body func(f *ir.Func, b *ir.Block), strs ...string) *ir.Program {
	p := program(body)
	p.Strings = strs
	return p
}
