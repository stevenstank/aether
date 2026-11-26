package runtime

// Weak pointers and cleanups (D-057): package weak's handles and
// AddCleanup, over Aether's collector.
//
// Each is a record of specials, a table the collector reads after it
// marks (ir.SysSpecials, heap.s): a weak pointer whose object is not
// marked is cleared, and a cleanup whose object is not marked is queued.
// The records are uintptrs in an object without pointers (D-047), so they
// keep nothing alive; nor do the other words here holding an object's
// address, which hold it complemented (^p), never a heap address, for the
// conservative collector (D-046) to see. The scheduler calls specialsPoll
// at every switch (frontend.RuntimeHooks): once a collection has queued
// cleanups, a goroutine runs them, one after another, as gc's does.
//
// An Aether program's goroutines never run in parallel (D-049), so
// nothing here is locked; a collection runs only when a goroutine
// allocates.

import "unsafe"

// setSpecials gives the collector the table: an intrinsic.
func setSpecials(t *specialTable)

const (
	specialWeak    = 1
	specialCleanup = 3
)

// A special is a record of the table, as heap.s reads it.
type special struct {
	obj   uintptr // the object's address
	kind  uintptr // specialWeak, specialCleanup, or 0 for none
	state uintptr // 1: the collector queued the cleanup
	aux   uintptr // a weak pointer's handle, a cleanup's id
}

type specialTable struct {
	ready uintptr // set by the collector when it queues a cleanup
	recs  []special
}

var (
	specials    specialTable
	specialsSet bool
	// live is the records after the last compaction.
	live int

	// weakHandles are the handles of weak pointers, by ^address. A handle
	// is a word holding the address, which the collector zeroes; then the
	// entry is stale, and is dropped when weakHandles has doubled.
	weakHandles map[uintptr]*uintptr
	weakPurge   = 16

	cleanups      map[uint64]func() // by id
	cleanupAt     map[uint64]int    // their records' indices, by id
	lastCleanup   uint64
	cleanupsGoing bool
)

// addSpecial adds a record for the object at p.
func addSpecial(p unsafe.Pointer, kind, aux uintptr) {
	if !specialsSet {
		specialsSet = true
		setSpecials(&specials)
	}
	if len(specials.recs) == cap(specials.recs) && len(specials.recs) >= 2*live+16 {
		compact()
	}
	if kind == specialCleanup {
		cleanupAt[uint64(aux)] = len(specials.recs)
	}
	specials.recs = append(specials.recs, special{obj: uintptr(p), kind: kind, aux: aux})
}

// compact drops the records of no kind. The new array is allocated
// first: a collection then still sees the old one.
func compact() {
	n := 0
	for _, r := range specials.recs {
		if r.kind != 0 {
			n++
		}
	}
	recs := make([]special, 0, 2*n+16)
	for _, r := range specials.recs {
		if r.kind != 0 {
			if r.kind == specialCleanup {
				cleanupAt[uint64(r.aux)] = len(recs)
			}
			recs = append(recs, r)
		}
	}
	specials.recs = recs
	live = len(recs)
}

// registerWeakPointer is gc's runtime registerWeakPointer, for package
// weak (frontend.Linknames): the handle of the weak pointers to p, the
// same for every pointer equal to p while its object lives.
func registerWeakPointer(p unsafe.Pointer) unsafe.Pointer {
	if weakHandles == nil {
		weakHandles = map[uintptr]*uintptr{}
	}
	key := ^uintptr(p)
	if h := weakHandles[key]; h != nil && *h != 0 {
		return unsafe.Pointer(h)
	}
	if len(weakHandles) >= weakPurge {
		for k, h := range weakHandles {
			if *h == 0 {
				delete(weakHandles, k)
			}
		}
		weakPurge = 2*len(weakHandles) + 16
	}
	h := new(uintptr) // holds no pointer (D-047)
	*h = uintptr(p)
	weakHandles[key] = h
	addSpecial(p, specialWeak, uintptr(unsafe.Pointer(h)))
	return unsafe.Pointer(h)
}

// makeStrongFromWeak is gc's runtime makeStrongFromWeak: the address the
// handle u holds, nil once its object was collected.
func makeStrongFromWeak(u unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(*(*uintptr)(u))
}

// Cleanup is a handle to a cleanup call for a specific object.
type Cleanup struct {
	id  uint64
	ptr uintptr // ^the object's address
}

// AddCleanup attaches a cleanup function to ptr: some time after ptr is
// no longer reachable, cleanup(arg) runs on a goroutine of its own. As
// gc's, a ptr that is not on the heap (a package-level variable) never
// becomes unreachable, and its cleanup never runs. Unlike gc's, it does
// not detect an arg or a cleanup that keeps ptr reachable, whose cleanup
// then never runs, where gc panics.
func AddCleanup[T, S any](ptr *T, cleanup func(S), arg S) Cleanup {
	// Only the closure is generic: an instance another package hosts
	// cannot write Cleanup's fields (D-052).
	return addCleanup(unsafe.Pointer(ptr), func() { cleanup(arg) })
}

func addCleanup(p unsafe.Pointer, f func()) Cleanup {
	if p == nil {
		panic("runtime.AddCleanup: ptr is nil")
	}
	if cleanups == nil {
		cleanups, cleanupAt = map[uint64]func(){}, map[uint64]int{}
	}
	lastCleanup++
	id := lastCleanup
	cleanups[id] = f
	addSpecial(p, specialCleanup, uintptr(id))
	return Cleanup{id: id, ptr: ^uintptr(p)}
}

// Stop cancels the cleanup call, unless it is already queued because ptr
// became unreachable.
func (c Cleanup) Stop() {
	i, ok := cleanupAt[c.id]
	if !ok {
		return
	}
	if r := &specials.recs[i]; r.state == 0 {
		r.kind = 0
		delete(cleanups, c.id)
		delete(cleanupAt, c.id)
	}
}

// specialsPoll starts the goroutine running the queued cleanups, if a
// collection queued some and it is not running: the scheduler calls it at
// every switch (frontend.RuntimeHooks).
func specialsPoll() {
	if specials.ready == 0 || cleanupsGoing {
		return
	}
	specials.ready = 0
	cleanupsGoing = true
	go runCleanups()
}

// runCleanups runs the queued cleanups, in the order of their records,
// until there are none.
func runCleanups() {
	for {
		var fs []func()
		for i := 0; i < len(specials.recs); i++ {
			r := &specials.recs[i]
			if r.kind == specialCleanup && r.state == 1 {
				r.kind = 0
				delete(cleanupAt, uint64(r.aux))
				if f := cleanups[uint64(r.aux)]; f != nil {
					delete(cleanups, uint64(r.aux))
					fs = append(fs, f)
				}
			}
		}
		if len(fs) == 0 {
			cleanupsGoing = false
			return
		}
		for _, f := range fs {
			f()
		}
	}
}
