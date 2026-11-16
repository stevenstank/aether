package lower

// Slices (Phase 7 increment 7.2, D-036).
//
// A slice is three words: the Ptr of its first element, its I64 length,
// and its I64 capacity; nil is (0, 0, 0). Its array is a heap object, or
// the memory of an array variable whose slicing took its address
// (pointer.go), so an element is a place at the Ptr plus a multiple of the
// element's size. Untyped nil becomes a slice where its context gives it a
// slice type (valueFor).
//
// Bounds are checked as gc checks them, branching to its run-time errors.
// A slice expression with a low bound of 0 keeps the Ptr, so slicing a nil
// slice gives nil; any other low bound is at most the capacity, so the array
// exists and padd applies. append grows to the largest of the needed length,
// twice the capacity, and 4 elements (V-6), and moves the old elements.

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// heapLimit is the bytes the heap holds, interp.HeapLimit and the
// reservation of aether.alloc (D-010): make rejects a larger array, as gc
// rejects one larger than its own limit.
const heapLimit = 1 << 36

func isSlice(t types.Type) bool {
	_, ok := t.Underlying().(*types.Slice)
	return ok
}

// sliceElem returns the element type of the slice type t.
func sliceElem(t types.Type) types.Type { return t.Underlying().(*types.Slice).Elem() }

// isBuiltin reports whether call calls the predeclared builtin name.
func (l *lowerer) isBuiltin(call *ast.CallExpr, name string) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := l.pkg.Info.Uses[id].(*types.Builtin)
	return ok && b.Name() == name
}

// valueFor is value for an expression whose context gives it the type t,
// nil if unknown: untyped nil in a slice context is the nil slice.
func (l *lowerer) valueFor(e ast.Expr, t types.Type) []*ir.Instr {
	if t != nil && isSlice(t) && l.pkg.Info.Types[e].IsNil() {
		pos := l.pos(e, e.Pos())
		return []*ir.Instr{l.fn.TypedConst(l.b, ir.Ptr, 0, pos), l.fn.Const(l.b, 0, pos), l.fn.Const(l.b, 0, pos)}
	}
	return l.value(e)
}

// keepWords is keep for each word of a value.
func (l *lowerer) keepWords(w []*ir.Instr, pos ir.Pos) []func() *ir.Instr {
	out := make([]func() *ir.Instr, len(w))
	for i, v := range w {
		out[i] = l.keep(v, pos)
	}
	return out
}

// sliceValue lowers e, of slice type, and returns its three words.
func (l *lowerer) sliceValue(e ast.Expr) []*ir.Instr {
	pos := l.pos(e, e.Pos())
	t := l.pkg.Info.Types[e].Type
	switch e := e.(type) {
	case *ast.Ident:
		return l.load(l.lookup(e), pos)
	case *ast.ParenExpr:
		return l.valueFor(e.X, t)
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
		return l.loadPlace(l.addrOf(e, false), pos)
	case *ast.SliceExpr:
		return l.sliceExpr(e)
	case *ast.CompositeLit:
		return l.sliceLit(e)
	case *ast.CallExpr:
		switch {
		case l.pkg.Info.Types[e.Fun].IsType():
			if isString(l.pkg.Info.Types[e.Args[0]].Type) {
				if isRuneSlice(t) {
					return l.runesConversion(e)
				}
				return l.bytesConversion(e)
			}
			// Between slice types of identical underlying types, or of nil.
			return l.valueFor(e.Args[0], t)
		case l.isBuiltin(e, "make"):
			return l.makeSlice(e)
		case l.isBuiltin(e, "append"):
			return l.appendSlice(e)
		}
		vals := l.results(e, l.call(e))
		if len(vals) != 1 {
			l.fail(e, "call of %d results used as one value", len(vals))
		}
		return vals[0]
	}
	l.fail(e, "unexpected slice expression %s", types.ExprString(e))
	return nil
}

// sliceLit lowers a slice literal: a new heap array as long as the literal,
// zero, filled with its elements.
func (l *lowerer) sliceLit(lit *ast.CompositeLit) []*ir.Instr {
	pos := l.pos(lit, lit.Pos())
	var n, k int64
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			var exact bool
			if k, exact = constantIndex(l.pkg.Info.Types[kv.Key]); !exact {
				l.fail(kv, "slice literal key %s is not a constant index", types.ExprString(kv.Key))
			}
		}
		k++
		n = max(n, k)
	}
	arr := types.NewArray(sliceElem(l.pkg.Info.Types[lit].Type), n)
	p := place{base: l.keep(l.alloc(arr, pos), pos), t: arr}
	l.fill(p, lit) // the array holds zeros
	return []*ir.Instr{p.base(), l.fn.Const(l.b, n, pos), l.fn.Const(l.b, n, pos)}
}

// sliceIndex returns the place of s[i], an element of a slice. The slice is
// evaluated, then the index, which must be below the length (an unsigned
// comparison), checked now or, if lhs, at the place's first use.
func (l *lowerer) sliceIndex(e *ast.IndexExpr, lhs bool) place {
	pos, ipos := l.pos(e, e.Pos()), l.pos(e, e.Lbrack)
	et := sliceElem(l.pkg.Info.Types[e.X].Type)
	w := l.value(e.X)
	ptr, n := l.keep(w[0], pos), l.keep(w[1], pos)
	i := l.expr(e.Index)
	if !i.Type.IsInteger() {
		l.fail(e.Index, "index of type %s", i.Type)
	}
	idx := l.keep(i, ipos)
	p := place{base: ptr, t: et}
	if lhs {
		p.checks = []check{{idx: idx, nv: n, pos: ipos}}
	} else {
		l.checkLen(idx, n, ipos)
	}
	// Below the length, so the same number as an I64.
	return l.indexed(p, func() *ir.Instr { return l.toI64(idx(), ipos) }, sizes.Sizeof(et), et, pos)
}

// toI64 converts v, of an integer type, to I64.
func (l *lowerer) toI64(v *ir.Instr, pos ir.Pos) *ir.Instr {
	if v.Type == ir.I64 {
		return v
	}
	return l.fn.Conv(l.b, ir.I64, v, pos)
}

// toU64 converts v, of an integer type, to U64.
func (l *lowerer) toU64(v *ir.Instr, pos ir.Pos) *ir.Instr {
	if v.Type == ir.U64 {
		return v
	}
	return l.fn.Conv(l.b, ir.U64, v, pos)
}

// checkLen checks that the index idx is below the length n, an I64, as an
// unsigned number, and panics with both if not.
func (l *lowerer) checkLen(idx, n func() *ir.Instr, pos ir.Pos) {
	fail := l.fn.Compare(l.b, ir.Ge, l.toU64(idx(), pos), l.fn.Conv(l.b, ir.U64, n(), pos), pos)
	l.boundsFail(fail, ir.PanicIndex, idx, n, pos)
}

// boundsFail branches to a block that panics with the bounds error kind,
// x, and y, if fail is true, and continues in a new block otherwise.
func (l *lowerer) boundsFail(fail *ir.Instr, kind int, x, y func() *ir.Instr, pos ir.Pos) {
	panicking, ok := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, fail, panicking, ok, pos)
	l.b = panicking
	l.fn.Panic(panicking, kind, pos, x(), l.toI64(y(), pos))
	l.b = ok
}

// sliceExpr lowers x[lo:hi] and x[lo:hi:max] of a slice, an addressable
// array, or a pointer to an array, and x[lo:hi] of a string, which gives a
// string, its two words (D-037). The operand is evaluated, then the bounds,
// left to right; then the bounds are checked in gc's order, as unsigned
// numbers, against the capacity, or an array's or a string's length, and
// each other, and a pointer to an array is checked for nil before it is
// used.
func (l *lowerer) sliceExpr(e *ast.SliceExpr) []*ir.Instr {
	pos := l.pos(e, e.Lbrack)
	xt := l.pkg.Info.Types[e.X].Type
	var ptr, length, capacity func() *ir.Instr
	var arr place
	var et types.Type
	isStr := isString(xt)
	isArr := !isSlice(xt) && !isStr
	switch {
	case isStr:
		w := l.value(e.X)
		ptr, length, et = l.keep(w[0], pos), l.keep(w[1], pos), types.Typ[types.Byte]
		capacity = length
	case isArr:
		at := xt
		if t, ok := pointee(xt); ok {
			at, arr = t, l.deref(e.X)
		} else {
			arr = l.addrOf(e.X, false)
		}
		a, ok := at.Underlying().(*types.Array)
		if !ok {
			l.fail(e, "slice of %s", xt)
		}
		et, length = a.Elem(), func() *ir.Instr { return l.fn.Const(l.b, a.Len(), pos) }
		capacity = length
	default:
		w := l.value(e.X)
		ptr, length, capacity, et = l.keep(w[0], pos), l.keep(w[1], pos), l.keep(w[2], pos), sliceElem(xt)
	}
	bound := func(b ast.Expr) func() *ir.Instr {
		if b == nil {
			return nil
		}
		v := l.expr(b)
		if !v.Type.IsInteger() {
			l.fail(b, "slice bound of type %s", v.Type)
		}
		return l.keep(v, pos)
	}
	lo, hi, mx := bound(e.Low), bound(e.High), bound(e.Max)
	if isArr {
		ptr = l.keep(l.pointer(arr, pos), pos) // the nil check, for a pointer
	}
	gt := func(x, y func() *ir.Instr) *ir.Instr {
		return l.fn.Compare(l.b, ir.Gt, l.toU64(x(), pos), l.toU64(y(), pos), pos)
	}
	top, end := length, capacity // the new slice's end and capacity's end
	if mx != nil {
		kind := ir.PanicSlice3Acap
		if isArr {
			kind = ir.PanicSlice3Alen
		}
		l.boundsFail(gt(mx, capacity), kind, mx, capacity, pos)
		l.boundsFail(gt(hi, mx), ir.PanicSlice3B, hi, mx, pos)
		if lo != nil {
			l.boundsFail(gt(lo, hi), ir.PanicSlice3C, lo, hi, pos)
		}
		top, end = hi, mx
	} else {
		if hi != nil {
			kind := ir.PanicSliceAcap
			if isArr || isStr {
				kind = ir.PanicSliceAlen
			}
			l.boundsFail(gt(hi, capacity), kind, hi, capacity, pos)
			top = hi
		}
		if lo != nil {
			l.boundsFail(gt(lo, top), ir.PanicSliceB, lo, top, pos)
		}
	}
	size := sizes.Sizeof(et)
	base := ptr
	if lo != nil {
		if tv := l.pkg.Info.Types[e.Low]; tv.Value == nil {
			// Ptr plus lo elements, unless lo is 0, which a nil slice allows.
			s := l.fn.NewSlot("tmp", ir.Ptr, pos)
			l.fn.Store(l.b, s, ptr(), pos)
			add, join := l.fn.NewBlock(), l.fn.NewBlock()
			l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, l.toI64(lo(), pos), l.fn.Const(l.b, 0, pos), pos), add, join, pos)
			l.b = add
			l.fn.Store(l.b, s, l.fn.PAdd(l.b, ptr(), l.fn.Binary(l.b, ir.Mul, l.toI64(lo(), pos), l.fn.Const(l.b, size, pos), pos), pos), pos)
			l.fn.Jump(l.b, join, pos)
			l.b = join
			base = func() *ir.Instr { return l.fn.Load(l.b, s, pos) }
		} else if k, _ := constantIndex(tv); k != 0 {
			base = func() *ir.Instr { return l.fn.PAdd(l.b, ptr(), l.fn.Const(l.b, k*size, pos), pos) }
		}
	}
	low := func() *ir.Instr { return l.fn.Const(l.b, 0, pos) }
	if lo != nil {
		low = func() *ir.Instr { return l.toI64(lo(), pos) }
	}
	p := base()
	n := l.fn.Binary(l.b, ir.Sub, l.toI64(top(), pos), low(), pos)
	if isStr {
		return []*ir.Instr{p, n}
	}
	c := l.fn.Binary(l.b, ir.Sub, l.toI64(end(), pos), low(), pos)
	return []*ir.Instr{p, n, c}
}

// makeSlice lowers make([]T, n) and make([]T, n, c): the length, then the
// capacity, are checked as gc checks them, then a zero array of c elements
// is allocated.
func (l *lowerer) makeSlice(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	size := sizes.Sizeof(sliceElem(l.pkg.Info.Types[call].Type))
	args := call.Args[1:]
	n := l.keep(l.expr(args[0]), pos)
	c := n
	if len(args) == 2 {
		c = l.keep(l.expr(args[1]), pos)
	}
	limit := int64(1<<63 - 1)
	if size > 0 {
		limit = heapLimit / size
	}
	// Above the limit as unsigned numbers, so a negative one too.
	tooLarge := func(v func() *ir.Instr) *ir.Instr {
		return l.fn.Compare(l.b, ir.Gt, l.toU64(v(), pos), l.fn.TypedConst(l.b, ir.U64, limit, pos), pos)
	}
	l.guard(tooLarge(n), ir.PanicMakeLen, pos)
	if len(args) == 2 {
		l.guard(tooLarge(c), ir.PanicMakeCap, pos)
		l.guard(l.fn.Compare(l.b, ir.Gt, l.toU64(n(), pos), l.toU64(c(), pos), pos), ir.PanicMakeCap, pos)
	}
	nv, cv := l.toI64(n(), pos), l.toI64(c(), pos)
	p := l.fn.Alloc(l.b, l.fn.Binary(l.b, ir.Mul, cv, l.fn.Const(l.b, size, pos), pos), pos)
	return []*ir.Instr{p, nv, cv}
}

// appendSlice lowers append(s, x…) and append(s, t...): the arguments are
// evaluated; if the new length exceeds the capacity, a larger array is
// allocated and the old elements moved to it; then the new elements are
// stored, or t's moved, after the old ones.
func (l *lowerer) appendSlice(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	st := l.pkg.Info.Types[call].Type
	et := sliceElem(st)
	size := sizes.Sizeof(et)
	ts := []types.Type{st}
	for range call.Args[1:] {
		if call.Ellipsis.IsValid() {
			ts = append(ts, st)
		} else {
			ts = append(ts, et)
		}
	}
	vals := l.values(call.Args, ts)
	var ws [][]func() *ir.Instr
	for _, w := range vals {
		ws = append(ws, l.keepWords(w, pos))
	}
	s := ws[0]
	add := func() *ir.Instr { return l.fn.Const(l.b, int64(len(call.Args)-1), pos) }
	if call.Ellipsis.IsValid() {
		add = ws[1][1]
	}
	c := func(v int64) *ir.Instr { return l.fn.Const(l.b, v, pos) }
	newLen := l.keep(l.fn.Binary(l.b, ir.Add, s[1](), add(), pos), pos)
	if size == 0 {
		// Only a zero-size element's length can reach the largest int.
		l.guard(l.fn.Compare(l.b, ir.Lt, newLen(), s[1](), pos), ir.PanicGrowLen, pos)
	}
	ptrS, capS := l.fn.NewSlot("ptr", ir.Ptr, pos), l.fn.NewSlot("cap", ir.I64, pos)
	l.fn.Store(l.b, ptrS, s[0](), pos)
	l.fn.Store(l.b, capS, s[2](), pos)
	load := func(sl *ir.Slot) *ir.Instr { return l.fn.Load(l.b, sl, pos) }
	grow, join := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Gt, newLen(), load(capS), pos), grow, join, pos)
	l.b = grow
	m := l.fn.NewSlot("newcap", ir.I64, pos)
	l.fn.Store(l.b, m, newLen(), pos)
	raise := func(v func() *ir.Instr) {
		up, next := l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Gt, v(), load(m), pos), up, next, pos)
		l.b = up
		l.fn.Store(l.b, m, v(), pos)
		l.fn.Jump(l.b, next, pos)
		l.b = next
	}
	if size > 0 {
		raise(func() *ir.Instr { return l.fn.Binary(l.b, ir.Add, load(capS), load(capS), pos) })
		raise(func() *ir.Instr { return c(4) })
	}
	np := l.fn.Alloc(l.b, l.fn.Binary(l.b, ir.Mul, load(m), c(size), pos), pos)
	l.fn.Move(l.b, np, load(ptrS), l.fn.Binary(l.b, ir.Mul, s[1](), c(size), pos), pos)
	l.fn.Store(l.b, ptrS, np, pos)
	l.fn.Store(l.b, capS, load(m), pos)
	l.fn.Jump(l.b, join, pos)
	l.b = join
	if call.Ellipsis.IsValid() {
		// t's elements after the old ones; with none, the Ptr may be nil.
		body, done := l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Gt, add(), c(0), pos), body, done, pos)
		l.b = body
		dst := l.fn.PAdd(l.b, load(ptrS), l.fn.Binary(l.b, ir.Mul, s[1](), c(size), pos), pos)
		l.fn.Move(l.b, dst, ws[1][0](), l.fn.Binary(l.b, ir.Mul, add(), c(size), pos), pos)
		l.fn.Jump(l.b, done, pos)
		l.b = done
	} else {
		for i, w := range ws[1:] {
			dst := place{base: func() *ir.Instr { return load(ptrS) }, t: et, dyn: func() *ir.Instr {
				return l.fn.Binary(l.b, ir.Mul, l.fn.Binary(l.b, ir.Add, s[1](), c(int64(i)), pos), c(size), pos)
			}}
			v := make([]*ir.Instr, len(w))
			for j, get := range w {
				v[j] = get()
			}
			l.storePlace(dst, v, pos)
		}
	}
	return []*ir.Instr{load(ptrS), newLen(), load(capS)}
}

// copySlice lowers copy(dst, src) of slices: both are evaluated, then the
// shorter length's elements are moved, overlap allowed, and that length is
// the result.
func (l *lowerer) copySlice(call *ast.CallExpr) *ir.Instr {
	pos := l.pos(call, call.Pos())
	size := sizes.Sizeof(sliceElem(l.pkg.Info.Types[call.Args[0]].Type))
	vals := l.values(call.Args, []types.Type{l.pkg.Info.Types[call.Args[0]].Type, l.pkg.Info.Types[call.Args[1]].Type})
	d, s := l.keepWords(vals[0], pos), l.keepWords(vals[1], pos)
	n := l.fn.NewSlot("n", ir.I64, pos)
	l.fn.Store(l.b, n, d[1](), pos)
	lower, next := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Lt, s[1](), l.fn.Load(l.b, n, pos), pos), lower, next, pos)
	l.b = lower
	l.fn.Store(l.b, n, s[1](), pos)
	l.fn.Jump(l.b, next, pos)
	l.b = next
	// With n = 0, move does nothing, whatever the Ptrs (D-036).
	l.fn.Move(l.b, d[0](), s[0](), l.fn.Binary(l.b, ir.Mul, l.fn.Load(l.b, n, pos), l.fn.Const(l.b, size, pos), pos), pos)
	return l.fn.Load(l.b, n, pos)
}
