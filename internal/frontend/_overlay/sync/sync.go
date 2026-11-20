// Package sync is Aether's replacement of gc's sync package (D-049,
// increment 11.6): Mutex, RWMutex, WaitGroup, and Once. Aether runs every
// goroutine on one thread and switches only when one blocks or yields
// (D-048), so plain memory operations are atomic here; goroutines wait on
// channels.
package sync

// fatal ends the program with gc's fatal error "fatal error: " + s, which
// is never recovered. It is an intrinsic.
func fatal(s string)

// A Locker represents an object that can be locked and unlocked.
type Locker interface {
	Lock()
	Unlock()
}

// A Mutex is a mutual exclusion lock. Its zero value is unlocked.
type Mutex struct {
	locked  bool
	waiters int           // the goroutines blocked in Lock
	ch      chan struct{} // where they wait, made on first use
}

// Lock locks m, waiting until it is unlocked if it is locked.
func (m *Mutex) Lock() {
	if !m.locked {
		m.locked = true
		return
	}
	if m.ch == nil {
		m.ch = make(chan struct{})
	}
	m.waiters++
	<-m.ch // Unlock hands the lock over: m.locked stays true
}

// TryLock locks m if it is unlocked, and reports whether it did.
func (m *Mutex) TryLock() bool {
	if m.locked {
		return false
	}
	m.locked = true
	return true
}

// Unlock unlocks m, handing it to the first goroutine waiting in Lock. It
// is a fatal error if m is not locked.
func (m *Mutex) Unlock() {
	if !m.locked {
		fatal("sync: unlock of unlocked mutex")
	}
	if m.waiters > 0 {
		m.waiters--
		m.ch <- struct{}{}
		return
	}
	m.locked = false
}

// An RWMutex is a reader/writer mutual exclusion lock: held by any number
// of readers or one writer. Once a writer waits in Lock, new readers wait
// until it has unlocked. Its zero value is unlocked.
type RWMutex struct {
	w       Mutex         // held by the writer that holds or awaits the lock
	writer  bool          // a writer holds or awaits the lock
	readers int           // the readers holding the lock
	rwait   int           // the readers waiting for the writer
	rch     chan struct{} // where they wait
	wwait   bool          // the writer waits for the readers to leave
	wch     chan struct{} // where it waits
}

// RLock locks rw for reading.
func (rw *RWMutex) RLock() {
	if !rw.writer {
		rw.readers++
		return
	}
	if rw.rch == nil {
		rw.rch = make(chan struct{})
	}
	rw.rwait++
	<-rw.rch // Unlock counts this reader in rw.readers
}

// TryRLock locks rw for reading if no writer holds or awaits it, and
// reports whether it did.
func (rw *RWMutex) TryRLock() bool {
	if rw.writer {
		return false
	}
	rw.readers++
	return true
}

// RUnlock undoes one RLock. It is a fatal error if rw is not locked for
// reading.
func (rw *RWMutex) RUnlock() {
	if rw.readers == 0 {
		fatal("sync: RUnlock of unlocked RWMutex")
	}
	rw.readers--
	if rw.wwait && rw.readers == 0 {
		rw.wwait = false
		rw.wch <- struct{}{}
	}
}

// Lock locks rw for writing, waiting for the readers holding it.
func (rw *RWMutex) Lock() {
	rw.w.Lock()
	rw.writer = true
	if rw.readers > 0 {
		if rw.wch == nil {
			rw.wch = make(chan struct{})
		}
		rw.wwait = true
		<-rw.wch
	}
}

// TryLock locks rw for writing if it is unlocked, and reports whether it
// did.
func (rw *RWMutex) TryLock() bool {
	if !rw.w.TryLock() {
		return false
	}
	if rw.readers > 0 {
		rw.w.Unlock()
		return false
	}
	rw.writer = true
	return true
}

// Unlock unlocks rw for writing, letting the readers waiting for it in. It
// is a fatal error if rw is not locked for writing.
func (rw *RWMutex) Unlock() {
	if !rw.writer {
		fatal("sync: Unlock of unlocked RWMutex")
	}
	rw.writer = false
	n := rw.rwait
	rw.rwait = 0
	rw.readers += n
	for range n {
		rw.rch <- struct{}{}
	}
	rw.w.Unlock()
}

// RLocker returns a Locker whose Lock and Unlock call rw.RLock and
// rw.RUnlock.
func (rw *RWMutex) RLocker() Locker {
	return (*rlocker)(rw)
}

type rlocker RWMutex

func (r *rlocker) Lock()   { (*RWMutex)(r).RLock() }
func (r *rlocker) Unlock() { (*RWMutex)(r).RUnlock() }

// A WaitGroup waits for a collection of tasks to finish. Its zero value
// has a counter of zero.
type WaitGroup struct {
	n       int32         // the counter, which wraps as gc's 32 bits
	waiters int           // the goroutines blocked in Wait
	ch      chan struct{} // where they wait
}

// Add adds delta to the counter. When it reaches zero, the goroutines
// blocked in Wait are released. A negative counter panics.
func (wg *WaitGroup) Add(delta int) {
	v := wg.n + int32(delta)
	wg.n = v
	if v < 0 {
		panic("sync: negative WaitGroup counter")
	}
	if wg.waiters != 0 && delta > 0 && v == int32(delta) {
		panic("sync: WaitGroup misuse: Add called concurrently with Wait")
	}
	if v > 0 || wg.waiters == 0 {
		return
	}
	w := wg.waiters
	wg.waiters = 0
	for range w {
		wg.ch <- struct{}{}
	}
}

// Done decrements the counter by one.
func (wg *WaitGroup) Done() {
	wg.Add(-1)
}

// Wait blocks until the counter is zero.
func (wg *WaitGroup) Wait() {
	if wg.n == 0 {
		return
	}
	if wg.ch == nil {
		wg.ch = make(chan struct{})
	}
	wg.waiters++
	<-wg.ch
	if wg.n != 0 || wg.waiters != 0 {
		panic("sync: WaitGroup is reused before previous Wait has returned")
	}
}

// Once performs exactly one action.
type Once struct {
	done bool
	m    Mutex
}

// Do calls f if and only if Do is being called for the first time for o.
// When Do returns, f has returned or panicked; either way, o is done.
func (o *Once) Do(f func()) {
	if o.done {
		return
	}
	o.m.Lock()
	defer o.m.Unlock()
	if !o.done {
		defer o.finish()
		f()
	}
}

func (o *Once) finish() { o.done = true }

// A Pool is a set of temporary objects that may be individually saved and
// retrieved. Aether's keeps every object put, and gives back the last.
type Pool struct {
	items []any
	// New optionally specifies a function to generate a value when Get
	// would otherwise return nil.
	New func() any
}

// Put adds x to the pool; a nil x is dropped.
func (p *Pool) Put(x any) {
	if x != nil {
		p.items = append(p.items, x)
	}
}

// Get removes an item from the pool and returns it, or else returns the
// result of calling p.New, if it is not nil, or nil.
func (p *Pool) Get() any {
	if n := len(p.items); n > 0 {
		x := p.items[n-1]
		p.items[n-1] = nil
		p.items = p.items[:n-1]
		return x
	}
	if p.New != nil {
		return p.New()
	}
	return nil
}
