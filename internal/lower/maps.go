package lower

// Maps (Phase 8 increment 8.5, D-042).
//
// A map value is one Ptr: 0 for nil, or a heap header of hdrSize bytes
// holding the number of live entries, the entry array, the entries used
// in it, its capacity, the index, the index's mask, and the number of
// clear calls. The entry array, after a first word written only when the
// array is replaced, holds the entries in order of insertion, each the
// key's hash, whether it is live, the key, and the element. The index is
// a power of two of I64 slots, each 0 or an entry's position plus 1,
// probed linearly from the hash; a deleted entry keeps its slot, which
// probing passes over, until the next growth.
//
// Lowering generates, for each map type N, .mapaccessN, .mapassignN, and
// .mapdeleteN, which take the map and the key's words and return the Ptr
// of the entry's element (a missing key's is that of the zero object
// .mzero), and, for each hashed type, .hashN; .mapgrow, .mapclear, and
// .maplen serve every map type.

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// The words of a map's header, and the offsets in an entry (D-042).
const (
	hdrCount   = 0
	hdrEntries = 8
	hdrUsed    = 16
	hdrCap     = 24
	hdrIndex   = 32
	hdrMask    = 40
	hdrClears  = 48
	hdrSize    = 56

	entFirst = 8 // the entry array's first entry; its first word is its final used
	entHash  = 0
	entLive  = 8
	entKey   = 16

	descHash = 24 // the offset of a type descriptor's hash function (D-041, D-042)
)

// The hash's multiplier and fmix64's, from MurmurHash3.
const (
	hashMul = -0x61c8864680b583eb // 0x9e3779b97f4a7c15
	fmixMul = -0x00ae502812aa7333 // 0xff51afd7ed558ccd
)

func isMap(t types.Type) bool {
	_, ok := t.Underlying().(*types.Map)
	return ok
}

// mapT is a map type and its generated functions.
type mapT struct {
	key, elem   types.Type
	id          int
	voff, esize int64 // the element's offset in an entry, and an entry's size
}

// mapType returns the map type of t, the same for identical ones.
func (l *lowerer) mapType(t types.Type) *mapT {
	u := t.Underlying().(*types.Map)
	for _, mt := range l.maps {
		if types.Identical(mt.key, u.Key()) && types.Identical(mt.elem, u.Elem()) {
			return mt
		}
	}
	mt := &mapT{key: u.Key(), elem: u.Elem(), id: len(l.maps)}
	a := sizes.Alignof(mt.elem)
	mt.voff = (entKey + sizes.Sizeof(mt.key) + a - 1) / a * a
	mt.esize = (mt.voff + sizes.Sizeof(mt.elem) + 7) &^ 7
	l.maps = append(l.maps, mt)
	return mt
}

// mb builds the code of the map functions in the current block. Its
// values live in slots between blocks.
type mb struct {
	l   *lowerer
	pos ir.Pos
}

// mvar is a temporary slot.
type mvar struct {
	m mb
	s *ir.Slot
}

func (v mvar) get() *ir.Instr  { return v.m.l.fn.Load(v.m.l.b, v.s, v.m.pos) }
func (v mvar) set(x *ir.Instr) { v.m.l.fn.Store(v.m.l.b, v.s, x, v.m.pos) }
func (m mb) v(name string, x *ir.Instr) mvar {
	v := mvar{m, m.l.fn.NewSlot(name, x.Type, m.pos)}
	v.set(x)
	return v
}

func (m mb) c(v int64) *ir.Instr                    { return m.l.fn.Const(m.l.b, v, m.pos) }
func (m mb) nilPtr() *ir.Instr                      { return m.l.fn.TypedConst(m.l.b, ir.Ptr, 0, m.pos) }
func (m mb) bin(op ir.Op, x, y *ir.Instr) *ir.Instr { return m.l.fn.Binary(m.l.b, op, x, y, m.pos) }
func (m mb) cmp(op ir.Op, x, y *ir.Instr) *ir.Instr {
	return m.l.fn.Compare(m.l.b, op, x, y, m.pos)
}
func (m mb) ld(t ir.Type, p *ir.Instr, off int64) *ir.Instr {
	return m.l.fn.LdM(m.l.b, t, p, m.c(off), m.pos)
}
func (m mb) st(p *ir.Instr, off int64, x *ir.Instr) { m.l.fn.StM(m.l.b, p, m.c(off), x, m.pos) }

// slotAt loads slot i, an I64, of the index idx.
func (m mb) slotAt(idx, i *ir.Instr) *ir.Instr {
	return m.l.fn.LdM(m.l.b, ir.I64, idx, m.bin(ir.Mul, i, m.c(8)), m.pos)
}

// when branches on cond to a new block, where it leaves the current block,
// and returns the other target.
func (m mb) when(cond *ir.Instr) *ir.Block {
	yes, no := m.l.fn.NewBlock(), m.l.fn.NewBlock()
	m.l.fn.Branch(m.l.b, cond, yes, no, m.pos)
	m.l.b = yes
	return no
}

// join ends the current block with a jump to b, and continues there.
func (m mb) join(b *ir.Block) {
	m.l.fn.Jump(m.l.b, b, m.pos)
	m.l.b = b
}

// loop builds while cond() { body() } and continues after it.
func (m mb) loop(cond func() *ir.Instr, body func()) {
	head := m.l.fn.NewBlock()
	m.join(head)
	exit := m.when(cond())
	body()
	m.l.fn.Jump(m.l.b, head, m.pos)
	m.l.b = exit
}

// entry returns the Ptr of entry k, an I64, of the entry array ents, whose
// entries are es bytes, an I64.
func (m mb) entry(ents, k, es *ir.Instr) *ir.Instr {
	return m.l.fn.PAdd(m.l.b, ents, m.bin(ir.Add, m.bin(ir.Mul, k, es), m.c(entFirst)), m.pos)
}

// probeEmpty returns the position of the first empty slot of the index
// idx, of mask mask, probing from the hash h.
func (m mb) probeEmpty(idx, mask, h func() *ir.Instr) mvar {
	i := m.v("i", m.bin(ir.And, h(), mask()))
	m.loop(func() *ir.Instr {
		return m.cmp(ir.Ne, m.slotAt(idx(), i.get()), m.c(0))
	}, func() {
		i.set(m.bin(ir.And, m.bin(ir.Add, i.get(), m.c(1)), mask()))
	})
	return i
}

// find probes the index of the non-empty map hdr for the key at kp, of
// hash h. It continues where the entry is found, its Ptr in the returned
// slot, and returns the block reached if it is not.
func (l *lowerer) find(mt *mapT, hdr, h func() *ir.Instr, kp place, pos ir.Pos) (mvar, *ir.Block) {
	m := mb{l, pos}
	mask := func() *ir.Instr { return m.ld(ir.I64, hdr(), hdrMask) }
	i := m.v("i", m.bin(ir.And, h(), mask()))
	head, next := l.fn.NewBlock(), l.fn.NewBlock()
	m.join(head)
	s := m.v("s", m.slotAt(m.ld(ir.Ptr, hdr(), hdrIndex), i.get()))
	miss := m.when(m.cmp(ir.Ne, s.get(), m.c(0)))
	e := m.v("e", m.entry(m.ld(ir.Ptr, hdr(), hdrEntries), m.bin(ir.Sub, s.get(), m.c(1)), m.c(mt.esize)))
	l.fn.Jump(m.when(m.cmp(ir.Ne, m.ld(ir.I64, e.get(), entLive), m.c(0))), next, pos)
	l.fn.Jump(m.when(m.cmp(ir.Eq, m.ld(ir.I64, e.get(), entHash), h())), next, pos)
	l.fn.Jump(m.when(l.equal(place{base: e.get, c: entKey, t: mt.key}, kp, pos)), next, pos)
	found := l.b
	l.b = next
	i.set(m.bin(ir.And, m.bin(ir.Add, i.get(), m.c(1)), mask()))
	l.fn.Jump(l.b, head, pos)
	l.b = found
	return e, miss
}

// Kinds of mapFunc.
const (
	mapAccess = iota
	mapAssign
	mapDelete
)

// mapFunc returns .mapaccessN, .mapassignN, or .mapdeleteN of mt: given
// the map and the key's words, the Ptr of the key's element, inserting a
// zeroed one for an assignment, or deleting it. A missing key's element is
// .mzero.
func (l *lowerer) mapFunc(mt *mapT, kind int, pos ir.Pos) *ir.Func {
	name := []string{"mapaccess", "mapassign", "mapdelete"}[kind] + itoa(mt.id)
	params := append([]ir.Type{ir.Ptr}, l.words(nil, mt.key)...)
	var results []ir.Type
	if kind != mapDelete {
		results = []ir.Type{ir.Ptr}
	}
	return l.wrapper(name, params, results, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		m := mb{l, pos}
		hdr := ps[0]
		kp := place{base: ps[1], t: mt.key}
		if !isAggregate(mt.key) {
			// The key's words in memory, to hash and compare in place.
			kp = l.memPlace(l.newMem("key", mt.key, pos), mt.key, pos)
			var kw []*ir.Instr
			for _, p := range ps[1:] {
				kw = append(kw, p())
			}
			l.storePlace(kp, kw, pos)
		}
		hash := func(form int64) *ir.Instr {
			return l.fn.Call(l.b, l.hashFunc(mt.key, pos), []*ir.Instr{m.nilPtr(), kp.base(), m.c(form)}, pos)
		}
		ret := func(v func() *ir.Instr) {
			if v == nil {
				l.fn.Ret(l.b, pos)
			} else {
				l.fn.Ret(l.b, pos, v())
			}
		}
		elem := func(e mvar) func() *ir.Instr {
			return func() *ir.Instr { return l.fn.PAdd(l.b, e.get(), m.c(mt.voff), pos) }
		}
		zero := func() *ir.Instr { return l.zeroAddr(mt.elem, pos) }
		if kind == mapDelete {
			zero = nil
		}
		count := func() *ir.Instr { return m.ld(ir.I64, hdr(), hdrCount) }
		insert := l.fn.NewBlock()
		if kind == mapAssign {
			ok := m.when(m.cmp(ir.Eq, hdr(), m.nilPtr()))
			l.panicMsg([][]*ir.Instr{l.strWords("assignment to entry in nil map", pos)}, pos)
			l.b = ok
		} else {
			// A nil or empty map holds no key, but gc checks that the key
			// is hashable all the same, with another message (D-042).
			miss := l.fn.NewBlock()
			nonNil := m.when(m.cmp(ir.Eq, hdr(), m.nilPtr()))
			l.fn.Jump(l.b, miss, pos)
			l.b = nonNil
			nonEmpty := m.when(m.cmp(ir.Eq, count(), m.c(0)))
			l.fn.Jump(l.b, miss, pos)
			l.b = miss
			if hasIface(mt.key) {
				hash(1)
			}
			ret(zero)
			l.b = nonEmpty
		}
		h := m.v("h", hash(0))
		if kind == mapAssign {
			// An empty map may have no index yet.
			l.fn.Jump(m.when(m.cmp(ir.Ne, count(), m.c(0))), insert, pos)
		}
		e, miss := l.find(mt, hdr, h.get, kp, pos)
		switch kind {
		case mapAccess:
			ret(elem(e))
			l.b = miss
			ret(zero)
		case mapDelete:
			m.st(e.get(), entLive, m.c(0))
			m.st(hdr(), hdrCount, m.bin(ir.Sub, count(), m.c(1)))
			ret(nil)
			l.b = miss
			ret(nil)
		case mapAssign:
			// The key is stored again: +0 and -0 are one key (D-042).
			l.copyPlace(place{base: e.get, c: entKey, t: mt.key}, kp, pos)
			ret(elem(e))
			l.b = miss
			m.join(insert)
			after := m.when(m.cmp(ir.Eq, m.ld(ir.I64, hdr(), hdrUsed), m.ld(ir.I64, hdr(), hdrCap)))
			l.fn.Call(l.b, l.mapGrowFunc(pos), []*ir.Instr{hdr(), m.c(mt.esize)}, pos)
			m.join(after)
			idx := func() *ir.Instr { return m.ld(ir.Ptr, hdr(), hdrIndex) }
			i := m.probeEmpty(idx, func() *ir.Instr { return m.ld(ir.I64, hdr(), hdrMask) }, h.get)
			u := m.v("u", m.ld(ir.I64, hdr(), hdrUsed))
			ne := m.v("e", m.entry(m.ld(ir.Ptr, hdr(), hdrEntries), u.get(), m.c(mt.esize)))
			m.st(ne.get(), entHash, h.get())
			m.st(ne.get(), entLive, m.c(1))
			l.copyPlace(place{base: ne.get, c: entKey, t: mt.key}, kp, pos)
			l.zero(place{base: ne.get, c: mt.voff, t: mt.elem}, pos)
			l.fn.StM(l.b, idx(), m.bin(ir.Mul, i.get(), m.c(8)), m.bin(ir.Add, u.get(), m.c(1)), pos)
			m.st(hdr(), hdrUsed, m.bin(ir.Add, u.get(), m.c(1)))
			m.st(hdr(), hdrCount, m.bin(ir.Add, count(), m.c(1)))
			ret(elem(ne))
		}
		prune(l.fn)
	})
}

// zeroAddr returns the address of .mzero, a global object of zero bytes at
// least as large as a value of t.
func (l *lowerer) zeroAddr(t types.Type, pos ir.Pos) *ir.Instr {
	n := max(sizes.Sizeof(t), 8)
	if l.mzero == nil {
		l.mzero = l.prog.NewMem(genPrefix+"mzero", n, 8, l.fn.File, pos)
	}
	l.mzero.Size = max(l.mzero.Size, n)
	return l.addr(l.mzero, pos)
}

// mapGrowFunc returns .mapgrow(m, esize): it moves the live entries of the
// map m, whose entries are esize bytes, in order, to new arrays of
// capacity max(8, 2·count), with an index of at least twice as many
// slots, and writes the used of the old array in its first word (D-042).
func (l *lowerer) mapGrowFunc(pos ir.Pos) *ir.Func {
	return l.wrapper("mapgrow", []ir.Type{ir.Ptr, ir.I64}, nil, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		m := mb{l, pos}
		hdr, es := ps[0], ps[1]
		ncap := m.v("cap", m.bin(ir.Mul, m.ld(ir.I64, hdr(), hdrCount), m.c(2)))
		big := m.when(m.cmp(ir.Lt, ncap.get(), m.c(8)))
		ncap.set(m.c(8))
		m.join(big)
		n := m.v("n", m.c(16))
		m.loop(func() *ir.Instr { return m.cmp(ir.Lt, n.get(), m.bin(ir.Mul, ncap.get(), m.c(2))) },
			func() { n.set(m.bin(ir.Mul, n.get(), m.c(2))) })
		ents := m.v("ents", l.fn.Alloc(l.b, m.bin(ir.Add, m.bin(ir.Mul, ncap.get(), es()), m.c(entFirst)), pos))
		idx := m.v("idx", l.fn.AllocNoScan(l.b, m.bin(ir.Mul, n.get(), m.c(8)), pos)) // entry numbers (D-047)
		mask := func() *ir.Instr { return m.bin(ir.Sub, n.get(), m.c(1)) }
		old := m.v("old", m.ld(ir.Ptr, hdr(), hdrEntries))
		used := m.v("used", m.ld(ir.I64, hdr(), hdrUsed))
		none := m.when(m.cmp(ir.Ne, old.get(), m.nilPtr()))
		m.st(old.get(), 0, used.get())
		m.join(none)
		j, k := m.v("j", m.c(0)), m.v("k", m.c(0))
		m.loop(func() *ir.Instr { return m.cmp(ir.Lt, k.get(), used.get()) }, func() {
			e := m.v("e", m.entry(old.get(), k.get(), es()))
			k.set(m.bin(ir.Add, k.get(), m.c(1)))
			dead := m.when(m.cmp(ir.Ne, m.ld(ir.I64, e.get(), entLive), m.c(0)))
			l.fn.Move(l.b, m.entry(ents.get(), j.get(), es()), e.get(), es(), pos)
			h := m.v("h", m.ld(ir.I64, e.get(), entHash))
			i := m.probeEmpty(idx.get, mask, h.get)
			j.set(m.bin(ir.Add, j.get(), m.c(1)))
			l.fn.StM(l.b, idx.get(), m.bin(ir.Mul, i.get(), m.c(8)), j.get(), pos)
			m.join(dead)
		})
		m.st(hdr(), hdrEntries, ents.get())
		m.st(hdr(), hdrUsed, j.get())
		m.st(hdr(), hdrCap, ncap.get())
		m.st(hdr(), hdrIndex, idx.get())
		m.st(hdr(), hdrMask, mask())
		l.fn.Ret(l.b, pos)
		prune(l.fn)
	})
}

// mapClearFunc returns .mapclear(m): it empties the index of the map m,
// if not nil, sets its count and used to 0, and counts the clear.
func (l *lowerer) mapClearFunc(pos ir.Pos) *ir.Func {
	return l.wrapper("mapclear", []ir.Type{ir.Ptr}, nil, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		m := mb{l, pos}
		hdr := ps[0]
		done := l.fn.NewBlock()
		l.fn.Jump(m.when(m.cmp(ir.Ne, hdr(), m.nilPtr())), done, pos)
		idx := m.v("idx", m.ld(ir.Ptr, hdr(), hdrIndex))
		empty := m.when(m.cmp(ir.Ne, idx.get(), m.nilPtr()))
		i := m.v("i", m.c(0))
		m.loop(func() *ir.Instr { return m.cmp(ir.Le, i.get(), m.ld(ir.I64, hdr(), hdrMask)) }, func() {
			l.fn.StM(l.b, idx.get(), m.bin(ir.Mul, i.get(), m.c(8)), m.c(0), pos)
			i.set(m.bin(ir.Add, i.get(), m.c(1)))
		})
		m.join(empty)
		m.st(hdr(), hdrCount, m.c(0))
		m.st(hdr(), hdrUsed, m.c(0))
		m.st(hdr(), hdrClears, m.bin(ir.Add, m.ld(ir.I64, hdr(), hdrClears), m.c(1)))
		m.join(done)
		l.fn.Ret(l.b, pos)
		prune(l.fn)
	})
}

// mapLenFunc returns .maplen(m): the count of the map m, 0 if nil.
func (l *lowerer) mapLenFunc(pos ir.Pos) *ir.Func {
	return l.wrapper("maplen", []ir.Type{ir.Ptr}, []ir.Type{ir.I64}, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		m := mb{l, pos}
		nonNil := m.when(m.cmp(ir.Eq, ps[0](), m.nilPtr()))
		l.fn.Ret(l.b, pos, m.c(0))
		l.b = nonNil
		l.fn.Ret(l.b, pos, m.ld(ir.I64, ps[0](), hdrCount))
		prune(l.fn)
	})
}

// hasIface reports whether a value of t holds an interface.
func hasIface(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return true
	case *types.Array:
		return hasIface(u.Elem())
	case *types.Struct:
		for f := range u.Fields() {
			if hasIface(f.Type()) {
				return true
			}
		}
	}
	return false
}

// hashSig is the signature of the hash functions: the closure, unused, the
// Ptr of the value, and the form of an unhashable value's panic.
var hashSig = &ir.Sig{Params: []ir.Type{ir.Ptr, ir.Ptr, ir.I64}, Results: []ir.Type{ir.I64}}

// hashFunc returns .hashN(ctx, p, form), the hash of the value of the
// comparable type t at p: each leaf, as == compares them, blank fields
// skipped, mixed into the hash in order. A float's bits are hashed, with
// -0 as +0; a string's bytes and length; an interface's dynamic value, by
// its type's hash function, which panics if that type is not comparable:
// with gc's message for a hash if form is 0, and for a key check if 1.
func (l *lowerer) hashFunc(t types.Type, pos ir.Pos) *ir.Func {
	id := -1
	for i, u := range l.hashed {
		if types.Identical(u, t) {
			id = i
		}
	}
	if id < 0 {
		id = len(l.hashed)
		l.hashed = append(l.hashed, t)
	}
	return l.wrapper("hash"+itoa(id), hashSig.Params, hashSig.Results, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		m := mb{l, pos}
		h := m.v("h", l.fn.TypedConst(l.b, ir.U64, 0, pos))
		mix := func(x *ir.Instr) {
			if x.Type != ir.U64 {
				x = l.fn.Conv(l.b, ir.U64, x, pos)
			}
			h.set(m.bin(ir.Mul, m.bin(ir.Xor, h.get(), x), l.fn.TypedConst(l.b, ir.U64, hashMul, pos)))
		}
		l.walkValues(t, []place{{base: ps[1], t: t}}, true, pos, func(lt types.Type, qs []place) {
			p := qs[0]
			switch {
			case isIface(lt):
				l.hashIface(lt, p, ps[2], mix, pos)
			case isString(lt):
				s := m.v("s", l.ldm(ir.Ptr, p, pos))
				n := m.v("n", l.ldm(ir.I64, p.field(8, lt), pos))
				i := m.v("i", m.c(0))
				m.loop(func() *ir.Instr { return m.cmp(ir.Lt, i.get(), n.get()) }, func() {
					mix(l.fn.LoadB(l.b, s.get(), i.get(), pos))
					i.set(m.bin(ir.Add, i.get(), m.c(1)))
				})
				mix(n.get())
			default:
				switch it := l.irType(nil, lt); {
				case it.IsFloat():
					bits := ir.U64
					if it == ir.F32 {
						bits = ir.U32
					}
					b := m.v("bits", l.ldm(bits, p, pos))
					z := l.fn.Float(l.b, ir.FEq, pos, l.ldm(it, p, pos), l.fn.FloatConst(l.b, it, 0, pos))
					nz := m.when(z)
					b.set(l.fn.TypedConst(l.b, bits, 0, pos)) // -0 hashes as +0
					m.join(nz)
					mix(b.get())
				case it == ir.Bool:
					mix(l.ldm(ir.U8, p, pos))
				case it == ir.Ptr:
					mix(l.ldm(ir.U64, p, pos))
				default:
					mix(l.ldm(it, p, pos))
				}
			}
		})
		l.fn.Ret(l.b, pos, l.fn.Conv(l.b, ir.I64, l.fmix(h.get(), pos), pos))
		prune(l.fn)
	})
}

// fmix is MurmurHash3's finalizer of the U64 x, so that every bit of the
// hash affects its low bits, which probing uses.
func (l *lowerer) fmix(x *ir.Instr, pos ir.Pos) *ir.Instr {
	m := mb{l, pos}
	shr := func(x *ir.Instr) *ir.Instr {
		return m.bin(ir.Xor, x, l.fn.Shift(l.b, ir.Shr, x, l.fn.TypedConst(l.b, ir.U64, 33, pos), pos))
	}
	return shr(m.bin(ir.Mul, shr(x), l.fn.TypedConst(l.b, ir.U64, fmixMul, pos)))
}

// hashIface mixes into the hash the hash of the interface at p, of type t,
// 0 for nil, or panics if its dynamic type is not comparable.
func (l *lowerer) hashIface(t types.Type, p place, form func() *ir.Instr, mix func(*ir.Instr), pos ir.Pos) {
	m := mb{l, pos}
	l.hashIfaces = true
	tab := m.v("tab", l.ldm(ir.Ptr, p, pos))
	isNil := m.when(m.cmp(ir.Ne, tab.get(), m.nilPtr()))
	d := m.v("d", l.dynDesc(t, tab.get(), pos))
	code := m.v("code", m.ld(ir.Ptr, d.get(), descHash))
	ok := m.when(m.cmp(ir.Eq, code.get(), m.nilPtr()))
	plain := m.when(m.cmp(ir.Eq, form(), m.c(0)))
	l.panicMsg([][]*ir.Instr{l.strWords("runtime error: hash of unhashable type ", pos), l.descName(d.get(), pos)}, pos)
	l.b = plain
	l.panicMsg([][]*ir.Instr{l.strWords("hash of unhashable type: ", pos), l.descName(d.get(), pos)}, pos)
	l.b = ok
	mix(l.fn.CallInd(l.b, hashSig, code.get(), []*ir.Instr{d.get(), l.ldm(ir.Ptr, p.field(8, t), pos), form()}, pos))
	m.join(isNil)
}

// ptrHashFunc returns .hashptr, the hash function of a descriptor of a
// pointer type, whose interface data is the pointer itself (D-041).
func (l *lowerer) ptrHashFunc(pos ir.Pos) *ir.Func {
	return l.wrapper("hashptr", hashSig.Params, hashSig.Results, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		mem := l.newMem("p", types.Typ[types.Uintptr], pos)
		l.fn.StM(l.b, l.addr(mem, pos), l.fn.Const(l.b, 0, pos), ps[1](), pos)
		x := l.fn.Binary(l.b, ir.Mul, l.fn.LdM(l.b, ir.U64, l.addr(mem, pos), l.fn.Const(l.b, 0, pos), pos), l.fn.TypedConst(l.b, ir.U64, hashMul, pos), pos)
		l.fn.Ret(l.b, pos, l.fn.Conv(l.b, ir.I64, l.fmix(x, pos), pos))
		prune(l.fn)
	})
}

// descHashes gives every comparable type descriptor its hash function, in
// a program that hashes an interface (D-042); Init writes them.
func (l *lowerer) descHashes() {
	if !l.hashIfaces {
		return
	}
	for _, d := range l.descs {
		if d.eq == nil {
			continue
		}
		l.fn = d.eq // the file of the functions made
		if isDirect(d.t) {
			d.hash = l.ptrHashFunc(d.eq.Pos)
		} else {
			d.hash = l.hashFunc(d.t, d.eq.Pos)
		}
	}
	l.fn = nil
}

// mapOperands evaluates the map of m[k], then its key, converted to the
// key type, and returns them.
func (l *lowerer) mapOperands(e *ast.IndexExpr) (*mapT, func() *ir.Instr, []*ir.Instr) {
	pos := l.pos(e, e.Lbrack)
	mt := l.mapType(l.pkg.Info.Types[e.X].Type)
	m := l.expr(e.X)
	get := func() *ir.Instr { return m }
	if l.branches([]ast.Expr{e.Index}) {
		get = l.spill(m, pos)
	}
	k := l.valueFor(e.Index, mt.key)
	return mt, get, k
}

// mapAccess lowers m[k] read: the Ptr of its element, or of .mzero.
func (l *lowerer) mapAccess(e *ast.IndexExpr) (*mapT, *ir.Instr) {
	mt, m, k := l.mapOperands(e)
	pos := l.pos(e, e.Lbrack)
	return mt, l.fn.Call(l.b, l.mapFunc(mt, mapAccess, pos), append([]*ir.Instr{m()}, k...), pos)
}

// mapIndex returns the place of the value of m[k], which the caller reads
// at once: the element, or for an array or struct a copy of it, since
// what follows, an index of it, could change the map (D-042).
func (l *lowerer) mapIndex(e *ast.IndexExpr) place {
	mt, p := l.mapAccess(e)
	pos := l.pos(e, e.Lbrack)
	src := l.valuePlace(p, mt.elem, pos)
	if !isAggregate(mt.elem) {
		return src
	}
	dst := l.memPlace(l.newMem("tmp", mt.elem, pos), mt.elem, pos)
	l.copyPlace(dst, src, pos)
	return dst
}

// mapIndexOk lowers v, ok := m[k]: the value, or the zero value, and
// whether k is in m.
func (l *lowerer) mapIndexOk(e *ast.IndexExpr) [][]*ir.Instr {
	mt, p := l.mapAccess(e)
	pos := l.pos(e, e.Lbrack)
	get := l.keep(p, pos)
	v := l.loadPlace(place{base: get, t: mt.elem}, pos)
	ok := l.fn.Compare(l.b, ir.Ne, get(), l.zeroAddr(mt.elem, pos), pos)
	return [][]*ir.Instr{v, {ok}}
}

// mapTarget returns the place of m[k] as the target of an assignment: its
// map and key are evaluated now, and its element located by .mapassignN
// as its deferred check, just before the store (D-042).
func (l *lowerer) mapTarget(e *ast.IndexExpr) place {
	mt, m, k := l.mapOperands(e)
	pos := l.pos(e, e.Lbrack)
	getM, getK := l.spill(m(), pos), l.spillWords(k, pos)
	elem := l.fn.NewSlot("elem", ir.Ptr, pos)
	f := l.mapFunc(mt, mapAssign, pos)
	assign := func() {
		l.fn.Store(l.b, elem, l.fn.Call(l.b, f, append([]*ir.Instr{getM()}, getK()...), pos), pos)
	}
	return place{base: func() *ir.Instr { return l.fn.Load(l.b, elem, pos) }, t: mt.elem, checks: []check{{assign: assign, pos: pos}}}
}

// mapLit lowers a map literal: a new map, then each key and element, in
// order, assigned.
func (l *lowerer) mapLit(lit *ast.CompositeLit) *ir.Instr {
	pos := l.pos(lit, lit.Lbrace)
	mt := l.mapType(l.pkg.Info.Types[lit].Type)
	hdr := l.spill(l.fn.Alloc(l.b, l.fn.Const(l.b, hdrSize, pos), pos), pos)
	f := l.mapFunc(mt, mapAssign, pos)
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			l.fail(elt, "map literal element without a key")
		}
		k := l.spillWords(l.litElem(kv.Key, mt.key), pos)
		v := l.spillWords(l.litElem(kv.Value, mt.elem), pos)
		p := l.fn.Call(l.b, f, append([]*ir.Instr{hdr()}, k()...), pos)
		l.storePlace(l.valuePlace(p, mt.elem, pos), v(), pos)
	}
	return hdr()
}

// litElem lowers a key or element e of a map literal, of type t: an elided
// &T{…} is a new heap object.
func (l *lowerer) litElem(e ast.Expr, t types.Type) []*ir.Instr {
	if lit, ok := ast.Unparen(e).(*ast.CompositeLit); ok && !isIface(t) {
		if pt, ok := pointee(t); ok {
			return []*ir.Instr{l.heapLit(lit, pt, l.pos(e, e.Pos()))}
		}
	}
	return l.valueFor(e, t)
}

// mapMake lowers make of a map: a new, empty header. The size hint is
// evaluated and otherwise ignored (D-042).
func (l *lowerer) mapMake(call *ast.CallExpr) *ir.Instr {
	pos := l.pos(call, call.Pos())
	if len(call.Args) == 2 {
		l.value(call.Args[1])
	}
	return l.fn.Alloc(l.b, l.fn.Const(l.b, hdrSize, pos), pos)
}

// mapBuiltin lowers delete(m, k) and clear(m).
func (l *lowerer) mapBuiltin(call *ast.CallExpr) {
	pos := l.pos(call, call.Pos())
	if l.isBuiltin(call, "clear") {
		l.fn.Call(l.b, l.mapClearFunc(pos), []*ir.Instr{l.expr(call.Args[0])}, pos)
		return
	}
	mt := l.mapType(l.pkg.Info.Types[call.Args[0]].Type)
	m := l.expr(call.Args[0])
	get := func() *ir.Instr { return m }
	if l.branches(call.Args[1:]) {
		get = l.spill(m, pos)
	}
	k := l.valueFor(call.Args[1], mt.key)
	l.fn.Call(l.b, l.mapFunc(mt, mapDelete, pos), append([]*ir.Instr{get()}, k...), pos)
}

// mapNextFunc returns .mapnextN(m, ents, i, clears) of mt, a step of a
// range over the map m that began on the entry array ents, after clears
// calls of clear, now at position i: the Ptr of the next entry to visit,
// or 0 at the end, and the next position (D-042). While ents is m's, it
// is the next live entry. Once m has grown, it is m's entry of the next
// live key of ents that m still holds or, while no clear has run, an
// entry of ents whose key is not equal to itself, which m cannot hold
// and which only a clear removes.
func (l *lowerer) mapNextFunc(mt *mapT, pos ir.Pos) *ir.Func {
	return l.wrapper("mapnext"+itoa(mt.id), []ir.Type{ir.Ptr, ir.Ptr, ir.I64, ir.I64}, []ir.Type{ir.Ptr, ir.I64}, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		m := mb{l, pos}
		hdr, ents, clears := ps[0], ps[1], ps[3]
		i := m.v("i", ps[2]())
		ret := func(e func() *ir.Instr) { l.fn.Ret(l.b, pos, e(), i.get()) }
		nonNil := m.when(m.cmp(ir.Eq, ents(), m.nilPtr()))
		ret(m.nilPtr)
		l.b = nonNil
		head := l.fn.NewBlock()
		m.join(head)
		same := func() *ir.Instr { return m.cmp(ir.Eq, m.ld(ir.Ptr, hdr(), hdrEntries), ents()) }
		// The entries to visit: m's used, or the old array's final one.
		n := m.v("n", m.c(0))
		grown := m.when(same())
		n.set(m.ld(ir.I64, hdr(), hdrUsed))
		join := l.fn.NewBlock()
		l.fn.Jump(l.b, join, pos)
		l.b = grown
		n.set(m.ld(ir.I64, ents(), 0))
		m.join(join)
		more := m.when(m.cmp(ir.Ge, i.get(), n.get()))
		ret(m.nilPtr)
		l.b = more
		e := m.v("e", m.entry(ents(), i.get(), m.c(mt.esize)))
		i.set(m.bin(ir.Add, i.get(), m.c(1)))
		live := m.when(m.cmp(ir.Eq, m.ld(ir.I64, e.get(), entLive), m.c(0)))
		l.fn.Jump(l.b, head, pos)
		l.b = live
		old := m.when(same())
		ret(e.get)
		l.b = old
		// m has grown: its entry of the key, if it still holds it.
		miss := l.fn.NewBlock()
		nonEmpty := m.when(m.cmp(ir.Eq, m.ld(ir.I64, hdr(), hdrCount), m.c(0)))
		l.fn.Jump(l.b, miss, pos)
		l.b = nonEmpty
		kp := place{base: e.get, c: entKey, t: mt.key}
		h := m.v("h", m.ld(ir.I64, e.get(), entHash))
		f, notFound := l.find(mt, hdr, h.get, kp, pos)
		ret(f.get)
		l.b = notFound
		m.join(miss)
		// Otherwise a key not equal to itself, unless a clear has run.
		noClear := m.when(m.cmp(ir.Ne, clears(), m.ld(ir.I64, hdr(), hdrClears)))
		l.fn.Jump(l.b, head, pos)
		l.b = noClear
		differ := m.when(l.equal(kp, kp, pos))
		l.fn.Jump(l.b, head, pos)
		l.b = differ
		ret(e.get)
		prune(l.fn)
	})
}

// mapRange lowers a range over a map (increment 8.5b, D-042): the map is
// evaluated once, then each step of .mapnextN gives the entry whose key
// and element the iteration assigns, as := declares them anew, in each
// iteration, or as = assigns them.
func (l *lowerer) mapRange(s *ast.RangeStmt) {
	pos := l.pos(s, s.For)
	mt := l.mapType(l.pkg.Info.Types[s.X].Type)
	m := mb{l, pos}
	hdr := m.v("map", l.expr(s.X))
	ents := m.v("ents", m.nilPtr())
	clears := m.v("clears", m.c(0))
	i := m.v("i", m.c(0))
	isNil := m.when(m.cmp(ir.Ne, hdr.get(), m.nilPtr()))
	ents.set(m.ld(ir.Ptr, hdr.get(), hdrEntries))
	clears.set(m.ld(ir.I64, hdr.get(), hdrClears))
	head, exit := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Jump(l.b, head, pos)
	l.b = isNil
	m.join(head)
	r := l.fn.Call(l.b, l.mapNextFunc(mt, pos), []*ir.Instr{hdr.get(), ents.get(), i.get(), clears.get()}, pos)
	e := m.v("e", l.fn.Extract(l.b, r, 0, pos))
	i.set(l.fn.Extract(l.b, r, 1, pos))
	body := l.fn.NewBlock()
	l.fn.Branch(l.b, m.cmp(ir.Eq, e.get(), m.nilPtr()), exit, body, pos)
	l.b, l.done = body, false
	parts := []place{{base: e.get, c: entKey, t: mt.key}, {base: e.get, c: mt.voff, t: mt.elem}}
	var targets []ast.Expr
	var vals []func() []*ir.Instr
	for k, x := range []ast.Expr{s.Key, s.Value} {
		if id, ok := x.(*ast.Ident); x == nil || ok && id.Name == "_" {
			continue
		}
		xpos := l.pos(x, x.Pos())
		if s.Tok == token.DEFINE {
			// An array or struct is copied straight from the entry.
			var w []*ir.Instr
			if isAggregate(parts[k].t) {
				w = []*ir.Instr{l.fn.PAdd(l.b, e.get(), m.c(parts[k].c), xpos)}
			} else {
				w = l.loadPlace(parts[k], xpos)
			}
			l.store(l.defineVar(l.ident(x)), w, xpos)
			continue
		}
		targets = append(targets, x)
		vals = append(vals, l.spillWords(l.loadPlace(parts[k], xpos), xpos))
	}
	// As an assignment: the targets' operands, then the stores.
	vs := make([]variable, len(targets))
	for k, x := range targets {
		vs[k] = l.lvalue(x)
	}
	for k, x := range targets {
		l.store(vs[k], vals[k](), l.pos(x, x.Pos()))
	}
	lp := &target{next: head, exit: exit}
	l.targets = append(l.targets, lp)
	l.stmts(s.Body.List)
	if !l.done {
		l.fn.Jump(l.b, head, l.pos(s.Body, s.Body.Rbrace))
	}
	l.targets = l.targets[:len(l.targets)-1]
	l.b, l.done = exit, false
}
