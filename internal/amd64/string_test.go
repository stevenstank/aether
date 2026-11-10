package amd64

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// TestPanicIndex checks the index panic natively and in the interpreter for
// signed and unsigned indices at their extremes: gc's message (go1.26.1,
// D-033), with no length for a negative index, and exit status 2.
func TestPanicIndex(t *testing.T) {
	tests := []struct {
		t    ir.Type
		i, n int64
		want string
	}{
		{ir.I64, 3, 3, "index out of range [3] with length 3"},
		{ir.I64, 0, 0, "index out of range [0] with length 0"},
		{ir.I64, -1, 3, "index out of range [-1]"},
		{ir.I64, math.MinInt64, 3, "index out of range [-9223372036854775808]"},
		{ir.I64, math.MaxInt64, 1 << 40, "index out of range [9223372036854775807] with length 1099511627776"},
		{ir.I8, -128, 5, "index out of range [-128]"},
		{ir.U8, 200, 3, "index out of range [200] with length 3"},
		{ir.U64, -1, 2, "index out of range [18446744073709551615] with length 2"},
		{ir.U64, math.MinInt64, 3, "index out of range [9223372036854775808] with length 3"},
		{ir.U32, 4294967295, 7, "index out of range [4294967295] with length 7"},
	}
	for _, tt := range tests {
		if got := ir.IndexMessage(tt.i, tt.t.Signed(), tt.n); got != tt.want {
			t.Errorf("IndexMessage(%d, %s, %d) = %q, want %q", tt.i, tt.t, tt.n, got, tt.want)
		}
		p := &ir.Program{}
		f := p.NewFunc("main", "p.go", p1)
		b := f.Entry()
		f.Print(b, []*ir.Instr{f.Const(b, 7, p1)}, p1)
		f.Panic(b, ir.PanicIndex, p1, f.TypedConst(b, tt.t, tt.t.Canonical(tt.i), p1), f.Const(b, tt.n, p1))
		want := "7panic: runtime error: " + tt.want + "\n"
		var got bytes.Buffer
		if code, err := interp.Run(p, &got); err != nil || code != 2 || got.String() != want {
			t.Errorf("interpreter: exit %d, %v, stderr %q; want exit 2, %q", code, err, got.String(), want)
		}
		asm, err := Emit(p)
		if err != nil {
			t.Fatal(err)
		}
		if out, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 2 || out != want {
			t.Errorf("native: exit %d, stderr %q; want exit 2, %q", code, out, want)
		}
	}
}

// stringProgram prints and indexes strings with every byte value, an
// embedded NUL, and multibyte UTF-8, held in slots and globals, passed to a
// function of seven string arguments (so that ABI-R1 passes some on the
// stack), and returned as a pair.
func stringProgram() *ir.Program {
	p := &ir.Program{}
	var all strings.Builder
	for c := range 256 {
		all.WriteByte(byte(c))
	}
	strs := []string{"a", "héllo, 世界", "x\x00y", all.String(), "\xff\xfe\n"}
	g := p.NewGlobal("s.ptr", ir.Ptr, "p.go", p1)
	gl := p.NewGlobal("s.len", ir.I64, "p.go", p1)
	pick := p.NewFunc("pick", "p.go", p1)
	for range 7 {
		pick.Params = append(pick.Params, ir.Ptr, ir.I64)
	}
	pick.Params = append(pick.Params, ir.I64)
	pick.Results = []ir.Type{ir.Ptr, ir.I64, ir.U8}
	{
		b := pick.Entry()
		var ps []*ir.Instr
		for i := range pick.Params {
			ps = append(ps, pick.Param(b, i, p1))
		}
		// Print every argument, then return the sixth string and the byte
		// at the given offset of the fourth.
		pick.Println(b, ps[:14], p1)
		pick.Ret(b, p1, ps[10], ps[11], pick.LoadB(b, ps[6], ps[14], p1))
	}
	main := p.NewFunc("main", "p.go", p1)
	b := main.Entry()
	val := func(s string) (*ir.Instr, *ir.Instr) {
		if s == "" {
			return main.TypedConst(b, ir.Ptr, 0, p1), main.Const(b, 0, p1)
		}
		ptr := main.NewSlot("p", ir.Ptr, p1)
		main.Store(b, ptr, main.Str(b, p.Intern(s), p1), p1)
		return main.Load(b, ptr, p1), main.Const(b, int64(len(s)), p1)
	}
	for _, s := range strs {
		ptr, n := val(s)
		var bytes []*ir.Instr
		for _, i := range []int64{0, int64(len(s)) - 1, int64(len(s)) / 2} {
			bytes = append(bytes, main.LoadB(b, ptr, main.Const(b, i, p1), p1))
		}
		main.Print(b, append([]*ir.Instr{ptr, n}, bytes...), p1)
		main.Println(b, append([]*ir.Instr{ptr, n, ptr, n}, bytes...), p1)
	}
	gp, gn := val("héllo, 世界")
	main.GStore(b, g, gp, p1)
	main.GStore(b, gl, gn, p1)
	var args []*ir.Instr
	for i := range 7 {
		ptr, n := val(append([]string{""}, strs...)[i%6])
		if i == 3 {
			ptr, n = main.GLoad(b, g, p1), main.GLoad(b, gl, p1)
		}
		args = append(args, ptr, n)
	}
	call := main.Call(b, pick, append(args, main.Const(b, 4, p1)), p1)
	r := []*ir.Instr{main.Extract(b, call, 0, p1), main.Extract(b, call, 1, p1), main.Extract(b, call, 2, p1)}
	e0, e1 := val("")
	main.Println(b, append(r, e0, e1), p1)
	main.Ret(b, p1)
	return p
}

// TestNativeStrings runs stringProgram in the interpreter, with the
// slot-based backend, and with aether build's backend, and requires identical
// output, which must hold every byte of the strings exactly.
func TestNativeStrings(t *testing.T) {
	var want bytes.Buffer
	if code, err := interp.Run(stringProgram(), &want); err != nil || code != 0 {
		t.Fatalf("interpreter: exit %d, %v", code, err)
	}
	for _, s := range []string{"x\x00y1201210x\x00y x\x00y 120 121 0\n", "héllo, 世界10414032héllo, 世界 héllo, 世界 104 140 32\n",
		"\xff\xfe\n25510254\xff\xfe\n \xff\xfe\n 255 10 254\n"} {
		if !strings.Contains(want.String(), s) {
			t.Errorf("interpreter output lacks %q", s)
		}
	}
	slots, err := Emit(stringProgram())
	if err != nil {
		t.Fatal(err)
	}
	prog := stringProgram()
	for _, f := range prog.Funcs {
		if err := ir.Promote(f); err != nil {
			t.Fatal(err)
		}
	}
	regs, err := EmitWith(prog, EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true, Coalesce: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, asm := range map[string][]byte{"slots": slots, "registers": regs} {
		if got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || got != want.String() {
			t.Errorf("%s: exit %d; native output %q differs from the interpreter's %q", name, code, got, want.String())
		}
	}
}
