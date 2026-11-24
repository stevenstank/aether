// Package reflectlite is Aether's replacement of gc's internal/reflectlite
// (D-049): the part sort, errors, and context use, over Aether's type
// descriptors, which a program with this package has in full, as one with
// package reflect (Aether's internal/lower/reflect.go). Type.Implements,
// Type.AssignableTo, and Value.Set read the words D-050 adds.
package reflectlite

import "unsafe"

// A Kind represents the specific kind of type that a Type represents.
type Kind uint

const (
	Invalid Kind = iota
	Bool
	Int
	Int8
	Int16
	Int32
	Int64
	Uint
	Uint8
	Uint16
	Uint32
	Uint64
	Uintptr
	Float32
	Float64
	Complex64
	Complex128
	Array
	Chan
	Func
	Interface
	Map
	Pointer
	Slice
	String
	Struct
	UnsafePointer
)

// Ptr is the old name for the Pointer kind.
const Ptr = Pointer

var kindNames = []string{
	Invalid: "invalid", Bool: "bool", Int: "int", Int8: "int8", Int16: "int16", Int32: "int32", Int64: "int64",
	Uint: "uint", Uint8: "uint8", Uint16: "uint16", Uint32: "uint32", Uint64: "uint64", Uintptr: "uintptr",
	Float32: "float32", Float64: "float64", Complex64: "complex64", Complex128: "complex128",
	Array: "array", Chan: "chan", Func: "func", Interface: "interface", Map: "map", Pointer: "ptr",
	Slice: "slice", String: "string", Struct: "struct", UnsafePointer: "unsafe.Pointer",
}

// String returns the name of k.
func (k Kind) String() string {
	if uint(k) < uint(len(kindNames)) {
		return kindNames[uint(k)]
	}
	return "kind" + itoa(int(k))
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		if n /= 10; n == 0 {
			return string(b[i:])
		}
	}
}

// rtype is a type descriptor, as Aether lays it out (internal/lower's
// reflect.go).
type rtype struct {
	str    string
	equal  unsafe.Pointer
	hash   unsafe.Pointer
	kind   Kind
	size   uintptr
	elem   *rtype
	key    *rtype
	n      int // an array's length, an interface's methods, a channel's direction
	fields unsafe.Pointer
	esize  uintptr
	voff   uintptr
	find   unsafe.Pointer // a non-empty interface's .findK (D-050)
	under  *rtype         // a defined type's underlying type, or nil (D-050)
}

// Type is the representation of a Go type.
type Type interface {
	// String returns a string representation of the type.
	String() string
	// Kind returns the specific kind of this type.
	Kind() Kind
	// Size returns the number of bytes needed to store a value of the
	// given type.
	Size() uintptr
	// Comparable reports whether values of this type are comparable.
	Comparable() bool
	// Elem returns a type's element type. It panics if the type's Kind
	// is not Array, Chan, Map, Pointer, or Slice.
	Elem() Type
	// Implements reports whether the type implements the interface type
	// u.
	Implements(u Type) bool
	// AssignableTo reports whether a value of the type is assignable to
	// type u.
	AssignableTo(u Type) bool
}

// TypeOf returns the reflection Type that represents the dynamic type of
// i. If i is a nil interface value, TypeOf returns nil.
func TypeOf(i any) Type {
	t := (*[2]*rtype)(unsafe.Pointer(&i))[0]
	if t == nil {
		return nil
	}
	return t
}

func (t *rtype) String() string   { return t.str }
func (t *rtype) Kind() Kind       { return t.kind }
func (t *rtype) Size() uintptr    { return t.size }
func (t *rtype) Comparable() bool { return t.equal != nil }

func (t *rtype) Elem() Type {
	switch t.kind {
	case Array, Chan, Map, Pointer, Slice:
		return t.elem
	}
	panic("reflect: Elem of invalid type " + t.str)
}

func (t *rtype) Implements(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.Implements")
	}
	if u.Kind() != Interface {
		panic("reflect: non-interface type passed to Type.Implements")
	}
	return implementsType(u.(*rtype), t)
}

func (t *rtype) AssignableTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.AssignableTo")
	}
	uu := u.(*rtype)
	return directlyAssignable(uu, t) || implementsType(uu, t)
}

// implementsType reports whether the type V implements the interface type
// T, as gc's implements: every type implements an empty interface; a
// non-interface type, by T's .findK, which lists every type of the program
// that implements T. Aether's descriptors do not give an interface's
// methods, so whether an interface type implements another is not decided:
// gc's packages that use reflectlite never ask it.
func implementsType(T, V *rtype) bool {
	if T.kind != Interface {
		return false
	}
	if T.n == 0 {
		return true
	}
	if V.kind == Interface {
		panic("reflectlite: Aether does not decide whether interface type " + V.str + " implements " + T.str)
	}
	return implements(unsafe.Pointer(T), unsafe.Pointer(V)) != nil
}

// implements returns the itab of the type t and the non-empty interface
// type iface, or nil if t does not implement iface. It is an intrinsic.
func implements(iface, t unsafe.Pointer) unsafe.Pointer

// directlyAssignable reports whether a value of type V can be assigned to
// type T directly, as gc's: the types are identical, or one is not a
// defined type and their underlying types are identical, which their
// descriptors decide, since Aether makes one descriptor of identical
// types; or V is a bidirectional channel type and T a channel type of the
// same element type.
func directlyAssignable(T, V *rtype) bool {
	if T == V {
		return true
	}
	if T.under != nil && V.under != nil || T.kind != V.kind {
		return false
	}
	ut, uv := T, V
	if T.under != nil {
		ut = T.under
	}
	if V.under != nil {
		uv = V.under
	}
	if ut == uv {
		return true
	}
	return T.kind == Chan && uv.n == 3 && ut.elem == uv.elem
}

// Value is the reflection interface to a Go value. Aether's holds the
// address of the value, of type typ, whether it is stored in a variable or
// element (addressable) or in a copy of its own, as reflect's.
type Value struct {
	typ  *rtype
	ptr  unsafe.Pointer
	flag flag
}

type flag int

const flagAddr flag = 2 // addressable

// A ValueError occurs when a Value method is invoked on a Value that does
// not support it.
type ValueError struct {
	Method string
	Kind   Kind
}

func (e *ValueError) Error() string {
	if e.Kind == 0 {
		return "reflect: call of " + e.Method + " on zero Value"
	}
	return "reflect: call of " + e.Method + " on " + e.Kind.String() + " Value"
}

// direct reports whether an interface holds a value of t itself, a
// pointer, rather than the address of a copy (D-041).
func (t *rtype) direct() bool { return t.kind == Pointer || t.kind == UnsafePointer }

// ValueOf returns a new Value initialized to the concrete value stored in
// i. ValueOf(nil) returns the zero Value.
func ValueOf(i any) Value {
	w := (*[2]unsafe.Pointer)(unsafe.Pointer(&i))
	return fromIface((*rtype)(w[0]), w[1])
}

// fromIface returns the Value of an interface's dynamic type t and data.
func fromIface(t *rtype, data unsafe.Pointer) Value {
	if t == nil {
		return Value{}
	}
	if t.direct() {
		p := new(unsafe.Pointer)
		*p = data
		return Value{typ: t, ptr: unsafe.Pointer(p)}
	}
	return Value{typ: t, ptr: data}
}

// Kind returns v's Kind, or Invalid for the zero Value.
func (v Value) Kind() Kind {
	if v.typ == nil {
		return Invalid
	}
	return v.typ.kind
}

// Type returns v's type.
func (v Value) Type() Type {
	if v.typ == nil {
		panic(&ValueError{"reflectlite.Value.Type", Invalid})
	}
	return v.typ
}

func (v Value) word() unsafe.Pointer { return *(*unsafe.Pointer)(v.ptr) }

// IsNil reports whether its argument v is nil. The argument must be a
// chan, func, interface, map, pointer, or slice value.
func (v Value) IsNil() bool {
	switch v.Kind() {
	case Chan, Func, Map, Pointer, UnsafePointer, Slice:
		return v.word() == nil
	case Interface:
		return v.word() == nil // the tab word
	}
	panic(&ValueError{"reflectlite.Value.IsNil", v.Kind()})
}

// Elem returns the value that the interface v contains or that the
// pointer v points to. It panics if v's Kind is not Interface or Pointer.
// It returns the zero Value if v is nil.
func (v Value) Elem() Value {
	switch v.Kind() {
	case Interface:
		w := (*[2]unsafe.Pointer)(v.ptr)
		t := (*rtype)(w[0])
		if t != nil && v.typ.n > 0 {
			t = *(**rtype)(w[0]) // an itab's descriptor
		}
		return fromIface(t, w[1])
	case Pointer:
		p := v.word()
		if p == nil {
			return Value{}
		}
		return Value{typ: v.typ.elem, ptr: p, flag: flagAddr}
	}
	panic(&ValueError{"reflectlite.Value.Elem", v.Kind()})
}

// Set assigns x to the value v. It panics if v is not addressable, or if x
// is not assignable to v's type, with gc's messages.
func (v Value) Set(x Value) {
	if v.flag&flagAddr == 0 {
		if v.typ == nil {
			panic(&ValueError{"reflectlite.Value.Set", Invalid})
		}
		panic("reflect: reflectlite.Value.Set using unaddressable value")
	}
	if x.typ == nil {
		panic(&ValueError{"reflectlite.Value.Set", Invalid})
	}
	T, V := v.typ, x.typ
	switch {
	case directlyAssignable(T, V):
		if V.direct() {
			*(*unsafe.Pointer)(v.ptr) = x.word()
		} else {
			copy(unsafe.Slice((*byte)(v.ptr), T.size), unsafe.Slice((*byte)(x.ptr), T.size))
		}
	case implementsType(T, V):
		w := (*[2]unsafe.Pointer)(v.ptr)
		data := x.ptr
		if V.direct() {
			data = x.word()
		}
		if V.kind == Interface {
			// An interface value of a type that implements T: its
			// dynamic type's (gc's valueInterface and ifaceE2I).
			iw := (*[2]unsafe.Pointer)(x.ptr)
			if iw[0] == nil {
				w[0], w[1] = nil, nil
				return
			}
			dyn := (*rtype)(iw[0])
			if V.n > 0 {
				dyn = *(**rtype)(iw[0])
			}
			V, data = dyn, iw[1]
		}
		if T.n == 0 {
			w[0] = unsafe.Pointer(V)
		} else {
			w[0] = implements(unsafe.Pointer(T), unsafe.Pointer(V))
		}
		w[1] = data
	default:
		panic("reflectlite.Set: value of type " + V.str + " is not assignable to type " + T.str)
	}
}

// Len returns v's length.
func (v Value) Len() int {
	switch v.Kind() {
	case Slice, String:
		return *(*int)(unsafe.Add(v.ptr, 8))
	case Array:
		return v.typ.n
	}
	panic(&ValueError{"reflect.Value.Len", v.Kind()})
}

// Swapper returns a function that swaps the elements in the provided
// slice.
//
// Swapper panics if the provided interface is not a slice.
func Swapper(slice any) func(i, j int) {
	v := ValueOf(slice)
	if v.Kind() != Slice {
		panic(&ValueError{Method: "Swapper", Kind: v.Kind()})
	}
	// Fast path for slices of size 0 and 1. Nothing to swap.
	n := v.Len()
	switch n {
	case 0:
		return func(i, j int) { panic("reflect: slice index out of range") }
	case 1:
		return func(i, j int) {
			if i != 0 || j != 0 {
				panic("reflect: slice index out of range")
			}
		}
	}
	data := *(*unsafe.Pointer)(v.ptr)
	size := v.typ.elem.size
	// The scratch space, in words the collector scans, as reflect's
	// MakeSlice: an element may hold pointers.
	tmp := unsafe.Slice((*byte)(unsafe.Pointer(&make([]unsafe.Pointer, size/8+1)[0])), size)
	return func(i, j int) {
		if uint(i) >= uint(n) || uint(j) >= uint(n) {
			panic("reflect: slice index out of range")
		}
		a := unsafe.Slice((*byte)(unsafe.Add(data, uintptr(i)*size)), size)
		b := unsafe.Slice((*byte)(unsafe.Add(data, uintptr(j)*size)), size)
		copy(tmp, a)
		copy(a, b)
		copy(b, tmp)
	}
}
