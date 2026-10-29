package amd64

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// The tests in this file check the sized integer types of increment 4.4
// against Go's own arithmetic: the expected output is computed by the test
// with typed Go operations, and both the interpreter and the native
// executable must produce it exactly.

type integer interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// edges returns boundary values of T: 0, 1, 2, its maximum, its minimum,
// and their neighbors, and -1 converted to T.
func edges[T integer]() []T {
	var zero T
	m := ^zero // all ones: -1 if signed, the maximum if not
	vals := []T{0, 1, 2, 7, m, m - 1}
	if m < zero {
		// Signed: m is -1; the maximum has every bit but the top.
		w := 0
		for x := ^zero; x != 0; x <<= 1 {
			w++
		}
		max := T(1)<<(w-1) - 1
		vals = append(vals, max, max-1, -max-1, -max)
	} else {
		vals = append(vals, m>>1, m>>1+1)
	}
	return vals
}

// table returns the IR to print, for every pair of edge values x and y of
// type t, x+y x-y x*y x&y x|y x^y x&^y -x ^x and the six comparisons, and
// the same text computed in Go.
func table[T integer](f *ir.Func, b *ir.Block, t ir.Type) string {
	var want strings.Builder
	vals := edges[T]()
	for _, x := range vals {
		for _, y := range vals {
			cx := f.TypedConst(b, t, int64(x), p1)
			cy := f.TypedConst(b, t, int64(y), p1)
			var args []*ir.Instr
			for _, op := range []ir.Op{ir.Add, ir.Sub, ir.Mul, ir.And, ir.Or, ir.Xor, ir.AndNot} {
				args = append(args, f.Binary(b, op, cx, cy, p1))
			}
			args = append(args, f.Neg(b, cx, p1), f.Com(b, cx, p1))
			for _, op := range []ir.Op{ir.Eq, ir.Ne, ir.Lt, ir.Le, ir.Gt, ir.Ge} {
				args = append(args, f.Compare(b, op, cx, cy, p1))
			}
			f.Println(b, args, p1)
			fmt.Fprintln(&want, x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, x == y, x != y, x < y, x <= y, x > y, x >= y)
		}
	}
	return want.String()
}

var allIntegers = []ir.Type{ir.I8, ir.I16, ir.I32, ir.I64, ir.U8, ir.U16, ir.U32, ir.U64}

// goConv converts v, the canonical form of a value, to t with Go's own
// conversions, returning the result's canonical form.
func goConv(v int64, t ir.Type) int64 {
	switch t {
	case ir.I8:
		return int64(int8(v))
	case ir.I16:
		return int64(int16(v))
	case ir.I32:
		return int64(int32(v))
	case ir.U8:
		return int64(uint8(v))
	case ir.U16:
		return int64(uint16(v))
	case ir.U32:
		return int64(uint32(v))
	}
	return v
}

// format prints a canonical value of t as println does.
func format(v int64, t ir.Type) string {
	if t.Signed() {
		return fmt.Sprint(v)
	}
	return fmt.Sprint(uint64(v))
}

// runBoth runs prog in the interpreter and natively and requires both to
// print want and exit 0.
func runBoth(t *testing.T, prog *ir.Program, want string) {
	t.Helper()
	var got bytes.Buffer
	if code, err := interp.Run(prog, &got); err != nil || code != 0 || got.String() != want {
		t.Errorf("interpreter: exit %d, %v, output differs from Go's:\n%s", code, err, diffLine(got.String(), want))
	}
	asm, err := Emit(prog)
	if err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || out != want {
		t.Errorf("native: exit %d, output differs from Go's:\n%s", code, diffLine(out, want))
	}
}

// diffLine reports the first line where got and want differ.
func diffLine(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range min(len(g), len(w)) {
		if g[i] != w[i] {
			return fmt.Sprintf("line %d:\n got  %q\n want %q", i+1, g[i], w[i])
		}
	}
	return fmt.Sprintf("got %d lines, want %d", len(g), len(w))
}

// TestIntegerTypes checks every operation of every integer type on its
// edge values, natively and in the interpreter, against Go.
func TestIntegerTypes(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	want := table[int8](f, b, ir.I8) + table[int16](f, b, ir.I16) + table[int32](f, b, ir.I32) + table[int64](f, b, ir.I64) +
		table[uint8](f, b, ir.U8) + table[uint16](f, b, ir.U16) + table[uint32](f, b, ir.U32) + table[uint64](f, b, ir.U64)
	f.Ret(b, p1)
	runBoth(t, p, want)
}

// TestConversions converts boundary values of every integer type to every
// integer type, and prints with print, which has no separators.
func TestConversions(t *testing.T) {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	b := f.Entry()
	var want strings.Builder
	for _, from := range allIntegers {
		for _, v := range []int64{0, 1, -1, 127, 128, 255, 256, -129, 32767, 32768, 65535, math.MaxInt32, math.MinInt32, math.MaxUint32, math.MaxInt64, math.MinInt64} {
			v = goConv(v, from)
			x := f.TypedConst(b, from, v, p1)
			var args []*ir.Instr
			var line []string
			for _, to := range allIntegers {
				args = append(args, f.Conv(b, to, x, p1))
				line = append(line, format(goConv(v, to), to))
			}
			f.Println(b, args, p1)
			fmt.Fprintln(&want, strings.Join(line, " "))
		}
	}
	// print writes its operands, of any type, with nothing between them.
	f.Print(b, []*ir.Instr{f.BoolConst(b, true, p1), f.TypedConst(b, ir.U64, -1, p1), f.TypedConst(b, ir.I8, -128, p1)}, p1)
	f.Print(b, nil, p1)
	f.Println(b, []*ir.Instr{f.TypedConst(b, ir.U8, 255, p1)}, p1)
	want.WriteString("true18446744073709551615-128255\n")
	f.Ret(b, p1)
	runBoth(t, p, want.String())
}
