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

// memBase is the handle of the first memory object. Handles lie far above
// the string Ptrs, which are string indices plus 1, so the two never meet.
const memBase = 1 << 32

// memory holds the memory objects of a run (D-034), by handle. Each object
// records which of its bytes have been written, so that a read of a byte
// never written, which the IR forbids and native code would read as
// garbage, is an error rather than a value.
type memory struct {
	objs    map[int64]*object
	next    int64   // the handle of the next object
	globals []int64 // the handle of each global memory object
	live    int64   // the bytes of the local objects in existence
}

type object struct {
	data    []byte
	written []bool
}

// newMemory returns the memory of a run of prog, with its global memory
// objects, all zero bytes.
func newMemory(prog *ir.Program) *memory {
	m := &memory{objs: map[int64]*object{}, next: memBase}
	for _, g := range prog.Mems {
		m.globals = append(m.globals, m.alloc(g.Size, true))
	}
	return m
}

// alloc creates an object of size bytes, written (as zero) if zeroed, and
// returns its handle.
func (m *memory) alloc(size int64, zeroed bool) int64 {
	o := &object{data: make([]byte, size), written: make([]bool, size)}
	if zeroed {
		for i := range o.written {
			o.written[i] = true
		}
	}
	h := m.next
	m.next++
	m.objs[h] = o
	return h
}

func (m *memory) free(h int64) { delete(m.objs, h) }

// at returns the bytes of a t at offset off of the object at p, checking
// the preconditions of LdM and StM.
func (m *memory) at(t ir.Type, p, off int64) (*object, int64, error) {
	o := m.objs[p]
	n := t.MemSize()
	switch {
	case o == nil:
		return nil, 0, fmt.Errorf("address %#x is not a memory object in existence", p)
	case n == 0:
		return nil, 0, fmt.Errorf("%s has no memory form", t)
	case off < 0 || off > int64(len(o.data))-n:
		return nil, 0, fmt.Errorf("%s at offset %d of an object of %d bytes", t, off, len(o.data))
	case off%n != 0:
		return nil, 0, fmt.Errorf("%s at misaligned offset %d", t, off)
	}
	return o, n, nil
}

// load reads a t at offset off of the object at p, in canonical form.
func (m *memory) load(t ir.Type, p, off int64) (int64, error) {
	o, n, err := m.at(t, p, off)
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
	o, n, err := m.at(t, p, off)
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
