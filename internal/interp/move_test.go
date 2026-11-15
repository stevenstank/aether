package interp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// TestMove checks move (D-036): between objects, overlapping in both
// directions as if through a temporary, of 0 bytes even at nil, and that
// the bytes it writes count as written.
func TestMove(t *testing.T) {
	p := program(func(f *ir.Func, b *ir.Block) {
		c := func(v int64) *ir.Instr { return f.Const(b, v, p1) }
		a, z := f.Alloc(b, c(40), p1), f.Alloc(b, c(16), p1)
		for i := range int64(5) {
			f.StM(b, a, c(8*i), c(i+1), p1) // 1 2 3 4 5
		}
		f.Move(b, z, a, c(16), p1)                       // z = 1 2
		f.Move(b, f.PAdd(b, a, c(8), p1), a, c(32), p1)  // a = 1 1 2 3 4, forward overlap
		f.Move(b, a, f.PAdd(b, a, c(16), p1), c(24), p1) // a = 2 3 4 3 4, backward overlap
		nilp := f.TypedConst(b, ir.Ptr, 0, p1)
		f.Move(b, nilp, nilp, c(0), p1)                    // nothing
		f.Move(b, f.PAdd(b, a, c(40), p1), nilp, c(0), p1) // nothing, at the end
		var vs []*ir.Instr
		for i := range int64(5) {
			vs = append(vs, f.LdM(b, ir.I64, a, c(8*i), p1))
		}
		vs = append(vs, f.LdM(b, ir.I64, z, c(0), p1), f.LdM(b, ir.I64, z, c(8), p1))
		f.Println(b, vs, p1)
	})
	if got, want := output(t, p), "2 3 4 3 4 1 2\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestMoveErrors checks the precondition of move.
func TestMoveErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		body func(f *ir.Func, b *ir.Block, o *ir.Instr)
		want string
	}{
		{"past the source", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			f.Move(b, o, f.PAdd(b, o, f.Const(b, 8, p1), p1), f.Const(b, 9, p1), p1)
		}, "move of 9 bytes at offset 8 of an object of 16 bytes"},
		{"nil with bytes", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			f.Move(b, f.TypedConst(b, ir.Ptr, 0, p1), o, f.Const(b, 8, p1), p1)
		}, "address 0x0 is not a memory object in existence"},
		{"negative", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			f.Move(b, o, o, f.Const(b, -1, p1), p1)
		}, "move of -1 bytes"},
		{"unwritten source", func(f *ir.Func, b *ir.Block, o *ir.Instr) {
			m := f.NewMem("m", 8, 8, p1)
			f.Move(b, o, f.Addr(b, m, p1), f.Const(b, 8, p1), p1)
		}, "move reads byte 0, never written"},
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
}

// TestPanicSlice checks that the slice bounds and makeslice errors write
// gc's messages and exit 2.
func TestPanicSlice(t *testing.T) {
	for _, tt := range []struct {
		kind int
		x    int64
		xt   ir.Type
		y    int64
		want string
	}{
		{ir.PanicSliceAcap, 6, ir.I64, 5, "slice bounds out of range [:6] with capacity 5"},
		{ir.PanicSliceB, 4, ir.U8, 3, "slice bounds out of range [4:3]"},
		{ir.PanicSlice3C, -1, ir.I32, 1, "slice bounds out of range [-1::]"},
		{ir.PanicSlice3Alen, -1, ir.U64, 4, "slice bounds out of range [::18446744073709551615] with length 4"},
		{ir.PanicMakeCap, 0, ir.NoType, 0, "makeslice: cap out of range"},
		{ir.PanicGrowLen, 0, ir.NoType, 0, "growslice: len out of range"},
	} {
		p := &ir.Program{}
		f := p.NewFunc("main", "p.go", p1)
		b := f.Entry()
		var args []*ir.Instr
		if tt.xt != ir.NoType {
			args = []*ir.Instr{f.TypedConst(b, tt.xt, tt.x, p1), f.Const(b, tt.y, p1)}
		}
		f.Panic(b, tt.kind, p1, args...)
		var stderr bytes.Buffer
		if code, err := Run(p, &stderr); err != nil || code != 2 || stderr.String() != "panic: runtime error: "+tt.want+"\n" {
			t.Errorf("%s: exit %d, %v, %q", tt.want, code, err, stderr.String())
		}
	}
}
