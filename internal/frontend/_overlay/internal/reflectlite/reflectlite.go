// Package reflectlite is Aether's replacement of gc's internal/reflectlite
// (D-049): the part sort uses, over Aether's type descriptors, which a
// program with this package has in full, as one with package reflect
// (Aether's internal/lower/reflect.go).
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

// rtype is the start of a type descriptor, as Aether lays it out (the
// overlay's reflect/type.go).
type rtype struct {
	str   string
	equal unsafe.Pointer
	hash  unsafe.Pointer
	kind  Kind
	size  uintptr
	elem  *rtype
	key   *rtype
	n     int // an array's length
}

// Value is the reflection interface to a Go value: its type, and the
// address of the value, as an interface holding it gives it (D-041).
type Value struct {
	typ *rtype
	ptr unsafe.Pointer
}

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

// ValueOf returns a new Value initialized to the concrete value stored in
// i. ValueOf(nil) returns the zero Value. Only a slice's, a string's, and
// an array's are read, whose interfaces hold the address of a copy.
func ValueOf(i any) Value {
	w := (*[2]unsafe.Pointer)(unsafe.Pointer(&i))
	return Value{typ: (*rtype)(w[0]), ptr: w[1]}
}

// Kind returns v's Kind, or Invalid for the zero Value.
func (v Value) Kind() Kind {
	if v.typ == nil {
		return Invalid
	}
	return v.typ.kind
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
