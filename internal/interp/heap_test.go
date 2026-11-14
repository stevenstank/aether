package interp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// TestHeap checks heap objects (D-035): zero, distinct, alive after the call
// that allocated them returns, addressable inside by padd, and compared as
// addresses, with every zero-size allocation the same object.
func TestHeap(t *testing.T) {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	mk := p.NewFunc("mk", "p.go", p1)
	mk.Results = []ir.Type{ir.Ptr}
	{
		b := mk.Entry()
		o := mk.Alloc(b, mk.Const(b, 24, p1), p1)
		mk.StM(b, mk.PAdd(b, o, mk.Const(b, 16, p1), p1), mk.Const(b, 0, p1), mk.TypedConst(b, ir.I32, -9, p1), p1)
		mk.Ret(b, p1, o)
	}
	b := main.Entry()
	c := func(v int64) *ir.Instr { return main.Const(b, v, p1) }
	x, y := main.Call(b, mk, nil, p1), main.Call(b, mk, nil, p1)
	mid := main.PAdd(b, x, c(8), p1)
	main.StM(b, mid, c(0), c(77), p1)
	back := main.PAdd(b, mid, c(-8), p1)
	z1, z2 := main.Alloc(b, c(0), p1), main.Alloc(b, c(0), p1)
	end := main.PAdd(b, x, c(24), p1)
	main.Println(b, []*ir.Instr{
		main.LdM(b, ir.I64, x, c(0), p1),                              // zero
		main.LdM(b, ir.I64, x, c(8), p1),                              // 77, through mid
		main.LdM(b, ir.I32, y, c(16), p1),                             // -9, alive after mk returned
		main.LdM(b, ir.I32, mid, c(8), p1),                            // -9 again, from inside
		main.Compare(b, ir.Eq, x, y, p1),                              // distinct objects
		main.Compare(b, ir.Eq, back, x, p1),                           // the same address
		main.Compare(b, ir.Ne, mid, x, p1),                            // inside, not the start
		main.Compare(b, ir.Eq, z1, z2, p1),                            // one zero-size object
		main.Compare(b, ir.Eq, end, main.PAdd(b, mid, c(16), p1), p1), // just past the end
		main.Compare(b, ir.Eq, x, main.TypedConst(b, ir.Ptr, 0, p1), p1),
	}, p1)
	main.Ret(b, p1)
	if got, want := output(t, p), "0 77 -9 -9 false true true true true false\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestHeapErrors checks what the interpreter reports rather than executes:
// a padd leaving its object and a load past an interior pointer's object;
// and what a program does: the nil panic, and running out of heap.
func TestHeapErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		body func(f *ir.Func, b *ir.Block, o *ir.Instr)
		want string
	}{
		{"padd past the end", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			f.PAdd(b, o, f.Const(b, 17, nil1), nil1)
		}, "offset 17 from offset 0 leaves an object of 16 bytes"},
		{"padd before the start", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			f.PAdd(b, f.PAdd(b, o, f.Const(b, 8, nil1), nil1), f.Const(b, -9, nil1), nil1)
		}, "offset -9 from offset 8 leaves an object of 16 bytes"},
		{"load past an interior pointer", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			f.LdM(b, ir.I64, f.PAdd(b, o, f.Const(b, 8, nil1), nil1), f.Const(b, 8, nil1), nil1)
		}, "i64 at offset 16 of an object of 16 bytes"},
		{"nil", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			f.LdM(b, ir.I64, f.TypedConst(b, ir.Ptr, 0, nil1), f.Const(b, 0, nil1), nil1)
		}, "address 0x0 is not a memory object in existence"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := program(func(f *ir.Func, b *ir.Block) {
				tt.body(f, b, f.Alloc(b, f.Const(b, 16, p1), p1))
			})
			if _, err := Run(p, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want an error containing %q", err, tt.want)
			}
		})
	}
	var stderr bytes.Buffer
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	f.Println(b, []*ir.Instr{f.Const(b, 1, p1)}, p1)
	f.Panic(b, ir.PanicNil, p1)
	if code, err := Run(p, &stderr); err != nil || code != 2 || stderr.String() != "1\npanic: runtime error: invalid memory address or nil pointer dereference\n" {
		t.Errorf("nil panic: exit %d, %v, %q", code, err, stderr.String())
	}
	// Running out of heap is the program's fatal error, before anything is
	// allocated; a large object that would fit is beyond the interpreter.
	stderr.Reset()
	p = program(func(f *ir.Func, b *ir.Block) {
		f.Alloc(b, f.Const(b, 1<<20, p1), p1)
		f.Println(b, []*ir.Instr{f.Const(b, 1, p1)}, p1)
		f.Alloc(b, f.Const(b, HeapLimit-(1<<20)+1, p1), p1) // 8 bytes too many, rounded
		f.Println(b, []*ir.Instr{f.Const(b, 2, p1)}, p1)
	})
	if code, err := Run(p, &stderr); err != nil || code != 2 || stderr.String() != "1\n"+outOfMemory {
		t.Errorf("out of memory: exit %d, %v, %q", code, err, stderr.String())
	}
	p = program(func(f *ir.Func, b *ir.Block) { f.Alloc(b, f.Const(b, ir.MaxMemSize+1, p1), p1) })
	if _, err := Run(p, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "exceeds the interpreter's limit") {
		t.Errorf("large object: %v", err)
	}
}

var nil1 = p1
