package lower

// Goroutines and channels (Phase 10 increment 10.2, D-048).
//
// The scheduler is generated IR over the goroutine primitives GoNew,
// GoSwitch, and GoExit. Each goroutine has a G object: its handle, the
// record of its go statement, its run-queue link, and its saved panic
// state. The globals .curg (the running goroutine's G, nil for main until
// it needs one), .runnext, and the FIFO run queue .runqhead and .runqtail
// hold the rest. Following gc's single-P order, a goroutine started by go
// or woken by a channel operation goes to .runnext, and the one there
// before to the queue's tail; one that blocks or ends hands over to
// .runnext, or else to the queue's head, or else the program ends in
// deadlock. .park, the only switch of a running goroutine, saves the panic
// globals of D-043 in its G and restores those of the G it resumes as.
//
// A channel is a heap header (ch* below) with a ring buffer of cap
// elements of esz bytes, and FIFO queues of waiters: a waiter is a heap
// record of a blocked goroutine's G, the address of its element (the
// value to send, or where to receive, perhaps in its frame), a link, and
// whether the operation completed. Elements are copied with Move, so a
// value sent from a frame object is first written there whole, padding
// included.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/stevenstank/aether/internal/ir"
)

// The fields of a G object.
const (
	gHandle = 0
	gRec    = 8
	gLink   = 16
	gPanics = 24
	gUnwind = 32
	gRframe = 33
	gSize   = 40
)

// The fields of a waiter.
const (
	wG    = 0
	wElem = 8
	wNext = 16
	wOk   = 24
	wSize = 32
)

// The fields of a channel's header.
const (
	chEsz       = 0
	chCap       = 8
	chCount     = 16
	chBuf       = 24
	chRecvx     = 32
	chSendx     = 40
	chClosed    = 48
	chRecvq     = 56
	chRecvqTail = 64
	chSendq     = 72
	chSendqTail = 80
	chSize      = 88
)

// maxChanBytes is gc's bound on a channel's buffer: maxAlloc (1<<48 on
// linux/amd64) less its header's size.
const maxChanBytes = 1<<48 - 96

// sched is the program's scheduler and channel functions, each created on
// first use.
type sched struct {
	curg, runnext, runqHead, runqTail *ir.Global
	getg, ready, next, newproc, entry *ir.Func
	park                              *ir.Func
	fns                               map[string]*ir.Func
	gos                               int // the go statements lowered so far
}

func (l *lowerer) sched() *sched {
	if l.sch != nil {
		return l.sch
	}
	f0 := l.pkg.Files[0]
	saved := l.fn
	l.fn = nil
	pos := l.pos(f0, f0.Package)
	l.fn = saved
	file := l.prog.Funcs[0].File
	sc := &sched{fns: map[string]*ir.Func{}}
	l.sch = sc
	sc.curg = l.prog.NewGlobal(".curg", ir.Ptr, file, pos)
	sc.runnext = l.prog.NewGlobal(".runnext", ir.Ptr, file, pos)
	sc.runqHead = l.prog.NewGlobal(".runqhead", ir.Ptr, file, pos)
	sc.runqTail = l.prog.NewGlobal(".runqtail", ir.Ptr, file, pos)
	sc.getg = l.genFunc("getg", nil, []ir.Type{ir.Ptr}, func(_ []func() *ir.Instr, pos ir.Pos) {
		m := mb{l, pos}
		g := m.v("g", l.fn.GLoad(l.b, sc.curg, pos))
		no := m.when(m.cmp(ir.Eq, g.get(), m.nilPtr()))
		g.set(l.fn.Alloc(l.b, m.c(gSize), pos)) // main's: handle 0
		l.fn.GStore(l.b, sc.curg, g.get(), pos)
		m.join(no)
		l.fn.Ret(l.b, pos, g.get())
	})
	sc.ready = l.genFunc("ready", []ir.Type{ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
		m := mb{l, pos}
		old := m.v("old", l.fn.GLoad(l.b, sc.runnext, pos))
		l.fn.GStore(l.b, sc.runnext, p[0](), pos)
		some := m.when(m.cmp(ir.Eq, old.get(), m.nilPtr()))
		l.fn.Ret(l.b, pos)
		l.b = some
		m.st(old.get(), gLink, m.nilPtr())
		tail := m.v("tail", l.fn.GLoad(l.b, sc.runqTail, pos))
		after := m.when(m.cmp(ir.Eq, tail.get(), m.nilPtr()))
		l.fn.GStore(l.b, sc.runqHead, old.get(), pos)
		join := l.fn.NewBlock()
		m.join(join)
		l.b = after
		m.st(tail.get(), gLink, old.get())
		m.join(join)
		l.fn.GStore(l.b, sc.runqTail, old.get(), pos)
		l.fn.Ret(l.b, pos)
	})
	sc.next = l.genFunc("nextg", nil, []ir.Type{ir.Ptr}, func(_ []func() *ir.Instr, pos ir.Pos) {
		m := mb{l, pos}
		g := m.v("g", l.fn.GLoad(l.b, sc.runnext, pos))
		queue := m.when(m.cmp(ir.Ne, g.get(), m.nilPtr()))
		l.fn.GStore(l.b, sc.runnext, m.nilPtr(), pos)
		l.fn.Ret(l.b, pos, g.get())
		l.b = queue
		g.set(l.fn.GLoad(l.b, sc.runqHead, pos))
		dead := m.when(m.cmp(ir.Ne, g.get(), m.nilPtr()))
		nx := m.v("next", m.ld(ir.Ptr, g.get(), gLink))
		l.fn.GStore(l.b, sc.runqHead, nx.get(), pos)
		keep := m.when(m.cmp(ir.Eq, nx.get(), m.nilPtr()))
		l.fn.GStore(l.b, sc.runqTail, m.nilPtr(), pos)
		m.join(keep)
		l.fn.Ret(l.b, pos, g.get())
		l.b = dead
		msg := l.strWords("all goroutines are asleep - deadlock!", pos)
		l.fn.Panic(l.b, ir.PanicFatal, pos, msg[0], msg[1])
	})
	sc.entry = l.genFunc("goentry", nil, nil, func(_ []func() *ir.Instr, pos ir.Pos) {
		m := mb{l, pos}
		g := m.v("g", l.fn.GLoad(l.b, sc.curg, pos))
		if l.unwinds {
			// A new goroutine has no panics (D-043's state).
			l.fn.GStore(l.b, l.ps.panics, m.nilPtr(), pos)
			l.fn.GStore(l.b, l.ps.unwinding, l.fn.BoolConst(l.b, false, pos), pos)
			if l.ps.recovers {
				l.fn.GStore(l.b, l.ps.rframe, l.fn.BoolConst(l.b, false, pos), pos)
			}
		}
		rec := m.v("rec", m.ld(ir.Ptr, g.get(), gRec))
		m.st(g.get(), gRec, m.nilPtr())
		code := m.ld(ir.Ptr, rec.get(), 8)
		l.fn.CallInd(l.b, &ir.Sig{Params: []ir.Type{ir.Ptr}}, code, []*ir.Instr{rec.get()}, pos)
		// A panic leaving the record ends the program (unwind.go exit).
		n := m.v("n", l.fn.Call(l.b, sc.next, nil, pos))
		l.fn.GStore(l.b, sc.curg, n.get(), pos)
		l.fn.GoExit(l.b, m.ld(ir.I64, n.get(), gHandle), pos)
	})
	sc.newproc = l.genFunc("newproc", []ir.Type{ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
		m := mb{l, pos}
		g := m.v("g", l.fn.Alloc(l.b, m.c(gSize), pos))
		m.st(g.get(), gRec, p[0]())
		m.st(g.get(), gHandle, l.fn.GoNew(l.b, sc.entry, pos))
		l.fn.Call(l.b, sc.ready, []*ir.Instr{g.get()}, pos)
		l.fn.Ret(l.b, pos)
	})
	sc.park = l.genFunc("park", nil, nil, func(_ []func() *ir.Instr, pos ir.Pos) {
		m := mb{l, pos}
		g := m.v("g", l.fn.Call(l.b, sc.getg, nil, pos))
		if l.unwinds {
			m.st(g.get(), gPanics, l.fn.GLoad(l.b, l.ps.panics, pos))
			m.st(g.get(), gUnwind, l.fn.GLoad(l.b, l.ps.unwinding, pos))
			if l.ps.recovers {
				m.st(g.get(), gRframe, l.fn.GLoad(l.b, l.ps.rframe, pos))
			}
		}
		n := m.v("n", l.fn.Call(l.b, sc.next, nil, pos))
		l.fn.GStore(l.b, sc.curg, n.get(), pos)
		l.fn.GoSwitch(l.b, m.ld(ir.I64, n.get(), gHandle), pos)
		if l.unwinds {
			// Resumed: .curg is this goroutine's G again.
			g.set(l.fn.GLoad(l.b, sc.curg, pos))
			l.fn.GStore(l.b, l.ps.panics, m.ld(ir.Ptr, g.get(), gPanics), pos)
			l.fn.GStore(l.b, l.ps.unwinding, m.ld(ir.Bool, g.get(), gUnwind), pos)
			if l.ps.recovers {
				l.fn.GStore(l.b, l.ps.rframe, m.ld(ir.Bool, g.get(), gRframe), pos)
			}
		}
		l.fn.Ret(l.b, pos)
	})
	return sc
}

// chanFunc returns the generated channel function name, creating it on
// first use.
func (l *lowerer) chanFunc(name string) *ir.Func {
	sc := l.sched()
	if f := sc.fns[name]; f != nil {
		return f
	}
	var f *ir.Func
	switch name {
	case "memclr":
		// .memclr(p, n) writes n zero bytes at p: words, then bytes.
		f = l.genFunc(name, []ir.Type{ir.Ptr, ir.I64}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			i := m.v("i", m.c(0))
			m.loop(func() *ir.Instr { return m.cmp(ir.Le, m.bin(ir.Add, i.get(), m.c(8)), p[1]()) }, func() {
				l.fn.StM(l.b, p[0](), i.get(), m.c(0), pos)
				i.set(m.bin(ir.Add, i.get(), m.c(8)))
			})
			m.loop(func() *ir.Instr { return m.cmp(ir.Lt, i.get(), p[1]()) }, func() {
				l.fn.StM(l.b, p[0](), i.get(), l.fn.TypedConst(l.b, ir.U8, 0, pos), pos)
				i.set(m.bin(ir.Add, i.get(), m.c(1)))
			})
			l.fn.Ret(l.b, pos)
		})
	case "makechan":
		// .makechan(esz, n, noscan) returns a new channel of n elements of
		// esz bytes, whose buffer is noscan if they hold no pointer.
		f = l.genFunc(name, []ir.Type{ir.I64, ir.I64, ir.Bool}, []ir.Type{ir.Ptr}, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			bad := l.fn.NewBlock()
			ok := m.when(m.cmp(ir.Lt, p[1](), m.c(0)))
			m.join(bad)
			l.panicMsg([][]*ir.Instr{l.strWords("makechan: size out of range", pos)}, pos)
			l.b = ok
			sized := m.when(m.cmp(ir.Gt, p[0](), m.c(0)))
			fits := m.when(m.cmp(ir.Gt, p[1](), m.bin(ir.Div, m.c(maxChanBytes), p[0]())))
			m.join(bad)
			l.b = fits
			m.join(sized)
			hdr := m.v("hdr", l.fn.Alloc(l.b, m.c(chSize), pos))
			m.st(hdr.get(), chEsz, p[0]())
			m.st(hdr.get(), chCap, p[1]())
			scan := m.when(p[2]())
			m.st(hdr.get(), chBuf, l.fn.AllocNoScan(l.b, m.bin(ir.Mul, p[0](), p[1]()), pos))
			done := l.fn.NewBlock()
			m.join(done)
			l.b = scan
			m.st(hdr.get(), chBuf, l.fn.Alloc(l.b, m.bin(ir.Mul, p[0](), p[1]()), pos))
			m.join(done)
			l.fn.Ret(l.b, pos, hdr.get())
		})
	case "chansend":
		// .chansend(c, src) sends the element at src on c.
		f = l.genFunc(name, []ir.Type{ir.Ptr, ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			l.blockOnNil(p[0], pos)
			open := m.when(m.ld(ir.Bool, p[0](), chClosed))
			l.panicMsg([][]*ir.Instr{l.strWords("send on closed channel", pos)}, pos)
			l.b = open
			esz := m.v("esz", m.ld(ir.I64, p[0](), chEsz))
			none := m.when(m.cmp(ir.Ne, m.ld(ir.Ptr, p[0](), chRecvq), m.nilPtr()))
			w := l.dequeue(p[0], chRecvq, chRecvqTail, pos)
			l.fn.Move(l.b, m.ld(ir.Ptr, w(), wElem), p[1](), esz.get(), pos)
			l.wake(w, pos)
			l.fn.Ret(l.b, pos)
			l.b = none
			full := m.when(m.cmp(ir.Lt, m.ld(ir.I64, p[0](), chCount), m.ld(ir.I64, p[0](), chCap)))
			l.fn.Move(l.b, l.chanSlot(p[0], chSendx, esz, pos), p[1](), esz.get(), pos)
			l.advance(p[0], chSendx, pos)
			m.st(p[0](), chCount, m.bin(ir.Add, m.ld(ir.I64, p[0](), chCount), m.c(1)))
			l.fn.Ret(l.b, pos)
			l.b = full
			w = l.wait(p[0], p[1], chSendq, chSendqTail, pos)
			sent := m.when(m.cmp(ir.Eq, m.ld(ir.Bool, w(), wOk), l.fn.BoolConst(l.b, false, pos)))
			l.panicMsg([][]*ir.Instr{l.strWords("send on closed channel", pos)}, pos)
			l.b = sent
			l.fn.Ret(l.b, pos)
		})
	case "chanrecv":
		// .chanrecv(c, dst) receives an element of c at dst, and returns
		// whether it was sent, or else, c being closed, writes zeros.
		f = l.genFunc(name, []ir.Type{ir.Ptr, ir.Ptr}, []ir.Type{ir.Bool}, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			l.blockOnNil(p[0], pos)
			esz := m.v("esz", m.ld(ir.I64, p[0](), chEsz))
			none := m.when(m.cmp(ir.Ne, m.ld(ir.Ptr, p[0](), chSendq), m.nilPtr()))
			w := l.dequeue(p[0], chSendq, chSendqTail, pos)
			unbuf := m.when(m.cmp(ir.Gt, m.ld(ir.I64, p[0](), chCap), m.c(0)))
			// A full buffer: its head is received and the sender's
			// element takes its place at the tail.
			slot := l.keep(l.chanSlot(p[0], chRecvx, esz, pos), pos)
			l.fn.Move(l.b, p[1](), slot(), esz.get(), pos)
			l.fn.Move(l.b, slot(), m.ld(ir.Ptr, w(), wElem), esz.get(), pos)
			l.advance(p[0], chRecvx, pos)
			m.st(p[0](), chSendx, m.ld(ir.I64, p[0](), chRecvx))
			woken := l.fn.NewBlock()
			m.join(woken)
			l.b = unbuf
			l.fn.Move(l.b, p[1](), m.ld(ir.Ptr, w(), wElem), esz.get(), pos)
			m.join(woken)
			l.wake(w, pos)
			l.fn.Ret(l.b, pos, l.fn.BoolConst(l.b, true, pos))
			l.b = none
			empty := m.when(m.cmp(ir.Gt, m.ld(ir.I64, p[0](), chCount), m.c(0)))
			l.fn.Move(l.b, p[1](), l.chanSlot(p[0], chRecvx, esz, pos), esz.get(), pos)
			l.advance(p[0], chRecvx, pos)
			m.st(p[0](), chCount, m.bin(ir.Sub, m.ld(ir.I64, p[0](), chCount), m.c(1)))
			l.fn.Ret(l.b, pos, l.fn.BoolConst(l.b, true, pos))
			l.b = empty
			open := m.when(m.ld(ir.Bool, p[0](), chClosed))
			l.fn.Call(l.b, l.chanFunc("memclr"), []*ir.Instr{p[1](), esz.get()}, pos)
			l.fn.Ret(l.b, pos, l.fn.BoolConst(l.b, false, pos))
			l.b = open
			w = l.wait(p[0], p[1], chRecvq, chRecvqTail, pos)
			l.fn.Ret(l.b, pos, m.ld(ir.Bool, w(), wOk))
		})
	case "closechan":
		// .closechan(c) closes c and wakes every waiter: the receivers
		// with zeros, the senders to panic, in gc's order (its closechan
		// collects them on a stack, then readies them as it pops it).
		f = l.genFunc(name, []ir.Type{ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			nonNil := m.when(m.cmp(ir.Eq, p[0](), m.nilPtr()))
			l.panicMsg([][]*ir.Instr{l.strWords("close of nil channel", pos)}, pos)
			l.b = nonNil
			open := m.when(m.ld(ir.Bool, p[0](), chClosed))
			l.panicMsg([][]*ir.Instr{l.strWords("close of closed channel", pos)}, pos)
			l.b = open
			m.st(p[0](), chClosed, l.fn.BoolConst(l.b, true, pos))
			stack := m.v("stack", m.nilPtr())
			for _, q := range [][2]int64{{chRecvq, chRecvqTail}, {chSendq, chSendqTail}} {
				m.loop(func() *ir.Instr { return m.cmp(ir.Ne, m.ld(ir.Ptr, p[0](), q[0]), m.nilPtr()) }, func() {
					w := l.dequeue(p[0], q[0], q[1], pos)
					if q[0] == chRecvq {
						l.fn.Call(l.b, l.chanFunc("memclr"), []*ir.Instr{m.ld(ir.Ptr, w(), wElem), m.ld(ir.I64, p[0](), chEsz)}, pos)
					}
					m.st(w(), wNext, stack.get())
					stack.set(w())
				})
			}
			m.loop(func() *ir.Instr { return m.cmp(ir.Ne, stack.get(), m.nilPtr()) }, func() {
				w := l.keep(stack.get(), pos)
				stack.set(m.ld(ir.Ptr, w(), wNext))
				l.fn.Call(l.b, l.sched().ready, []*ir.Instr{m.ld(ir.Ptr, w(), wG)}, pos)
			})
			l.fn.Ret(l.b, pos)
		})
	case "chanlen", "chancap":
		// len and cap of a channel: 0 for nil.
		off := int64(chCount)
		if name == "chancap" {
			off = chCap
		}
		f = l.genFunc(name, []ir.Type{ir.Ptr}, []ir.Type{ir.I64}, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			nonNil := m.when(m.cmp(ir.Eq, p[0](), m.nilPtr()))
			l.fn.Ret(l.b, pos, m.c(0))
			l.b = nonNil
			l.fn.Ret(l.b, pos, m.ld(ir.I64, p[0](), off))
		})
	default:
		l.fail(nil, "unknown channel function %s", name)
	}
	sc.fns[name] = f
	return f
}

// blockOnNil blocks forever if the channel c is nil: the goroutine parks
// with nothing to wake it.
func (l *lowerer) blockOnNil(c func() *ir.Instr, pos ir.Pos) {
	m := mb{l, pos}
	nonNil := m.when(m.cmp(ir.Eq, c(), m.nilPtr()))
	l.fn.Call(l.b, l.sched().park, nil, pos)
	l.fn.Panic(l.b, ir.PanicFatal, pos, l.strWords("aether: woken on a nil channel", pos)...)
	l.b = nonNil
}

// dequeue removes the first waiter of the nonempty queue of c at head and
// tail, and returns it.
func (l *lowerer) dequeue(c func() *ir.Instr, head, tail int64, pos ir.Pos) func() *ir.Instr {
	m := mb{l, pos}
	w := l.keep(m.ld(ir.Ptr, c(), head), pos)
	nx := l.keep(m.ld(ir.Ptr, w(), wNext), pos)
	m.st(c(), head, nx())
	last := m.when(m.cmp(ir.Eq, nx(), m.nilPtr()))
	m.st(c(), tail, m.nilPtr())
	m.join(last)
	return w
}

// wake marks the waiter w's operation completed and readies its
// goroutine.
func (l *lowerer) wake(w func() *ir.Instr, pos ir.Pos) {
	m := mb{l, pos}
	m.st(w(), wOk, l.fn.BoolConst(l.b, true, pos))
	l.fn.Call(l.b, l.sched().ready, []*ir.Instr{m.ld(ir.Ptr, w(), wG)}, pos)
}

// wait queues a waiter for the running goroutine's element at elem on the
// queue of c at head and tail, parks until it is woken, and returns the
// waiter.
func (l *lowerer) wait(c, elem func() *ir.Instr, head, tail int64, pos ir.Pos) func() *ir.Instr {
	m := mb{l, pos}
	w := m.v("w", l.fn.Alloc(l.b, m.c(wSize), pos))
	m.st(w.get(), wG, l.fn.Call(l.b, l.sched().getg, nil, pos))
	m.st(w.get(), wElem, elem())
	t := l.keep(m.ld(ir.Ptr, c(), tail), pos)
	first := m.when(m.cmp(ir.Ne, t(), m.nilPtr()))
	m.st(t(), wNext, w.get())
	join := l.fn.NewBlock()
	m.join(join)
	l.b = first
	m.st(c(), head, w.get())
	m.join(join)
	m.st(c(), tail, w.get())
	l.fn.Call(l.b, l.sched().park, nil, pos)
	return w.get
}

// chanSlot returns the address of the buffer element of c at the index
// in the field at off.
func (l *lowerer) chanSlot(c func() *ir.Instr, off int64, esz mvar, pos ir.Pos) *ir.Instr {
	m := mb{l, pos}
	return l.fn.PAdd(l.b, m.ld(ir.Ptr, c(), chBuf), m.bin(ir.Mul, m.ld(ir.I64, c(), off), esz.get()), pos)
}

// advance increments the buffer index of c in the field at off, wrapping
// at the capacity.
func (l *lowerer) advance(c func() *ir.Instr, off int64, pos ir.Pos) {
	m := mb{l, pos}
	i := m.v("i", m.bin(ir.Add, m.ld(ir.I64, c(), off), m.c(1)))
	keep := m.when(m.cmp(ir.Eq, i.get(), m.ld(ir.I64, c(), chCap)))
	i.set(m.c(0))
	m.join(keep)
	m.st(c(), off, i.get())
}

// chanElem returns the element type of the channel type t.
func chanElem(t types.Type) types.Type { return t.Underlying().(*types.Chan).Elem() }

func isChan(t types.Type) bool {
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

// isRecv reports whether e is a receive, <-ch.
func isRecv(e ast.Expr) bool {
	u, ok := ast.Unparen(e).(*ast.UnaryExpr)
	return ok && u.Op == token.ARROW
}

// goStmt lowers a go statement: its call's record (callRecord), then a
// new goroutine that will make the call. gc passes the func value of a
// call with no arguments and no results to the runtime as it is, which
// ends with a fatal error if it is nil; it wraps any other call in a
// closure, whose call of a nil func value panics in the new goroutine, as
// the record's function does.
func (l *lowerer) goStmt(s *ast.GoStmt) {
	pos := l.pos(s, s.Go)
	sc := l.sched()
	sc.gos++
	rec, fv := l.callRecord(s, s.Call, fmt.Sprintf("go%d", sc.gos), pos)
	sig, _ := l.pkg.Info.Types[s.Call.Fun].Type.Underlying().(*types.Signature)
	if fv != nil && sig != nil && sig.Params().Len() == 0 && sig.Results().Len() == 0 {
		no := l.when(l.fn.Compare(l.b, ir.Eq, fv(), l.nilPtr(pos), pos), pos)
		l.fn.Panic(l.b, ir.PanicFatal, pos, l.strWords("go of nil func value", pos)...)
		l.b = no
	}
	l.fn.Call(l.b, sc.newproc, []*ir.Instr{rec()}, pos)
}

// sendStmt lowers ch <- v: the channel, then the value, converted to the
// element type and written whole into a frame object, then .chansend.
func (l *lowerer) sendStmt(s *ast.SendStmt) {
	pos := l.pos(s, s.Arrow)
	t := chanElem(l.pkg.Info.Types[s.Chan].Type)
	c := l.spill(l.expr(s.Chan), pos)
	v := l.spillWords(l.valueFor(s.Value, t), pos)
	tmp := l.newMem("send", t, pos)
	if isAggregate(t) {
		l.fn.Call(l.b, l.chanFunc("memclr"), []*ir.Instr{l.addr(tmp, pos), l.fn.Const(l.b, tmp.Size, pos)}, pos)
	}
	l.storePlace(l.memPlace(tmp, t, pos), v(), pos)
	l.fn.Call(l.b, l.chanFunc("chansend"), []*ir.Instr{c(), l.addr(tmp, pos)}, pos)
}

// recvPlace lowers <-ch: the element received into a frame object, and
// whether it was sent.
func (l *lowerer) recvPlace(e ast.Expr) (place, *ir.Instr) {
	u := ast.Unparen(e).(*ast.UnaryExpr)
	pos := l.pos(u, u.OpPos)
	t := chanElem(l.pkg.Info.Types[u.X].Type)
	c := l.expr(u.X)
	tmp := l.newMem("recv", t, pos)
	ok := l.fn.Call(l.b, l.chanFunc("chanrecv"), []*ir.Instr{c, l.addr(tmp, pos)}, pos)
	return l.memPlace(tmp, t, pos), ok
}

// recvValue lowers <-ch as a value: its words, an array or struct's the
// Ptr of the frame object holding it.
func (l *lowerer) recvValue(e ast.Expr) []*ir.Instr {
	p, _ := l.recvPlace(e)
	return l.recvWords(p, e)
}

// recvOk lowers v, ok := <-ch.
func (l *lowerer) recvOk(e ast.Expr) [][]*ir.Instr {
	p, ok := l.recvPlace(e)
	get := l.spill(ok, l.pos(e, e.Pos()))
	v := l.recvWords(p, e)
	return [][]*ir.Instr{v, {get()}}
}

func (l *lowerer) recvWords(p place, e ast.Expr) []*ir.Instr {
	if isAggregate(p.t) {
		return []*ir.Instr{p.base()}
	}
	return l.loadPlace(p, l.pos(e, e.Pos()))
}

// chanBuiltin lowers make, len, and cap of a channel, reporting whether
// call is one.
func (l *lowerer) chanBuiltin(call *ast.CallExpr) (*ir.Instr, bool) {
	pos := l.pos(call, call.Pos())
	switch {
	case l.isBuiltin(call, "make") && isChan(l.pkg.Info.Types[call].Type):
		t := chanElem(l.pkg.Info.Types[call].Type)
		n := l.fn.Const(l.b, 0, pos)
		if len(call.Args) == 2 {
			n = l.expr(call.Args[1])
			if n.Type != ir.I64 {
				n = l.fn.Conv(l.b, ir.I64, n, pos)
			}
		}
		esz := l.fn.Const(l.b, sizes.Sizeof(t), pos)
		return l.fn.Call(l.b, l.chanFunc("makechan"), []*ir.Instr{esz, n, l.fn.BoolConst(l.b, !hasPointers(t), pos)}, pos), true
	case (l.isBuiltin(call, "len") || l.isBuiltin(call, "cap")) && len(call.Args) == 1 && isChan(l.pkg.Info.Types[call.Args[0]].Type):
		name := "chanlen"
		if l.isBuiltin(call, "cap") {
			name = "chancap"
		}
		return l.fn.Call(l.b, l.chanFunc(name), []*ir.Instr{l.expr(call.Args[0])}, pos), true
	}
	return nil, false
}

// closeCall lowers close(ch).
func (l *lowerer) closeCall(call *ast.CallExpr) {
	pos := l.pos(call, call.Pos())
	l.fn.Call(l.b, l.chanFunc("closechan"), []*ir.Instr{l.expr(call.Args[0])}, pos)
}

// chanRange lowers a range over a channel: receive until it is closed and
// empty, each element assigned or declared as a map range's key.
func (l *lowerer) chanRange(s *ast.RangeStmt) {
	pos := l.pos(s, s.For)
	t := chanElem(l.pkg.Info.Types[s.X].Type)
	c := l.spill(l.expr(s.X), pos)
	head, body, exit := l.fn.NewBlock(), l.fn.NewBlock(), l.fn.NewBlock()
	l.fn.Jump(l.b, head, pos)
	l.b = head
	tmp := l.newMem("recv", t, pos)
	ok := l.fn.Call(l.b, l.chanFunc("chanrecv"), []*ir.Instr{c(), l.addr(tmp, pos)}, pos)
	l.fn.Branch(l.b, ok, body, exit, pos)
	l.b, l.done = body, false
	if id, isId := s.Key.(*ast.Ident); s.Key != nil && !(isId && id.Name == "_") {
		xpos := l.pos(s.Key, s.Key.Pos())
		w := l.recvWords(l.memPlace(tmp, t, xpos), s.Key)
		if s.Tok == token.DEFINE {
			l.store(l.defineVar(l.ident(s.Key)), w, xpos)
		} else {
			get := l.spillWords(w, xpos)
			v := l.lvalue(s.Key)
			l.store(v, get(), xpos)
		}
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
