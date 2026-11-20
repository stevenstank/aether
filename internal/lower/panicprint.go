package lower

// The functions that write the panics when the program ends (D-043), as
// gc's printpanics and printpanicval do (runtime/panic.go and error.go,
// go1.26.1).

import (
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// printStr writes the constant string s.
func (l *lowerer) printStr(s string, pos ir.Pos) {
	if s != "" {
		l.fn.Print(l.b, l.strWords(s, pos), pos)
	}
}

// field loads the word of type t at offset off of the record p.
func (l *lowerer) recField(p func() *ir.Instr, t ir.Type, off int64, pos ir.Pos) *ir.Instr {
	return l.fn.LdM(l.b, t, p(), l.fn.Const(l.b, off, pos), pos)
}

// when continues in a new block if c is true, and returns the block to
// continue in otherwise.
func (l *lowerer) when(c *ir.Instr, pos ir.Pos) *ir.Block {
	yes, no := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, c, yes, no, pos)
	l.b = yes
	return no
}

// stringMethod returns the selection of t's method name, if t's method
// set has one of type func() string.
func stringMethod(t types.Type, name string) *types.Selection {
	sel := types.NewMethodSet(t).Lookup(nil, name)
	if sel == nil {
		return nil
	}
	s := sel.Type().(*types.Signature)
	if s.Params().Len() != 0 || s.Results().Len() != 1 || !types.Identical(s.Results().At(0).Type(), types.Typ[types.String]) {
		return nil
	}
	return sel
}

// printMethod returns the method whose result replaces a panic value of
// type t when the panics are written: Error, else String, or nil.
func printMethod(t types.Type) *types.Selection {
	if sel := stringMethod(t, "Error"); sel != nil {
		return sel
	}
	return stringMethod(t, "String")
}

// buildPreprint builds .preprint(rec): if the panic's value is of a type
// with an Error or String method, the method's result replaces it.
func (l *lowerer) buildPreprint(pos ir.Pos) {
	ps := l.enter(l.ps.preprint, pos)
	rec := ps[0]
	end := l.fn.NewBlock()
	other := l.when(l.fn.Compare(l.b, ir.Eq, l.recField(rec, ir.I64, recKind, pos), l.fn.Const(l.b, kindValue, pos), pos), pos)
	for _, d := range append([]*tdesc(nil), l.descs...) {
		sel := printMethod(d.t)
		if sel == nil {
			continue
		}
		no := l.when(l.fn.Compare(l.b, ir.Eq, l.recField(rec, ir.Ptr, recTab, pos), l.addr(d.mem, pos), pos), pos)
		c := l.fn.Call(l.b, l.ifaceMethod(d, sel, pos), []*ir.Instr{l.nilPtr(pos), l.recField(rec, ir.Ptr, recData, pos)}, pos)
		s, n := l.fn.Extract(l.b, c, 0, pos), l.fn.Extract(l.b, c, 1, pos)
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, recMsg, pos), s, pos)
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, recMsgLen, pos), n, pos)
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, recKind, pos), l.fn.Const(l.b, kindString, pos), pos)
		l.fn.Jump(l.b, end, pos)
		l.b = no
	}
	l.fn.Jump(l.b, end, pos)
	l.b = other
	l.fn.Jump(l.b, end, pos)
	l.b = end
	l.fn.Ret(l.b, pos)
	prune(l.fn)
}

// buildPrintval builds .printval(rec), which writes the panic's value.
func (l *lowerer) buildPrintval(pos ir.Pos) {
	ps := l.enter(l.ps.printv, pos)
	rec := ps[0]
	kind := func() *ir.Instr { return l.recField(rec, ir.I64, recKind, pos) }
	is := func(k int64) *ir.Block {
		return l.when(l.fn.Compare(l.b, ir.Eq, kind(), l.fn.Const(l.b, k, pos), pos), pos)
	}
	no := is(kindNil)
	l.printStr("panic called with nil argument", pos)
	l.fn.Ret(l.b, pos)
	l.b = no
	no = is(kindString)
	l.fn.Call(l.b, l.ps.indented, []*ir.Instr{l.recField(rec, ir.Ptr, recMsg, pos), l.recField(rec, ir.I64, recMsgLen, pos)}, pos)
	l.fn.Ret(l.b, pos)
	l.b = no
	for k := range ir.PanicMessages {
		if k == ir.PanicExit || k == ir.PanicFatal { // never in a panic record
			continue
		}
		no = is(kindRuntime + int64(k))
		l.printRuntime(k, rec, pos)
		l.fn.Ret(l.b, pos)
		l.b = no
	}
	for _, d := range append([]*tdesc(nil), l.descs...) {
		no = l.when(l.fn.Compare(l.b, ir.Eq, l.recField(rec, ir.Ptr, recTab, pos), l.addr(d.mem, pos), pos), pos)
		l.printValue(d.t, rec, pos)
		l.fn.Ret(l.b, pos)
		l.b = no
	}
	l.fn.Ret(l.b, pos)
	prune(l.fn)
}

// printRuntime writes the message of the run-time error kind of rec.
func (l *lowerer) printRuntime(k int, rec func() *ir.Instr, pos ir.Pos) {
	switch {
	case k == ir.PanicMsg:
		l.fn.Print(l.b, []*ir.Instr{l.recField(rec, ir.Ptr, recMsg, pos), l.recField(rec, ir.I64, recMsgLen, pos)}, pos)
	case ir.IsBounds(k):
		f := ir.BoundsFormat(k)
		l.printStr("runtime error: "+ir.PanicMessages[k]+" "+f[0], pos)
		tail := l.fn.NewBlock()
		unsigned := l.when(l.recField(rec, ir.Bool, recSigned, pos), pos)
		x := l.recField(rec, ir.I64, recX, pos)
		l.fn.Print(l.b, []*ir.Instr{x}, pos)
		nonneg := l.when(l.fn.Compare(l.b, ir.Lt, x, l.fn.Const(l.b, 0, pos), pos), pos)
		l.printStr(f[3], pos)
		l.fn.Ret(l.b, pos)
		l.b = nonneg
		l.fn.Jump(l.b, tail, pos)
		l.b = unsigned
		l.fn.Print(l.b, []*ir.Instr{l.fn.Conv(l.b, ir.U64, l.recField(rec, ir.I64, recX, pos), pos)}, pos)
		l.fn.Jump(l.b, tail, pos)
		l.b = tail
		l.printStr(f[1], pos)
		l.fn.Print(l.b, []*ir.Instr{l.recField(rec, ir.I64, recY, pos)}, pos)
		l.printStr(f[2], pos)
	default:
		l.printStr("runtime error: "+ir.PanicMessages[k], pos)
	}
}

// printValue writes the panic value of rec, of dynamic type t, as gc's
// printpanicval: a value of a basic type as print writes it, a string with
// a tab after each newline, and of any other type its type's name and its
// data word in hex. A value of a defined basic type is written as its
// type's name and the value in parentheses, a string in quotes (gc's
// printanycustomtype).
func (l *lowerer) printValue(t types.Type, rec func() *ir.Instr, pos ir.Pos) {
	t = types.Unalias(t)
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		l.printStr("("+gcName(t)+") ", pos)
		l.fn.Call(l.b, l.ps.hex, []*ir.Instr{l.recField(rec, ir.I64, recData, pos)}, pos)
		return
	}
	_, named := t.(*types.Named)
	str := b.Info()&types.IsString != 0
	if named {
		l.printStr(gcName(t)+map[bool]string{false: "(", true: `("`}[str], pos)
	}
	w := l.loadPlace(l.valuePlace(l.recField(rec, ir.Ptr, recData, pos), t, pos), pos)
	if str {
		l.fn.Call(l.b, l.ps.indented, w, pos)
	} else {
		l.fn.Print(l.b, w, pos)
	}
	if named {
		l.printStr(map[bool]string{false: ")", true: `")`}[str], pos)
	}
}

// buildIndented builds .printindented(p, n), which writes the string with
// a tab after each newline.
func (l *lowerer) buildIndented(ps []func() *ir.Instr, pos ir.Pos) {
	start, i := l.fn.NewSlot("start", ir.I64, pos), l.fn.NewSlot("i", ir.I64, pos)
	l.fn.Store(l.b, start, l.fn.Const(l.b, 0, pos), pos)
	l.fn.Store(l.b, i, l.fn.Const(l.b, 0, pos), pos)
	head, next := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Jump(l.b, head, pos)
	l.b = head
	done := l.when(l.fn.Compare(l.b, ir.Lt, l.fn.Load(l.b, i, pos), ps[1](), pos), pos)
	c := l.fn.LoadB(l.b, ps[0](), l.fn.Load(l.b, i, pos), pos)
	other := l.when(l.fn.Compare(l.b, ir.Eq, c, l.fn.TypedConst(l.b, ir.U8, '\n', pos), pos), pos)
	// Write s[start:i+1], then a tab.
	from := l.fn.Load(l.b, start, pos)
	end := l.fn.Binary(l.b, ir.Add, l.fn.Load(l.b, i, pos), l.fn.Const(l.b, 1, pos), pos)
	l.fn.Print(l.b, []*ir.Instr{l.fn.PAdd(l.b, ps[0](), from, pos), l.fn.Binary(l.b, ir.Sub, end, from, pos)}, pos)
	l.printStr("\t", pos)
	l.fn.Store(l.b, start, l.fn.Binary(l.b, ir.Add, l.fn.Load(l.b, i, pos), l.fn.Const(l.b, 1, pos), pos), pos)
	l.fn.Jump(l.b, next, pos)
	l.b = other
	l.fn.Jump(l.b, next, pos)
	l.b = next
	l.fn.Store(l.b, i, l.fn.Binary(l.b, ir.Add, l.fn.Load(l.b, i, pos), l.fn.Const(l.b, 1, pos), pos), pos)
	l.fn.Jump(l.b, head, pos)
	l.b = done
	from = l.fn.Load(l.b, start, pos)
	skip := l.when(l.fn.Compare(l.b, ir.Lt, from, ps[1](), pos), pos)
	from = l.fn.Load(l.b, start, pos)
	l.fn.Print(l.b, []*ir.Instr{l.fn.PAdd(l.b, ps[0](), from, pos), l.fn.Binary(l.b, ir.Sub, ps[1](), from, pos)}, pos)
	l.fn.Jump(l.b, skip, pos)
	l.b = skip
	l.fn.Ret(l.b, pos)
}

// buildHex builds .printhex(v), which writes 0x and v's hex digits, as
// gc's print of a pointer.
func (l *lowerer) buildHex(ps []func() *ir.Instr, pos ir.Pos) {
	l.printStr("0x", pos)
	shift := l.fn.NewSlot("shift", ir.I64, pos)
	l.fn.Store(l.b, shift, l.fn.Const(l.b, 60, pos), pos)
	digit := func() *ir.Instr {
		v := l.fn.Conv(l.b, ir.U64, ps[0](), pos)
		s := l.fn.Conv(l.b, ir.U64, l.fn.Load(l.b, shift, pos), pos)
		return l.fn.Conv(l.b, ir.I64, l.fn.Binary(l.b, ir.And, l.fn.Binary(l.b, ir.Shr, v, s, pos), l.fn.TypedConst(l.b, ir.U64, 15, pos), pos), pos)
	}
	skip, write := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Jump(l.b, skip, pos)
	// Skip the leading zero digits but the last.
	l.b = skip
	more := l.when(l.fn.Compare(l.b, ir.Gt, l.fn.Load(l.b, shift, pos), l.fn.Const(l.b, 0, pos), pos), pos)
	zero := l.when(l.fn.Compare(l.b, ir.Eq, digit(), l.fn.Const(l.b, 0, pos), pos), pos)
	l.fn.Store(l.b, shift, l.fn.Binary(l.b, ir.Sub, l.fn.Load(l.b, shift, pos), l.fn.Const(l.b, 4, pos), pos), pos)
	l.fn.Jump(l.b, skip, pos)
	l.b = zero
	l.fn.Jump(l.b, write, pos)
	l.b = more
	l.fn.Jump(l.b, write, pos)
	l.b = write
	digits := l.fn.Str(l.b, l.prog.Intern("0123456789abcdef"), pos)
	l.fn.Print(l.b, []*ir.Instr{l.fn.PAdd(l.b, digits, digit(), pos), l.fn.Const(l.b, 1, pos)}, pos)
	last := l.when(l.fn.Compare(l.b, ir.Eq, l.fn.Load(l.b, shift, pos), l.fn.Const(l.b, 0, pos), pos), pos)
	l.fn.Ret(l.b, pos)
	l.b = last
	l.fn.Store(l.b, shift, l.fn.Binary(l.b, ir.Sub, l.fn.Load(l.b, shift, pos), l.fn.Const(l.b, 4, pos), pos), pos)
	l.fn.Jump(l.b, write, pos)
}
