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

// The fields of a waiter: for one of a select statement's cases, its
// select record and the case's index (increment 10.3).
const (
	wG    = 0
	wElem = 8
	wNext = 16
	wOk   = 24
	wSel  = 32
	wCase = 40
	wSize = 48
)

// The fields of a select record, shared by a blocked select's waiters:
// whether one of them has completed, and which.
const (
	selDone = 0
	selWin  = 8
	selSize = 16
)

// The fields of a case of .selectgo's array: the channel, the element's
// address, whether it is a send, and its waiter while the select blocks.
const (
	scC    = 0
	scElem = 8
	scSend = 16
	scW    = 24
	scSize = 32
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
	ngo                               *ir.Global // the goroutines besides main, if counted
	getg, ready, next, newproc, entry *ir.Func
	park                              *ir.Func
	fns                               map[string]*ir.Func
	gos                               int // the go statements lowered so far
}

func (l *lowerer) sched() *sched {
	if l.sch != nil {
		return l.sch
	}
	f0 := l.mainFile
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
	if l.countG {
		sc.ngo = l.prog.NewGlobal(".ngo", ir.I64, file, pos)
	}
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
		l.runqPut(old, pos)
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
		if sc.ngo != nil {
			l.fn.GStore(l.b, sc.ngo, m.bin(ir.Sub, l.fn.GLoad(l.b, sc.ngo, pos), m.c(1)), pos)
		}
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
		if sc.ngo != nil {
			l.fn.GStore(l.b, sc.ngo, m.bin(ir.Add, l.fn.GLoad(l.b, sc.ngo, pos), m.c(1)), pos)
		}
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

// runqPut appends the G g to the tail of the run queue.
func (l *lowerer) runqPut(g mvar, pos ir.Pos) {
	m, sc := mb{l, pos}, l.sch
	m.st(g.get(), gLink, m.nilPtr())
	tail := m.v("tail", l.fn.GLoad(l.b, sc.runqTail, pos))
	after := m.when(m.cmp(ir.Eq, tail.get(), m.nilPtr()))
	l.fn.GStore(l.b, sc.runqHead, g.get(), pos)
	join := l.fn.NewBlock()
	m.join(join)
	l.b = after
	m.st(tail.get(), gLink, g.get())
	m.join(join)
	l.fn.GStore(l.b, sc.runqTail, g.get(), pos)
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
			w := l.dequeueLive(p[0], chRecvq, pos)
			none := m.when(m.cmp(ir.Ne, w(), m.nilPtr()))
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
			w = l.wait(p[0], p[1], chSendq, pos)
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
			w := l.dequeueLive(p[0], chSendq, pos)
			none := m.when(m.cmp(ir.Ne, w(), m.nilPtr()))
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
			w = l.wait(p[0], p[1], chRecvq, pos)
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
			for _, q := range []int64{chRecvq, chSendq} {
				head := l.fn.NewBlock()
				m.join(head)
				w := l.dequeueLive(p[0], q, pos)
				done := m.when(m.cmp(ir.Ne, w(), m.nilPtr()))
				if q == chRecvq {
					l.fn.Call(l.b, l.chanFunc("memclr"), []*ir.Instr{m.ld(ir.Ptr, w(), wElem), m.ld(ir.I64, p[0](), chEsz)}, pos)
				}
				m.st(w(), wNext, stack.get())
				stack.set(w())
				l.fn.Jump(l.b, head, pos)
				l.b = done
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
	case "dequeue":
		// .dequeue(c, q) removes the first waiter of the queue of c at
		// offset q that can still complete and returns it, or nil. It
		// drops the waiters of selects another case has completed, and
		// marks a select's waiter it returns as the one that completes.
		f = l.genFunc(name, []ir.Type{ir.Ptr, ir.I64}, []ir.Type{ir.Ptr}, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			head := l.fn.NewBlock()
			m.join(head)
			w := m.v("w", l.fn.LdM(l.b, ir.Ptr, p[0](), p[1](), pos))
			empty := m.when(m.cmp(ir.Ne, w.get(), m.nilPtr()))
			l.unlinkFirst(p[0], p[1], w.get, pos)
			sel := m.v("sel", m.ld(ir.Ptr, w.get(), wSel))
			plain := m.when(m.cmp(ir.Ne, sel.get(), m.nilPtr()))
			fresh := m.when(m.ld(ir.Bool, sel.get(), selDone))
			l.fn.Jump(l.b, head, pos)
			l.b = fresh
			m.st(sel.get(), selDone, l.fn.BoolConst(l.b, true, pos))
			m.st(sel.get(), selWin, w.get())
			m.join(plain)
			l.fn.Ret(l.b, pos, w.get())
			l.b = empty
			l.fn.Ret(l.b, pos, m.nilPtr())
		})
	case "peek":
		// .peek(c, q) returns the first waiter of the queue of c at offset
		// q that can still complete, or nil, dropping those before it.
		f = l.genFunc(name, []ir.Type{ir.Ptr, ir.I64}, []ir.Type{ir.Ptr}, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			head := l.fn.NewBlock()
			m.join(head)
			w := m.v("w", l.fn.LdM(l.b, ir.Ptr, p[0](), p[1](), pos))
			empty := m.when(m.cmp(ir.Ne, w.get(), m.nilPtr()))
			sel := m.v("sel", m.ld(ir.Ptr, w.get(), wSel))
			live := l.fn.NewBlock()
			plain := m.when(m.cmp(ir.Ne, sel.get(), m.nilPtr()))
			fresh := m.when(m.ld(ir.Bool, sel.get(), selDone))
			l.unlinkFirst(p[0], p[1], w.get, pos)
			l.fn.Jump(l.b, head, pos)
			l.b = fresh
			m.join(live)
			l.b = plain
			m.join(live)
			l.fn.Ret(l.b, pos, w.get())
			l.b = empty
			l.fn.Ret(l.b, pos, m.nilPtr())
		})
	case "unqueue":
		// .unqueue(c, q, w) removes the waiter w from the queue of c at
		// offset q, if it is there.
		f = l.genFunc(name, []ir.Type{ir.Ptr, ir.I64, ir.Ptr}, nil, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			prev := m.v("prev", m.nilPtr())
			cur := m.v("cur", l.fn.LdM(l.b, ir.Ptr, p[0](), p[1](), pos))
			head := l.fn.NewBlock()
			m.join(head)
			more := m.when(m.cmp(ir.Eq, cur.get(), m.nilPtr()))
			l.fn.Ret(l.b, pos)
			l.b = more
			other := m.when(m.cmp(ir.Eq, cur.get(), p[2]()))
			nx := m.v("next", m.ld(ir.Ptr, cur.get(), wNext))
			inner := m.when(m.cmp(ir.Eq, prev.get(), m.nilPtr()))
			l.fn.StM(l.b, p[0](), p[1](), nx.get(), pos)
			linked := l.fn.NewBlock()
			m.join(linked)
			l.b = inner
			m.st(prev.get(), wNext, nx.get())
			m.join(linked)
			tail := func() *ir.Instr { return m.bin(ir.Add, p[1](), m.c(8)) }
			keep := m.when(m.cmp(ir.Eq, l.fn.LdM(l.b, ir.Ptr, p[0](), tail(), pos), p[2]()))
			l.fn.StM(l.b, p[0](), tail(), prev.get(), pos)
			m.join(keep)
			l.fn.Ret(l.b, pos)
			l.b = other
			prev.set(cur.get())
			cur.set(m.ld(ir.Ptr, cur.get(), wNext))
			l.fn.Jump(l.b, head, pos)
		})
	case "rand":
		// .rand() returns the next number of a xorshift generator of
		// fixed seed, the same in every run (D-048).
		g := l.prog.NewGlobal(".randstate", ir.U64, l.prog.Funcs[0].File, l.prog.Funcs[0].Pos)
		f = l.genFunc(name, nil, []ir.Type{ir.U64}, func(_ []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			u := func(v int64) *ir.Instr { return l.fn.TypedConst(l.b, ir.U64, v, pos) }
			x := m.v("x", l.fn.GLoad(l.b, g, pos))
			seeded := m.when(m.cmp(ir.Eq, x.get(), u(0)))
			x.set(u(0x2545f4914f6cdd1d))
			m.join(seeded)
			x.set(m.bin(ir.Xor, x.get(), m.bin(ir.Shl, x.get(), u(13))))
			x.set(m.bin(ir.Xor, x.get(), m.bin(ir.Shr, x.get(), u(7))))
			x.set(m.bin(ir.Xor, x.get(), m.bin(ir.Shl, x.get(), u(17))))
			l.fn.GStore(l.b, g, x.get(), pos)
			l.fn.Ret(l.b, pos, x.get())
		})
	case "caseready":
		// .caseready(e) reports whether the case at e of .selectgo's array can
		// proceed at once: a send on a closed channel can, and panics.
		f = l.genFunc(name, []ir.Type{ir.Ptr}, []ir.Type{ir.Bool}, func(p []func() *ir.Instr, pos ir.Pos) {
			m := mb{l, pos}
			yes := func() { l.fn.Ret(l.b, pos, l.fn.BoolConst(l.b, true, pos)) }
			c := m.v("c", m.ld(ir.Ptr, p[0](), scC))
			nonNil := m.when(m.cmp(ir.Eq, c.get(), m.nilPtr()))
			l.fn.Ret(l.b, pos, l.fn.BoolConst(l.b, false, pos))
			l.b = nonNil
			recv := m.when(m.ld(ir.Bool, p[0](), scSend))
			open := m.when(m.ld(ir.Bool, c.get(), chClosed))
			yes()
			l.b = open
			none := m.when(m.cmp(ir.Ne, l.fn.Call(l.b, l.chanFunc("peek"), []*ir.Instr{c.get(), m.c(chRecvq)}, pos), m.nilPtr()))
			yes()
			l.b = none
			l.fn.Ret(l.b, pos, m.cmp(ir.Lt, m.ld(ir.I64, c.get(), chCount), m.ld(ir.I64, c.get(), chCap)))
			l.b = recv
			none = m.when(m.cmp(ir.Ne, l.fn.Call(l.b, l.chanFunc("peek"), []*ir.Instr{c.get(), m.c(chSendq)}, pos), m.nilPtr()))
			yes()
			l.b = none
			empty := m.when(m.cmp(ir.Gt, m.ld(ir.I64, c.get(), chCount), m.c(0)))
			yes()
			l.b = empty
			l.fn.Ret(l.b, pos, m.ld(ir.Bool, c.get(), chClosed))
		})
	case "selectgo":
		// .selectgo(cases, n, block) runs a select of the n cases of the
		// array at cases (sc* above): one of the ready cases, chosen at
		// random, or with none ready, if not block, none (-1), or else
		// waits on every case with a channel until one completes. It
		// returns the case's index and, for a receive, whether a value was
		// sent.
		f = l.genFunc(name, []ir.Type{ir.Ptr, ir.I64, ir.Bool}, []ir.Type{ir.I64, ir.Bool}, l.buildSelectgo)
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

// unlinkFirst removes the first waiter, w, of the queue of c at offset q.
func (l *lowerer) unlinkFirst(c, q, w func() *ir.Instr, pos ir.Pos) {
	m := mb{l, pos}
	nx := l.keep(m.ld(ir.Ptr, w(), wNext), pos)
	l.fn.StM(l.b, c(), q(), nx(), pos)
	last := m.when(m.cmp(ir.Eq, nx(), m.nilPtr()))
	l.fn.StM(l.b, c(), m.bin(ir.Add, q(), m.c(8)), m.nilPtr(), pos)
	m.join(last)
}

// buildSelectgo builds .selectgo (chanFunc).
func (l *lowerer) buildSelectgo(p []func() *ir.Instr, pos ir.Pos) {
	m := mb{l, pos}
	ent := func(i *ir.Instr) *ir.Instr { return l.fn.PAdd(l.b, p[0](), m.bin(ir.Mul, i, m.c(scSize)), pos) }
	ready := func(i *ir.Instr) *ir.Instr { return l.fn.Call(l.b, l.chanFunc("caseready"), []*ir.Instr{ent(i)}, pos) }
	// queue is the offset of the queue a case's waiter goes on.
	queue := func(e func() *ir.Instr) *ir.Instr {
		q := m.v("q", m.c(chRecvq))
		recv := m.when(m.ld(ir.Bool, e(), scSend))
		q.set(m.c(chSendq))
		m.join(recv)
		return q.get()
	}
	i, k := m.v("i", m.c(0)), m.v("k", m.c(0))
	m.loop(func() *ir.Instr { return m.cmp(ir.Lt, i.get(), p[1]()) }, func() {
		not := m.when(ready(i.get()))
		k.set(m.bin(ir.Add, k.get(), m.c(1)))
		m.join(not)
		i.set(m.bin(ir.Add, i.get(), m.c(1)))
	})
	// The j-th ready case, j uniform below k.
	none := m.when(m.cmp(ir.Gt, k.get(), m.c(0)))
	r := l.fn.Call(l.b, l.chanFunc("rand"), nil, pos)
	j := m.v("j", l.fn.Conv(l.b, ir.I64, m.bin(ir.Rem, r, l.fn.Conv(l.b, ir.U64, k.get(), pos)), pos))
	i.set(m.c(0))
	head := l.fn.NewBlock()
	m.join(head)
	next := l.fn.NewBlock()
	not := m.when(ready(i.get()))
	later := m.when(m.cmp(ir.Eq, j.get(), m.c(0)))
	e := l.keep(ent(i.get()), pos)
	recv := m.when(m.ld(ir.Bool, e(), scSend))
	l.fn.Call(l.b, l.chanFunc("chansend"), []*ir.Instr{m.ld(ir.Ptr, e(), scC), m.ld(ir.Ptr, e(), scElem)}, pos)
	l.fn.Ret(l.b, pos, i.get(), l.fn.BoolConst(l.b, true, pos))
	l.b = recv
	ok := l.fn.Call(l.b, l.chanFunc("chanrecv"), []*ir.Instr{m.ld(ir.Ptr, e(), scC), m.ld(ir.Ptr, e(), scElem)}, pos)
	l.fn.Ret(l.b, pos, i.get(), ok)
	l.b = later
	j.set(m.bin(ir.Sub, j.get(), m.c(1)))
	m.join(next)
	l.b = not
	m.join(next)
	i.set(m.bin(ir.Add, i.get(), m.c(1)))
	l.fn.Jump(l.b, head, pos)
	l.b = none
	blocks := m.when(l.fn.Not(l.b, p[2](), pos))
	l.fn.Ret(l.b, pos, m.c(-1), l.fn.BoolConst(l.b, false, pos))
	l.b = blocks
	// Wait on every case with a channel.
	sel := m.v("sel", l.fn.Alloc(l.b, m.c(selSize), pos))
	g := m.v("g", l.fn.Call(l.b, l.sched().getg, nil, pos))
	i.set(m.c(0))
	m.loop(func() *ir.Instr { return m.cmp(ir.Lt, i.get(), p[1]()) }, func() {
		e := l.keep(ent(i.get()), pos)
		m.st(e(), scW, m.nilPtr())
		c := l.keep(m.ld(ir.Ptr, e(), scC), pos)
		isNil := m.when(m.cmp(ir.Ne, c(), m.nilPtr()))
		w := m.v("w", l.fn.Alloc(l.b, m.c(wSize), pos))
		m.st(w.get(), wG, g.get())
		m.st(w.get(), wElem, m.ld(ir.Ptr, e(), scElem))
		m.st(w.get(), wSel, sel.get())
		m.st(w.get(), wCase, i.get())
		m.st(e(), scW, w.get())
		q := m.v("qoff", queue(e))
		l.enqueue(c, q.get, w.get, pos)
		m.join(isNil)
		i.set(m.bin(ir.Add, i.get(), m.c(1)))
	})
	l.fn.Call(l.b, l.sched().park, nil, pos)
	// Woken by the case that completed: the others' waiters leave their
	// queues.
	win := m.v("win", m.ld(ir.Ptr, sel.get(), selWin))
	i.set(m.c(0))
	m.loop(func() *ir.Instr { return m.cmp(ir.Lt, i.get(), p[1]()) }, func() {
		e := l.keep(ent(i.get()), pos)
		w := l.keep(m.ld(ir.Ptr, e(), scW), pos)
		skip := m.when(m.cmp(ir.Ne, w(), m.nilPtr()))
		self := m.when(m.cmp(ir.Ne, w(), win.get()))
		q := m.v("qoff", queue(e))
		l.fn.Call(l.b, l.chanFunc("unqueue"), []*ir.Instr{m.ld(ir.Ptr, e(), scC), q.get(), w()}, pos)
		m.join(self)
		m.join(skip)
		i.set(m.bin(ir.Add, i.get(), m.c(1)))
	})
	idx := m.v("idx", m.ld(ir.I64, win.get(), wCase))
	okv := m.v("ok", m.ld(ir.Bool, win.get(), wOk))
	sent := m.when(m.ld(ir.Bool, ent(idx.get()), scSend))
	closed := m.when(l.fn.Not(l.b, okv.get(), pos))
	l.panicMsg([][]*ir.Instr{l.strWords("send on closed channel", pos)}, pos)
	l.b = closed
	m.join(sent)
	l.fn.Ret(l.b, pos, idx.get(), okv.get())
}

// dequeueLive removes from the queue of c at the offset q the first
// waiter that can still complete, claiming it if it is a select's, and
// returns it, or nil.
func (l *lowerer) dequeueLive(c func() *ir.Instr, q int64, pos ir.Pos) func() *ir.Instr {
	return l.keep(l.fn.Call(l.b, l.chanFunc("dequeue"), []*ir.Instr{c(), l.fn.Const(l.b, q, pos)}, pos), pos)
}

// wake marks the waiter w's operation completed and readies its
// goroutine.
func (l *lowerer) wake(w func() *ir.Instr, pos ir.Pos) {
	m := mb{l, pos}
	m.st(w(), wOk, l.fn.BoolConst(l.b, true, pos))
	l.fn.Call(l.b, l.sched().ready, []*ir.Instr{m.ld(ir.Ptr, w(), wG)}, pos)
}

// wait queues a waiter for the running goroutine's element at elem on the
// queue of c at the offset q, parks until it is woken, and returns the
// waiter.
func (l *lowerer) wait(c, elem func() *ir.Instr, q int64, pos ir.Pos) func() *ir.Instr {
	m := mb{l, pos}
	w := m.v("w", l.fn.Alloc(l.b, m.c(wSize), pos))
	m.st(w.get(), wG, l.fn.Call(l.b, l.sched().getg, nil, pos))
	m.st(w.get(), wElem, elem())
	l.enqueue(c, func() *ir.Instr { return m.c(q) }, w.get, pos)
	l.fn.Call(l.b, l.sched().park, nil, pos)
	return w.get
}

// enqueue appends the waiter w to the queue of c at the offset q, whose
// tail is at q+8.
func (l *lowerer) enqueue(c, q, w func() *ir.Instr, pos ir.Pos) {
	m := mb{l, pos}
	tail := func() *ir.Instr { return m.bin(ir.Add, q(), m.c(8)) }
	t := l.keep(l.fn.LdM(l.b, ir.Ptr, c(), tail(), pos), pos)
	first := m.when(m.cmp(ir.Ne, t(), m.nilPtr()))
	m.st(t(), wNext, w())
	join := l.fn.NewBlock()
	m.join(join)
	l.b = first
	l.fn.StM(l.b, c(), q(), w(), pos)
	m.join(join)
	l.fn.StM(l.b, c(), tail(), w(), pos)
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
			get := l.spillWords(l.assignTo(w, t, s.Key, xpos), xpos)
			v := l.lvalue(s.Key)
			l.store(v, get(), xpos)
		}
	}
	lp := &target{next: head, exit: exit, stmt: s}
	l.targets = append(l.targets, lp)
	l.stmts(s.Body.List)
	if !l.done {
		l.fn.Jump(l.b, head, l.pos(s.Body, s.Body.Rbrace))
	}
	l.targets = l.targets[:len(l.targets)-1]
	l.b, l.done = exit, false
}

// selectStmt lowers a select statement (increment 10.3): the channel
// operands and the values to send, once, in source order, into the
// array of .selectgo's cases, each value written whole into a frame
// object and each received element into one; then .selectgo, and the
// chosen case's assignments and statements. select {} parks for good.
func (l *lowerer) selectStmt(s *ast.SelectStmt) {
	pos := l.pos(s, s.Select)
	type cas struct {
		cc   *ast.CommClause
		send bool
		t    types.Type
		c    func() *ir.Instr
		tmp  *ir.Mem
	}
	var cases []cas
	var dflt *ast.CommClause
	for _, st := range s.Body.List {
		cc := st.(*ast.CommClause)
		var ch ast.Expr
		switch comm := cc.Comm.(type) {
		case nil:
			dflt = cc
			continue
		case *ast.SendStmt:
			ch = comm.Chan
		case *ast.ExprStmt:
			ch = ast.Unparen(comm.X).(*ast.UnaryExpr).X
		case *ast.AssignStmt:
			ch = ast.Unparen(comm.Rhs[0]).(*ast.UnaryExpr).X
		default:
			l.fail(cc, "unexpected select case %T", comm)
		}
		k := cas{cc: cc, t: chanElem(l.pkg.Info.Types[ch].Type)}
		cpos := l.pos(cc, cc.Case)
		k.c = l.spill(l.expr(ch), cpos)
		k.tmp = l.newMem("case", k.t, cpos)
		if send, ok := cc.Comm.(*ast.SendStmt); ok {
			k.send = true
			v := l.spillWords(l.valueFor(send.Value, k.t), cpos)
			if isAggregate(k.t) {
				l.fn.Call(l.b, l.chanFunc("memclr"), []*ir.Instr{l.addr(k.tmp, cpos), l.fn.Const(l.b, k.tmp.Size, cpos)}, cpos)
			}
			l.storePlace(l.memPlace(k.tmp, k.t, cpos), v(), cpos)
		}
		cases = append(cases, k)
	}
	exit := l.fn.NewBlock()
	bodies := make([]*ir.Block, len(cases))
	var dfltBody *ir.Block
	if dflt != nil {
		dfltBody = l.fn.NewBlock()
	}
	var idx, ok func() *ir.Instr
	switch {
	case len(cases) == 0 && dflt != nil:
		l.fn.Jump(l.b, dfltBody, pos)
	default:
		arr := l.fn.NewMem("cases", int64(len(cases))*scSize, 8, pos)
		for i, k := range cases {
			at := func(off int64) *ir.Instr { return l.fn.Const(l.b, int64(i)*scSize+off, pos) }
			l.fn.StM(l.b, l.addr(arr, pos), at(scC), k.c(), pos)
			l.fn.StM(l.b, l.addr(arr, pos), at(scElem), l.addr(k.tmp, pos), pos)
			l.fn.StM(l.b, l.addr(arr, pos), at(scSend), l.fn.BoolConst(l.b, k.send, pos), pos)
		}
		base := l.fn.TypedConst(l.b, ir.Ptr, 0, pos)
		if len(cases) > 0 {
			base = l.addr(arr, pos)
		}
		r := l.fn.Call(l.b, l.chanFunc("selectgo"), []*ir.Instr{base, l.fn.Const(l.b, int64(len(cases)), pos), l.fn.BoolConst(l.b, dflt == nil, pos)}, pos)
		idx = l.spill(l.fn.Extract(l.b, r, 0, pos), pos)
		ok = l.spill(l.fn.Extract(l.b, r, 1, pos), pos)
		for i := range cases {
			bodies[i] = l.fn.NewBlock()
			next := l.fn.NewBlock()
			l.fn.Branch(l.b, l.fn.Compare(l.b, ir.Eq, idx(), l.fn.Const(l.b, int64(i), pos), pos), bodies[i], next, pos)
			l.b = next
		}
		if dflt != nil {
			l.fn.Jump(l.b, dfltBody, pos)
		} else {
			// No case completed: select {} never resumes.
			l.fn.Panic(l.b, ir.PanicFatal, pos, l.strWords("aether: select resumed without a case", pos)...)
		}
	}
	sw := &target{exit: exit, stmt: s}
	l.targets = append(l.targets, sw)
	done := true
	body := func(b *ir.Block, cc *ast.CommClause, assign func()) {
		l.b, l.done = b, false
		if assign != nil {
			assign()
		}
		l.stmts(cc.Body)
		if !l.done {
			l.fn.Jump(l.b, exit, l.pos(cc, cc.End()))
		}
		done = done && l.done
	}
	for i, k := range cases {
		var assign func()
		if as, isAssign := k.cc.Comm.(*ast.AssignStmt); isAssign {
			assign = func() { l.selectAssign(as, l.memPlace(k.tmp, k.t, l.pos(as, as.Pos())), ok) }
		}
		body(bodies[i], k.cc, assign)
	}
	if dflt != nil {
		body(dfltBody, dflt, nil)
	}
	l.targets = l.targets[:len(l.targets)-1]
	l.b, l.done = exit, done && !sw.broken
}

// selectAssign declares or assigns the variables of a select case's
// receive, as a range over a map does its key and element: the element at
// p and whether it was sent, ok; blank ones are skipped.
func (l *lowerer) selectAssign(as *ast.AssignStmt, p place, ok func() *ir.Instr) {
	var targets []ast.Expr
	var vals []func() []*ir.Instr
	for k, x := range as.Lhs {
		if id, isId := x.(*ast.Ident); isId && id.Name == "_" {
			continue
		}
		xpos := l.pos(x, x.Pos())
		var w []*ir.Instr
		from := p.t
		if k == 0 {
			w = l.recvWords(p, x)
		} else {
			w, from = []*ir.Instr{ok()}, types.Typ[types.Bool]
		}
		if as.Tok == token.DEFINE {
			l.store(l.defineVar(l.ident(x)), w, xpos)
			continue
		}
		targets = append(targets, x)
		vals = append(vals, l.spillWords(l.assignTo(w, from, x, xpos), xpos))
	}
	vs := make([]variable, len(targets))
	for k, x := range targets {
		vs[k] = l.lvalue(x)
	}
	for k, x := range targets {
		l.store(vs[k], vals[k](), l.pos(x, x.Pos()))
	}
}
