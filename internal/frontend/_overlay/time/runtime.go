// Aether's part of gc's runtime for package time (D-054): the clocks, and
// the timers of Sleep, Timer, Ticker, After, and AfterFunc, which gc's
// runtime keeps ($GOROOT/src/runtime/time.go) and Aether keeps here, in
// Go, for its scheduler. The functions time declares without a body are
// given bodies calling these (frontend patches).
//
// As gc's, a timer of a channel (Timer.C, Ticker.C) is in the heap only
// while a goroutine is blocked receiving from its channel; a receive from
// it first runs it if it is due (maybeRunChan); Stop and Reset return
// whether it was pending and drain a value it sent that was not received.
// The scheduler, lowered IR, runs the due timers whenever it switches
// goroutines, and, when every goroutine is blocked, sleeps until the next
// one instead of reporting a deadlock (runTimers). An Aether program's
// goroutines never run in parallel (D-049), so nothing here is locked.

package time

import "unsafe"

// syscall6 makes a Linux x86-64 system call (D-051): an intrinsic.
func syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) int

// setChanTimer records t as the timer of the channel c: an intrinsic.
func setChanTimer(c, t unsafe.Pointer)

const (
	sysClockGettime   = 228
	sysClockNanosleep = 230
	clockRealtime     = 0
	clockMonotonic    = 1
	timerAbstime      = 1
	maxWhen           = 1<<63 - 1
)

type timespec struct {
	sec, nsec int64
}

func clockGettime(id uintptr) timespec {
	var ts timespec
	syscall6(sysClockGettime, id, uintptr(unsafe.Pointer(&ts)), 0, 0, 0, 0)
	return ts
}

// runtimeClock is gc's runtime time_now: the wall clock and the
// monotonic clock.
func runtimeClock() (sec int64, nsec int32, mono int64) {
	ts := clockGettime(clockRealtime)
	return ts.sec, int32(ts.nsec), nanotime()
}

// nanotime is gc's runtime nanotime: the monotonic clock, in nanoseconds.
func nanotime() int64 {
	ts := clockGettime(clockMonotonic)
	return ts.sec*1e9 + ts.nsec
}

// isBubbled reports whether the goroutine is in a synctest bubble, which
// Aether does not have.
func isBubbled() bool { return false }

// sleep is gc's runtime timeSleep: Sleep pauses the goroutine for at least
// d, running the others.
func sleep(d Duration) {
	if d <= 0 {
		return
	}
	c := make(chan Time, 1)
	newRuntimeTimer(when(d), 0, sendTime, c, *(*unsafe.Pointer)(unsafe.Pointer(&c)))
	<-c
}

// runtimeTimer is gc's runtime timer of a Timer or a Ticker, whose C and
// initTimer (or initTicker) it begins with: what a *Timer points to.
type runtimeTimer struct {
	Timer
	when    int64 // when it fires, by nanotime; 0 if it does not
	period  int64 // a Ticker's
	f       func(arg any, seq uintptr, delay int64)
	arg     any
	seq     uintptr
	hchan   unsafe.Pointer // the channel of a channel timer
	blocked int            // goroutines blocked receiving from hchan
	idx     int            // its index in timers, plus 1; 0 if not there
}

// timers is the timers that run, a heap by when.
var timers []*runtimeTimer

// newRuntimeTimer is gc's runtime newTimer.
func newRuntimeTimer(when, period int64, f func(any, uintptr, int64), arg any, cp unsafe.Pointer) *Timer {
	t := &runtimeTimer{when: when, period: period, f: f, arg: arg}
	t.initTimer = true
	if cp != nil {
		t.hchan = cp
		setChanTimer(cp, unsafe.Pointer(t))
	}
	t.update()
	return &t.Timer
}

// stopRuntimeTimer is gc's runtime stopTimer: whether the timer was
// pending, or had sent a value not received, which it removes.
func stopRuntimeTimer(tt *Timer) bool {
	t := (*runtimeTimer)(unsafe.Pointer(tt))
	pending := t.when > 0
	t.when = 0
	t.update()
	if t.hchan != nil {
		t.seq++
		if t.drain() {
			pending = true
		}
	}
	return pending
}

// resetRuntimeTimer is gc's runtime resetTimer: stopRuntimeTimer, then the
// timer fires at when, and again every period if it is not 0.
func resetRuntimeTimer(tt *Timer, when, period int64) bool {
	t := (*runtimeTimer)(unsafe.Pointer(tt))
	pending := t.when > 0
	t.when = 0
	t.period = period
	if t.hchan != nil {
		t.seq++
		if t.drain() {
			pending = true
		}
	}
	t.when = when
	t.update()
	return pending
}

// drain removes a value the timer sent to its channel, if there is one,
// and reports whether it did. With when 0, the receive does not run it.
func (t *runtimeTimer) drain() bool {
	select {
	case <-t.arg.(chan Time):
		return true
	default:
		return false
	}
}

// update puts the timer in the heap if it should be there, fixing its
// place, or takes it out.
func (t *runtimeTimer) update() {
	in := t.when > 0 && (t.hchan == nil || t.blocked > 0)
	switch {
	case in && t.idx == 0:
		timers = append(timers, t)
		t.idx = len(timers)
		siftUp(len(timers) - 1)
	case in:
		siftUp(t.idx - 1)
		siftDown(t.idx - 1)
	case t.idx != 0:
		i, last := t.idx-1, len(timers)-1
		swap(i, last)
		timers[last] = nil
		timers = timers[:last]
		t.idx = 0
		if i < last {
			siftUp(i)
			siftDown(i)
		}
	}
}

func swap(i, j int) {
	timers[i], timers[j] = timers[j], timers[i]
	timers[i].idx, timers[j].idx = i+1, j+1
}

func siftUp(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if timers[p].when <= timers[i].when {
			return
		}
		swap(i, p)
		i = p
	}
}

func siftDown(i int) {
	for {
		c := 2*i + 1
		if c >= len(timers) {
			return
		}
		if c+1 < len(timers) && timers[c+1].when < timers[c].when {
			c++
		}
		if timers[i].when <= timers[c].when {
			return
		}
		swap(i, c)
		i = c
	}
}

// run runs the timer, which is due at now, as gc's unlockAndRun: a
// Ticker's next time is the first of its period after now.
func (t *runtimeTimer) run(now int64) {
	delay := now - t.when
	if t.period > 0 {
		next := t.when + t.period*(1+delay/t.period)
		if next < 0 {
			next = maxWhen
		}
		t.when = next
	} else {
		t.when = 0
	}
	t.update()
	t.f(t.arg, t.seq, delay)
}

// maybeRunChan runs the timer of a channel being received from, if it is
// due: lowering calls it, with the channel's timer (D-054).
func maybeRunChan(p unsafe.Pointer) {
	t := (*runtimeTimer)(p)
	if t.when == 0 {
		return
	}
	if now := nanotime(); t.when <= now {
		t.run(now)
	}
}

// blockTimerChan and unblockTimerChan count a goroutine blocking to
// receive from a timer's channel, and its end: lowering calls them.
func blockTimerChan(p unsafe.Pointer) {
	t := (*runtimeTimer)(p)
	t.blocked++
	t.update()
}

func unblockTimerChan(p unsafe.Pointer) {
	t := (*runtimeTimer)(p)
	t.blocked--
	t.update()
}

// runTimers runs the due timers. If block, and none is due, it first
// sleeps until the next one, and it reports whether there were timers;
// else whether it ran one. The scheduler calls it at every switch, with
// every goroutine blocked, and when a select with a default case finds no
// case ready, so that a loop polling a timer's channel sees it fire, as
// gc's runtime runs timers on its own (D-054).
func runTimers(block bool) bool {
	if len(timers) == 0 {
		return false
	}
	now := nanotime()
	if block && timers[0].when > now {
		ts := timespec{timers[0].when / 1e9, timers[0].when % 1e9}
		syscall6(sysClockNanosleep, clockMonotonic, timerAbstime, uintptr(unsafe.Pointer(&ts)), 0, 0, 0)
		now = nanotime()
	}
	ran := false
	for len(timers) > 0 && timers[0].when <= now {
		timers[0].run(now)
		ran = true
	}
	return block || ran
}
