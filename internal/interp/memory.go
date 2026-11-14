package interp

import (
	"encoding/binary"
	"fmt"

	"github.com/stevenstank/aether/internal/ir"
)

// MaxMemory bounds the bytes of the local memory objects in existence at
// once, as the native stack bounds the frames holding them (D-034). An addr
// that would exceed it is a stack overflow.
const MaxMemory = 1 << 30

// HeapLimit is the bytes of heap objects a run may allocate, as the native
// runtime reserves (D-010); an alloc beyond it is out of memory.
const HeapLimit = 1 << 36

// offBits is the width of the offset in a Ptr into an object: a Ptr is the
// object's number times 2^offBits plus the offset (D-035). Objects are
// numbered from 1, so every such Ptr lies above the string Ptrs, which are
// string indices plus 1, and an object holds at most ir.MaxMemSize bytes,
// below 2^offBits.
const offBits = 31

// memory holds the memory objects of a run (D-034, D-035), by number. Each
// object records which of its bytes have been written, so that a read of a
// byte never written, which the IR forbids and native code would read as
// garbage, is an error rather than a value.
type memory struct {
	objs     map[int64]*object
	next     int64   // the number of the next object
	globals  []int64 // the Ptr of each global memory object
	live     int64   // the bytes of the local objects in existence
	heap     int64   // the bytes of the heap objects, each rounded up to 8
	zerobase int64   // the Ptr every zero-size allocation returns, 0 until the first
}

type object struct {
	data    []byte
	written []bool
}

// newMemory returns the memory of a run of prog, with its global memory
// objects, all zero bytes.
func newMemory(prog *ir.Program) *memory {
	m := &memory{objs: map[int64]*object{}, next: 1}
	for _, g := range prog.Mems {
		m.globals = append(m.globals, m.alloc(g.Size, true))
	}
	return m
}

// alloc creates an object of size bytes, written (as zero) if zeroed, and
// returns its Ptr.
func (m *memory) alloc(size int64, zeroed bool) int64 {
	o := &object{data: make([]byte, size), written: make([]bool, size)}
	if zeroed {
		for i := range o.written {
			o.written[i] = true
		}
	}
	id := m.next
	m.next++
	m.objs[id] = o
	return id << offBits
}

func (m *memory) free(p int64) { delete(m.objs, p>>offBits) }

// heapAlloc creates a heap object of n bytes, all zero (D-010, D-035): for
// n = 0, the one zero-size object. ok is false if the heap is exhausted, as
// the native runtime's would be. The interpreter holds every object in its
// own memory, so it does not create one larger than ir.MaxMemSize that
// would fit, and reports that as an error, not as the program's behavior.
func (m *memory) heapAlloc(n int64) (p int64, ok bool, err error) {
	if n == 0 {
		if m.zerobase == 0 {
			m.zerobase = m.alloc(0, true)
		}
		return m.zerobase, true, nil
	}
	// As aether.alloc: the size rounded up to 8 must fit what is left.
	if n < 0 || n > HeapLimit || (n+7)&^7 > HeapLimit-m.heap {
		return 0, false, nil
	}
	if n > ir.MaxMemSize {
		return 0, false, fmt.Errorf("heap object of %d bytes exceeds the interpreter's limit of %d", n, ir.MaxMemSize)
	}
	m.heap += (n + 7) &^ 7
	return m.alloc(n, true), true, nil
}

// object returns the object p points into and p's offset in it.
func (m *memory) object(p int64) (*object, int64, error) {
	o := m.objs[p>>offBits]
	if p < 1<<offBits || o == nil {
		return nil, 0, fmt.Errorf("address %#x is not a memory object in existence", p)
	}
	return o, p & (1<<offBits - 1), nil
}

// padd returns p plus i bytes, which must lie in p's object or just past
// its end.
func (m *memory) padd(p, i int64) (int64, error) {
	o, base, err := m.object(p)
	if err != nil {
		return 0, err
	}
	if i < -base || i > int64(len(o.data))-base {
		return 0, fmt.Errorf("offset %d from offset %d leaves an object of %d bytes", i, base, len(o.data))
	}
	return p + i, nil
}

// at returns the bytes of a t at offset off from p, checking the
// preconditions of LdM and StM, and the offset in the object.
func (m *memory) at(t ir.Type, p, off int64) (*object, int64, int64, error) {
	o, base, err := m.object(p)
	n := t.MemSize()
	switch {
	case err != nil:
		return nil, 0, 0, err
	case n == 0:
		return nil, 0, 0, fmt.Errorf("%s has no memory form", t)
	case off < -base || base+off > int64(len(o.data))-n:
		return nil, 0, 0, fmt.Errorf("%s at offset %d of an object of %d bytes", t, base+off, len(o.data))
	case (base+off)%n != 0:
		return nil, 0, 0, fmt.Errorf("%s at misaligned offset %d", t, base+off)
	}
	return o, n, base + off, nil
}

// load reads a t at offset off of the object at p, in canonical form.
func (m *memory) load(t ir.Type, p, off int64) (int64, error) {
	o, n, off, err := m.at(t, p, off)
	if err != nil {
		return 0, err
	}
	var buf [8]byte
	for i := range n {
		if !o.written[off+i] {
			return 0, fmt.Errorf("read of byte %d, never written", off+i)
		}
		buf[i] = o.data[off+i]
	}
	v := int64(binary.LittleEndian.Uint64(buf[:]))
	if t == ir.Bool && v > 1 {
		return 0, fmt.Errorf("bool byte %d", v)
	}
	return t.Canonical(v), nil
}

// store writes v, of type t, at offset off of the object at p.
func (m *memory) store(t ir.Type, p, off, v int64) error {
	o, n, off, err := m.at(t, p, off)
	if err != nil {
		return err
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	copy(o.data[off:off+n], buf[:n])
	for i := range n {
		o.written[off+i] = true
	}
	return nil
}
