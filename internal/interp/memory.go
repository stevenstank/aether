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
// numbered from 1, so 0 is never one, and an object holds at most
// ir.MaxMemSize bytes, below 2^offBits.
const offBits = 31

// memory holds the memory objects of a run (D-034, D-035), by number. Each
// object records which of its bytes have been written, so that a read of a
// byte never written, which the IR forbids and native code would read as
// garbage, is an error rather than a value.
type memory struct {
	objs     map[int64]*object
	next     int64   // the number of the next object
	globals  []int64 // the Ptr of each global memory object
	strs     []int64 // the Ptr of each program string, a read-only object (D-037)
	live     int64   // the bytes of the local objects in existence
	heap     int64   // the bytes of the heap objects, each rounded up to 8
	zerobase int64   // the Ptr every zero-size allocation returns, 0 until the first
	// The code addresses of functions (D-039): each a read-only object of
	// no bytes, made when FuncAddr first takes it, so that no LdM or StM
	// can go through one.
	funcAddrs map[*ir.Func]int64
	funcs     map[int64]*ir.Func
}

// funcAddr returns the code address of f.
func (m *memory) funcAddr(f *ir.Func) int64 {
	if p, ok := m.funcAddrs[f]; ok {
		return p
	}
	if m.funcAddrs == nil {
		m.funcAddrs, m.funcs = map[*ir.Func]int64{}, map[int64]*ir.Func{}
	}
	p := m.alloc(0, true)
	m.objs[p>>offBits].readOnly = true
	m.funcAddrs[f], m.funcs[p] = p, f
	return p
}

type object struct {
	data     []byte
	written  []bool
	readOnly bool // a string constant's bytes, which native code keeps in .rodata
}

// newMemory returns the memory of a run of prog, with its global memory
// objects, all zero bytes.
func newMemory(prog *ir.Program) *memory {
	m := &memory{objs: map[int64]*object{}, next: 1}
	for _, g := range prog.Mems {
		m.globals = append(m.globals, m.alloc(g.Size, true))
	}
	for _, s := range prog.Strings {
		p := m.alloc(int64(len(s)), true)
		o := m.objs[p>>offBits]
		copy(o.data, s)
		o.readOnly = true
		m.strs = append(m.strs, p)
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
	if i == 0 {
		return p, nil // any Ptr, nil included
	}
	o, base, err := m.object(p)
	if err != nil {
		return 0, err
	}
	if i < -base || i > int64(len(o.data))-base {
		return 0, fmt.Errorf("offset %d from offset %d leaves an object of %d bytes", i, base, len(o.data))
	}
	return p + i, nil
}

// move copies n bytes from src to dst as if through a temporary (D-036),
// checking Move's precondition: n is 0, or each range lies in one object
// and every byte of src's has been written.
func (m *memory) move(dst, src, n int64) error {
	if n == 0 {
		return nil
	}
	ranges := [2]struct {
		o   *object
		off int64
	}{}
	for i, p := range []int64{dst, src} {
		o, off, err := m.object(p)
		switch {
		case err != nil:
			return err
		case i == 0 && o.readOnly:
			return fmt.Errorf("move into a string constant")
		case n < 0 || n > int64(len(o.data))-off:
			return fmt.Errorf("move of %d bytes at offset %d of an object of %d bytes", n, off, len(o.data))
		}
		ranges[i].o, ranges[i].off = o, off
	}
	d, s := ranges[0], ranges[1]
	for i := range n {
		if !s.o.written[s.off+i] {
			return fmt.Errorf("move reads byte %d, never written", s.off+i)
		}
	}
	// Go's copy, like memmove, allows overlap.
	copy(d.o.data[d.off:d.off+n], s.o.data[s.off:s.off+n])
	for i := range n {
		d.o.written[d.off+i] = true
	}
	return nil
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
	if o.readOnly {
		return fmt.Errorf("store into a string constant")
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	copy(o.data[off:off+n], buf[:n])
	for i := range n {
		o.written[off+i] = true
	}
	return nil
}

// writable returns the n bytes at p, which a read system call fills,
// checking that they lie in one object that is not read-only, and marks
// them written (increment 11.8).
func (m *memory) writable(p, n int64) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}
	o, off, err := m.object(p)
	switch {
	case err != nil:
		return nil, err
	case o.readOnly:
		return nil, fmt.Errorf("write to a read-only object")
	case n < 0 || n > int64(len(o.data))-off:
		return nil, fmt.Errorf("%d bytes at offset %d of an object of %d bytes", n, off, len(o.data))
	}
	for i := off; i < off+n; i++ {
		o.written[i] = true
	}
	return o.data[off : off+n], nil
}

// bytes returns the n bytes at p, a string's (D-033, D-037), checking that
// they lie in one object and were written. A string of length 0 has no
// bytes, whatever its Ptr.
func (m *memory) bytes(p, n int64) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}
	o, off, err := m.object(p)
	switch {
	case err != nil:
		return nil, err
	case n < 0 || n > int64(len(o.data))-off:
		return nil, fmt.Errorf("%d bytes at offset %d of an object of %d bytes", n, off, len(o.data))
	}
	for i := off; i < off+n; i++ {
		if !o.written[i] {
			return nil, fmt.Errorf("read of byte %d, never written", i)
		}
	}
	return o.data[off : off+n], nil
}

// cstring returns the bytes at p up to and including the first 0 byte, a
// path a system call takes (D-051), checking that they lie in one object
// and were written.
func (m *memory) cstring(p int64) ([]byte, error) {
	o, off, err := m.object(p)
	if err != nil {
		return nil, err
	}
	for i := off; i < int64(len(o.data)); i++ {
		if !o.written[i] {
			return nil, fmt.Errorf("read of byte %d, never written", i)
		}
		if o.data[i] == 0 {
			return o.data[off : i+1], nil
		}
	}
	return nil, fmt.Errorf("no 0 byte after offset %d of an object of %d bytes", off, len(o.data))
}
