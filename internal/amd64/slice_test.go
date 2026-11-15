package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// moveProgram fills a heap array of 1000 int64s with 0 to 999 in a loop,
// then moves overlapping ranges of it in both directions, 13 bytes at an odd
// offset to another object, and nothing between nil pointers, with a value
// live across the moves, and prints a sum, some elements, and bytes.
func moveProgram() *ir.Program {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b0 := f.Entry()
	c := func(b *ir.Block, v int64) *ir.Instr { return f.Const(b, v, p1) }
	o := f.NewSlot("o", ir.Ptr, p1)
	f.Store(b0, o, f.Alloc(b0, c(b0, 8000), p1), p1)
	i := f.NewSlot("i", ir.I64, p1)
	loop := func(from *ir.Block, body func(b *ir.Block, iv *ir.Instr)) *ir.Block {
		f.Store(from, i, c(from, 0), p1)
		head, b, done := f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Jump(from, head, p1)
		f.Branch(head, f.Compare(head, ir.Lt, f.Load(head, i, p1), c(head, 1000), p1), b, done, p1)
		iv := f.Load(b, i, p1)
		body(b, iv)
		f.Store(b, i, f.Binary(b, ir.Add, iv, c(b, 1), p1), p1)
		f.Jump(b, head, p1)
		return done
	}
	b := loop(b0, func(b *ir.Block, iv *ir.Instr) {
		f.StM(b, f.Load(b, o, p1), f.Binary(b, ir.Mul, iv, c(b, 8), p1), iv, p1)
	})
	base := f.Load(b, o, p1)
	live := f.Binary(b, ir.Mul, f.LdM(b, ir.I64, base, c(b, 8*999), p1), c(b, 3), p1)
	f.Move(b, f.PAdd(b, base, c(b, 8), p1), base, c(b, 8*999), p1)  // a[1:] = a[:999]
	f.Move(b, base, f.PAdd(b, base, c(b, 16), p1), c(b, 8*500), p1) // a[:500] = a[2:502]
	q := f.Alloc(b, c(b, 13), p1)
	f.Move(b, q, f.PAdd(b, base, c(b, 3), p1), c(b, 13), p1)
	nilp := f.TypedConst(b, ir.Ptr, 0, p1)
	f.Move(b, nilp, nilp, c(b, 0), p1)
	f.Println(b, []*ir.Instr{live, f.LdM(b, ir.U8, q, c(b, 0), p1), f.LdM(b, ir.U8, q, c(b, 5), p1), f.LdM(b, ir.U8, q, c(b, 12), p1)}, p1)
	sum := f.NewSlot("sum", ir.I64, p1)
	f.Store(b, sum, c(b, 0), p1)
	b = loop(b, func(b *ir.Block, iv *ir.Instr) {
		x := f.LdM(b, ir.I64, f.Load(b, o, p1), f.Binary(b, ir.Mul, iv, c(b, 8), p1), p1)
		// A weighted sum, so that a misplaced element changes it.
		f.Store(b, sum, f.Binary(b, ir.Add, f.Load(b, sum, p1), f.Binary(b, ir.Mul, x, iv, p1), p1), p1)
	})
	base = f.Load(b, o, p1)
	f.Println(b, []*ir.Instr{f.Load(b, sum, p1), f.LdM(b, ir.I64, base, c(b, 0), p1),
		f.LdM(b, ir.I64, base, c(b, 8*499), p1), f.LdM(b, ir.I64, base, c(b, 8*500), p1), f.LdM(b, ir.I64, base, c(b, 8*999), p1)}, p1)
	f.Ret(b, p1)
	return p
}

// moveWant computes moveProgram's output with Go's copy, which allows
// overlap as move does.
func moveWant() string {
	a := make([]byte, 8000)
	for i := range 1000 {
		binary.LittleEndian.PutUint64(a[8*i:], uint64(i))
	}
	live := 3 * 999
	copy(a[8:], a[:8*999])
	copy(a, a[16:16+8*500])
	q := append([]byte(nil), a[3:16]...)
	el := func(i int) int64 { return int64(binary.LittleEndian.Uint64(a[8*i:])) }
	var sum int64
	for i := range 1000 {
		sum += el(i) * int64(i)
	}
	return fmt.Sprintf("%d %d %d %d\n%d %d %d %d %d\n", live, q[0], q[5], q[12], sum, el(0), el(499), el(500), el(999))
}

// TestNativeMove runs moveProgram in the interpreter and natively with every
// backend variant, against Go's copy.
func TestNativeMove(t *testing.T) {
	want := moveWant()
	var got bytes.Buffer
	if code, err := interp.Run(moveProgram(), &got); err != nil || code != 0 || got.String() != want {
		t.Fatalf("interpreter: exit %d, %v, output %q, want %q", code, err, got.String(), want)
	}
	for _, v := range memVariants {
		prog := moveProgram()
		if v.promote {
			for _, f := range prog.Funcs {
				if err := ir.Promote(f); err != nil {
					t.Fatal(err)
				}
			}
		}
		asm, err := EmitWith(prog, v.opts)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		if out, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || out != want {
			t.Errorf("%s: exit %d, output %q, want %q", v.name, code, out, want)
		}
	}
}

// TestNativeBounds checks every slice bounds error natively against the
// interpreter, whose messages TestBoundsMessage checks against gc's: x
// signed and positive, signed and negative, and unsigned with the top bit
// set, after earlier output.
func TestNativeBounds(t *testing.T) {
	for kind := ir.PanicSliceAlen; kind <= ir.PanicSlice3C; kind++ {
		for _, x := range []struct {
			t ir.Type
			v int64
		}{{ir.I64, 9}, {ir.I16, -3}, {ir.U64, -2}} {
			p := &ir.Program{}
			f := p.NewFunc("main", "p.go", p1)
			b := f.Entry()
			f.Println(b, []*ir.Instr{f.Const(b, 1, p1)}, p1)
			f.Panic(b, kind, p1, f.TypedConst(b, x.t, x.v, p1), f.Const(b, 4, p1))
			want := "1\npanic: runtime error: " + ir.BoundsMessage(kind, x.v, x.t.Signed(), 4) + "\n"
			var got bytes.Buffer
			if code, err := interp.Run(p, &got); err != nil || code != 2 || got.String() != want {
				t.Errorf("interpreter: exit %d, %v, %q, want %q", code, err, got.String(), want)
			}
			asm, err := EmitWith(p, memVariants[len(memVariants)-1].opts)
			if err != nil {
				t.Fatal(err)
			}
			if out, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 2 || out != want {
				t.Errorf("native: exit %d, %q, want %q", code, out, want)
			}
		}
	}
}
