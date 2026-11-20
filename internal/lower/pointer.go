package lower

// Pointers and the heap (Phase 7 increment 7.1, D-035).
//
// A pointer is one Ptr word, 0 for nil. It addresses a heap object or a
// global memory object, never a frame: every local variable or parameter
// whose address the program takes lives in a heap object, allocated where
// it is declared (a parameter at entry, copied there), and every such
// package-level variable is a global memory object. Such a variable is a
// place, as an array or struct variable is (aggregate.go); the slot in ptrs
// holds its object's Ptr.
//
// Every access through a pointer is preceded by an explicit nil check,
// which panics with gc's message. The check is one of the place's deferred
// checks, made at its first access: after the operands of the expression
// selecting it, and in an assignment, at the store, as the target's bounds
// checks are (spec §Assignment statements). A pointer loaded on the way to
// a place is checked before that load.

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// addressTaken returns the variables whose address the program takes: the
// variable that the operand of each & lies in, if it is not reached through
// a pointer, and likewise for the implicit & of a method call.
func addressTaken(info *types.Info, files []*ast.File) map[*types.Var]bool {
	taken := map[*types.Var]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
				if v := rootVar(info, u.X); v != nil {
					taken[v] = true
				}
			}
			// So does calling a method with a pointer receiver on it, or
			// on an embedded field of it (D-038).
			if e, ok := n.(*ast.SelectorExpr); ok {
				if sel := info.Selections[e]; sel != nil && sel.Kind() == types.MethodVal && frontend.TakesAddress(sel) {
					if v := rootVar(info, e.X); v != nil {
						taken[v] = true
					}
				}
			}
			// Slicing an array takes its address (D-036).
			if s, ok := n.(*ast.SliceExpr); ok {
				if _, ok := info.Types[s.X].Type.Underlying().(*types.Array); ok {
					if v := rootVar(info, s.X); v != nil {
						taken[v] = true
					}
				}
			}
			return true
		})
	}
	return taken
}

// rootVar returns the variable whose memory x lies in, or nil if x is
// reached through a pointer or is not addressable memory of a variable.
func rootVar(info *types.Info, x ast.Expr) *types.Var {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		v, _ := info.Uses[x].(*types.Var)
		return v
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil && sel.Kind() == types.FieldVal && !sel.Indirect() {
			return rootVar(info, x.X)
		}
	case *ast.IndexExpr:
		if _, ok := info.Types[x.X].Type.Underlying().(*types.Array); ok {
			return rootVar(info, x.X)
		}
	}
	return nil
}

// pointee returns the type t points to, if t is a pointer type.
func pointee(t types.Type) (types.Type, bool) {
	p, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return nil, false
	}
	return p.Elem(), true
}

// alloc appends the allocation of a zero heap object for a value of t.
func (l *lowerer) alloc(t types.Type, pos ir.Pos) *ir.Instr {
	return l.allocOf(t, l.fn.Const(l.b, sizes.Sizeof(t), pos), pos)
}

// allocOf appends the allocation of n bytes for values of t: noscan if t
// holds no pointer (D-047).
func (l *lowerer) allocOf(t types.Type, n *ir.Instr, pos ir.Pos) *ir.Instr {
	if hasPointers(t) {
		return l.fn.Alloc(l.b, n, pos)
	}
	return l.fn.AllocNoScan(l.b, n, pos)
}

// hasPointers reports whether a value of t may hold a pointer word: a
// pointer, a string, a slice, a map, a func, an interface, or an array or
// struct of any of them. A type it does not know holds one.
func hasPointers(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0 || u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() > 0 && hasPointers(u.Elem())
	case *types.Struct:
		for f := range u.Fields() {
			if hasPointers(f.Type()) {
				return true
			}
		}
		return false
	}
	return true
}

// heapVar returns the storage of the address-taken variable v, whose heap
// object's Ptr is in slot s.
func (l *lowerer) heapVar(v *types.Var, s *ir.Slot, pos ir.Pos) variable {
	return variable{mem: &place{base: func() *ir.Instr { return l.fn.Load(l.b, s, pos) }, t: v.Type()}}
}

// defineHeap allocates the heap object of the address-taken variable v,
// declared by id, and returns its storage, which holds v's zero value.
func (l *lowerer) defineHeap(id *ast.Ident, v *types.Var) variable {
	if l.slots[v] != nil || l.ptrs[v] != nil {
		l.fail(id, "variable %s declared twice", id.Name)
	}
	pos := l.pos(id, id.Pos())
	s := l.fn.NewSlot(id.Name, ir.Ptr, pos)
	l.fn.Store(l.b, s, l.alloc(v.Type(), pos), pos)
	l.ptrs[v] = s
	return l.heapVar(v, s, pos)
}

// moveToHeap copies the address-taken parameter v, declared by id, from its
// slots, or the caller's copy of an array or struct, into a new heap object,
// which is its storage from then on.
func (l *lowerer) moveToHeap(id *ast.Ident, v *types.Var) {
	pos := l.pos(id, id.Pos())
	var src place
	var words []*ir.Instr
	if s := l.ptrs[v]; s != nil {
		src = place{base: func() *ir.Instr { return l.fn.Load(l.b, s, pos) }, t: v.Type()}
	} else {
		words = l.load(variable{slots: l.slots[v]}, pos)
	}
	delete(l.slots, v)
	delete(l.ptrs, v)
	dst := *l.defineHeap(id, v).mem
	if words == nil {
		l.copyPlace(dst, src, pos)
		return
	}
	l.storePlace(dst, words, pos)
}

// through returns the place that the pointer v, to a value of t, points to,
// with its nil check deferred to the place's first access.
func (l *lowerer) through(v *ir.Instr, t types.Type, pos ir.Pos) place {
	p := place{base: l.keep(v, pos), t: t}
	p.checks = []check{{ptr: p.base, pos: pos}}
	return p
}

// deref returns the place that x, an expression of pointer type, points to.
func (l *lowerer) deref(x ast.Expr) place {
	t, ok := pointee(l.pkg.Info.Types[x].Type)
	if !ok {
		l.fail(x, "indirection of a non-pointer %s", types.ExprString(x))
	}
	return l.through(l.expr(x), t, l.pos(x, x.Pos()))
}

// checkNil panics with gc's message if the pointer ptr is nil (D-035).
func (l *lowerer) checkNil(ptr func() *ir.Instr, pos ir.Pos) {
	fail := l.fn.Compare(l.b, ir.Eq, ptr(), l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos)
	l.guard(fail, ir.PanicNil, pos)
}

// pointer returns the address of the place p, after its checks.
func (l *lowerer) pointer(p place, pos ir.Pos) *ir.Instr {
	p = l.checkAll(p)
	if p.dyn == nil && p.c == 0 {
		return p.base()
	}
	base := p.base()
	return l.fn.PAdd(l.b, base, l.offset(p, pos), pos)
}

// addressOf lowers &x: the address of x, a variable or a field or element
// reached from one or through a pointer, whose checks come now; or of a new
// heap object holding the value of x, a composite literal.
func (l *lowerer) addressOf(e *ast.UnaryExpr) *ir.Instr {
	pos := l.pos(e, e.OpPos)
	if lit, ok := ast.Unparen(e.X).(*ast.CompositeLit); ok {
		return l.heapLit(lit, l.pkg.Info.Types[lit].Type, pos)
	}
	return l.pointer(l.addrOf(e.X, false), pos)
}

// heapLit returns the Ptr of a new heap object holding the value of the
// composite literal lit, of type t.
func (l *lowerer) heapLit(lit *ast.CompositeLit, t types.Type, pos ir.Pos) *ir.Instr {
	p := place{base: l.keep(l.alloc(t, pos), pos), t: t}
	l.fillElement(p, lit) // the object holds zeros; a slice or map literal is stored whole
	return p.base()
}
