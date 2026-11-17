package lower

// Methods (Phase 8 increment 8.1, D-038).
//
// A method is an ordinary IR function named T.M, after its receiver's
// defined type T, whose first parameter is the receiver: for a value
// receiver, the Ptr of the caller's copy of the array or struct (D-034), and
// for a pointer receiver, the pointer. Go forbids T and *T from both having
// a method M, and a type and a function of the same name, so T.M names one
// function, and no Go function name has a dot. Lowering's own functions
// begin with a dot (genPrefix), so no method shares a name with one.
//
// A call x.M(args) evaluates the receiver, then the arguments, left to right
// (D-024). The receiver follows the path of embedded fields to M, as a
// selector does, through any pointers among them, each nil-checked. At its
// end, it takes the address of the field or variable reached, if M has a
// pointer receiver and it is not a pointer: such a variable is
// address-taken, so on the heap (D-035). A pointer reached where M has a
// value receiver is followed, with a nil check, and the value it points to
// copied. A pointer passed as a pointer receiver is not checked: the method
// may be called on nil.

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// funcName returns the IR name of the function or method fd declares.
func (l *lowerer) funcName(fd *ast.FuncDecl, obj *types.Func) string {
	r := obj.Type().(*types.Signature).Recv()
	if r == nil {
		return fd.Name.Name
	}
	t := r.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		l.fail(fd, "method %s of a type %s that is not defined", fd.Name.Name, t)
	}
	return n.Obj().Name() + "." + fd.Name.Name
}

// selectPath returns the place that the path of field indices reaches from
// p, as in a selector e: through the embedded fields of the path, and the
// pointers among them, each followed with its nil check deferred.
func (l *lowerer) selectPath(e ast.Expr, p place, path []int, pos ir.Pos) place {
	for _, k := range path {
		if t, ok := pointee(p.t); ok {
			p = l.through(l.scalar(e, l.loadPlace(p, pos)), t, pos)
		}
		s, ok := p.t.Underlying().(*types.Struct)
		if !ok {
			l.fail(e, "selector of a non-struct")
		}
		var fields []*types.Var
		for f := range s.Fields() {
			fields = append(fields, f)
		}
		p = p.field(sizes.Offsetsof(fields)[k], fields[k].Type())
	}
	return p
}

// receiver lowers the receiver of the call e(...) of the method that sel,
// of kind MethodVal, selects, and returns its words.
func (l *lowerer) receiver(e *ast.SelectorExpr, sel *types.Selection) []*ir.Instr {
	_, xPtr := pointee(l.pkg.Info.Types[e.X].Type)
	return l.recvOf(e, sel, xPtr, func() []*ir.Instr { return l.value(e.X) },
		func() place { return l.deref(e.X) }, func() place { return l.addrOf(e.X, false) }, l.pos(e, e.Sel.Pos()))
}

// recvOf returns the receiver words of the method sel selects on an operand
// x, a pointer if xPtr: value lowers x, and deref, if xPtr, and addr, if
// not, return the place x points to or is. Each is called at most once.
func (l *lowerer) recvOf(e ast.Expr, sel *types.Selection, xPtr bool, value func() []*ir.Instr, deref, addr func() place, pos ir.Pos) []*ir.Instr {
	_, ptrRecv := sel.Obj().Type().(*types.Signature).Recv().Type().(*types.Pointer)
	path := sel.Index()[:len(sel.Index())-1]
	if len(path) == 0 {
		switch {
		case xPtr && ptrRecv:
			return value() // not checked for nil
		case xPtr:
			return l.loadPlace(deref(), pos)
		case ptrRecv:
			return []*ir.Instr{l.pointer(addr(), pos)}
		}
		return value()
	}
	var p place
	if xPtr {
		p = deref()
	} else {
		p = addr()
	}
	p = l.selectPath(e, p, path, pos)
	if t, ok := pointee(p.t); ok {
		v := l.scalar(e, l.loadPlace(p, pos))
		if ptrRecv {
			return []*ir.Instr{v}
		}
		return l.loadPlace(l.through(v, t, pos), pos)
	}
	if ptrRecv {
		return []*ir.Instr{l.pointer(p, pos)}
	}
	return l.loadPlace(p, pos)
}
