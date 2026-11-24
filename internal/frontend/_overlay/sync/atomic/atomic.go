// Package atomic is Aether's replacement of gc's sync/atomic package
// (D-049, increment 11.6). Aether runs every goroutine on one thread and
// switches only when one blocks or yields (D-048), so a plain memory
// operation is atomic. Swap, And, and Or return the old value, Add the new
// one, and CompareAndSwap whether it stored new.
package atomic

import "unsafe"

func SwapInt32(addr *int32, new int32) (old int32)  { old = *addr; *addr = new; return old }
func AddInt32(addr *int32, delta int32) (new int32) { *addr += delta; return *addr }
func AndInt32(addr *int32, mask int32) (old int32)  { old = *addr; *addr = old & mask; return old }
func OrInt32(addr *int32, mask int32) (old int32)   { old = *addr; *addr = old | mask; return old }
func LoadInt32(addr *int32) (val int32)             { return *addr }
func StoreInt32(addr *int32, val int32)             { *addr = val }

func CompareAndSwapInt32(addr *int32, old, new int32) (swapped bool) {
	if *addr != old {
		return false
	}
	*addr = new
	return true
}

// An Int32 is an atomic int32. Its zero value is zero.
type Int32 struct{ v int32 }

func (x *Int32) Load() int32                 { return LoadInt32(&x.v) }
func (x *Int32) Store(val int32)             { StoreInt32(&x.v, val) }
func (x *Int32) Swap(new int32) (old int32)  { return SwapInt32(&x.v, new) }
func (x *Int32) Add(delta int32) (new int32) { return AddInt32(&x.v, delta) }
func (x *Int32) And(mask int32) (old int32)  { return AndInt32(&x.v, mask) }
func (x *Int32) Or(mask int32) (old int32)   { return OrInt32(&x.v, mask) }
func (x *Int32) CompareAndSwap(old, new int32) (swapped bool) {
	return CompareAndSwapInt32(&x.v, old, new)
}

func SwapInt64(addr *int64, new int64) (old int64)  { old = *addr; *addr = new; return old }
func AddInt64(addr *int64, delta int64) (new int64) { *addr += delta; return *addr }
func AndInt64(addr *int64, mask int64) (old int64)  { old = *addr; *addr = old & mask; return old }
func OrInt64(addr *int64, mask int64) (old int64)   { old = *addr; *addr = old | mask; return old }
func LoadInt64(addr *int64) (val int64)             { return *addr }
func StoreInt64(addr *int64, val int64)             { *addr = val }

func CompareAndSwapInt64(addr *int64, old, new int64) (swapped bool) {
	if *addr != old {
		return false
	}
	*addr = new
	return true
}

// An Int64 is an atomic int64. Its zero value is zero.
type Int64 struct{ v int64 }

func (x *Int64) Load() int64                 { return LoadInt64(&x.v) }
func (x *Int64) Store(val int64)             { StoreInt64(&x.v, val) }
func (x *Int64) Swap(new int64) (old int64)  { return SwapInt64(&x.v, new) }
func (x *Int64) Add(delta int64) (new int64) { return AddInt64(&x.v, delta) }
func (x *Int64) And(mask int64) (old int64)  { return AndInt64(&x.v, mask) }
func (x *Int64) Or(mask int64) (old int64)   { return OrInt64(&x.v, mask) }
func (x *Int64) CompareAndSwap(old, new int64) (swapped bool) {
	return CompareAndSwapInt64(&x.v, old, new)
}

func SwapUint32(addr *uint32, new uint32) (old uint32)  { old = *addr; *addr = new; return old }
func AddUint32(addr *uint32, delta uint32) (new uint32) { *addr += delta; return *addr }
func AndUint32(addr *uint32, mask uint32) (old uint32)  { old = *addr; *addr = old & mask; return old }
func OrUint32(addr *uint32, mask uint32) (old uint32)   { old = *addr; *addr = old | mask; return old }
func LoadUint32(addr *uint32) (val uint32)              { return *addr }
func StoreUint32(addr *uint32, val uint32)              { *addr = val }

func CompareAndSwapUint32(addr *uint32, old, new uint32) (swapped bool) {
	if *addr != old {
		return false
	}
	*addr = new
	return true
}

// A Uint32 is an atomic uint32. Its zero value is zero.
type Uint32 struct{ v uint32 }

func (x *Uint32) Load() uint32                  { return LoadUint32(&x.v) }
func (x *Uint32) Store(val uint32)              { StoreUint32(&x.v, val) }
func (x *Uint32) Swap(new uint32) (old uint32)  { return SwapUint32(&x.v, new) }
func (x *Uint32) Add(delta uint32) (new uint32) { return AddUint32(&x.v, delta) }
func (x *Uint32) And(mask uint32) (old uint32)  { return AndUint32(&x.v, mask) }
func (x *Uint32) Or(mask uint32) (old uint32)   { return OrUint32(&x.v, mask) }
func (x *Uint32) CompareAndSwap(old, new uint32) (swapped bool) {
	return CompareAndSwapUint32(&x.v, old, new)
}

func SwapUint64(addr *uint64, new uint64) (old uint64)  { old = *addr; *addr = new; return old }
func AddUint64(addr *uint64, delta uint64) (new uint64) { *addr += delta; return *addr }
func AndUint64(addr *uint64, mask uint64) (old uint64)  { old = *addr; *addr = old & mask; return old }
func OrUint64(addr *uint64, mask uint64) (old uint64)   { old = *addr; *addr = old | mask; return old }
func LoadUint64(addr *uint64) (val uint64)              { return *addr }
func StoreUint64(addr *uint64, val uint64)              { *addr = val }

func CompareAndSwapUint64(addr *uint64, old, new uint64) (swapped bool) {
	if *addr != old {
		return false
	}
	*addr = new
	return true
}

// A Uint64 is an atomic uint64. Its zero value is zero.
type Uint64 struct{ v uint64 }

func (x *Uint64) Load() uint64                  { return LoadUint64(&x.v) }
func (x *Uint64) Store(val uint64)              { StoreUint64(&x.v, val) }
func (x *Uint64) Swap(new uint64) (old uint64)  { return SwapUint64(&x.v, new) }
func (x *Uint64) Add(delta uint64) (new uint64) { return AddUint64(&x.v, delta) }
func (x *Uint64) And(mask uint64) (old uint64)  { return AndUint64(&x.v, mask) }
func (x *Uint64) Or(mask uint64) (old uint64)   { return OrUint64(&x.v, mask) }
func (x *Uint64) CompareAndSwap(old, new uint64) (swapped bool) {
	return CompareAndSwapUint64(&x.v, old, new)
}

func SwapUintptr(addr *uintptr, new uintptr) (old uintptr)  { old = *addr; *addr = new; return old }
func AddUintptr(addr *uintptr, delta uintptr) (new uintptr) { *addr += delta; return *addr }
func AndUintptr(addr *uintptr, mask uintptr) (old uintptr) {
	old = *addr
	*addr = old & mask
	return old
}
func OrUintptr(addr *uintptr, mask uintptr) (old uintptr) {
	old = *addr
	*addr = old | mask
	return old
}
func LoadUintptr(addr *uintptr) (val uintptr) { return *addr }
func StoreUintptr(addr *uintptr, val uintptr) { *addr = val }

func CompareAndSwapUintptr(addr *uintptr, old, new uintptr) (swapped bool) {
	if *addr != old {
		return false
	}
	*addr = new
	return true
}

// A Uintptr is an atomic uintptr. Its zero value is zero.
type Uintptr struct{ v uintptr }

func (x *Uintptr) Load() uintptr                   { return LoadUintptr(&x.v) }
func (x *Uintptr) Store(val uintptr)               { StoreUintptr(&x.v, val) }
func (x *Uintptr) Swap(new uintptr) (old uintptr)  { return SwapUintptr(&x.v, new) }
func (x *Uintptr) Add(delta uintptr) (new uintptr) { return AddUintptr(&x.v, delta) }
func (x *Uintptr) And(mask uintptr) (old uintptr)  { return AndUintptr(&x.v, mask) }
func (x *Uintptr) Or(mask uintptr) (old uintptr)   { return OrUintptr(&x.v, mask) }
func (x *Uintptr) CompareAndSwap(old, new uintptr) (swapped bool) {
	return CompareAndSwapUintptr(&x.v, old, new)
}

// A Bool is an atomic bool. Its zero value is false.
type Bool struct{ v bool }

func (x *Bool) Load() bool               { return x.v }
func (x *Bool) Store(val bool)           { x.v = val }
func (x *Bool) Swap(new bool) (old bool) { old = x.v; x.v = new; return old }

func (x *Bool) CompareAndSwap(old, new bool) (swapped bool) {
	if x.v != old {
		return false
	}
	x.v = new
	return true
}

// A Pointer is an atomic pointer of type *T. The zero value is a nil *T.
type Pointer[T any] struct {
	// Mention *T in a field to disallow conversion between Pointer types,
	// as gc's does.
	_ [0]*T
	v unsafe.Pointer
}

func (x *Pointer[T]) Load() *T     { return (*T)(x.v) }
func (x *Pointer[T]) Store(val *T) { x.v = unsafe.Pointer(val) }
func (x *Pointer[T]) Swap(new *T) (old *T) {
	old = (*T)(x.v)
	x.v = unsafe.Pointer(new)
	return old
}
func (x *Pointer[T]) CompareAndSwap(old, new *T) (swapped bool) {
	if x.v != unsafe.Pointer(old) {
		return false
	}
	x.v = unsafe.Pointer(new)
	return true
}

// A Value provides an atomic load and store of a consistently typed value.
// The zero Value returns nil from Load. Its methods panic as gc's do: a nil
// value, or a value of another dynamic type than the first stored.
type Value struct {
	v any
}

// efaceWords is an empty interface's words: its type descriptor and data.
type efaceWords struct {
	typ  unsafe.Pointer
	data unsafe.Pointer
}

// typeOf returns the type word of the empty interface *p.
func typeOf(p *any) unsafe.Pointer { return (*efaceWords)(unsafe.Pointer(p)).typ }

func (v *Value) Load() (val any) { return v.v }

func (v *Value) Store(val any) {
	if val == nil {
		panic("sync/atomic: store of nil value into Value")
	}
	if v.v != nil && typeOf(&v.v) != typeOf(&val) {
		panic("sync/atomic: store of inconsistently typed value into Value")
	}
	v.v = val
}

func (v *Value) Swap(new any) (old any) {
	if new == nil {
		panic("sync/atomic: swap of nil value into Value")
	}
	if v.v != nil && typeOf(&v.v) != typeOf(&new) {
		panic("sync/atomic: swap of inconsistently typed value into Value")
	}
	old, v.v = v.v, new
	return old
}

// CompareAndSwap compares with ==, so a value of a type that is not
// comparable panics as gc's does.
func (v *Value) CompareAndSwap(old, new any) (swapped bool) {
	if new == nil {
		panic("sync/atomic: compare and swap of nil value into Value")
	}
	if old != nil && typeOf(&new) != typeOf(&old) {
		panic("sync/atomic: compare and swap of inconsistently typed values")
	}
	if v.v == nil {
		if old != nil {
			return false
		}
		v.v = new
		return true
	}
	if typeOf(&v.v) != typeOf(&new) {
		panic("sync/atomic: compare and swap of inconsistently typed value into Value")
	}
	if v.v != old {
		return false
	}
	v.v = new
	return true
}
