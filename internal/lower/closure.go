package lower

// Closures (Phase 8 increment 8.3, D-040).
//
// A function literal is the generated function .litN, numbered in source
// order, whose first parameter is its closure, as for every function a
// func value calls (D-039). A variable of a function around the literal
// that its body uses, or that a literal nested in it uses, is captured: it
// is address-taken, so on the heap (D-035), and the closure holds its Ptr,
// after the code address, one word for each, in order of first use. The
// literal's body reads each captured variable's Ptr from its closure at
// entry and keeps it in a slot, as for a variable of its own on the heap,
// so the literal and the function around it share the variable. A literal
// that captures nothing has a static closure, as a function does.

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// literals numbers the function literals of pkg in source order, from 1,
// and finds the variables each captures: the local variables it, or a
// literal in it, uses that are declared outside it, in order of first use.
func literals(pkg *frontend.Package) (map[*ast.FuncLit]int, map[*ast.FuncLit][]*types.Var) {
	nums := map[*ast.FuncLit]int{}
	caps := map[*ast.FuncLit][]*types.Var{}
	var open []*ast.FuncLit // the literals around the node visited
	// Each declaration is walked on its own: the copies of generic code
	// have the positions of the code they copy (D-045), so positions
	// order the nodes of one declaration only.
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			open = open[:0]
			ast.Inspect(d, func(n ast.Node) bool {
				if n == nil {
					return true
				}
				for len(open) > 0 && n.Pos() >= open[len(open)-1].End() {
					open = open[:len(open)-1]
				}
				switch n := n.(type) {
				case *ast.FuncLit:
					nums[n] = len(nums) + 1
					open = append(open, n)
				case *ast.Ident:
					v, ok := pkg.Info.Uses[n].(*types.Var)
					if !ok || v.IsField() || v.Parent() == nil || v.Pkg() != nil && v.Parent() == v.Pkg().Scope() || v.Parent() == types.Universe {
						break
					}
					for _, lit := range open {
						inside := v.Pos() >= lit.Pos() && v.Pos() < lit.End()
						if !inside && !contains(caps[lit], v) {
							caps[lit] = append(caps[lit], v)
						}
					}
				}
				return true
			})
		}
	}
	return nums, caps
}

func contains(vs []*types.Var, v *types.Var) bool {
	for _, w := range vs {
		if w == v {
			return true
		}
	}
	return false
}

// litName is the name of the function of the literal numbered n.
func litName(n int) string { return genPrefix + "lit" + itoa(n) }

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// funcLit lowers a function literal: the closure of its function, which
// it creates, with the Ptrs of the variables it captures.
func (l *lowerer) funcLit(e *ast.FuncLit) *ir.Instr {
	pos := l.pos(e, e.Pos())
	f := l.litFuncs[e]
	if f == nil {
		f = l.litFunc(e, pos)
	}
	caps := l.captures[e]
	if len(caps) == 0 {
		return l.closure(f, pos)
	}
	var ptrs []*ir.Instr
	for _, v := range caps {
		s := l.ptrs[v]
		if s == nil {
			l.fail(e, "captured variable %s is not on the heap", v.Name())
		}
		ptrs = append(ptrs, l.fn.Load(l.b, s, pos))
	}
	c := l.fn.Alloc(l.b, l.fn.Const(l.b, int64(8+8*len(caps)), pos), pos)
	l.fn.StM(l.b, c, l.fn.Const(l.b, 0, pos), l.fn.FuncAddr(l.b, f, pos), pos)
	for i, p := range ptrs {
		l.fn.StM(l.b, c, l.fn.Const(l.b, int64(8+8*i), pos), p, pos)
	}
	return c
}

// litFunc creates the function of the literal e, with its signature, and
// queues the lowering of its body.
func (l *lowerer) litFunc(e *ast.FuncLit, pos ir.Pos) *ir.Func {
	sig, ok := l.pkg.Info.Types[e].Type.(*types.Signature)
	if !ok || sig.Variadic() {
		l.fail(e, "unexpected signature for a function literal")
	}
	f := l.prog.NewFunc(litName(l.lits[e]), l.fn.File, pos)
	f.Params = []ir.Type{ir.Ptr} // the closure
	for v := range sig.Params().Variables() {
		f.Params = append(f.Params, l.words(e, v.Type())...)
	}
	for v := range sig.Results().Variables() {
		if !isAggregate(v.Type()) {
			f.Results = append(f.Results, l.words(e, v.Type())...)
		}
	}
	for v := range sig.Results().Variables() {
		if isAggregate(v.Type()) {
			f.Params = append(f.Params, ir.Ptr) // the caller's object (D-034)
		}
	}
	if l.litFuncs == nil {
		l.litFuncs = map[*ast.FuncLit]*ir.Func{}
	}
	l.litFuncs[e] = f
	l.bodies = append(l.bodies, func() {
		l.fn, l.sig = f, sig
		ctx := l.fn.Param(l.fn.Entry(), 0, pos)
		cs := l.fn.NewSlot("closure", ir.Ptr, pos)
		l.fn.Store(l.fn.Entry(), cs, ctx, pos)
		l.body(e.Type.Params.List, 1, e.Type.Results, e.Body, func() {
			// The Ptr of each captured variable, from the closure.
			for i, v := range l.captures[e] {
				s := l.fn.NewSlot(v.Name(), ir.Ptr, pos)
				p := l.fn.LdM(l.b, ir.Ptr, l.fn.Load(l.b, cs, pos), l.fn.Const(l.b, int64(8+8*i), pos), pos)
				l.fn.Store(l.b, s, p, pos)
				delete(l.slots, v)
				l.ptrs[v] = s
			}
		}, "literal")
	})
	return f
}
