// Aether's part of gc's runtime for package internal/poll (D-056): the
// network poller of $GOROOT/src/runtime/netpoll.go and netpoll_epoll.go,
// and the semaphores of fd_mutex.go, in Go, over the scheduler. The
// functions internal/poll declares without a body are given bodies
// calling these (frontend patches).
//
// As gc's, the poller is edge-triggered epoll: a descriptor is registered
// once for reading and writing; an event marks it ready, and a goroutine
// waiting on it runs; reading or writing first clears the mark
// (pollReset), and waits only after EAGAIN. A goroutine waits on a
// channel of its descriptor. The scheduler polls without waiting every
// pollEvery switches, and, when every goroutine is blocked and one waits
// on a descriptor, waits in epoll_wait until an event or the next timer
// (package time's runTimers calls netpoll). Deadlines are time's timers.
// An Aether program's goroutines never run in parallel (D-049), so
// nothing here is locked.

package poll

import (
	"syscall"
	"time"
)

// setNetpoll gives package time's scheduler hook the poller: time's
// setNetpoll (frontend.Linknames).
func setNetpoll(f func(delay int64) int)

// A runtimePD is gc's runtime pollDesc: a descriptor on the poller.
type runtimePD struct {
	fd      int
	gen     int32 // of its slot in pds, to tell a stale event
	closing bool
	// rready and wready record an event not yet consumed; rwait and
	// wwait are the channels a goroutine waiting to read or write
	// receives from, or nil.
	rready, wready bool
	rwait, wwait   chan struct{}
	// rd and wd are the deadlines, by nanotime: 0 none, -1 expired; rt
	// and wt their timers.
	rd, wd int64
	rt, wt *time.Timer
}

var (
	epfd    = -1
	pds     []*runtimePD // a ctx is an index in pds, plus 1
	freePDs []int
	gens    int32
	waiting int // goroutines waiting on descriptors
	events  [128]syscall.EpollEvent
)

func aetherPollServerInit() {
	fd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		println("runtime: netpollinit failed with", err.Error())
		panic("runtime: netpollinit failed")
	}
	epfd = fd
	setNetpoll(netpoll)
}

func aetherIsPollServerDescriptor(fd uintptr) bool {
	return epfd >= 0 && fd == uintptr(epfd)
}

func aetherPollOpen(fd uintptr) (uintptr, int) {
	var i int
	if n := len(freePDs); n > 0 {
		i = freePDs[n-1]
		freePDs = freePDs[:n-1]
	} else {
		i = len(pds)
		pds = append(pds, nil)
	}
	gens++
	pd := &runtimePD{fd: int(fd), gen: gens}
	// syscall.EPOLLET is negative, as an int.
	ev := syscall.EpollEvent{Events: syscall.EPOLLIN | syscall.EPOLLOUT | syscall.EPOLLRDHUP | 1<<31, Fd: int32(i), Pad: pd.gen}
	if err := syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, int(fd), &ev); err != nil {
		freePDs = append(freePDs, i)
		return 0, int(err.(syscall.Errno))
	}
	pds[i] = pd
	return uintptr(i + 1), 0
}

func lookupPD(ctx uintptr) *runtimePD {
	return pds[ctx-1]
}

func aetherPollClose(ctx uintptr) {
	pd := lookupPD(ctx)
	if !pd.closing {
		panic("runtime: close polldesc w/o unblock")
	}
	var ev syscall.EpollEvent
	syscall.EpollCtl(epfd, syscall.EPOLL_CTL_DEL, pd.fd, &ev)
	pds[ctx-1] = nil
	freePDs = append(freePDs, int(ctx-1))
}

// check is gc's netpollcheckerr: why a wait for mode ends at once.
func (pd *runtimePD) check(mode int) int {
	if pd.closing {
		return pollErrClosing
	}
	if mode == 'r' && pd.rd < 0 || mode == 'w' && pd.wd < 0 {
		return pollErrTimeout
	}
	return pollNoError
}

func aetherPollReset(ctx uintptr, mode int) int {
	pd := lookupPD(ctx)
	if e := pd.check(mode); e != pollNoError {
		return e
	}
	if mode == 'r' {
		pd.rready = false
	} else {
		pd.wready = false
	}
	return pollNoError
}

// aetherPollWait waits until the descriptor is ready for mode, its
// deadline passes, or it closes.
func aetherPollWait(ctx uintptr, mode int) int {
	pd := lookupPD(ctx)
	for {
		if e := pd.check(mode); e != pollNoError {
			return e
		}
		ready, wait := &pd.rready, &pd.rwait
		if mode == 'w' {
			ready, wait = &pd.wready, &pd.wwait
		}
		if *ready {
			*ready = false
			return pollNoError
		}
		c := make(chan struct{}, 1)
		*wait = c
		waiting++
		<-c
		waiting--
		*wait = nil
	}
}

func aetherPollWaitCanceled(ctx uintptr, mode int) {}

// wake makes the goroutine waiting on c, if any, runnable.
func wake(c chan struct{}) {
	if c != nil {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

func aetherPollUnblock(ctx uintptr) {
	pd := lookupPD(ctx)
	if pd.closing {
		panic("runtime: unblock on closing polldesc")
	}
	pd.closing = true
	stopTimer(&pd.rt)
	stopTimer(&pd.wt)
	wake(pd.rwait)
	wake(pd.wwait)
}

func stopTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

// aetherPollSetDeadline is gc's poll_runtime_pollSetDeadline: d is a
// duration from now, 0 for no deadline, negative for one passed.
func aetherPollSetDeadline(ctx uintptr, d int64, mode int) {
	pd := lookupPD(ctx)
	if pd.closing {
		return
	}
	if d > 0 {
		d += runtimeNano()
		if d <= 0 {
			d = 1<<63 - 1 // overflow
		}
	}
	if mode == 'r' || mode == 'r'+'w' {
		pd.setDeadline(&pd.rd, &pd.rt, &pd.rwait, d)
	}
	if mode == 'w' || mode == 'r'+'w' {
		pd.setDeadline(&pd.wd, &pd.wt, &pd.wwait, d)
	}
}

// setDeadline sets one of the descriptor's deadlines, its timer, and,
// if it has passed, wakes the goroutine waiting.
func (pd *runtimePD) setDeadline(dl *int64, t **time.Timer, wait *chan struct{}, d int64) {
	stopTimer(t)
	*dl = d
	if d < 0 {
		wake(*wait)
		return
	}
	if d == 0 {
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(time.Duration(d-runtimeNano()), func() {
		if *t != timer {
			return // stopped or replaced
		}
		*t = nil
		*dl = -1
		wake(*wait)
	})
	*t = timer
}

// pollEvery is how many of the scheduler's switches poll once without
// waiting, so that a goroutine waiting on a descriptor runs although
// others never block.
const pollEvery = 61

var switches int

// netpoll is the scheduler's poll, which package time's runTimers calls:
// with delay 0 at a switch, and when every goroutine is blocked with the
// nanoseconds until the next timer, or -1 for none. Unless a goroutine
// waits on a descriptor it returns -1 at once. Else it waits for events,
// up to delay (only after pollEvery switches if 0), makes the goroutines
// waiting on ready descriptors runnable, and returns how many.
func netpoll(delay int64) int {
	if waiting == 0 {
		return -1
	}
	if delay == 0 {
		if switches++; switches < pollEvery {
			return 0
		}
	}
	switches = 0
	ms := -1
	switch {
	case delay == 0:
		ms = 0
	case delay > 0 && delay < 1e15:
		ms = int((delay + 999999) / 1e6)
	case delay > 0:
		ms = 1e9 // about 11 days
	}
	n, err := syscall.EpollWait(epfd, events[:], ms)
	if err != nil {
		if err != syscall.EINTR {
			println("runtime: epollwait on fd", epfd, "failed with", err.Error())
			panic("runtime: netpoll failed")
		}
		return 0
	}
	woke := 0
	for _, ev := range events[:n] {
		i := int(ev.Fd)
		if i < 0 || i >= len(pds) || pds[i] == nil || pds[i].gen != ev.Pad {
			continue // closed since
		}
		pd := pds[i]
		if ev.Events&(syscall.EPOLLIN|syscall.EPOLLRDHUP|syscall.EPOLLHUP|syscall.EPOLLERR) != 0 {
			pd.rready = true
			if pd.rwait != nil {
				wake(pd.rwait)
				woke++
			}
		}
		if ev.Events&(syscall.EPOLLOUT|syscall.EPOLLHUP|syscall.EPOLLERR) != 0 {
			pd.wready = true
			if pd.wwait != nil {
				wake(pd.wwait)
				woke++
			}
		}
	}
	return woke
}

// semWaiters are the goroutines blocked in aetherSemacquire, by
// semaphore, in order.
var semWaiters map[*uint32][]chan struct{}

// aetherSemacquire is gc's runtime semacquire: it waits until *sema > 0,
// then decrements it.
func aetherSemacquire(sema *uint32) {
	if *sema > 0 {
		*sema--
		return
	}
	if semWaiters == nil {
		semWaiters = map[*uint32][]chan struct{}{}
	}
	c := make(chan struct{}, 1)
	semWaiters[sema] = append(semWaiters[sema], c)
	<-c
}

// aetherSemrelease is gc's runtime semrelease: it increments *sema, or
// hands the count to the first goroutine waiting.
func aetherSemrelease(sema *uint32) {
	if ws := semWaiters[sema]; len(ws) > 0 {
		if len(ws) == 1 {
			delete(semWaiters, sema)
		} else {
			semWaiters[sema] = ws[1:]
		}
		ws[0] <- struct{}{}
		return
	}
	*sema++
}
