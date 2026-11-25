package lower

// Interfaces (Phase 8 increment 8.4, D-041).
//
// An interface value is two Ptr words, tab and data; both are 0 for nil.
// For a value of dynamic type T:
//
//   - data is T's value itself if T is a pointer type, and otherwise the Ptr
//     of a heap object holding a copy of it, never written again.
//   - tab, for an empty interface, is the address of T's type descriptor,
//     the global memory object .tN: its name's Ptr and length, as gc writes
//     it in panics, then the code address of its equality function .eqN, or
//     0 if T is not comparable. For a non-empty interface I, tab is the
//     address of the itab of T and I, .itN: the descriptor's address, then
//     the code address of a wrapper .iN.M for each method M of I, in I's
//     order, which selects M on data as a call does on a value of T. Init
//     writes every descriptor and itab.
//
// So two values of the same interface type have the same dynamic type
// exactly if their tabs are equal. A conversion from one interface to
// another, and an assertion to an interface J, find the itab of the dynamic
// type and J by calling .findK, which lowering generates last, comparing
// the descriptor with each of the program's that implement J; .missK
// names the first method a type lacks.

import (
	"fmt"
	"github.com/stevenstank/aether/internal/frontend"
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"github.com/stevenstank/aether/internal/ir"
)

func isIface(t types.Type) bool {
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

func isEmptyIface(t types.Type) bool {
	return t.Underlying().(*types.Interface).NumMethods() == 0
}

// isDirect reports whether an interface's data holds a value of t itself:
// whether t is a pointer type or unsafe.Pointer.
func isDirect(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	}
	return false
}

// tdesc is a type descriptor.
type tdesc struct {
	t    types.Type
	id   int
	mem  *ir.Mem
	eq   *ir.Func // nil if t is not comparable
	hash *ir.Func // nil unless the program hashes an interface (D-042)
	// With package reflect (reflect.go): the descriptors of the element
	// and key types, and a struct's field records and field types.
	elem, key  *tdesc
	fields     *ir.Mem
	fieldDescs []*tdesc
	// With internal/reflectlite's implements (reflect.go, D-050): an
	// interface's .findK, and a defined type's underlying type.
	find  *ir.Func
	under *tdesc
}

// itabT is the itab of a type and an interface.
type itabT struct {
	d     *tdesc
	iface int // the interface's key
	mem   *ir.Mem
	funcs []*ir.Func
}

// ifaceKey returns the number of the interface type t, the same for
// identical ones, and records it.
func (l *lowerer) ifaceKey(t types.Type) int {
	u := t.Underlying()
	for i, v := range l.ifaces {
		if types.Identical(v, u) {
			return i
		}
	}
	l.ifaces = append(l.ifaces, u.(*types.Interface))
	return len(l.ifaces) - 1
}

// desc returns the descriptor of the type t, creating it on first use.
func (l *lowerer) desc(t types.Type) *tdesc {
	for _, d := range l.descs {
		if types.Identical(d.t, t) {
			return d
		}
	}
	if l.descsDone {
		l.fail(nil, "type descriptor of %s made after the interface functions", t)
	}
	d := &tdesc{t: t, id: len(l.descs)}
	pos := l.fn.Pos
	size := int64(32)
	if l.reflect {
		size = reflectDescSize
	}
	d.mem = l.prog.NewMem(fmt.Sprintf("%st%d", genPrefix, d.id), size, 8, l.fn.File, pos)
	if types.Comparable(t) {
		d.eq = l.wrapper(fmt.Sprintf("eq%d", d.id), []ir.Type{ir.Ptr, ir.Ptr, ir.Ptr}, []ir.Type{ir.Bool}, pos, func(w *ir.Func) {
			ps := l.enter(w, pos)
			var eq *ir.Instr
			if isDirect(t) {
				eq = l.fn.Compare(l.b, ir.Eq, ps[1](), ps[2](), pos)
			} else {
				eq = l.equal(l.valuePlace(ps[1](), t, pos), l.valuePlace(ps[2](), t, pos), pos)
			}
			l.fn.Ret(l.b, pos, eq)
			prune(l.fn)
		})
	}
	l.descs = append(l.descs, d)
	if l.reflect {
		l.reflectDesc(d, pos)
	}
	return d
}

// dynDescs returns the descriptors of the types that may be dynamic types
// of interface values: not of interface types, which reflect describes
// too (reflect.go).
func (l *lowerer) dynDescs() []*tdesc {
	var ds []*tdesc
	for _, d := range l.descs {
		if !isIface(d.t) {
			ds = append(ds, d)
		}
	}
	return ds
}

// methodIndex returns the index of the method name in the interface t.
func methodIndex(t types.Type, name string) int {
	it := t.Underlying().(*types.Interface)
	for i := range it.NumMethods() {
		if it.Method(i).Name() == name {
			return i
		}
	}
	return -1
}

// itab returns the itab of t and the interface iface, creating it and its
// method wrappers on first use; t implements iface.
func (l *lowerer) itab(t, iface types.Type, pos ir.Pos) *itabT {
	d, k := l.desc(t), l.ifaceKey(iface)
	for _, it := range l.itabs {
		if it.d == d && it.iface == k {
			return it
		}
	}
	it := &itabT{d: d, iface: k}
	n := l.ifaces[k].NumMethods()
	it.mem = l.prog.NewMem(fmt.Sprintf("%sit%d", genPrefix, len(l.itabs)), int64(8+8*n), 8, l.fn.File, pos)
	ms := types.NewMethodSet(t)
	for i := range n {
		m := l.ifaces[k].Method(i)
		sel := ms.Lookup(m.Pkg(), m.Name())
		if sel == nil {
			l.fail(nil, "type %s has no method %s", t, m.Name())
		}
		it.funcs = append(it.funcs, l.ifaceMethod(d, sel, pos))
	}
	l.itabs = append(l.itabs, it)
	return it
}

// ifaceMethod returns the wrapper .iN.M that an itab of the descriptor d
// holds for the method sel selects: it takes the closure (unused) and the
// interface's data, then the method's parameters, and calls the method
// with the receiver selected on the value data holds or points to. A
// value is copied first: the method may change its receiver, and the
// interface's copy must not change.
func (l *lowerer) ifaceMethod(d *tdesc, sel *types.Selection, pos ir.Pos) *ir.Func {
	f, _ := l.method(nil, sel)
	t := d.t
	name := fmt.Sprintf("i%d.%s", d.id, sel.Obj().Name())
	nrecv := len(l.words(nil, sel.Obj().Type().(*types.Signature).Recv().Type()))
	return l.wrapper(name, append([]ir.Type{ir.Ptr, ir.Ptr}, f.Params[nrecv:]...), f.Results, pos, func(w *ir.Func) {
		ps := l.enter(w, pos)
		data := ps[1]
		var r []*ir.Instr
		if isDirect(t) {
			et, _ := pointee(t)
			l.wrapNilCheck(t, sel, data, pos)
			r = l.recvOf(nil, sel, true, func() []*ir.Instr { return []*ir.Instr{data()} },
				func() place { return l.through(data(), et, pos) }, nil, pos)
		} else {
			m := l.newMem("recv", t, pos)
			l.copyPlace(l.memPlace(m, t, pos), l.valuePlace(data(), t, pos), pos)
			// An array or struct is passed as its Ptr; a value of any other
			// type, of a defined type (D-049), as its words.
			value := func() []*ir.Instr { return []*ir.Instr{l.addr(m, pos)} }
			if !isAggregate(t) {
				value = func() []*ir.Instr { return l.loadPlace(l.memPlace(m, t, pos), pos) }
			}
			r = l.recvOf(nil, sel, false, value, nil, func() place { return l.memPlace(m, t, pos) }, pos)
		}
		l.forward(f, nil, append(l.keepAll(r, pos), ps[2:]...), pos)
	})
}

// wrapNilCheck panics, as gc's runtime.panicwrap does, if p(), the receiver
// of the wrapper from *T, of type pt, to sel's method, is nil and the
// method is T's own with a value receiver: gc generates that check for
// such a wrapper only, not for a promoted method, whose nil pointer is
// dereferenced (go.dev/issue/19040). The message names T as gc's function
// names do, with [...] for type arguments, after the package's path.
func (l *lowerer) wrapNilCheck(pt types.Type, sel *types.Selection, p func() *ir.Instr, pos ir.Pos) {
	et, ok := pointee(pt)
	named, isNamed := types.Unalias(et).(*types.Named)
	recv := sel.Obj().Type().(*types.Signature).Recv().Type()
	if !ok || !isNamed || len(sel.Index()) != 1 || !types.Identical(recv, named) || named.Obj().Pkg() == nil {
		return
	}
	name := frontend.SourceName(named.Obj().Name())
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i] + "[...]"
	} else if named.TypeArgs().Len() > 0 {
		name += "[...]"
	}
	msg := "value method " + l.home(named.Obj()).Path() + "." + name + "." + sel.Obj().Name() + " called using nil *" + name + " pointer"
	notNil := l.when(l.fn.Compare(l.b, ir.Eq, p(), l.nilPtr(pos), pos), pos)
	l.panicMsg([][]*ir.Instr{l.strWords(msg, pos)}, pos)
	l.b = notNil
}

// assignTo returns w, a value of type from, as a value of the type of the
// assignment target x: converted to it if it is an interface type and from
// is not (spec §Assignability). Values of any other assignable types have
// the same words.
func (l *lowerer) assignTo(w []*ir.Instr, from types.Type, x ast.Expr, pos ir.Pos) []*ir.Instr {
	to := l.pkg.Info.Types[x].Type
	if !isIface(to) || types.Identical(from, to) {
		return w
	}
	return l.toIface(w, from, to, pos)
}

// strWords returns the words of the constant string s.
func (l *lowerer) strWords(s string, pos ir.Pos) []*ir.Instr {
	return []*ir.Instr{l.fn.Str(l.b, l.prog.Intern(s), pos), l.fn.Const(l.b, int64(len(s)), pos)}
}

// dynDesc returns the descriptor address of a non-nil interface value of
// static type t, whose tab is tab.
func (l *lowerer) dynDesc(t types.Type, tab *ir.Instr, pos ir.Pos) *ir.Instr {
	if isEmptyIface(t) {
		return tab
	}
	return l.fn.LdM(l.b, ir.Ptr, tab, l.fn.Const(l.b, 0, pos), pos)
}

// descName returns the name words of the descriptor at d.
func (l *lowerer) descName(d *ir.Instr, pos ir.Pos) []*ir.Instr {
	return []*ir.Instr{l.fn.LdM(l.b, ir.Ptr, d, l.fn.Const(l.b, 0, pos), pos), l.fn.LdM(l.b, ir.I64, d, l.fn.Const(l.b, 8, pos), pos)}
}

// panicMsg ends the current block with a panic whose message is the
// concatenation of parts.
func (l *lowerer) panicMsg(parts [][]*ir.Instr, pos ir.Pos) {
	m := l.concat(parts, pos)
	l.fn.Panic(l.b, ir.PanicMsg, pos, m[0], m[1])
}

// findFunc returns .findK for the interface t: given a descriptor, the
// address of its itab with t, or 0 if its type does not implement t.
func (l *lowerer) findFunc(t types.Type, pos ir.Pos) *ir.Func {
	k := l.ifaceKey(t)
	if f := l.finds[k]; f != nil {
		return f
	}
	f := l.prog.NewFunc(fmt.Sprintf("%sfind%d", genPrefix, k), l.fn.File, pos)
	f.Params, f.Results = []ir.Type{ir.Ptr, ir.Ptr}, []ir.Type{ir.Ptr}
	if l.finds == nil {
		l.finds = map[int]*ir.Func{}
	}
	l.finds[k] = f
	l.late = append(l.late, func() {
		ps := l.enter(f, pos)
		for _, d := range l.dynDescs() {
			if !types.Implements(d.t, l.ifaces[k]) {
				continue
			}
			it := l.itab(d.t, l.ifaces[k], pos)
			yes, no := l.fn.NewBlock(), l.fn.NewBlock()
			l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, ps[1](), l.addr(d.mem, pos), pos), yes, no, pos)
			l.b = yes
			l.fn.Ret(l.b, pos, l.addr(it.mem, pos))
			l.b = no
		}
		l.fn.Ret(l.b, pos, l.fn.TypedConst(l.b, ir.Ptr, 0, pos))
		prune(l.fn)
	})
	return f
}

// missFunc returns .missK for the interface t: given a descriptor of a type
// that does not implement t, the name of the first method, by name, that
// it lacks, as gc reports it.
func (l *lowerer) missFunc(t types.Type, pos ir.Pos) *ir.Func {
	k := l.ifaceKey(t)
	if f := l.misses[k]; f != nil {
		return f
	}
	f := l.prog.NewFunc(fmt.Sprintf("%smiss%d", genPrefix, k), l.fn.File, pos)
	f.Params, f.Results = []ir.Type{ir.Ptr, ir.Ptr}, []ir.Type{ir.Ptr, ir.I64}
	if l.misses == nil {
		l.misses = map[int]*ir.Func{}
	}
	l.misses[k] = f
	l.late = append(l.late, func() {
		ps := l.enter(f, pos)
		var names []string
		for m := range l.ifaces[k].Methods() {
			names = append(names, m.Name())
		}
		slices.Sort(names)
		for _, d := range l.dynDescs() {
			ms := types.NewMethodSet(d.t)
			for _, n := range names {
				if sel := ms.Lookup(l.methodPkg(k, n), n); sel != nil && types.Identical(sel.Type(), l.methodType(k, n)) {
					continue
				}
				yes, no := l.fn.NewBlock(), l.fn.NewBlock()
				l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, ps[1](), l.addr(d.mem, pos), pos), yes, no, pos)
				l.b = yes
				l.fn.Ret(l.b, pos, l.strWords(n, pos)...)
				l.b = no
				break
			}
		}
		l.fn.Ret(l.b, pos, l.fn.TypedConst(l.b, ir.Ptr, 0, pos), l.fn.Const(l.b, 0, pos))
		prune(l.fn)
	})
	return f
}

func (l *lowerer) methodPkg(k int, name string) *types.Package {
	for m := range l.ifaces[k].Methods() {
		if m.Name() == name {
			return m.Pkg()
		}
	}
	return nil
}

// methodType is the method's type without its receiver, as a method set's
// selection gives it.
func (l *lowerer) methodType(k int, name string) types.Type {
	for m := range l.ifaces[k].Methods() {
		if m.Name() == name {
			s := m.Type().(*types.Signature)
			return types.NewSignatureType(nil, nil, nil, s.Params(), s.Results(), s.Variadic())
		}
	}
	return nil
}

// buildLate builds the interface functions, after every other body: their
// bodies list every descriptor. The itab wrappers they create follow.
func (l *lowerer) buildLate() {
	l.buildWrappers()
	l.descsDone = true
	for i := 0; i < len(l.late); i++ {
		l.late[i]()
	}
	l.descHashes()
	l.fn = nil
	l.buildWrappers()
}

// toIface converts the words w of a value of type from to the interface
// type to (D-041).
func (l *lowerer) toIface(w []*ir.Instr, from, to types.Type, pos ir.Pos) []*ir.Instr {
	if from == nil || types.Identical(from.Underlying(), to.Underlying()) {
		return w
	}
	if isIface(from) {
		if isEmptyIface(from) && isEmptyIface(to) {
			return w
		}
		// tab 0 stays 0; otherwise the dynamic type's descriptor, or its
		// itab with to.
		data := l.spill(w[1], pos)
		tab := l.spill(w[0], pos)
		res := l.fn.NewSlot("tab", ir.Ptr, pos)
		l.fn.Store(l.b, res, l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos)
		conv, join := l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, tab(), l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), conv, join, pos)
		l.b = conv
		d := l.dynDesc(from, tab(), pos)
		if !isEmptyIface(to) {
			d = l.fn.Call(l.b, l.findFunc(to, pos), []*ir.Instr{d, d}, pos)
		}
		l.fn.Store(l.b, res, d, pos)
		l.fn.Jump(l.b, join, pos)
		l.b = join
		return []*ir.Instr{l.fn.Load(l.b, res, pos), data()}
	}
	var data *ir.Instr
	if isDirect(from) {
		data = l.scalar(nil, w)
	} else {
		get := l.spillWords(w, pos)
		p := l.alloc(from, pos)
		box := l.keep(p, pos)
		l.storePlace(place{base: box, t: from}, get(), pos)
		data = box()
	}
	d := l.spill(data, pos)
	var tab *ir.Instr
	if isEmptyIface(to) {
		tab = l.addr(l.desc(from).mem, pos)
	} else {
		tab = l.addr(l.itab(from, to, pos).mem, pos)
	}
	return []*ir.Instr{tab, d()}
}

// ifaceValue lowers an expression of interface type.
func (l *lowerer) ifaceValue(e ast.Expr) []*ir.Instr {
	pos := l.pos(e, e.Pos())
	t := l.pkg.Info.Types[e].Type
	switch e := e.(type) {
	case *ast.ParenExpr:
		return l.ifaceValue(e.X)
	case *ast.Ident:
		return l.load(l.lookup(e), pos)
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
		return l.loadPlace(l.addrOf(e, false), pos)
	case *ast.CallExpr:
		if l.pkg.Info.Types[e.Fun].IsType() {
			return l.valueFor(e.Args[0], t)
		}
		if l.isBuiltin(e, "recover") {
			return l.recoverCall(e) // D-043
		}
		vals := l.results(e, l.call(e))
		if len(vals) != 1 {
			l.fail(e, "call of %d results used as one value", len(vals))
		}
		return vals[0]
	case *ast.TypeAssertExpr:
		return l.assert(e, false)[0]
	}
	l.fail(e, "unexpected interface expression %s", types.ExprString(e))
	return nil
}

// ifaceCall lowers a call of the method of an interface value that sel
// selects: the interface, then the arguments, then its nil check, then
// the call of the itab's code.
func (l *lowerer) ifaceCall(call *ast.CallExpr, se *ast.SelectorExpr, sel *types.Selection) *ir.Instr {
	pos := l.pos(call, call.Pos())
	w := l.value(se.X)
	get := func() []*ir.Instr { return w }
	if l.branches(call.Args) {
		get = l.spillWords(w, pos)
	}
	sig := sel.Obj().Type().(*types.Signature)
	args := flatten(l.args(call, sig))
	w = get()
	out := l.guard(l.fn.Compare(l.b, ir.Eq, w[0], l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), ir.PanicNil, pos, append(slices.Clone(w), args...)...)
	return l.dispatch(call, sel.Recv(), sel.Obj().Name(), sig, out[0], out[1], out[2:], pos)
}

// dispatch calls the method name of the non-nil interface value tab, data
// of type t with args, after which come the objects for its array and
// struct results.
func (l *lowerer) dispatch(n ast.Node, t types.Type, name string, sig *types.Signature, tab, data *ir.Instr, args []*ir.Instr, pos ir.Pos) *ir.Instr {
	var temps []*ir.Mem
	var results []ir.Type
	for r := range sig.Results().Variables() {
		if isAggregate(r.Type()) {
			m := l.newMem("tmp", r.Type(), pos)
			temps = append(temps, m)
			args = append(args, l.addr(m, pos))
			continue
		}
		results = append(results, l.words(n, r.Type())...)
	}
	v := l.callMethod(n, t, name, tab, data, args, results, pos)
	if temps != nil {
		l.temps[v] = temps
	}
	return v
}

// callMethod calls the method name of the non-nil interface value tab, data
// of type t with the words args, result objects included, and results of
// the IR types results: the code at the method's index in the itab, with
// the itab as the closure.
func (l *lowerer) callMethod(n ast.Node, t types.Type, name string, tab, data *ir.Instr, args []*ir.Instr, results []ir.Type, pos ir.Pos) *ir.Instr {
	idx := methodIndex(t, name)
	if idx < 0 {
		l.fail(n, "interface %s has no method %s", t, name)
	}
	s := &ir.Sig{Params: []ir.Type{ir.Ptr, ir.Ptr}, Results: results}
	for _, a := range args {
		s.Params = append(s.Params, a.Type)
	}
	code := l.fn.LdM(l.b, ir.Ptr, tab, l.fn.Const(l.b, int64(8+8*idx), pos), pos)
	return l.fn.CallInd(l.b, s, code, append([]*ir.Instr{tab, data}, args...), pos)
}

// retCall ends a wrapper with the return of the results of the call v.
func (l *lowerer) retCall(v *ir.Instr, pos ir.Pos) {
	var rs []*ir.Instr
	switch n := len(v.CallResults()); {
	case n == 1:
		rs = []*ir.Instr{v}
	case n > 1:
		for i := range n {
			rs = append(rs, l.fn.Extract(l.b, v, i, pos))
		}
	}
	l.fn.Ret(l.b, pos, rs...)
	prune(l.fn)
}

// ifaceMethodValue lowers x.M of an interface value x: its nil check now,
// as gc's, then a closure holding tab and data, whose code .biK.M calls M.
func (l *lowerer) ifaceMethodValue(e *ast.SelectorExpr, sel *types.Selection) *ir.Instr {
	pos := l.pos(e, e.Sel.Pos())
	t, name := sel.Recv(), sel.Obj().Name()
	ps, rs := l.ifaceWrapperSig(e, sel.Obj().Type().(*types.Signature))
	w := l.value(e.X)
	out := l.guard(l.fn.Compare(l.b, ir.Eq, w[0], l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), ir.PanicNil, pos, w...)
	f := l.wrapper(fmt.Sprintf("bi%d.%s", l.ifaceKey(t), name), append([]ir.Type{ir.Ptr}, ps...), rs, pos, func(f *ir.Func) {
		gets := l.enter(f, pos)
		ctx := gets[0]
		tab := l.fn.LdM(l.b, ir.Ptr, ctx(), l.fn.Const(l.b, 8, pos), pos)
		data := l.fn.LdM(l.b, ir.Ptr, ctx(), l.fn.Const(l.b, 16, pos), pos)
		var args []*ir.Instr
		for _, g := range gets[1:] {
			args = append(args, g())
		}
		l.retCall(l.callMethod(e, t, name, tab, data, args, rs, pos), pos)
	})
	c := l.fn.Alloc(l.b, l.fn.Const(l.b, 24, pos), pos)
	l.fn.StM(l.b, c, l.fn.Const(l.b, 0, pos), l.fn.FuncAddr(l.b, f, pos), pos)
	l.fn.StM(l.b, c, l.fn.Const(l.b, 8, pos), out[0], pos)
	l.fn.StM(l.b, c, l.fn.Const(l.b, 16, pos), out[1], pos)
	return c
}

// ifaceMethodExpr lowers I.M: the static closure of .miK.M, which takes
// the interface value's two words, then M's parameters, and calls M, after
// a nil check.
func (l *lowerer) ifaceMethodExpr(e *ast.SelectorExpr, sel *types.Selection) *ir.Instr {
	pos := l.pos(e, e.Sel.Pos())
	t, name := sel.Recv(), sel.Obj().Name()
	ps, rs := l.ifaceWrapperSig(e, sel.Obj().Type().(*types.Signature))
	f := l.wrapper(fmt.Sprintf("mi%d.%s", l.ifaceKey(t), name), append([]ir.Type{ir.Ptr, ir.Ptr, ir.Ptr}, ps...), rs, pos, func(f *ir.Func) {
		gets := l.enter(f, pos)
		l.guard(l.fn.Compare(l.b, ir.Eq, gets[1](), l.fn.TypedConst(l.b, ir.Ptr, 0, pos), pos), ir.PanicNil, pos)
		var args []*ir.Instr
		for _, g := range gets[3:] {
			args = append(args, g())
		}
		l.retCall(l.callMethod(e, t, name, gets[1](), gets[2](), args, rs, pos), pos)
	})
	return l.closure(f, pos)
}

// ifaceWrapperSig returns the IR parameters after the closure, and the
// results, of a function with the Go signature sig.
func (l *lowerer) ifaceWrapperSig(n ast.Node, sig *types.Signature) ([]ir.Type, []ir.Type) {
	var ps, rs []ir.Type
	for v := range sig.Params().Variables() {
		ps = append(ps, l.words(n, v.Type())...)
	}
	for v := range sig.Results().Variables() {
		if isAggregate(v.Type()) {
			continue
		}
		rs = append(rs, l.words(n, v.Type())...)
	}
	for v := range sig.Results().Variables() {
		if isAggregate(v.Type()) {
			ps = append(ps, ir.Ptr)
		}
	}
	return ps, rs
}

// gcName returns the name of t as gc's run time writes it in panics.
func (l *lowerer) gcName(t types.Type) string {
	switch t := t.(type) {
	case *types.Alias:
		return l.gcName(types.Unalias(t))
	case *types.Basic:
		if t.Kind() == types.UnsafePointer {
			return "unsafe.Pointer"
		}
		return types.Typ[t.Kind()].Name()
	case *types.Named:
		if t.Obj().Pkg() == nil {
			return t.Obj().Name()
		}
		return l.home(t.Obj()).Name() + "." + frontend.SourceName(t.Obj().Name())
	case *types.Pointer:
		return "*" + l.gcName(t.Elem())
	case *types.Slice:
		return "[]" + l.gcName(t.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), l.gcName(t.Elem()))
	case *types.Map:
		return "map[" + l.gcName(t.Key()) + "]" + l.gcName(t.Elem())
	case *types.Chan:
		switch t.Dir() {
		case types.SendOnly:
			return "chan<- " + l.gcName(t.Elem())
		case types.RecvOnly:
			return "<-chan " + l.gcName(t.Elem())
		}
		return "chan " + l.gcName(t.Elem())
	case *types.Struct:
		if t.NumFields() == 0 {
			return "struct {}"
		}
		var fs []string
		for f := range t.Fields() {
			if f.Embedded() {
				fs = append(fs, l.gcName(f.Type()))
			} else {
				fs = append(fs, f.Name()+" "+l.gcName(f.Type()))
			}
		}
		return "struct { " + strings.Join(fs, "; ") + " }"
	case *types.Signature:
		return "func" + l.gcSig(t)
	case *types.Interface:
		if t.NumMethods() == 0 {
			return "interface {}"
		}
		var ms []string
		for m := range t.Methods() {
			ms = append(ms, m.Name()+l.gcSig(m.Type().(*types.Signature)))
		}
		return "interface { " + strings.Join(ms, "; ") + " }"
	}
	return t.String()
}

// gcSig returns a signature as gc writes it after "func".
func (l *lowerer) gcSig(s *types.Signature) string {
	var ps []string
	for i := range s.Params().Len() {
		pt := s.Params().At(i).Type()
		if s.Variadic() && i == s.Params().Len()-1 {
			ps = append(ps, "..."+l.gcName(pt.(*types.Slice).Elem()))
			continue
		}
		ps = append(ps, l.gcName(pt))
	}
	r := "(" + strings.Join(ps, ", ") + ")"
	switch s.Results().Len() {
	case 0:
	case 1:
		r += " " + l.gcName(s.Results().At(0).Type())
	default:
		var rs []string
		for v := range s.Results().Variables() {
			rs = append(rs, l.gcName(v.Type()))
		}
		r += " (" + strings.Join(rs, ", ") + ")"
	}
	return r
}
