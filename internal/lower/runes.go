package lower

// Runes (Phase 7 increment 7.3c, D-037).
//
// A rune literal is an integer constant, of type rune (int32), and needs
// nothing here. The conversions string(x) of an integer, []rune(s), and
// string(rs) encode and decode UTF-8 in IR functions that lowering adds to
// the program when it first needs each, as it adds .streq: they
// follow the Go specification and gc's runtime, so a value that is not a
// Unicode code point, a surrogate included, becomes U+FFFD, and each byte
// of invalid UTF-8 decodes to U+FFFD, as unicode/utf8.DecodeRune decodes
// it.

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// The generated UTF-8 functions, indexes of lowerer.runeFuncs.
const (
	encodeRune  = iota // .encoderune
	decodeRune         // .decoderune
	stringRunes        // .stringtorunes
	runesString        // .runestostring
	nRuneFuncs
)

// runeError is U+FFFD, the replacement character.
const runeError = 0xFFFD

// gen helps build a function that lowering generates. Its values live in
// slots between blocks, as in every function lowering produces.
type gen struct {
	f   *ir.Func
	pos ir.Pos
}

func (g gen) slot(name string, t ir.Type) *ir.Slot       { return g.f.NewSlot(name, t, g.pos) }
func (g gen) param(b *ir.Block, i int) *ir.Instr         { return g.f.Param(b, i, g.pos) }
func (g gen) c(b *ir.Block, v int64) *ir.Instr           { return g.f.Const(b, v, g.pos) }
func (g gen) ld(b *ir.Block, s *ir.Slot) *ir.Instr       { return g.f.Load(b, s, g.pos) }
func (g gen) store(b *ir.Block, s *ir.Slot, v *ir.Instr) { g.f.Store(b, s, v, g.pos) }
func (g gen) cmp(b *ir.Block, op ir.Op, x, y *ir.Instr) *ir.Instr {
	return g.f.Compare(b, op, x, y, g.pos)
}
func (g gen) bin(b *ir.Block, op ir.Op, x, y *ir.Instr) *ir.Instr {
	return g.f.Binary(b, op, x, y, g.pos)
}

// inc adds 1 to the I64 in s.
func (g gen) inc(b *ir.Block, s *ir.Slot) { g.store(b, s, g.bin(b, ir.Add, g.ld(b, s), g.c(b, 1))) }

// ret returns a new block that returns the I64 v.
func (g gen) ret(v int64) *ir.Block {
	b := g.f.NewBlock()
	g.f.Ret(b, g.pos, g.c(b, v))
	return b
}

// ifThen branches from b on x op k, a constant, to t or else to a new
// block, which it returns.
func (g gen) ifThen(b *ir.Block, op ir.Op, x *ir.Instr, k int64, t *ir.Block) *ir.Block {
	e := g.f.NewBlock()
	g.f.Branch(b, g.cmp(b, op, x, g.f.TypedConst(b, x.Type, k, g.pos)), t, e, g.pos)
	return e
}

// runeFunc returns the generated function i, made on its first use.
func (l *lowerer) runeFunc(i int, pos ir.Pos) *ir.Func {
	if f := l.runeFuncs[i]; f != nil {
		return f
	}
	var f *ir.Func
	switch i {
	case encodeRune:
		f = l.encodeRuneFunc(pos)
	case decodeRune:
		f = l.decodeRuneFunc(pos)
	case stringRunes:
		f = l.stringRunesFunc(pos)
	case runesString:
		f = l.runesStringFunc(pos)
	}
	l.runeFuncs[i] = f
	return f
}

// encodeRuneFunc generates
//
//	func .encoderune(p ptr, r i64) i64
//
// which writes the UTF-8 encoding of r, 1 to 4 bytes, at p and returns its
// length. An r that is not a code point, negative (or, read unsigned, above
// U+10FFFF) or a surrogate, U+D800 to U+DFFF, is encoded as U+FFFD.
func (l *lowerer) encodeRuneFunc(pos ir.Pos) *ir.Func {
	f := l.prog.NewFunc(genPrefix+"encoderune", l.fn.File, pos)
	f.Params = []ir.Type{ir.Ptr, ir.I64}
	f.Results = []ir.Type{ir.I64}
	g := gen{f, pos}
	entry := f.Entry()
	ps, rs := g.slot("p", ir.Ptr), g.slot("r", ir.I64)
	p, r := g.param(entry, 0), g.param(entry, 1)
	g.store(entry, ps, p)
	g.store(entry, rs, r)
	bad, valid := f.NewBlock(), f.NewBlock()
	b := g.ifThen(entry, ir.Gt, f.Conv(entry, ir.U64, r, pos), 0x10FFFF, bad)
	b = g.ifThen(b, ir.Lt, g.ld(b, rs), 0xD800, valid)
	b = g.ifThen(b, ir.Le, g.ld(b, rs), 0xDFFF, bad)
	f.Jump(b, valid, pos)
	g.store(bad, rs, g.c(bad, runeError))
	f.Jump(bad, valid, pos)
	// The first byte holds the high bits of r, after a marker of the
	// length; each later byte 0b10 and the next 6 bits.
	put := func(b *ir.Block, n int64, lead int64) {
		r := g.ld(b, rs)
		p := g.ld(b, ps)
		for k := int64(0); k < n; k++ {
			v := r
			if sh := 6 * (n - 1 - k); sh > 0 {
				v = f.Shift(b, ir.Shr, r, f.TypedConst(b, ir.U64, sh, pos), pos)
			}
			if k == 0 {
				v = g.bin(b, ir.Or, v, g.c(b, lead))
			} else {
				v = g.bin(b, ir.Or, g.bin(b, ir.And, v, g.c(b, 0x3F)), g.c(b, 0x80))
			}
			f.StM(b, p, g.c(b, k), f.Conv(b, ir.U8, v, pos), pos)
		}
		f.Ret(b, pos, g.c(b, n))
	}
	b = valid
	for _, size := range []struct{ n, below, lead int64 }{{1, 0x80, 0}, {2, 0x800, 0xC0}, {3, 0x10000, 0xE0}} {
		t := f.NewBlock()
		b = g.ifThen(b, ir.Lt, g.ld(b, rs), size.below, t)
		put(t, size.n, size.lead)
	}
	put(b, 4, 0xF0)
	return f
}

// decodeRuneFunc generates
//
//	func .decoderune(p ptr, n i64, i i64) (i64, i64)
//
// which decodes the rune that starts at byte i < n of the n bytes at p and
// returns it and the offset after it, as unicode/utf8.DecodeRune does: a
// first byte of 0x80 to 0xC1 or 0xF5 to 0xFF, a missing or out-of-range
// continuation byte (which rules out overlong forms, surrogates, and runes
// above U+10FFFF), or a sequence cut short by the end, gives U+FFFD and
// i+1.
func (l *lowerer) decodeRuneFunc(pos ir.Pos) *ir.Func {
	f := l.prog.NewFunc(genPrefix+"decoderune", l.fn.File, pos)
	f.Params = []ir.Type{ir.Ptr, ir.I64, ir.I64}
	f.Results = []ir.Type{ir.I64, ir.I64}
	g := gen{f, pos}
	entry := f.Entry()
	ps, ns, is := g.slot("p", ir.Ptr), g.slot("n", ir.I64), g.slot("i", ir.I64)
	// r is the rune so far, need the continuation bytes still to come,
	// k the offset from i of the next one, lo and hi its range.
	rs, needs, ks, los, his := g.slot("r", ir.I64), g.slot("need", ir.I64), g.slot("k", ir.I64), g.slot("lo", ir.I64), g.slot("hi", ir.I64)
	p, n, i := g.param(entry, 0), g.param(entry, 1), g.param(entry, 2)
	g.store(entry, ps, p)
	g.store(entry, ns, n)
	g.store(entry, is, i)
	byteAt := func(b *ir.Block, k *ir.Instr) *ir.Instr {
		return f.Conv(b, ir.I64, f.LoadB(b, g.ld(b, ps), g.bin(b, ir.Add, g.ld(b, is), k), pos), pos)
	}
	ret := func(b *ir.Block, r, width *ir.Instr) {
		f.Ret(b, pos, r, g.bin(b, ir.Add, g.ld(b, is), width))
	}
	bad, ascii, trail := f.NewBlock(), f.NewBlock(), f.NewBlock()
	ret(bad, g.c(bad, runeError), g.c(bad, 1))
	x := byteAt(entry, g.c(entry, 0))
	g.store(entry, rs, x)
	b := g.ifThen(entry, ir.Lt, x, 0x80, ascii)
	ret(ascii, g.ld(ascii, rs), g.c(ascii, 1))
	b = g.ifThen(b, ir.Lt, g.ld(b, rs), 0xC2, bad)
	// start sets the rune's high bits from the first byte, and the number
	// and range of the continuation bytes, then goes on to trail.
	start := func(b *ir.Block, mask, need, lo, hi int64) {
		g.store(b, rs, g.bin(b, ir.And, g.ld(b, rs), g.c(b, mask)))
		g.store(b, needs, g.c(b, need))
		g.store(b, los, g.c(b, lo))
		g.store(b, his, g.c(b, hi))
		f.Jump(b, trail, pos)
	}
	// The ranges of the second byte, as utf8's acceptRanges give them.
	for _, c := range []struct {
		below, mask, need int64
		special           []struct{ lead, lo, hi int64 }
	}{
		{0xE0, 0x1F, 1, nil},
		{0xF0, 0x0F, 2, []struct{ lead, lo, hi int64 }{{0xE0, 0xA0, 0xBF}, {0xED, 0x80, 0x9F}}},
		{0xF5, 0x07, 3, []struct{ lead, lo, hi int64 }{{0xF0, 0x90, 0xBF}, {0xF4, 0x80, 0x8F}}},
	} {
		t := f.NewBlock()
		b = g.ifThen(b, ir.Lt, g.ld(b, rs), c.below, t)
		for _, s := range c.special {
			u := f.NewBlock()
			t2 := g.ifThen(t, ir.Eq, g.ld(t, rs), s.lead, u)
			start(u, c.mask, c.need, s.lo, s.hi)
			t = t2
		}
		start(t, c.mask, c.need, 0x80, 0xBF)
	}
	f.Jump(b, bad, pos)
	// trail: the whole sequence lies before n, each continuation byte in
	// its range, the first in [lo, hi] and the others in [0x80, 0xBF].
	first, loop, body, done := f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	f.Branch(trail, g.cmp(trail, ir.Ge, g.bin(trail, ir.Add, g.ld(trail, is), g.ld(trail, needs)), g.ld(trail, ns)), bad, first, pos)
	g.store(first, ks, g.c(first, 1))
	f.Jump(first, loop, pos)
	f.Branch(loop, g.cmp(loop, ir.Gt, g.ld(loop, ks), g.ld(loop, needs)), done, body, pos)
	c := byteAt(body, g.ld(body, ks))
	t := f.NewBlock()
	f.Branch(body, g.cmp(body, ir.Lt, c, g.ld(body, los)), bad, t, pos)
	c = byteAt(t, g.ld(t, ks))
	u := f.NewBlock()
	f.Branch(t, g.cmp(t, ir.Gt, c, g.ld(t, his)), bad, u, pos)
	c = byteAt(u, g.ld(u, ks))
	g.store(u, rs, g.bin(u, ir.Or, f.Shift(u, ir.Shl, g.ld(u, rs), f.TypedConst(u, ir.U64, 6, pos), pos), g.bin(u, ir.And, c, g.c(u, 0x3F))))
	// After the first continuation byte, the range is [0x80, 0xBF].
	g.store(u, los, g.c(u, 0x80))
	g.store(u, his, g.c(u, 0xBF))
	g.inc(u, ks)
	f.Jump(u, loop, pos)
	ret(done, g.ld(done, rs), g.ld(done, ks))
	return f
}

// stringRunesFunc generates
//
//	func .stringtorunes(p ptr, n i64) (ptr, i64)
//
// which returns a new array of the runes the n bytes at p decode to, and
// their number: it counts them, allocates 4 bytes for each, and decodes
// them again into it.
func (l *lowerer) stringRunesFunc(pos ir.Pos) *ir.Func {
	f := l.prog.NewFunc(genPrefix+"stringtorunes", l.fn.File, pos)
	f.Params = []ir.Type{ir.Ptr, ir.I64}
	f.Results = []ir.Type{ir.Ptr, ir.I64}
	g := gen{f, pos}
	dec := l.runeFunc(decodeRune, pos)
	entry := f.Entry()
	ps, ns, is, cs, qs, ks := g.slot("p", ir.Ptr), g.slot("n", ir.I64), g.slot("i", ir.I64), g.slot("c", ir.I64), g.slot("q", ir.Ptr), g.slot("k", ir.I64)
	p, n := g.param(entry, 0), g.param(entry, 1)
	g.store(entry, ps, p)
	g.store(entry, ns, n)
	g.store(entry, is, g.c(entry, 0))
	g.store(entry, cs, g.c(entry, 0))
	count, counted, alloc, fill, filled, end := f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	f.Jump(entry, count, pos)
	decode := func(b *ir.Block) *ir.Instr {
		d := f.Call(b, dec, []*ir.Instr{g.ld(b, ps), g.ld(b, ns), g.ld(b, is)}, pos)
		g.store(b, is, f.Extract(b, d, 1, pos))
		return f.Extract(b, d, 0, pos)
	}
	// Count the runes.
	f.Branch(count, g.cmp(count, ir.Lt, g.ld(count, is), g.ld(count, ns)), counted, alloc, pos)
	decode(counted)
	g.inc(counted, cs)
	f.Jump(counted, count, pos)
	g.store(alloc, qs, f.Alloc(alloc, g.bin(alloc, ir.Mul, g.ld(alloc, cs), g.c(alloc, 4)), pos))
	g.store(alloc, is, g.c(alloc, 0))
	g.store(alloc, ks, g.c(alloc, 0))
	f.Jump(alloc, fill, pos)
	// Decode them again into the array.
	f.Branch(fill, g.cmp(fill, ir.Lt, g.ld(fill, is), g.ld(fill, ns)), filled, end, pos)
	r := decode(filled)
	f.StM(filled, g.ld(filled, qs), g.bin(filled, ir.Mul, g.ld(filled, ks), g.c(filled, 4)), f.Conv(filled, ir.I32, r, pos), pos)
	g.inc(filled, ks)
	f.Jump(filled, fill, pos)
	f.Ret(end, pos, g.ld(end, qs), g.ld(end, cs))
	return f
}

// runesStringFunc generates
//
//	func .runestostring(p ptr, n i64) (ptr, i64)
//
// which returns the string of the UTF-8 encodings of the n runes, I32s, at
// p: a new object of 4 bytes for each rune, the most one takes, which holds
// the encodings from its start, and their total length.
func (l *lowerer) runesStringFunc(pos ir.Pos) *ir.Func {
	f := l.prog.NewFunc(genPrefix+"runestostring", l.fn.File, pos)
	f.Params = []ir.Type{ir.Ptr, ir.I64}
	f.Results = []ir.Type{ir.Ptr, ir.I64}
	g := gen{f, pos}
	enc := l.runeFunc(encodeRune, pos)
	entry := f.Entry()
	ps, ns, is, qs, offs := g.slot("p", ir.Ptr), g.slot("n", ir.I64), g.slot("i", ir.I64), g.slot("q", ir.Ptr), g.slot("off", ir.I64)
	p, n := g.param(entry, 0), g.param(entry, 1)
	g.store(entry, ps, p)
	g.store(entry, ns, n)
	// n runes take 4n bytes in the slice already, so 4n does not
	// overflow.
	g.store(entry, qs, f.Alloc(entry, g.bin(entry, ir.Mul, n, g.c(entry, 4)), pos))
	g.store(entry, is, g.c(entry, 0))
	g.store(entry, offs, g.c(entry, 0))
	loop, body, end := f.NewBlock(), f.NewBlock(), f.NewBlock()
	f.Jump(entry, loop, pos)
	f.Branch(loop, g.cmp(loop, ir.Lt, g.ld(loop, is), g.ld(loop, ns)), body, end, pos)
	r := f.LdM(body, ir.I32, g.ld(body, ps), g.bin(body, ir.Mul, g.ld(body, is), g.c(body, 4)), pos)
	dst := f.PAdd(body, g.ld(body, qs), g.ld(body, offs), pos)
	w := f.Call(body, enc, []*ir.Instr{dst, f.Conv(body, ir.I64, r, pos)}, pos)
	g.store(body, offs, g.bin(body, ir.Add, g.ld(body, offs), w))
	g.inc(body, is)
	f.Jump(body, loop, pos)
	f.Ret(end, pos, g.ld(end, qs), g.ld(end, offs))
	return f
}

// runeConversion lowers string(x) of an integer x: the UTF-8 encoding of
// its value, in a new object of 4 bytes, the most an encoding takes.
func (l *lowerer) runeConversion(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	x := l.expr(call.Args[0])
	if x.Type != ir.I64 {
		// The value as a signed or unsigned number: .encoderune
		// reads a negative one as above U+10FFFF.
		x = l.fn.Conv(l.b, ir.I64, x, pos)
	}
	p := l.fn.Alloc(l.b, l.fn.Const(l.b, 4, pos), pos)
	n := l.fn.Call(l.b, l.runeFunc(encodeRune, pos), []*ir.Instr{p, x}, pos)
	return []*ir.Instr{p, n}
}

// runesConversion lowers []rune(s) of a string: a new array of its runes,
// as long as their number.
func (l *lowerer) runesConversion(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	w := l.value(call.Args[0])
	r := l.fn.Call(l.b, l.runeFunc(stringRunes, pos), w, pos)
	n := l.fn.Extract(l.b, r, 1, pos)
	return []*ir.Instr{l.fn.Extract(l.b, r, 0, pos), n, n}
}

// runesStringConversion lowers string(rs) of a []rune.
func (l *lowerer) runesStringConversion(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	w := l.value(call.Args[0])
	r := l.fn.Call(l.b, l.runeFunc(runesString, pos), w[:2], pos)
	return []*ir.Instr{l.fn.Extract(l.b, r, 0, pos), l.fn.Extract(l.b, r, 1, pos)}
}

// isRuneSlice reports whether t is a slice of runes, int32s.
func isRuneSlice(t types.Type) bool {
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Int32
}

// isInteger reports whether t is an integer type, typed or untyped.
func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}
