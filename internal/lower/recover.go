package lower

// Recover (Phase 8 increment 8.6b, D-043).
//
// In a program with defer statements that calls recover, the exit loop
// sets .rframe while it runs a deferred call for a panic, and every
// function of the source copies it at entry and clears it: the copy is
// set only in the function a deferred call invokes directly, through any
// generated wrappers. recover() passes the copy to .gorecover, which
// recovers the newest panic if it may, making its value: a run-time error
// becomes a value of one of the six types gc uses, which this file
// declares, each holding the message gc writes.

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// programRecovers reports whether pkg calls the builtin recover.
func programRecovers(pkg *ast.File, info *types.Info) bool {
	found := false
	ast.Inspect(pkg, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if b, ok := info.Uses[id].(*types.Builtin); ok && b.Name() == "recover" {
				found = true
			}
		}
		return !found
	})
	return found
}

// hasRecover reports whether body, outside function literals, calls
// recover.
func (l *lowerer) hasRecover(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.Ident:
			if b, ok := l.pkg.Info.Uses[n].(*types.Builtin); ok && b.Name() == "recover" {
				found = true
			}
		}
		return !found
	})
	return found
}

// The run-time error types, by gc's name, each a struct of one string, the
// whole message, or a pointer to one. The subset has no named basic types,
// so gc's errorString and plainError are structs here too: their layout,
// equality, and names are the same.
var rtTypes = []struct {
	pkg, name string
	ptr       bool
}{
	{"runtime", "errorString", false},
	{"runtime", "boundsError", false},
	{"runtime", "plainError", false},
	{"internal/runtime/maps", "unhashableTypeError", false},
	{"runtime", "TypeAssertionError", true},
	{"runtime", "PanicNilError", true},
}

// recoverSetup declares the run-time error types with their methods and
// descriptors, and the functions recover uses.
func (l *lowerer) recoverSetup(file string, pos ir.Pos) {
	ps := l.ps
	ps.rframe = l.prog.NewGlobal(".rframe", ir.Bool, file, pos)
	ps.rt = map[string]types.Type{}
	pkgs := map[string]*types.Package{}
	var order []types.Type
	str := types.Typ[types.String]
	for _, rt := range rtTypes {
		p := pkgs[rt.pkg]
		if p == nil {
			name := rt.pkg
			if name != "runtime" {
				name = "maps"
			}
			p = types.NewPackage(rt.pkg, name)
			pkgs[rt.pkg] = p
		}
		under := types.NewStruct([]*types.Var{types.NewField(token.NoPos, p, "msg", str, false)}, nil)
		named := types.NewNamed(types.NewTypeName(token.NoPos, p, rt.name, nil), under, nil)
		var recv types.Type = named
		if rt.ptr {
			recv = types.NewPointer(named)
		}
		rv := types.NewVar(token.NoPos, p, "e", recv)
		errSig := types.NewSignatureType(rv, nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, p, "", str)), false)
		errM := types.NewFunc(token.NoPos, p, "Error", errSig)
		rtM := types.NewFunc(token.NoPos, p, "RuntimeError", types.NewSignatureType(rv, nil, nil, nil, nil, false))
		named.AddMethod(errM)
		named.AddMethod(rtM)
		// The receiver is the Ptr of the struct, a value's copy or the
		// pointer itself (D-034, D-038).
		params := []ir.Type{ir.Ptr}
		l.funcs[errM] = l.genFunc(rt.name+".Error", params, []ir.Type{ir.Ptr, ir.I64}, func(p []func() *ir.Instr, pos ir.Pos) {
			l.fn.Ret(l.b, pos, l.recField(p[0], ir.Ptr, 0, pos), l.recField(p[0], ir.I64, 8, pos))
		})
		l.funcs[rtM] = l.genFunc(rt.name+".RuntimeError", params, nil, func(_ []func() *ir.Instr, pos ir.Pos) {
			l.fn.Ret(l.b, pos)
		})
		ps.rt[gcName(recv)] = recv
		order = append(order, recv)
	}
	// The descriptors, made now: .materialize is built after them.
	saved := l.fn
	l.fn = ps.gopanic
	for _, t := range order {
		l.desc(t)
	}
	l.fn = saved
	ps.itoa = l.genFunc("itoa", []ir.Type{ir.I64, ir.Bool}, []ir.Type{ir.Ptr, ir.I64}, l.buildItoa)
	ps.mat = l.genFunc("materialize", []ir.Type{ir.Ptr}, nil, nil)
	l.late = append(l.late, func() { l.buildMaterialize(pos) })
	ps.gorecover = l.genFunc("gorecover", []ir.Type{ir.Bool}, []ir.Type{ir.Ptr, ir.Ptr}, func(p []func() *ir.Instr, pos ir.Pos) {
		none := func() { l.fn.Ret(l.b, pos, l.nilPtr(pos), l.nilPtr(pos)) }
		may := l.when(l.fn.Not(l.b, p[0](), pos), pos)
		none()
		l.b = may
		rec := l.keep(l.fn.GLoad(l.b, ps.panics, pos), pos)
		fresh := l.when(l.recField(rec, ir.Bool, recRecov, pos), pos)
		none()
		l.b = fresh
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, recRecov, pos), l.fn.BoolConst(l.b, true, pos), pos)
		l.fn.Call(l.b, ps.mat, []*ir.Instr{rec()}, pos)
		l.fn.Ret(l.b, pos, l.recField(rec, ir.Ptr, recTab, pos), l.recField(rec, ir.Ptr, recData, pos))
	})
}

// recoverCall lowers recover(): nil in a program that does not unwind, and
// otherwise .gorecover's result for the copy of .rframe taken at entry.
func (l *lowerer) recoverCall(call *ast.CallExpr) []*ir.Instr {
	pos := l.pos(call, call.Pos())
	if l.ps == nil || !l.ps.recovers {
		return []*ir.Instr{l.nilPtr(pos), l.nilPtr(pos)}
	}
	tok := l.fn.BoolConst(l.b, false, pos)
	if l.rtok != nil {
		tok = l.fn.Load(l.b, l.rtok, pos)
	}
	c := l.fn.Call(l.b, l.ps.gorecover, []*ir.Instr{tok}, pos)
	return []*ir.Instr{l.fn.Extract(l.b, c, 0, pos), l.fn.Extract(l.b, c, 1, pos)}
}

// enterRecover, at the entry of a function of the source, copies .rframe
// into a slot if body calls recover, and clears it.
func (l *lowerer) enterRecover(body ast.Node, pos ir.Pos) {
	l.rtok = nil
	if l.ps == nil || !l.ps.recovers {
		return
	}
	if l.hasRecover(body) {
		l.rtok = l.fn.NewSlot("rframe", ir.Bool, pos)
		l.fn.Store(l.b, l.rtok, l.fn.GLoad(l.b, l.ps.rframe, pos), pos)
	}
	l.fn.GStore(l.b, l.ps.rframe, l.fn.BoolConst(l.b, false, pos), pos)
}

// markRepanicked marks the next older panic than p repanicked if both
// are values with equal words, as gc's preprintpanics does.
func (l *lowerer) markRepanicked(p func() *ir.Instr, pos ir.Pos) {
	link := l.keep(l.recField(p, ir.Ptr, recLink, pos), pos)
	done := l.fn.NewBlock()
	for _, cond := range []func() *ir.Instr{
		func() *ir.Instr { return l.fn.Compare(l.b, ir.Ne, link(), l.nilPtr(pos), pos) },
		func() *ir.Instr {
			return l.fn.Compare(l.b, ir.Eq, l.recField(p, ir.I64, recKind, pos), l.fn.Const(l.b, kindValue, pos), pos)
		},
		func() *ir.Instr {
			return l.fn.Compare(l.b, ir.Eq, l.recField(link, ir.I64, recKind, pos), l.fn.Const(l.b, kindValue, pos), pos)
		},
		func() *ir.Instr {
			return l.fn.Compare(l.b, ir.Eq, l.recField(p, ir.Ptr, recTab, pos), l.recField(link, ir.Ptr, recTab, pos), pos)
		},
		func() *ir.Instr {
			return l.fn.Compare(l.b, ir.Eq, l.recField(p, ir.Ptr, recData, pos), l.recField(link, ir.Ptr, recData, pos), pos)
		},
	} {
		no := l.when(cond(), pos)
		l.fn.Jump(no, done, pos)
	}
	l.fn.StM(l.b, link(), l.fn.Const(l.b, recRepan, pos), l.fn.BoolConst(l.b, true, pos), pos)
	l.fn.Jump(l.b, done, pos)
	l.b = done
}

// recoveredMarks writes the marks of the panic p after its value.
func (l *lowerer) recoveredMarks(p func() *ir.Instr, pos ir.Pos) {
	done := l.fn.NewBlock()
	no := l.when(l.recField(p, ir.Bool, recRecov, pos), pos)
	l.fn.Jump(no, done, pos)
	plain := l.when(l.recField(p, ir.Bool, recRepan, pos), pos)
	l.printStr(" [recovered, repanicked]", pos)
	l.fn.Jump(l.b, done, pos)
	l.b = plain
	l.printStr(" [recovered]", pos)
	l.fn.Jump(l.b, done, pos)
	l.b = done
}

// buildMaterialize builds .materialize(rec), which makes the value of a
// recovered panic that is not one already: the run-time error, or
// panic(nil)'s *runtime.PanicNilError, of gc's type and message.
func (l *lowerer) buildMaterialize(pos ir.Pos) {
	ps := l.enter(l.ps.mat, pos)
	rec := ps[0]
	kind := func() *ir.Instr { return l.recField(rec, ir.I64, recKind, pos) }
	is := func(k int64) *ir.Block {
		return l.when(l.fn.Compare(l.b, ir.Eq, kind(), l.fn.Const(l.b, k, pos), pos), pos)
	}
	no := is(kindValue)
	l.fn.Ret(l.b, pos)
	l.b = no
	no = is(kindNil)
	l.setError(rec, "*runtime.PanicNilError", l.strWords("panic called with nil argument", pos), pos)
	l.b = no
	for k := range ir.PanicMessages {
		if k == ir.PanicExit {
			continue
		}
		no = is(kindRuntime + int64(k))
		switch {
		case k == ir.PanicMsg:
			l.classifyMsg(rec, pos)
		case ir.IsBounds(k):
			f := ir.BoundsFormat(k)
			signed := l.recField(rec, ir.Bool, recSigned, pos)
			x := l.fn.Call(l.b, l.ps.itoa, []*ir.Instr{l.recField(rec, ir.I64, recX, pos), signed}, pos)
			xw := l.spillWords([]*ir.Instr{l.fn.Extract(l.b, x, 0, pos), l.fn.Extract(l.b, x, 1, pos)}, pos)
			head := l.strWords("runtime error: "+ir.PanicMessages[k]+" "+f[0], pos)
			hw := l.spillWords(head, pos)
			full := l.fn.NewBlock()
			unsigned := l.when(l.recField(rec, ir.Bool, recSigned, pos), pos)
			nonneg := l.when(l.fn.Compare(l.b, ir.Lt, l.recField(rec, ir.I64, recX, pos), l.fn.Const(l.b, 0, pos), pos), pos)
			// A negative signed x has no y.
			l.setError(rec, "runtime.boundsError", l.concat([][]*ir.Instr{hw(), xw(), l.strWords(f[3], pos)}, pos), pos)
			l.fn.Jump(nonneg, full, pos)
			l.fn.Jump(unsigned, full, pos)
			l.b = full
			y := l.fn.Call(l.b, l.ps.itoa, []*ir.Instr{l.recField(rec, ir.I64, recY, pos), l.fn.BoolConst(l.b, true, pos)}, pos)
			yw := []*ir.Instr{l.fn.Extract(l.b, y, 0, pos), l.fn.Extract(l.b, y, 1, pos)}
			parts := [][]*ir.Instr{hw(), xw(), l.strWords(f[1], pos), yw}
			if f[2] != "" {
				parts = append(parts, l.strWords(f[2], pos))
			}
			l.setError(rec, "runtime.boundsError", l.concat(parts, pos), pos)
		default:
			l.setError(rec, "runtime.errorString", l.strWords("runtime error: "+ir.PanicMessages[k], pos), pos)
		}
		l.b = no
	}
	l.fn.Ret(l.b, pos)
	prune(l.fn)
}

// classifyMsg makes the value of a PanicMsg run-time error, whose message
// tells gc's type.
func (l *lowerer) classifyMsg(rec func() *ir.Instr, pos ir.Pos) {
	msg := func() []*ir.Instr {
		return []*ir.Instr{l.recField(rec, ir.Ptr, recMsg, pos), l.recField(rec, ir.I64, recMsgLen, pos)}
	}
	for _, c := range []struct {
		prefix, t string
		exact     bool
	}{
		{"interface conversion: ", "*runtime.TypeAssertionError", false},
		{"hash of unhashable type: ", "maps.unhashableTypeError", false},
		{"assignment to entry in nil map", "runtime.plainError", true},
	} {
		n := int64(len(c.prefix))
		cmp := ir.Ge
		if c.exact {
			cmp = ir.Eq
		}
		no := l.when(l.fn.Compare(l.b, cmp, msg()[1], l.fn.Const(l.b, n, pos), pos), pos)
		p := l.strWords(c.prefix, pos)
		eq := l.fn.Call(l.b, l.streqFunc(pos), []*ir.Instr{msg()[0], l.fn.Const(l.b, n, pos), p[0], p[1]}, pos)
		other := l.when(eq, pos)
		l.setError(rec, c.t, msg(), pos)
		l.fn.Jump(other, no, pos)
		l.b = no
	}
	l.setError(rec, "runtime.errorString", msg(), pos)
}

// setError makes rec's value the run-time error type named t holding the
// message w, and returns.
func (l *lowerer) setError(rec func() *ir.Instr, t string, w []*ir.Instr, pos ir.Pos) {
	get := l.spillWords(w, pos)
	box := l.keep(l.fn.Alloc(l.b, l.fn.Const(l.b, 16, pos), pos), pos)
	w = get()
	l.fn.StM(l.b, box(), l.fn.Const(l.b, 0, pos), w[0], pos)
	l.fn.StM(l.b, box(), l.fn.Const(l.b, 8, pos), w[1], pos)
	l.fn.StM(l.b, rec(), l.fn.Const(l.b, recTab, pos), l.addr(l.desc(l.ps.rt[t]).mem, pos), pos)
	l.fn.StM(l.b, rec(), l.fn.Const(l.b, recData, pos), box(), pos)
	l.fn.StM(l.b, rec(), l.fn.Const(l.b, recKind, pos), l.fn.Const(l.b, kindValue, pos), pos)
	l.fn.Ret(l.b, pos)
}

// buildItoa builds .itoa(v, signed), the decimal digits of v, signed or
// unsigned, as a new string.
func (l *lowerer) buildItoa(ps []func() *ir.Instr, pos ir.Pos) {
	buf := l.keep(l.fn.Alloc(l.b, l.fn.Const(l.b, 20, pos), pos), pos)
	bs := l.fn.NewSlot("buf", ir.Ptr, pos)
	l.fn.Store(l.b, bs, buf(), pos)
	i, u, neg := l.fn.NewSlot("i", ir.I64, pos), l.fn.NewSlot("u", ir.U64, pos), l.fn.NewSlot("neg", ir.Bool, pos)
	l.fn.Store(l.b, i, l.fn.Const(l.b, 20, pos), pos)
	l.fn.Store(l.b, u, l.fn.Conv(l.b, ir.U64, ps[0](), pos), pos)
	l.fn.Store(l.b, neg, l.fn.BoolConst(l.b, false, pos), pos)
	loop := l.fn.NewBlock()
	unsigned := l.when(ps[1](), pos)
	nonneg := l.when(l.fn.Compare(l.b, ir.Lt, ps[0](), l.fn.Const(l.b, 0, pos), pos), pos)
	l.fn.Store(l.b, neg, l.fn.BoolConst(l.b, true, pos), pos)
	l.fn.Store(l.b, u, l.fn.Conv(l.b, ir.U64, l.fn.Neg(l.b, ps[0](), pos), pos), pos)
	l.fn.Jump(l.b, loop, pos)
	l.fn.Jump(nonneg, loop, pos)
	l.fn.Jump(unsigned, loop, pos)
	l.b = loop
	dec := func(s *ir.Slot) {
		l.fn.Store(l.b, s, l.fn.Binary(l.b, ir.Sub, l.fn.Load(l.b, s, pos), l.fn.Const(l.b, 1, pos), pos), pos)
	}
	dec(i)
	ten := l.fn.TypedConst(l.b, ir.U64, 10, pos)
	d := l.fn.Binary(l.b, ir.Add, l.fn.Binary(l.b, ir.Rem, l.fn.Load(l.b, u, pos), ten, pos), l.fn.TypedConst(l.b, ir.U64, '0', pos), pos)
	l.fn.StM(l.b, l.fn.Load(l.b, bs, pos), l.fn.Load(l.b, i, pos), l.fn.Conv(l.b, ir.U8, d, pos), pos)
	l.fn.Store(l.b, u, l.fn.Binary(l.b, ir.Div, l.fn.Load(l.b, u, pos), l.fn.TypedConst(l.b, ir.U64, 10, pos), pos), pos)
	after := l.fn.NewBlock()
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, l.fn.Load(l.b, u, pos), l.fn.TypedConst(l.b, ir.U64, 0, pos), pos), loop, after, pos)
	l.b = after
	sign := l.when(l.fn.Load(l.b, neg, pos), pos)
	dec(i)
	l.fn.StM(l.b, l.fn.Load(l.b, bs, pos), l.fn.Load(l.b, i, pos), l.fn.TypedConst(l.b, ir.U8, '-', pos), pos)
	l.fn.Jump(l.b, sign, pos)
	l.b = sign
	at := l.fn.Load(l.b, i, pos)
	l.fn.Ret(l.b, pos, l.fn.PAdd(l.b, l.fn.Load(l.b, bs, pos), at, pos), l.fn.Binary(l.b, ir.Sub, l.fn.Const(l.b, 20, pos), at, pos))
}
