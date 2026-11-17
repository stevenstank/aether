package lower

// Function values (Phase 8 increment 8.2, D-039).
//
// A func value is one Ptr word, 0 for nil: the address of a closure object
// whose first word is the code address of a function that takes the
// closure's Ptr as its first parameter, its context, and then the
// parameters of the Go signature. A call of a func value evaluates the
// function value, then the arguments, left to right (D-024), then panics
// if the func value is nil, as gc's load of the code address faults, and
// calls the code with the closure and the arguments (CallInd).
//
// No Go function takes a context, so a func value calls a wrapper that
// lowering generates, with a name beginning with a dot (genPrefix):
//
//   - .f.F, for the function F: it calls F. Its closure, which holds only
//     the code address, is the global memory object .v.f.F, written by
//     Init before any package-level initializer runs, so every value of F
//     is the same Ptr.
//   - .m.T.M and .p.T.M, for the method expressions T.M and (*T).M: they
//     take the receiver as their first Go parameter and select the method
//     on it, as a call x.M(...) does, through any embedded fields; their
//     closures are .v.m.T.M and .v.p.T.M.
//   - .b.D.M, for a method value x.M of the method M declared on D: the
//     method value evaluates x, follows the path to M, and copies the
//     receiver into a new heap object after the code address: a copy of
//     the value, for a value receiver, or the pointer. Each call copies a
//     value receiver again, since a method may change its receiver.

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// isFunc reports whether t is a function type.
func isFunc(t types.Type) bool {
	_, ok := t.Underlying().(*types.Signature)
	return ok
}

// static is a closure of a wrapper that captures nothing: a global memory
// object that Init stores the wrapper's code address in.
type static struct {
	mem  *ir.Mem
	code *ir.Func
}

// wrapper returns the generated function named name, creating it with the
// given signature the first time; its body is built by body, with l.fn set
// to it, after every function of the program has been lowered.
func (l *lowerer) wrapper(name string, params, results []ir.Type, pos ir.Pos, body func(w *ir.Func)) *ir.Func {
	if w := l.wrappers[name]; w != nil {
		return w
	}
	w := l.prog.NewFunc(genPrefix+name, l.fn.File, pos)
	w.Params, w.Results = params, results
	if l.wrappers == nil {
		l.wrappers = map[string]*ir.Func{}
	}
	l.wrappers[name] = w
	l.bodies = append(l.bodies, func() { body(w) })
	return w
}

// buildWrappers builds the bodies of the wrappers, which create no others.
func (l *lowerer) buildWrappers() {
	for i := 0; i < len(l.bodies); i++ {
		l.bodies[i]()
	}
	l.fn = nil
}

// closure returns the Ptr of the static closure of the wrapper code.
func (l *lowerer) closure(code *ir.Func, pos ir.Pos) *ir.Instr {
	m := l.staticMems[code]
	if m == nil {
		m = l.prog.NewMem(".v"+code.Name, 8, 8, code.File, code.Pos)
		if l.staticMems == nil {
			l.staticMems = map[*ir.Func]*ir.Mem{}
		}
		l.staticMems[code] = m
		l.statics = append(l.statics, static{m, code})
	}
	return l.addr(m, pos)
}

// forward ends w's body: it calls callee with recv, if not nil, and then
// the parameters ps, and returns its results.
func (l *lowerer) forward(callee *ir.Func, recv func() *ir.Instr, ps []func() *ir.Instr, pos ir.Pos) {
	var args []*ir.Instr
	if recv != nil {
		args = append(args, recv())
	}
	for _, p := range ps {
		args = append(args, p())
	}
	c := l.fn.Call(l.b, callee, args, pos)
	var rs []*ir.Instr
	switch len(callee.Results) {
	case 0:
	case 1:
		rs = []*ir.Instr{c}
	default:
		for i := range callee.Results {
			rs = append(rs, l.fn.Extract(l.b, c, i, pos))
		}
	}
	l.fn.Ret(l.b, pos, rs...)
	prune(l.fn)
}

// enter makes w the function being lowered, at its entry, and returns a
// function for each of its parameters that produces it in the then current
// block. The params come first, as ABI-R1 requires (D-030), each stored in
// a slot.
func (l *lowerer) enter(w *ir.Func, pos ir.Pos) []func() *ir.Instr {
	l.fn, l.sig, l.b, l.done, l.retPtrs = w, nil, w.Entry(), false, nil
	var ps []*ir.Instr
	for i := range w.Params {
		ps = append(ps, l.fn.Param(l.b, i, pos))
	}
	gets := make([]func() *ir.Instr, len(ps))
	for i, p := range ps {
		gets[i] = l.spill(p, pos)
	}
	return gets
}

// funcValue returns the func value of the function obj of the program.
func (l *lowerer) funcValue(id *ast.Ident, obj *types.Func) *ir.Instr {
	f := l.funcs[obj]
	if f == nil {
		l.fail(id, "function value of %s, not a function of the program", id.Name)
	}
	pos := l.pos(id, id.Pos())
	w := l.wrapper("f."+f.Name, append([]ir.Type{ir.Ptr}, f.Params...), f.Results, f.Pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		l.forward(f, nil, ps[1:], pos)
	})
	return l.closure(w, pos)
}

// method returns the IR function of the method sel selects, and whether it
// has a pointer receiver.
func (l *lowerer) method(n ast.Node, sel *types.Selection) (*ir.Func, bool) {
	obj, _ := sel.Obj().(*types.Func)
	f := l.funcs[obj]
	if f == nil {
		l.fail(n, "method %s is not a method of the program", sel.Obj().Name())
	}
	_, ptr := obj.Type().(*types.Signature).Recv().Type().(*types.Pointer)
	return f, ptr
}

// methodValue lowers x.M, not called: the receiver, as for a call of it,
// then a new closure holding it.
func (l *lowerer) methodValue(e *ast.SelectorExpr, sel *types.Selection) *ir.Instr {
	pos := l.pos(e, e.Sel.Pos())
	f, ptrRecv := l.method(e, sel)
	rt := sel.Obj().Type().(*types.Signature).Recv().Type()
	r := l.scalar(e, l.receiver(e, sel))
	w := l.wrapper("b."+f.Name, append([]ir.Type{ir.Ptr}, f.Params[1:]...), f.Results, f.Pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		ctx := ps[0]
		recv := func() *ir.Instr { return l.fn.LdM(l.b, ir.Ptr, ctx(), l.fn.Const(l.b, 8, pos), pos) }
		if !ptrRecv {
			// A copy for each call: the method may change its receiver.
			m := l.newMem("recv", rt, pos)
			l.copyPlace(l.memPlace(m, rt, pos), place{base: ctx, c: 8, t: rt}, pos)
			recv = func() *ir.Instr { return l.addr(m, pos) }
		}
		l.forward(f, recv, ps[1:], pos)
	})
	size := int64(16)
	if !ptrRecv {
		size = 8 + sizes.Sizeof(rt)
	}
	get := l.keep(r, pos)
	c := l.fn.Alloc(l.b, l.fn.Const(l.b, size, pos), pos)
	cl := l.keep(c, pos)
	l.fn.StM(l.b, cl(), l.fn.Const(l.b, 0, pos), l.fn.FuncAddr(l.b, w, pos), pos)
	if ptrRecv {
		l.fn.StM(l.b, cl(), l.fn.Const(l.b, 8, pos), get(), pos)
	} else {
		l.copyPlace(place{base: cl, c: 8, t: rt}, l.valuePlace(get(), rt, pos), pos)
	}
	return cl()
}

// methodExpr lowers T.M or (*T).M: the static closure of a wrapper whose
// first Go parameter is the receiver operand, of type T or *T.
func (l *lowerer) methodExpr(e *ast.SelectorExpr, sel *types.Selection) *ir.Instr {
	pos := l.pos(e, e.Sel.Pos())
	f, _ := l.method(e, sel)
	xt := sel.Recv()
	n, xPtr := xt, false
	if t, ok := pointee(xt); ok {
		n, xPtr = t, true
	}
	named, ok := types.Unalias(n).(*types.Named)
	if !ok {
		l.fail(e, "method expression of a type %s that is not defined", n)
	}
	kind := "m."
	if xPtr {
		kind = "p."
	}
	params := append([]ir.Type{ir.Ptr, ir.Ptr}, f.Params[1:]...)
	w := l.wrapper(kind+named.Obj().Name()+"."+sel.Obj().Name(), params, f.Results, f.Pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		x := ps[1]
		var deref, addr func() place
		if xPtr {
			deref = func() place { return l.through(x(), n, pos) }
		} else {
			// The caller's copy of the operand, this call's own.
			addr = func() place { return l.valuePlace(x(), n, pos) }
		}
		r := l.recvOf(e, sel, xPtr, func() []*ir.Instr { return []*ir.Instr{x()} }, deref, addr, pos)
		get := l.keep(l.scalar(e, r), pos)
		l.forward(f, get, ps[2:], pos)
	})
	return l.closure(w, pos)
}

// valueCall lowers a call of a func value: the function value, then the
// arguments, then its nil check, then the call of the code its closure
// holds, with the closure as the first argument.
func (l *lowerer) valueCall(call *ast.CallExpr, sig *types.Signature) *ir.Instr {
	pos := l.pos(call, call.Pos())
	if sig.Variadic() {
		l.fail(call, "call of a variadic func value")
	}
	fv := l.expr(call.Fun)
	get := func() *ir.Instr { return fv }
	if l.branches(call.Args) {
		get = l.spill(fv, pos)
	}
	var ts []types.Type
	for v := range sig.Params().Variables() {
		ts = append(ts, v.Type())
	}
	args := flatten(l.valuesOrResults(call.Args, ts))
	fv = get()
	out := l.guard(l.fn.Compare(l.b, ir.Eq, fv, l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), ir.PanicNil, pos, append([]*ir.Instr{fv}, args...)...)
	fv, args = out[0], out[1:]
	s := &ir.Sig{Params: []ir.Type{ir.Ptr}}
	for _, a := range args {
		s.Params = append(s.Params, a.Type)
	}
	var temps []*ir.Mem
	for r := range sig.Results().Variables() {
		if isAggregate(r.Type()) {
			m := l.newMem("tmp", r.Type(), pos)
			temps = append(temps, m)
			args = append(args, l.addr(m, pos))
			s.Params = append(s.Params, ir.Ptr)
			continue
		}
		s.Results = append(s.Results, l.words(call, r.Type())...)
	}
	code := l.fn.LdM(l.b, ir.Ptr, fv, l.fn.Const(l.b, 0, pos), pos)
	v := l.fn.CallInd(l.b, s, code, append([]*ir.Instr{fv}, args...), pos)
	if temps != nil {
		l.temps[v] = temps
	}
	return v
}

// initStatics makes Init write the code address of each static closure
// first, in a new entry block, before any initializer can use one; it
// creates Init if the program has none. Nothing branches to the old entry,
// whose code moves to a block of its own.
func (l *lowerer) initStatics() {
	if len(l.statics) == 0 {
		return
	}
	if l.prog.Init == nil {
		f := l.pkg.Files[0]
		file := frontend.Position(l.pkg.Fset, f.Package).File
		l.prog.Init = l.prog.NewFunc("init", file, l.pos(f, f.Package))
		l.prog.Init.Ret(l.prog.Init.Entry(), l.prog.Init.Pos)
	}
	l.fn = l.prog.Init
	entry, body := l.fn.Entry(), l.fn.NewBlock()
	body.Instrs, entry.Instrs = entry.Instrs, nil
	pos := l.fn.Pos
	for _, s := range l.statics {
		a := l.fn.Addr(entry, s.mem, pos)
		l.fn.StM(entry, a, l.fn.Const(entry, 0, pos), l.fn.FuncAddr(entry, s.code, pos), pos)
	}
	l.fn.Jump(entry, body, pos)
	prune(l.fn)
	l.fn = nil
}

// isValueCall reports whether call is a call of a func value: not a
// conversion, nor a call of a builtin, a function, or a method.
func (l *lowerer) isValueCall(call *ast.CallExpr) bool {
	if l.pkg.Info.Types[call.Fun].IsType() {
		return false
	}
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		switch l.pkg.Info.Uses[fn].(type) {
		case *types.Func, *types.Builtin:
			return false
		}
	case *ast.SelectorExpr:
		if sel := l.pkg.Info.Selections[fn]; sel != nil && sel.Kind() == types.MethodVal {
			return false
		}
	}
	return true
}
