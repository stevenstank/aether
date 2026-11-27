package runtime

// Waiting for descriptors without stopping the other goroutines (D-058).
//
// A goroutine reading a pipe, a terminal, or another descriptor that can
// block, through package os, first asks poll(2) whether it is ready; if
// not, it waits here (ioWait) on a channel, and the scheduler polls the
// descriptors waited on: at a switch once pollSwitches switches or
// pollTicks have passed since the last poll, and, when every goroutine is
// blocked, until one is ready or the next timer is due. The network poller of internal/poll (D-056)
// registers here (setNetpoll): with goroutines waiting on both, one
// poll(2) waits for these descriptors and its epoll descriptor together.
// A program's goroutines never run in parallel (D-049), so nothing here
// is locked.

import "unsafe"

// syscall6 makes a Linux x86-64 system call (D-051), and cputicks reads
// the time-stamp counter (ir.SysTicks): intrinsics.
func syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) int
func cputicks() int64

const (
	sysPoll = 7
	pollIn  = 0x1
	pollOut = 0x4
	eintr   = 4
)

// The scheduler's switches poll without waiting once pollSwitches
// switches or pollTicks ticks of the time-stamp counter (about 50 µs)
// have passed since the last poll, whichever comes first: a goroutine
// waiting on a descriptor runs soon after it is ready whether the others
// switch every few nanoseconds or run for long between switches (D-058;
// until then, one switch in 61, D-056). A poll is a system call of about
// 0.2 µs; reading the counter takes a few nanoseconds.
const (
	pollSwitches = 61
	pollTicks    = 1 << 17
)

// A pollFd is the kernel's struct pollfd.
type pollFd struct {
	fd             int32
	events, revent int16
}

// An ioWaiter is a goroutine waiting on a descriptor.
type ioWaiter struct {
	fd     int32
	events int16
	c      chan struct{}
}

var (
	ioWaiters []*ioWaiter
	pollFds   []pollFd
	lastPoll  int64 // cputicks at the last poll
	switches  int   // since the last poll

	// netpoll is internal/poll's poller, and epfd its epoll descriptor:
	// netpoll waits up to delay nanoseconds (-1: no limit) for the
	// descriptors goroutines wait on, and returns how many goroutines it
	// made runnable, or -1 if none waits.
	netpoll func(delay int64) int
	epfd    int32
)

// setNetpoll is how internal/poll gives its poller (frontend.Linknames).
func setNetpoll(f func(delay int64) int, fd int) {
	netpoll, epfd = f, int32(fd)
}

// ioReady reports whether fd is ready to read, or to write if write, or
// has hung up or failed, without waiting.
func ioReady(fd int, write bool) bool {
	p := pollFd{fd: int32(fd), events: pollIn}
	if write {
		p.events = pollOut
	}
	for {
		r := syscall6(sysPoll, uintptr(unsafe.Pointer(&p)), 1, 0, 0, 0, 0)
		if r != -eintr {
			return r != 0 // an error too: the read or write reports it
		}
	}
}

// ioWait waits until fd is ready to read (or write) or ioClose is called
// for it, running the other goroutines meanwhile. A caller asks ioReady
// first, and again after.
func ioWait(fd int, write bool) {
	w := &ioWaiter{fd: int32(fd), events: pollIn, c: make(chan struct{}, 1)}
	if write {
		w.events = pollOut
	}
	ioWaiters = append(ioWaiters, w)
	<-w.c
}

// ioClose wakes the goroutines waiting on fd, which is being closed.
func ioClose(fd int) {
	for i := 0; i < len(ioWaiters); i++ {
		if w := ioWaiters[i]; w.fd == int32(fd) {
			ioWake(i)
			i--
		}
	}
}

// ioWake makes waiter i runnable and removes it, keeping the others' order.
func ioWake(i int) {
	w := ioWaiters[i]
	ioWaiters = append(ioWaiters[:i], ioWaiters[i+1:]...)
	ioWaiters[:len(ioWaiters)+1][len(ioWaiters)] = nil
	w.c <- struct{}{}
}

// ioPoll is the scheduler's poll of the descriptors goroutines wait on:
// with delay 0 at a switch (only once pollSwitches or pollTicks have
// passed since the last poll), else with every goroutine blocked, waiting up to delay
// nanoseconds, -1 for no limit (package time's runTimers gives the next
// timer's). It makes the goroutines whose descriptors are ready runnable
// and returns how many, or -1 if no goroutine waits on a descriptor.
func ioPoll(delay int64) int {
	now := cputicks()
	// Unsigned, so that a counter that went back polls at once.
	if switches++; delay == 0 && switches < pollSwitches && uint64(now-lastPoll) < pollTicks {
		if len(ioWaiters) == 0 && netpoll == nil {
			return -1
		}
		return 0
	}
	lastPoll, switches = now, 0
	if len(ioWaiters) == 0 {
		if netpoll == nil {
			return -1
		}
		return netpoll(delay)
	}
	woke, net := 0, -1
	if netpoll != nil {
		if net = netpoll(0); net > 0 {
			woke, delay = net, 0
		}
	}
	pollFds = pollFds[:0]
	for _, w := range ioWaiters {
		pollFds = append(pollFds, pollFd{fd: w.fd, events: w.events})
	}
	if net >= 0 {
		pollFds = append(pollFds, pollFd{fd: epfd, events: pollIn})
	}
	ms := -1
	switch {
	case delay == 0:
		ms = 0
	case delay > 0 && delay < 1e15:
		ms = int((delay + 999999) / 1e6)
	case delay > 0:
		ms = 1e9 // about 11 days
	}
	if r := syscall6(sysPoll, uintptr(unsafe.Pointer(&pollFds[0])), uintptr(len(pollFds)), uintptr(ms), 0, 0, 0); r <= 0 {
		return woke // none ready, or EINTR
	}
	// The waiters come first, in order; those woken are removed.
	n := len(ioWaiters)
	for i, j := 0, 0; j < n; j++ {
		if pollFds[j].revent != 0 {
			ioWake(i)
			woke++
		} else {
			i++
		}
	}
	if net >= 0 && pollFds[len(pollFds)-1].revent != 0 {
		woke += max(0, netpoll(0))
	}
	return woke
}
