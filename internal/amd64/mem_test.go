package amd64

import (
	"bytes"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// memProgram exercises memory objects (D-034): a load and store of every
// memory type, narrow reads of a wide store at every offset, several objects
// of mixed sizes and alignments in one frame, global objects with their zero
// start, recursion with an object per call, addresses of the caller's objects
// passed to callees (eight at once, so that ABI-R1 passes some on the stack),
// a string's words kept in memory, and the last bytes of a large object. Byte
// offsets are both constants and values computed at run time, so that the
// selected backend uses both displacements and index registers.
func memProgram() *ir.Program {
	p := &ir.Program{}
	ga := p.NewMem("ga", 24, 8, "p.go", p1)
	gb := p.NewMem("gb", 3, 1, "p.go", p1)
	gz := p.NewMem("gz", 0, 1, "p.go", p1)
	main := p.NewFunc("main", "p.go", p1)
	init := p.NewFunc("init", "p.go", p1)
	p.Init = init
	{
		b := init.Entry()
		init.StM(b, init.Addr(b, ga, p1), init.Const(b, 0, p1), init.Const(b, 7, p1), p1)
		init.Ret(b, p1)
	}

	// fill(p, n, k) stores the byte k+i at offset i of the object at p, for
	// i < n.
	fill := p.NewFunc("fill", "p.go", p1)
	fill.Params = []ir.Type{ir.Ptr, ir.I64, ir.I64}
	{
		i := fill.NewSlot("i", ir.I64, p1)
		b0, head, body, exit := fill.Entry(), fill.NewBlock(), fill.NewBlock(), fill.NewBlock()
		ptr, n, k := fill.NewSlot("p", ir.Ptr, p1), fill.NewSlot("n", ir.I64, p1), fill.NewSlot("k", ir.I64, p1)
		fill.Store(b0, ptr, fill.Param(b0, 0, p1), p1)
		fill.Store(b0, n, fill.Param(b0, 1, p1), p1)
		fill.Store(b0, k, fill.Param(b0, 2, p1), p1)
		fill.Store(b0, i, fill.Const(b0, 0, p1), p1)
		fill.Jump(b0, head, p1)
		fill.Branch(head, fill.Compare(head, ir.Lt, fill.Load(head, i, p1), fill.Load(head, n, p1), p1), body, exit, p1)
		iv := fill.Load(body, i, p1)
		v := fill.Conv(body, ir.U8, fill.Binary(body, ir.Add, fill.Load(body, k, p1), iv, p1), p1)
		fill.StM(body, fill.Load(body, ptr, p1), iv, v, p1)
		fill.Store(body, i, fill.Binary(body, ir.Add, iv, fill.Const(body, 1, p1), p1), p1)
		fill.Jump(body, head, p1)
		fill.Ret(exit, p1)
	}

	// dump(p, n) prints the n bytes of the object at p as one line.
	dump := p.NewFunc("dump", "p.go", p1)
	dump.Params = []ir.Type{ir.Ptr, ir.I64}
	{
		i := dump.NewSlot("i", ir.I64, p1)
		ptr, n := dump.NewSlot("p", ir.Ptr, p1), dump.NewSlot("n", ir.I64, p1)
		b0, head, body, exit := dump.Entry(), dump.NewBlock(), dump.NewBlock(), dump.NewBlock()
		dump.Store(b0, ptr, dump.Param(b0, 0, p1), p1)
		dump.Store(b0, n, dump.Param(b0, 1, p1), p1)
		dump.Store(b0, i, dump.Const(b0, 0, p1), p1)
		dump.Jump(b0, head, p1)
		dump.Branch(head, dump.Compare(head, ir.Lt, dump.Load(head, i, p1), dump.Load(head, n, p1), p1), body, exit, p1)
		iv := dump.Load(body, i, p1)
		dump.Print(body, []*ir.Instr{dump.LdM(body, ir.U8, dump.Load(body, ptr, p1), iv, p1), dump.Str(body, p.Intern(" "), p1), dump.Const(body, 1, p1)}, p1)
		dump.Store(body, i, dump.Binary(body, ir.Add, iv, dump.Const(body, 1, p1), p1), p1)
		dump.Jump(body, head, p1)
		dump.Println(exit, nil, p1)
		dump.Ret(exit, p1)
	}

	// rec(n) keeps n and 3n in its own object across a call of rec(n-1),
	// and has bump add 1000 to the first through the object's address.
	rec := p.NewFunc("rec", "p.go", p1)
	bump := p.NewFunc("bump", "p.go", p1)
	bump.Params = []ir.Type{ir.Ptr}
	{
		b := bump.Entry()
		a, z := bump.Param(b, 0, p1), bump.Const(b, 0, p1)
		bump.StM(b, a, z, bump.Binary(b, ir.Add, bump.LdM(b, ir.I64, a, z, p1), bump.Const(b, 1000, p1), p1), p1)
		bump.Ret(b, p1)
	}
	rec.Params = []ir.Type{ir.I64}
	{
		m := rec.NewMem("r", 16, 8, p1)
		b0, more, done := rec.Entry(), rec.NewBlock(), rec.NewBlock()
		n := rec.Param(b0, 0, p1)
		a := rec.Addr(b0, m, p1)
		rec.StM(b0, a, rec.Const(b0, 0, p1), n, p1)
		rec.StM(b0, a, rec.Const(b0, 8, p1), rec.Conv(b0, ir.I32, rec.Binary(b0, ir.Mul, n, rec.Const(b0, -3, p1), p1), p1), p1)
		rec.Branch(b0, rec.Compare(b0, ir.Gt, n, rec.Const(b0, 0, p1), p1), more, done, p1)
		an := rec.Addr(more, m, p1)
		rec.Call(more, rec, []*ir.Instr{rec.Binary(more, ir.Sub, rec.LdM(more, ir.I64, an, rec.Const(more, 0, p1), p1), rec.Const(more, 1, p1), p1)}, p1)
		rec.Call(more, bump, []*ir.Instr{rec.Addr(more, m, p1)}, p1)
		rec.Jump(more, done, p1)
		ad := rec.Addr(done, m, p1)
		rec.Println(done, []*ir.Instr{rec.LdM(done, ir.I64, ad, rec.Const(done, 0, p1), p1), rec.LdM(done, ir.I32, ad, rec.Const(done, 8, p1), p1)}, p1)
		rec.Ret(done, p1)
	}

	// sum8 adds the i64 at offset 0 of each of eight objects.
	sum8 := p.NewFunc("sum8", "p.go", p1)
	sum8.Results = []ir.Type{ir.I64}
	{
		b := sum8.Entry()
		var ps []*ir.Instr
		for i := range 8 {
			sum8.Params = append(sum8.Params, ir.Ptr)
			ps = append(ps, sum8.Param(b, i, p1))
		}
		var acc *ir.Instr
		for _, a := range ps {
			v := sum8.LdM(b, ir.I64, a, sum8.Const(b, 0, p1), p1)
			if acc == nil {
				acc = v
			} else {
				acc = sum8.Binary(b, ir.Add, acc, v, p1)
			}
		}
		sum8.Ret(b, p1, acc)
	}

	// setg writes the global objects.
	setg := p.NewFunc("setg", "p.go", p1)
	{
		b := setg.Entry()
		setg.StM(b, setg.Addr(b, gb, p1), setg.Const(b, 2, p1), setg.TypedConst(b, ir.U8, 0xff, p1), p1)
		setg.StM(b, setg.Addr(b, ga, p1), setg.Const(b, 8, p1), setg.FloatConst(b, ir.F64, -2.5, p1), p1)
		setg.Ret(b, p1)
	}

	b := main.Entry()
	c := func(v int64) *ir.Instr { return main.Const(b, v, p1) }
	println := func(vs ...*ir.Instr) { main.Println(b, vs, p1) }

	// Every type at its natural offsets, including the last ones of the
	// object.
	m0 := main.NewMem("m0", 64, 8, p1)
	vals := []struct {
		t   ir.Type
		v   int64
		off int64
	}{
		{ir.I8, -128, 0}, {ir.U8, 255, 1}, {ir.Bool, 1, 2}, {ir.I16, -32768, 4}, {ir.U16, 65535, 6},
		{ir.I32, math.MinInt32, 8}, {ir.U32, math.MaxUint32, 12}, {ir.F32, int64(math.Float32bits(-1.5)), 16},
		{ir.I64, math.MinInt64, 24}, {ir.U64, -1, 32}, {ir.F64, int64(math.Float64bits(0.1)), 40},
		{ir.I32, -7, 52}, {ir.I64, math.MaxInt64, 56}, {ir.Bool, 0, 63},
	}
	for _, v := range vals {
		x := main.TypedConst(b, v.t, v.v, p1)
		if v.t.IsFloat() {
			x = main.FloatConst(b, v.t, map[bool]float64{true: -1.5, false: 0.1}[v.t == ir.F32], p1)
		}
		main.StM(b, main.Addr(b, m0, p1), c(v.off), x, p1)
	}
	var loads []*ir.Instr
	for _, v := range vals {
		loads = append(loads, main.LdM(b, v.t, main.Addr(b, m0, p1), c(v.off), p1))
	}
	println(loads...)

	// Byte order and narrow reads: the bytes 0x11 ... 0x88 stored as one
	// i64, read back at every offset and width.
	main.StM(b, main.Addr(b, m0, p1), c(8), main.Const(b, -0x778899aabbccddef, p1), p1)
	loads = nil
	for off := int64(8); off < 16; off++ {
		loads = append(loads, main.LdM(b, ir.U8, main.Addr(b, m0, p1), c(off), p1), main.LdM(b, ir.I8, main.Addr(b, m0, p1), c(off), p1))
	}
	for off := int64(8); off < 16; off += 2 {
		loads = append(loads, main.LdM(b, ir.U16, main.Addr(b, m0, p1), c(off), p1), main.LdM(b, ir.I16, main.Addr(b, m0, p1), c(off), p1))
	}
	for off := int64(8); off < 16; off += 4 {
		loads = append(loads, main.LdM(b, ir.U32, main.Addr(b, m0, p1), c(off), p1), main.LdM(b, ir.I32, main.Addr(b, m0, p1), c(off), p1))
	}
	println(loads...)
	// A narrow store into the middle of the word leaves its other bytes.
	main.StM(b, main.Addr(b, m0, p1), c(10), main.TypedConst(b, ir.I16, -1, p1), p1)
	main.StM(b, main.Addr(b, m0, p1), c(15), main.TypedConst(b, ir.U8, 0, p1), p1)
	println(main.LdM(b, ir.U64, main.Addr(b, m0, p1), c(8), p1))

	// Objects of mixed sizes and alignments, each filled with its own
	// bytes, then all dumped: an overlap would show as a byte of another.
	sizes := []struct{ size, align int64 }{{1, 1}, {3, 1}, {2, 2}, {12, 4}, {8, 8}, {1, 1}, {0, 1}, {7, 1}, {16, 8}}
	var objs []*ir.Mem
	for i, s := range sizes {
		m := main.NewMem("o", s.size, s.align, p1)
		objs = append(objs, m)
		main.Call(b, fill, []*ir.Instr{main.Addr(b, m, p1), c(s.size), c(int64(16 * (i + 1)))}, p1)
	}
	for i, m := range objs {
		main.Call(b, dump, []*ir.Instr{main.Addr(b, m, p1), c(sizes[i].size)}, p1)
	}

	// Global objects: zero at startup, apart from Init's store; shared with
	// the function that writes them.
	main.Call(b, dump, []*ir.Instr{main.Addr(b, ga, p1), c(24)}, p1)
	main.Call(b, dump, []*ir.Instr{main.Addr(b, gb, p1), c(3)}, p1)
	main.Addr(b, gz, p1)
	main.Call(b, setg, nil, p1)
	println(main.LdM(b, ir.I64, main.Addr(b, ga, p1), c(0), p1), main.LdM(b, ir.F64, main.Addr(b, ga, p1), c(8), p1),
		main.LdM(b, ir.U8, main.Addr(b, gb, p1), c(2), p1), main.LdM(b, ir.I8, main.Addr(b, gb, p1), c(2), p1))

	// Recursion: each call's object survives the deeper calls.
	main.Call(b, rec, []*ir.Instr{c(4)}, p1)

	// Eight addresses of the caller's objects as arguments.
	var args []*ir.Instr
	for i := range 8 {
		m := main.NewMem("a", 8, 8, p1)
		main.StM(b, main.Addr(b, m, p1), c(0), c(int64(1)<<(5*i)), p1)
		args = append(args, main.Addr(b, m, p1))
	}
	println(main.Call(b, sum8, args, p1))

	// A string's words in memory.
	sm := main.NewMem("s", 16, 8, p1)
	main.StM(b, main.Addr(b, sm, p1), c(0), main.Str(b, p.Intern("héllo"), p1), p1)
	main.StM(b, main.Addr(b, sm, p1), c(8), c(6), p1)
	println(main.LdM(b, ir.Ptr, main.Addr(b, sm, p1), c(0), p1), main.LdM(b, ir.I64, main.Addr(b, sm, p1), c(8), p1))

	// The last bytes of a 1 MiB object, at a run-time offset.
	big := main.NewMem("big", 1<<20, 8, p1)
	main.Call(b, fill, []*ir.Instr{main.Addr(b, big, p1), c(1 << 20), c(3)}, p1)
	main.StM(b, main.Addr(b, big, p1), c(1<<20-16), c(-5), p1)
	end := main.Binary(b, ir.Sub, main.LdM(b, ir.I64, main.Addr(b, big, p1), c(1<<20-16), p1), c(-1<<20-4), p1)
	println(main.LdM(b, ir.U8, main.Addr(b, big, p1), c(0), p1), main.LdM(b, ir.U8, main.Addr(b, big, p1), end, p1),
		main.LdM(b, ir.U64, main.Addr(b, big, p1), c(1<<20-8), p1), end)
	main.Ret(b, p1)
	return p
}

// memVariants are the backends memory objects are checked with: the
// slot-based one of -O0, and the default one with each of its options added
// in turn, the last being aether build's.
var memVariants = []struct {
	name    string
	promote bool
	opts    EmitOptions
}{
	{"slots", false, EmitOptions{}},
	{"ssa", true, EmitOptions{}},
	{"registers", true, EmitOptions{Registers: true}},
	{"select", true, EmitOptions{Registers: true, Select: true}},
	{"default", true, EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true, Coalesce: true}},
}

// TestNativeMemory runs memProgram in the interpreter and natively with
// every variant, and requires identical output, which must hold the values
// worked out by hand below.
func TestNativeMemory(t *testing.T) {
	var want bytes.Buffer
	if code, err := interp.Run(memProgram(), &want); err != nil || code != 0 {
		t.Fatalf("interpreter: exit %d, %v", code, err)
	}
	for _, s := range []string{
		"-128 255 true -32768 65535 -2147483648 4294967295 -1.5 -9223372036854775808 18446744073709551615 0.1 -7 72057594037927935 false\n", // the bool at 63 is the i64's top byte
		// 0x8877665544332211, little-endian.
		"17 17 34 34 51 51 68 68 85 85 102 102 119 119 136 -120 8721 8721 17459 17459 26197 26197 34935 -30601 1144201745 1144201745 2289526357 -2005440939\n",
		"33608041781731857\n", // 0x00776655ffff2211
		"16 \n32 33 34 \n48 49 \n64 65 66 67 68 69 70 71 72 73 74 75 \n80 81 82 83 84 85 86 87 \n96 \n\n128 129 130 131 132 133 134 \n144 145 146 147 148 149 150 151 152 153 154 155 156 157 158 159 \n",
		"7 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 \n0 0 0 \n7 -2.5 255 -1\n",
		"0 0\n1001 -3\n1002 -6\n1003 -9\n1004 -12\n",
		"35468117025\n", // the sum of 1<<(5i), i < 8
		"héllo\n",
		"3 2 144397762547285243 1048575\n", // 0x020100fffefdfcfb
	} {
		if !strings.Contains(want.String(), s) {
			t.Errorf("interpreter output lacks %q", s)
		}
	}
	for _, v := range memVariants {
		prog := memProgram()
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
		if got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || got != want.String() {
			t.Errorf("%s: exit %d; native output differs from the interpreter's", v.name, code)
			g, w := strings.Split(got, "\n"), strings.Split(want.String(), "\n")
			for i := range min(len(g), len(w)) {
				if g[i] != w[i] {
					t.Errorf("%s: line %d: native %q, interpreter %q", v.name, i+1, g[i], w[i])
					break
				}
			}
		}
	}
}

// TestFrameMems checks the layout of local memory objects (D-034): each at an
// offset that is a multiple of its alignment, below the cells and above the
// outgoing area, overlapping nothing, in a frame that is still a multiple of
// 16. x86-64 reads misaligned data correctly, so only this test sees the
// alignment.
func TestFrameMems(t *testing.T) {
	sizes := []struct{ size, align int64 }{{1, 1}, {8, 8}, {2, 2}, {3, 1}, {4, 4}, {0, 8}, {16, 8}}
	var mems []*ir.Mem
	for i, s := range sizes {
		mems = append(mems, &ir.Mem{ID: i, Name: "m", Size: s.size, Align: s.align, Pos: p1})
	}
	fr, err := newFrame(1, 2, 3, mems)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{-25, -40, -42, -45, -52, -56, -72}; !slices.Equal(fr.Mem, want) {
		t.Errorf("offsets %v, want %v", fr.Mem, want)
	}
	if fr.MemBytes != 48 || fr.Size != 96 {
		t.Errorf("MemBytes %d and Size %d, want 48 and 96", fr.MemBytes, fr.Size)
	}
	used := map[int]bool{}
	for i, m := range mems {
		off := fr.Mem[i]
		if off%int(m.Align) != 0 {
			t.Errorf("m%d at %d is not aligned to %d", i, off, m.Align)
		}
		if off+int(m.Size) > -8*(fr.Slots+fr.Values) || off < -(fr.Size-8*fr.Out) {
			t.Errorf("m%d at %d of %d bytes is outside the objects' area", i, off, m.Size)
		}
		for b := off; b < off+int(m.Size); b++ {
			if used[b] {
				t.Errorf("m%d overlaps another object at %d", i, b)
			}
			used[b] = true
		}
	}
	if fr.Size%16 != 0 || fr.MemBytes%8 != 0 {
		t.Errorf("Size %d or MemBytes %d is not aligned", fr.Size, fr.MemBytes)
	}
	// Without objects, the layout is NewFrame's.
	if plain, _ := NewFrame(1, 2, 3); plain.Size != 48 || plain.MemBytes != 0 {
		t.Errorf("NewFrame(1, 2, 3) = %+v", plain)
	}
	// Objects that do not fit maxFrame are an error, not a wrapped offset.
	huge := []*ir.Mem{{Size: ir.MaxMemSize, Align: 8, Name: "a"}, {ID: 1, Size: 8, Align: 8, Name: "b"}}
	if _, err := newFrame(0, 0, 0, huge); err == nil {
		t.Error("frame over maxFrame accepted")
	}
}

// TestEmitMems checks the global memory objects' assembly: in .bss, each
// aligned and as large as its object, and a symbol clash is an error.
func TestEmitMems(t *testing.T) {
	p := &ir.Program{}
	p.NewMem("a", 3, 1, "p.go", p1)
	p.NewMem("b", 24, 8, "p.go", p1)
	p.NewMem("c", 0, 4, "p.go", p1)
	f := p.NewFunc("main", "p.go", p1)
	f.Ret(f.Entry(), p1)
	asm, err := Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "\t.bss\n" +
		"\t.p2align\t0\n\t.type\tmain.a, @object\n\t.size\tmain.a, 3\nmain.a:\n\t.zero\t3\n" +
		"\t.p2align\t3\n\t.type\tmain.b, @object\n\t.size\tmain.b, 24\nmain.b:\n\t.zero\t24\n" +
		"\t.p2align\t2\n\t.type\tmain.c, @object\n\t.size\tmain.c, 0\nmain.c:\n\t.zero\t0\n"
	if !bytes.Contains(asm, []byte(want)) {
		t.Errorf("assembly lacks\n%s\nin\n%s", want, asm)
	}
	p.NewGlobal("b", ir.I64, "p.go", p1)
	if _, err := Emit(p); err == nil || !strings.Contains(err.Error(), "symbol main.b is not unique") {
		t.Errorf("clash: %v", err)
	}
}
