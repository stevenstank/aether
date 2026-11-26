package reflect

// Values for encoding/json, encoding/xml, and encoding/gob (v2
// evaluation): addresses, slices, growth, zero values, equality, overflow
// tests, nested fields, and generic assertions. A new array's capacity
// grows as Aether's append grows it (lower/slice.go): to the largest of
// the length needed, twice the capacity, and 4.

import "unsafe"

func (v Value) mustBeExported(method string) {
	if v.flag&flagRO != 0 {
		panic("reflect: " + method + " using value obtained using unexported field")
	}
}

// Addr returns a pointer value representing the address of v.
func (v Value) Addr() Value {
	if v.flag&flagAddr == 0 {
		panic("reflect.Value.Addr of unaddressable value")
	}
	p := new(unsafe.Pointer)
	*p = v.ptr
	return Value{typ: v.typ.ptrTyp(), ptr: unsafe.Pointer(p), flag: v.flag & flagRO}
}

// NumMethod returns the number of methods in the value's method set: an
// interface's methods, or another type's exported methods.
func (v Value) NumMethod() int {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.NumMethod", Invalid})
	}
	return v.typ.nmethod
}

// FieldByIndex returns the nested field corresponding to index, following
// pointers to embedded structs; it panics on a nil one.
func (v Value) FieldByIndex(index []int) Value {
	if len(index) == 1 {
		return v.Field(index[0])
	}
	v.mustBe(Struct, "FieldByIndex")
	for i, x := range index {
		if i > 0 {
			if v.Kind() == Pointer && v.typ.elem.kind == Struct {
				if v.IsNil() {
					panic("reflect: indirection through nil pointer to embedded struct")
				}
				v = v.Elem()
			}
		}
		v = v.Field(x)
	}
	return v
}

// slice returns the words of a slice value.
func (v Value) slice() *sliceHeader { return (*sliceHeader)(v.ptr) }

// Slice returns v[i:j] of a slice, string, or addressable array.
func (v Value) Slice(i, j int) Value {
	var base unsafe.Pointer
	var cap int
	var typ *rtype
	switch v.Kind() {
	case Array:
		if v.flag&flagAddr == 0 {
			panic("reflect.Value.Slice: slice of unaddressable array")
		}
		base, cap, typ = v.ptr, v.typ.n, v.typ.sliceTyp()
	case Slice:
		s := v.slice()
		base, cap, typ = s.data, s.cap, v.typ
	case String:
		s := *(*string)(v.ptr)
		if i < 0 || j < i || j > len(s) {
			panic("reflect.Value.Slice: string slice index out of bounds")
		}
		t := new(string)
		*t = s[i:j]
		return Value{typ: v.typ, ptr: unsafe.Pointer(t), flag: v.flag & flagRO}
	default:
		panic(&ValueError{"reflect.Value.Slice", v.Kind()})
	}
	if i < 0 || j < i || j > cap {
		panic("reflect.Value.Slice: slice index out of bounds")
	}
	h := &sliceHeader{len: j - i, cap: cap - i}
	if cap-i > 0 {
		h.data = unsafe.Add(base, uintptr(i)*typ.elem.size)
	} else {
		h.data = base
	}
	return Value{typ: typ, ptr: unsafe.Pointer(h), flag: v.flag & flagRO}
}

// sliceTyp returns the descriptor of the slice type of an array type's
// elements: one made at run time, which reflect describes in full.
func (t *rtype) sliceTyp() *rtype {
	if s, ok := madeSlices[t.elem]; ok {
		return s
	}
	s := &rtype{str: "[]" + t.elem.str, kind: Slice, size: unsafe.Sizeof(sliceHeader{}), elem: t.elem}
	if madeSlices == nil {
		madeSlices = map[*rtype]*rtype{}
	}
	madeSlices[t.elem] = s
	return s
}

var madeSlices map[*rtype]*rtype

// SetLen sets v's length to n. It panics if v is not an assignable slice
// or n is out of range.
func (v Value) SetLen(n int) {
	v.mustBeAssignable("reflect.Value.SetLen")
	v.mustBe(Slice, "SetLen")
	s := v.slice()
	if uint(n) > uint(s.cap) {
		panic("reflect: slice length out of range in SetLen")
	}
	s.len = n
}

// SetCap sets v's capacity to n.
func (v Value) SetCap(n int) {
	v.mustBeAssignable("reflect.Value.SetCap")
	v.mustBe(Slice, "SetCap")
	s := v.slice()
	if n < s.len || n > s.cap {
		panic("reflect: slice capacity out of range in SetCap")
	}
	s.cap = n
}

// Grow increases the slice's capacity, if necessary, to guarantee space
// for another n elements.
func (v Value) Grow(n int) {
	v.mustBeAssignable("reflect.Value.Grow")
	v.mustBe(Slice, "Grow")
	if n < 0 {
		panic("reflect.Value.Grow: negative len")
	}
	s := v.slice()
	if s.len+n > s.cap {
		*s = grown(v.typ.elem, *s, s.len+n)
	}
}

// grown returns s moved to a new array of at least need elements, as
// Aether's append grows one.
func grown(et *rtype, s sliceHeader, need int) sliceHeader {
	if need < s.len {
		panic("reflect.Value.Grow: slice overflow")
	}
	ncap := max(need, 2*s.cap)
	if et.size > 0 {
		ncap = max(ncap, 4)
	}
	if et.size != 0 && uintptr(ncap) > (1<<47)/et.size {
		panic("reflect.Value.Grow: slice overflow")
	}
	words := make([]unsafe.Pointer, (et.size*uintptr(ncap)+7)/8+1)
	p := unsafe.Pointer(&words[0])
	if s.len > 0 {
		copy(unsafe.Slice((*byte)(p), uintptr(s.len)*et.size), unsafe.Slice((*byte)(s.data), uintptr(s.len)*et.size))
	}
	return sliceHeader{p, s.len, ncap}
}

// Append appends the values x to a slice s and returns the resulting
// slice.
func Append(s Value, x ...Value) Value {
	s.mustBe(Slice, "Append")
	n := s.Len()
	s = s.extendSlice(len(x))
	for i, v := range x {
		s.Index(n + i).Set(v)
	}
	return s
}

// AppendSlice appends a slice t to a slice s and returns the resulting
// slice.
func AppendSlice(s, t Value) Value {
	s.mustBe(Slice, "AppendSlice")
	t.mustBe(Slice, "AppendSlice")
	if s.typ.elem != t.typ.elem {
		panic("reflect.AppendSlice: " + s.typ.str + " != " + t.typ.str)
	}
	ns := s.Len()
	nt := t.Len()
	s = s.extendSlice(nt)
	Copy(s.Slice(ns, ns+nt), t)
	return s
}

// extendSlice returns a copy of the slice v, n elements longer, in a new
// header: its array grown if needed.
func (v Value) extendSlice(n int) Value {
	h := *v.slice()
	if h.len+n > h.cap {
		h = grown(v.typ.elem, h, h.len+n)
	}
	h.len += n
	return Value{typ: v.typ, ptr: unsafe.Pointer(&h), flag: v.flag & flagRO}
}

// Copy copies the contents of src into dst until either dst has been
// filled or src has been exhausted, and returns the number of elements
// copied.
func Copy(dst, src Value) int {
	dk := dst.Kind()
	if dk != Array && dk != Slice {
		panic(&ValueError{"reflect.Copy", dk})
	}
	if dk == Array && dst.flag&flagAddr == 0 {
		panic("reflect.Copy: unaddressable array value")
	}
	dst.mustBeExported("reflect.Copy")
	sk := src.Kind()
	var stringCopy bool
	if sk != Array && sk != Slice {
		stringCopy = sk == String && dst.typ.elem.kind == Uint8
		if !stringCopy {
			panic(&ValueError{"reflect.Copy", sk})
		}
	}
	src.mustBeExported("reflect.Copy")
	de := dst.typ.elem
	if !stringCopy && de != src.typ.elem {
		panic("reflect.Copy: " + de.str + " != " + src.typ.elem.str)
	}
	dp, dn := dst.arrayOf()
	var sp unsafe.Pointer
	var sn int
	if stringCopy {
		s := *(*string)(src.ptr)
		sn = len(s)
		if sn > 0 {
			sp = unsafe.Pointer(unsafe.StringData(s))
		}
	} else {
		sp, sn = src.arrayOf()
	}
	n := min(dn, sn)
	if n > 0 && de.size > 0 {
		copy(unsafe.Slice((*byte)(dp), uintptr(n)*de.size), unsafe.Slice((*byte)(sp), uintptr(n)*de.size))
	}
	return n
}

// arrayOf returns the address and length of the elements of an array or
// slice value.
func (v Value) arrayOf() (unsafe.Pointer, int) {
	if v.Kind() == Array {
		return v.ptr, v.typ.n
	}
	s := v.slice()
	return s.data, s.len
}

// SetBytes sets v's underlying value, a slice of bytes.
func (v Value) SetBytes(x []byte) {
	v.mustBeAssignable("reflect.Value.SetBytes")
	v.mustBe(Slice, "SetBytes")
	if v.typ.elem.kind != Uint8 {
		panic("reflect.Value.SetBytes of non-byte slice")
	}
	*(*[]byte)(v.ptr) = x
}

// SetZero sets v to be the zero value of v's type.
func (v Value) SetZero() {
	v.mustBeAssignable("reflect.Value.SetZero")
	clear(unsafe.Slice((*byte)(v.ptr), v.typ.size))
}

// IsZero reports whether v is the zero value for its type, as gc's: a
// float's or complex's zero is +0, a struct's or array's every field or
// element is.
func (v Value) IsZero() bool {
	switch v.Kind() {
	case Bool:
		return !v.Bool()
	case Int, Int8, Int16, Int32, Int64:
		return v.Int() == 0
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return v.Uint() == 0
	case Float32, Float64, Complex64, Complex128:
		for _, b := range unsafe.Slice((*byte)(v.ptr), v.typ.size) {
			if b != 0 {
				return false
			}
		}
		return true
	case Array:
		for i := range v.typ.n {
			if !v.Index(i).IsZero() {
				return false
			}
		}
		return true
	case Chan, Func, Map, Pointer, Slice, UnsafePointer:
		return v.word() == nil
	case Interface:
		return v.IsNil()
	case String:
		return v.Len() == 0
	case Struct:
		for i := range v.typ.n {
			if !v.Field(i).IsZero() {
				return false
			}
		}
		return true
	}
	panic(&ValueError{"reflect.Value.IsZero", v.Kind()})
}

// Equal reports true if v is equal to u, as gc's: for two invalid values,
// Equal reports true; values of different types are not equal.
func (v Value) Equal(u Value) bool {
	if v.Kind() == Interface {
		v = v.Elem()
	}
	if u.Kind() == Interface {
		u = u.Elem()
	}
	if !v.IsValid() || !u.IsValid() {
		return v.IsValid() == u.IsValid()
	}
	if v.Kind() != u.Kind() || v.typ != u.typ {
		return false
	}
	switch v.Kind() {
	case Bool:
		return v.Bool() == u.Bool()
	case Int, Int8, Int16, Int32, Int64:
		return v.Int() == u.Int()
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return v.Uint() == u.Uint()
	case Float32, Float64:
		return v.Float() == u.Float()
	case Complex64, Complex128:
		return v.Complex() == u.Complex()
	case String:
		return v.String() == u.String()
	case Chan, Pointer, UnsafePointer:
		return v.Pointer() == u.Pointer()
	case Array:
		for i := range v.typ.n {
			if !v.Index(i).Equal(u.Index(i)) {
				return false
			}
		}
		return true
	case Struct:
		for i := range v.typ.n {
			if !v.Field(i).Equal(u.Field(i)) {
				return false
			}
		}
		return true
	}
	panic("reflect.Value.Equal: values of type " + v.typ.str + " are not comparable")
}

// OverflowInt reports whether the int64 x cannot be represented by v's
// type.
func (v Value) OverflowInt(x int64) bool {
	switch v.Kind() {
	case Int, Int8, Int16, Int32, Int64:
		return v.typ.OverflowInt(x)
	}
	panic(&ValueError{"reflect.Value.OverflowInt", v.Kind()})
}

// OverflowUint reports whether the uint64 x cannot be represented by v's
// type.
func (v Value) OverflowUint(x uint64) bool {
	switch v.Kind() {
	case Uint, Uintptr, Uint8, Uint16, Uint32, Uint64:
		return v.typ.OverflowUint(x)
	}
	panic(&ValueError{"reflect.Value.OverflowUint", v.Kind()})
}

// OverflowFloat reports whether the float64 x cannot be represented by
// v's type.
func (v Value) OverflowFloat(x float64) bool {
	switch v.Kind() {
	case Float32, Float64:
		return v.typ.OverflowFloat(x)
	}
	panic(&ValueError{"reflect.Value.OverflowFloat", v.Kind()})
}

// TypeAssert is semantically equivalent to v2, ok := v.Interface().(T).
// Its instances are compiled in the packages using them (D-052), so it
// uses only reflect's exported API.
func TypeAssert[T any](v Value) (T, bool) {
	if !v.IsValid() {
		panic(&ValueError{"reflect.TypeAssert", Invalid})
	}
	if !v.CanInterface() {
		panic("reflect.TypeAssert: cannot return value obtained from unexported field or method")
	}
	x, ok := v.Interface().(T)
	return x, ok
}
