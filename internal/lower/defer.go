package lower

// Defer and panic (Phase 8 increment 8.6a, D-043).
//
// A program unwinds if it has a defer statement. The panic state is two
// globals: .panics, the Ptr of the newest panic record, and .unwinding,
// true while a panic passes through frames. A panic record is a heap
// object: the link to the next older record, the value's tab and data
// (as any), the kind, a run-time error's operands x and y and whether x
// is signed, and a message's Ptr and length. A function with a defer
// statement keeps the Ptr of its newest defer record in a slot; a record
// is the link, the code address of the statement's .deferN, then the func
// value, if any, and the arguments. Every return stores its results in
// slots and goes to the function's exit, which pops and calls the records
// (deferExit). The checks after calls, and the exits of the functions
// without defer statements, are added once lowering ends (unwind.go).

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// The fields of a panic record.
const (
	recLink   = 0
	recTab    = 8
	recData   = 16
	recKind   = 24
	recX      = 32
	recY      = 40
	recSigned = 48
	recRecov  = 49 // recovered (8.6b)
	recRepan  = 50 // repanicked: written once, with the next newer panic
	recMsg    = 56
	recMsgLen = 64
	recSize   = 72
)

// The kinds of a panic record.
const (
	kindValue   = iota // panic(v): tab and data
	kindNil            // panic(nil), gc's PanicNilError
	kindString         // a value replaced by its Error or String method's result: msg
	kindRuntime        // plus the ir run-time error kind: x, y, and signed, or msg for PanicMsg
)

// deferFn is the defer state of the function being lowered.
type deferFn struct {
	list    *ir.Slot   // the Ptr of the newest defer record, 0 if none
	entry   *ir.Slot   // in a program that recovers, the newest panic at entry
	results []*ir.Slot // the words of the results a return stores
	exit    *ir.Block
}

// panicState is the program's panic state and the functions generated
// for it, created when the program first needs them.
type panicState struct {
	unwinding, panics *ir.Global
	gopanic, raise    *ir.Func
	printpanics       *ir.Func
	preprint, printv  *ir.Func // late: they list every descriptor
	chain, indented   *ir.Func
	hex               *ir.Func
	defers            int        // the defer statements lowered so far
	recovers          bool       // the program calls recover (recover.go)
	rframe            *ir.Global // set while a deferred call runs for a panic
	gorecover, mat    *ir.Func   // .gorecover and .materialize
	itoa              *ir.Func
	rt                map[string]types.Type  // the run-time error types, by gc's name
	exits             map[*ir.Func]*ir.Block // each function's exit
	final             map[*ir.Instr]bool     // the PanicExits that end the program
	noCheck           map[*ir.Instr]bool     // the calls no check follows
}

// hasDefer reports whether the statements of body, outside function
// literals, include a defer statement.
func hasDefer(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			found = true
		}
		return !found
	})
	return found
}

// programDefers reports whether pkg has a defer statement, in a function
// literal or not.
func programDefers(pkg *frontend.Package) bool {
	found := false
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			if _, ok := n.(*ast.DeferStmt); ok {
				found = true
			}
			return !found
		})
	}
	return found
}

// genFunc returns a new generated function, at the package clause, whose
// body build makes, if not nil, after every function of the program has
// been lowered.
func (l *lowerer) genFunc(name string, params, results []ir.Type, build func(ps []func() *ir.Instr, pos ir.Pos)) *ir.Func {
	f0 := l.mainFile
	file := frontend.Position(l.pkg.Fset, f0.Package).File
	saved := l.fn
	l.fn = nil
	pos := l.pos(f0, f0.Package)
	l.fn = saved
	f := l.prog.NewFunc(genPrefix+name, file, pos)
	f.Params, f.Results = params, results
	if build != nil {
		l.bodies = append(l.bodies, func() {
			build(l.enter(f, pos), pos)
			prune(l.fn)
		})
	}
	return f
}

// panics returns the panic state, creating its globals and functions on
// first use.
func (l *lowerer) panics() *panicState {
	if l.ps != nil {
		return l.ps
	}
	saved := l.fn
	f0 := l.mainFile
	file := frontend.Position(l.pkg.Fset, f0.Package).File
	l.fn = nil
	pos := l.pos(f0, f0.Package)
	l.fn = saved
	ps := &panicState{exits: map[*ir.Func]*ir.Block{}, final: map[*ir.Instr]bool{}, noCheck: map[*ir.Instr]bool{}}
	l.ps = ps
	ps.unwinding = l.prog.NewGlobal(".unwinding", ir.Bool, file, pos)
	ps.panics = l.prog.NewGlobal(".panics", ir.Ptr, file, pos)
	ps.gopanic = l.genFunc("gopanic", []ir.Type{ir.Ptr, ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
		rec := l.push(pos)
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, recTab, pos), p[0](), pos)
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, recData, pos), p[1](), pos)
		isNil, done := l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, p[0](), l.nilPtr(pos), pos), isNil, done, pos)
		l.b = isNil
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, recKind, pos), l.fn.Const(l.b, kindNil, pos), pos)
		l.fn.Jump(l.b, done, pos)
		l.b = done
		l.fn.Ret(l.b, pos)
	})
	ps.raise = l.genFunc("raise", []ir.Type{ir.I64, ir.I64, ir.I64, ir.Bool, ir.Ptr, ir.I64}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
		rec := l.push(pos)
		for i, off := range []int64{recKind, recX, recY, recSigned, recMsg, recMsgLen} {
			l.fn.StM(l.b, rec(), l.fn.Const(l.b, off, pos), p[i](), pos)
		}
		l.fn.Ret(l.b, pos)
	})
	ps.preprint = l.genFunc("preprint", []ir.Type{ir.Ptr}, nil, nil)
	ps.printv = l.genFunc("printval", []ir.Type{ir.Ptr}, nil, nil)
	l.late = append(l.late, func() { l.buildPreprint(pos) }, func() { l.buildPrintval(pos) })
	if l.wantRecover {
		ps.recovers = true
		defer l.recoverSetup(file, pos)
	}
	ps.indented = l.genFunc("printindented", []ir.Type{ir.Ptr, ir.I64}, nil, l.buildIndented)
	ps.hex = l.genFunc("printhex", []ir.Type{ir.I64}, nil, l.buildHex)
	ps.chain = l.genFunc("printchain", []ir.Type{ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
		link := l.fn.LdM(l.b, ir.Ptr, p[0](), l.fn.Const(l.b, recLink, pos), pos)
		older, own := l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, link, l.nilPtr(pos), pos), older, own, pos)
		l.b = older
		l.fn.Call(l.b, ps.chain, []*ir.Instr{l.fn.LdM(l.b, ir.Ptr, p[0](), l.fn.Const(l.b, recLink, pos), pos)}, pos)
		if ps.recovers {
			// A repanicked value is written once, with the older panic.
			link := l.fn.LdM(l.b, ir.Ptr, p[0](), l.fn.Const(l.b, recLink, pos), pos)
			no := l.when(l.fn.LdM(l.b, ir.Bool, link, l.fn.Const(l.b, recRepan, pos), pos), pos)
			l.fn.Ret(l.b, pos)
			l.b = no
		}
		l.fn.Print(l.b, l.strWords("\t", pos), pos)
		l.fn.Jump(l.b, own, pos)
		l.b = own
		l.fn.Print(l.b, l.strWords("panic: ", pos), pos)
		l.fn.Call(l.b, ps.printv, []*ir.Instr{p[0]()}, pos)
		if ps.recovers {
			l.recoveredMarks(p[0], pos)
		}
		l.fn.Println(l.b, nil, pos)
		l.fn.Ret(l.b, pos)
	})
	// .printpanics first replaces values by their methods' results, newest
	// first, then writes the panics, oldest first, as gc's printpanics.
	ps.printpanics = l.genFunc("printpanics", nil, nil, func(_ []func() *ir.Instr, pos ir.Pos) {
		p := l.fn.NewSlot("p", ir.Ptr, pos)
		l.fn.Store(l.b, p, l.fn.GLoad(l.b, ps.panics, pos), pos)
		head, body, done := l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Jump(l.b, head, pos)
		l.b = head
		l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, l.fn.Load(l.b, p, pos), l.nilPtr(pos), pos), body, done, pos)
		l.b = body
		if ps.recovers {
			l.markRepanicked(func() *ir.Instr { return l.fn.Load(l.b, p, pos) }, pos)
		}
		l.fn.Call(l.b, ps.preprint, []*ir.Instr{l.fn.Load(l.b, p, pos)}, pos)
		l.fn.Store(l.b, p, l.fn.LdM(l.b, ir.Ptr, l.fn.Load(l.b, p, pos), l.fn.Const(l.b, recLink, pos), pos), pos)
		l.fn.Jump(l.b, head, pos)
		l.b = done
		l.fn.Call(l.b, ps.chain, []*ir.Instr{l.fn.GLoad(l.b, ps.panics, pos)}, pos)
		l.fn.Ret(l.b, pos)
	})
	return ps
}

func (l *lowerer) nilPtr(pos ir.Pos) *ir.Instr { return l.fn.TypedConst(l.b, ir.Ptr, 0, pos) }

// push allocates a panic record, links it as the newest, and sets
// .unwinding; it returns the record's Ptr.
func (l *lowerer) push(pos ir.Pos) func() *ir.Instr {
	ps := l.ps
	rec := l.keep(l.fn.Alloc(l.b, l.fn.Const(l.b, recSize, pos), pos), pos)
	l.fn.StM(l.b, rec(), l.fn.Const(l.b, recLink, pos), l.fn.GLoad(l.b, ps.panics, pos), pos)
	l.fn.GStore(l.b, ps.panics, rec(), pos)
	l.fn.GStore(l.b, ps.unwinding, l.fn.BoolConst(l.b, true, pos), pos)
	return rec
}

// panicCall lowers panic(v): v converted to any, then .gopanic, then the
// function's exit (unwind.go), or in a program that does not unwind the
// panics' output and the end of the program.
func (l *lowerer) panicCall(call *ast.CallExpr) {
	pos := l.pos(call, call.Pos())
	w := l.valueFor(call.Args[0], types.Universe.Lookup("any").Type())
	ps := l.panics()
	l.fn.Call(l.b, ps.gopanic, w, pos)
	if l.unwinds {
		l.fn.Panic(l.b, ir.PanicExit, pos) // the function's exit (unwind.go)
	} else {
		l.endProgram(pos)
	}
	l.done = true
}

// startDefers prepares the function being lowered, which has a defer
// statement: its list and its results' slots, zero at entry, and its exit.
func (l *lowerer) startDefers(pos ir.Pos) {
	d := &deferFn{list: l.fn.NewSlot("defers", ir.Ptr, pos), exit: l.fn.NewBlock()}
	l.fn.Store(l.b, d.list, l.nilPtr(pos), pos)
	if l.panics().recovers {
		d.entry = l.fn.NewSlot("entry", ir.Ptr, pos)
		l.fn.Store(l.b, d.entry, l.fn.GLoad(l.b, l.ps.panics, pos), pos)
	}
	if l.named != nil {
		// The exit returns the result variables (D-044).
		l.dfr = d
		l.panics().exits[l.fn] = d.exit
		return
	}
	if l.ps.recovers {
		// A recovering function returns zero results unless its return
		// stored others.
		k := 0
		for r := range l.sig.Results().Variables() {
			if isAggregate(r.Type()) {
				s := l.retPtrs[k]
				k++
				l.zero(place{base: func() *ir.Instr { return l.fn.Load(l.b, s, pos) }, t: r.Type()}, pos)
			}
		}
	}
	for _, t := range l.fn.Results {
		s := l.fn.NewSlot("result", t, pos)
		l.fn.Store(l.b, s, l.fn.TypedConst(l.b, t, 0, pos), pos)
		d.results = append(d.results, s)
	}
	l.dfr = d
	l.panics().exits[l.fn] = d.exit
}

// retWords ends the current block with a return of w: in a function with
// a defer statement, w is stored and the exit runs the deferred calls.
func (l *lowerer) retWords(pos ir.Pos, w ...*ir.Instr) {
	if l.dfr == nil {
		l.fn.Ret(l.b, pos, w...)
		return
	}
	for i, v := range w {
		l.fn.Store(l.b, l.dfr.results[i], v, pos)
	}
	l.fn.Jump(l.b, l.dfr.exit, pos)
}

// deferExit builds the exit: it pops each record and calls its .deferN
// with .unwinding cleared, then sets .unwinding if it was set before the
// call, and finally returns the results; main, if unwinding, writes the
// panics and ends the program.
func (l *lowerer) deferExit(pos ir.Pos) {
	ps, d := l.ps, l.dfr
	l.b = d.exit
	call, done := l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Ne, l.fn.Load(l.b, d.list, pos), l.nilPtr(pos), pos), call, done, pos)
	l.b = call
	rec := l.keep(l.fn.Load(l.b, d.list, pos), pos)
	l.fn.Store(l.b, d.list, l.fn.LdM(l.b, ir.Ptr, rec(), l.fn.Const(l.b, 0, pos), pos), pos)
	was := l.spill(l.fn.GLoad(l.b, ps.unwinding, pos), pos)
	l.fn.GStore(l.b, ps.unwinding, l.fn.BoolConst(l.b, false, pos), pos)
	code := l.fn.LdM(l.b, ir.Ptr, rec(), l.fn.Const(l.b, 8, pos), pos)
	if ps.recovers {
		l.fn.GStore(l.b, ps.rframe, was(), pos)
	}
	ps.noCheck[l.fn.CallInd(l.b, &ir.Sig{Params: []ir.Type{ir.Ptr}}, code, []*ir.Instr{rec()}, pos)] = true
	if ps.recovers {
		l.fn.GStore(l.b, ps.rframe, l.fn.BoolConst(l.b, false, pos), pos)
	}
	set := l.fn.NewBlock()
	l.fn.Branch(l.b, was(), set, d.exit, pos)
	l.b = set
	if ps.recovers {
		// Recovered: the panics raised since entry are dropped, and the
		// function goes on as if returning normally.
		p := l.fn.GLoad(l.b, ps.panics, pos)
		still := l.when(l.fn.LdM(l.b, ir.Bool, p, l.fn.Const(l.b, recRecov, pos), pos), pos)
		l.fn.GStore(l.b, ps.panics, l.fn.Load(l.b, d.entry, pos), pos)
		l.fn.Jump(l.b, d.exit, pos)
		l.b = still
	}
	l.fn.GStore(l.b, ps.unwinding, l.fn.BoolConst(l.b, true, pos), pos)
	l.fn.Jump(l.b, d.exit, pos)
	l.b = done
	if l.fn.Name == "main" {
		end, ret := l.fn.NewBlock(), l.fn.NewBlock()
		l.fn.Branch(l.b, l.fn.GLoad(l.b, ps.unwinding, pos), end, ret, pos)
		l.b = end
		l.endProgram(pos)
		l.b = ret
	}
	if l.named != nil {
		// The values of the result variables, which the deferred calls
		// may have set (D-044).
		l.dfr = nil
		l.retValues(nil, l.namedValues(pos), pos)
		return
	}
	var rs []*ir.Instr
	for _, s := range d.results {
		rs = append(rs, l.fn.Load(l.b, s, pos))
	}
	l.fn.Ret(l.b, pos, rs...)
}

// endProgram ends the current block: it writes the panics and ends the
// program with status 2.
func (l *lowerer) endProgram(pos ir.Pos) {
	ps := l.panics()
	ps.noCheck[l.fn.Call(l.b, ps.printpanics, nil, pos)] = true
	ps.final[l.fn.Panic(l.b, ir.PanicExit, pos)] = true
}

// deferStmt lowers a defer statement: its call's record (callRecord) is
// pushed on the function's list.
func (l *lowerer) deferStmt(s *ast.DeferStmt) {
	pos := l.pos(s, s.Defer)
	if l.dfr == nil {
		l.fail(s, "unexpected defer statement")
	}
	ps := l.panics()
	ps.defers++
	rec, _ := l.callRecord(s, s.Call, fmt.Sprintf("defer%d", ps.defers), pos)
	l.fn.StM(l.b, rec(), l.fn.Const(l.b, 0, pos), l.fn.Load(l.b, l.dfr.list, pos), pos)
	l.fn.Store(l.b, l.dfr.list, rec(), pos)
}

// callRecord evaluates the function value of call, unless it is a call of
// a builtin (print, println, close, delete) or of a variadic function of
// the program, and
// its arguments, as a defer or go statement s does, and copies them into a
// new record: the link (left zero), the code address of a function named
// name, generated for s, which makes the call from the record, then the
// func value and the arguments, a variadic function's last one the slice
// of its elements. It returns the record and the func value, or nil.
func (l *lowerer) callRecord(s ast.Node, call *ast.CallExpr, name string, pos ir.Pos) (rec, fv func() *ir.Instr) {
	builtin := ""
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if b, ok := l.pkg.Info.Uses[id].(*types.Builtin); ok {
			builtin = b.Name()
		}
	}
	var sig *types.Signature
	var direct *ir.Func // a variadic function, called directly
	var ts []types.Type // the Go type of each argument; nil for print's
	n := len(call.Args)
	switch builtin {
	case "print", "println":
		if n == 1 {
			if t, ok := l.pkg.Info.Types[call.Args[0]].Type.(*types.Tuple); ok {
				n = t.Len()
			}
		}
		ts = make([]types.Type, n)
	case "close":
		ts = []types.Type{l.pkg.Info.Types[call.Args[0]].Type}
	case "delete":
		mt := l.pkg.Info.Types[call.Args[0]].Type
		ts = []types.Type{mt, l.mapType(mt).key}
	case "":
		sig = l.pkg.Info.Types[call.Fun].Type.Underlying().(*types.Signature)
		if sig.Variadic() {
			// A variadic function of the program is called directly; a
			// method or func value as any other.
			if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
				if obj, ok := l.pkg.Info.Uses[id].(*types.Func); ok {
					direct = l.funcs[obj]
				}
			}
		}
		if direct == nil {
			fv = l.spill(l.expr(call.Fun), pos)
		}
		for v := range sig.Params().Variables() {
			ts = append(ts, v.Type())
		}
	default:
		l.fail(s, "call of builtin %s in a defer or go statement", builtin)
	}
	if call.Ellipsis.IsValid() && (sig == nil || !sig.Variadic()) {
		l.fail(s, "unexpected variadic call")
	}
	var vals [][]*ir.Instr
	if sig != nil && sig.Variadic() {
		// The elements after the fixed arguments make the slice now, as
		// every argument is evaluated now.
		vals = l.variadicArgs(call, ts)
	} else {
		vals = l.valuesOrResults(call.Args, ts)
	}
	// The layout after the link, the code, and the func value: 8 bytes
	// for each word, or an array's or struct's size rounded up to 8.
	type field struct {
		off int64
		t   types.Type // an array or struct, or nil
		w   ir.Type
	}
	off := int64(24)
	var fields []field
	var gets []func() []*ir.Instr
	for i, w := range vals {
		gets = append(gets, l.spillWords(w, pos))
		if ts[i] != nil && isAggregate(ts[i]) {
			fields = append(fields, field{off, ts[i], ir.Ptr})
			off += (sizes.Sizeof(ts[i]) + 7) &^ 7
			continue
		}
		for _, x := range w {
			fields = append(fields, field{off, nil, x.Type})
			off += 8
		}
	}
	code := l.genFunc(name, []ir.Type{ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
		var args []*ir.Instr
		for _, f := range fields {
			if f.t != nil {
				args = append(args, l.fn.PAdd(l.b, p[0](), l.fn.Const(l.b, f.off, pos), pos))
			} else {
				args = append(args, l.fn.LdM(l.b, f.w, p[0](), l.fn.Const(l.b, f.off, pos), pos))
			}
		}
		switch builtin {
		case "print":
			l.fn.Print(l.b, args, pos)
		case "println":
			l.fn.Println(l.b, args, pos)
		case "close":
			l.fn.Call(l.b, l.chanFunc("closechan"), args, pos)
		case "delete":
			l.fn.Call(l.b, l.mapFunc(l.mapType(ts[0]), mapDelete, pos), args, pos)
		default:
			if direct != nil {
				l.directCall(sig, direct, args, pos)
				break
			}
			l.deferredCall(sig, l.fn.LdM(l.b, ir.Ptr, p[0](), l.fn.Const(l.b, 16, pos), pos), args, pos)
		}
		l.fn.Ret(l.b, pos)
	})
	rec = l.keep(l.fn.Alloc(l.b, l.fn.Const(l.b, off, pos), pos), pos)
	if fv != nil {
		l.fn.StM(l.b, rec(), l.fn.Const(l.b, 16, pos), fv(), pos)
	}
	k := 0
	for i := range vals {
		w := gets[i]()
		if ts[i] != nil && isAggregate(ts[i]) {
			l.copyPlace(place{base: rec, c: fields[k].off, t: ts[i]}, l.valuePlace(w[0], ts[i], pos), pos)
			k++
			continue
		}
		for _, x := range w {
			l.fn.StM(l.b, rec(), l.fn.Const(l.b, fields[k].off, pos), x, pos)
			k++
		}
	}
	l.fn.StM(l.b, rec(), l.fn.Const(l.b, 8, pos), l.fn.FuncAddr(l.b, code, pos), pos)
	return rec, fv
}

// directCall calls f, a function of type sig, with args, its results
// discarded, as deferredCall calls a func value.
func (l *lowerer) directCall(sig *types.Signature, f *ir.Func, args []*ir.Instr, pos ir.Pos) {
	for r := range sig.Results().Variables() {
		if isAggregate(r.Type()) {
			args = append(args, l.addr(l.newMem("tmp", r.Type(), pos), pos))
		}
	}
	l.fn.Call(l.b, f, args, pos)
}

// deferredCall calls the func value fv of type sig with args, as a call of
// it does after evaluating them: its nil check, then the call.
func (l *lowerer) deferredCall(sig *types.Signature, fv *ir.Instr, args []*ir.Instr, pos ir.Pos) {
	out := l.guard(l.fn.Compare(l.b, ir.Eq, fv, l.nilPtr(pos), pos), ir.PanicNil, pos, append([]*ir.Instr{fv}, args...)...)
	fv, args = out[0], out[1:]
	s := &ir.Sig{Params: []ir.Type{ir.Ptr}}
	for _, a := range args {
		s.Params = append(s.Params, a.Type)
	}
	for r := range sig.Results().Variables() {
		if isAggregate(r.Type()) {
			args = append(args, l.addr(l.newMem("tmp", r.Type(), pos), pos))
			s.Params = append(s.Params, ir.Ptr)
			continue
		}
		s.Results = append(s.Results, l.words(nil, r.Type())...)
	}
	code := l.fn.LdM(l.b, ir.Ptr, fv, l.fn.Const(l.b, 0, pos), pos)
	l.fn.CallInd(l.b, s, code, append([]*ir.Instr{fv}, args...), pos)
}
