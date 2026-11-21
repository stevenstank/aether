// Package reflect is Aether's replacement of gc's reflect (D-049,
// increment 11.8): the part fmt uses, over Aether's type descriptors. A
// program with package reflect has full descriptors (Aether's
// internal/lower/reflect.go), which rtype reads. The functions without a
// body are not supported.
package reflect

import (
	"strconv"
	"unsafe"
)

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
	return "kind" + strconv.Itoa(int(k))
}

// Type is the representation of a Go type.
type Type interface {
	String() string
	Kind() Kind
	Size() uintptr
	Bits() int
	Elem() Type
	Key() Type
	Len() int
	NumField() int
	Field(i int) StructField
	Comparable() bool
}

// rtype is a type descriptor, as Aether lays it out.
type rtype struct {
	str    string
	equal  unsafe.Pointer
	hash   unsafe.Pointer
	kind   Kind
	size   uintptr
	elem   *rtype
	key    *rtype
	n      int // an array's length, a struct's fields, an interface's methods
	fields unsafe.Pointer
	esize  uintptr // a map entry's size
	voff   uintptr // a map element's offset in its entry
}

// fieldRec is a struct field's record in its type's descriptor.
type fieldRec struct {
	name    string
	typ     *rtype
	offset  uintptr
	flags   int    // 1 exported, 2 embedded
	pkgPath string // of an unexported field
}

func (t *rtype) String() string   { return t.str }
func (t *rtype) Kind() Kind       { return t.kind }
func (t *rtype) Size() uintptr    { return t.size }
func (t *rtype) Comparable() bool { return t.equal != nil }

// Bits returns the size of the type in bits. It panics if the type's Kind
// is not one of the sized or unsized Int, Uint, Float, or Complex kinds.
func (t *rtype) Bits() int {
	if t.kind < Int || t.kind > Complex128 {
		panic("reflect: Bits of non-arithmetic Type " + t.str)
	}
	return int(t.size) * 8
}

// Elem returns a type's element type. It panics if the type's Kind is
// not Array, Chan, Map, Pointer, or Slice.
func (t *rtype) Elem() Type {
	if t.elem == nil {
		panic("reflect: Elem of invalid type " + t.str)
	}
	return t.elem
}

// Key returns a map type's key type.
func (t *rtype) Key() Type {
	if t.kind != Map {
		panic("reflect: Key of non-map type " + t.str)
	}
	return t.key
}

// Len returns an array type's length.
func (t *rtype) Len() int {
	if t.kind != Array {
		panic("reflect: Len of non-array type " + t.str)
	}
	return t.n
}

// NumField returns a struct type's field count.
func (t *rtype) NumField() int {
	if t.kind != Struct {
		panic("reflect: NumField of non-struct type " + t.str)
	}
	return t.n
}

// A StructField describes a single field in a struct.
type StructField struct {
	Name      string
	PkgPath   string // the package path of an unexported field; empty for an exported one
	Type      Type
	Offset    uintptr
	Index     []int
	Anonymous bool
}

// IsExported reports whether the field is exported.
func (f StructField) IsExported() bool { return f.PkgPath == "" }

// Field returns a struct type's i'th field.
func (t *rtype) Field(i int) StructField {
	r := t.field(i)
	f := StructField{Name: r.name, Type: r.typ, Offset: r.offset, Index: []int{i}, Anonymous: r.flags&2 != 0}
	if r.flags&1 == 0 {
		f.PkgPath = r.pkgPath
	}
	return f
}

// field returns the record of a struct type's i'th field.
func (t *rtype) field(i int) *fieldRec {
	if t.kind != Struct {
		panic("reflect: Field of non-struct type " + t.str)
	}
	if i < 0 || i >= t.n {
		panic("reflect: Field index out of bounds")
	}
	return (*fieldRec)(unsafe.Add(t.fields, uintptr(i)*unsafe.Sizeof(fieldRec{})))
}

// TypeOf returns the dynamic type of i, or nil if i is nil.
func TypeOf(i any) Type {
	t := (*[2]*rtype)(unsafe.Pointer(&i))[0]
	if t == nil {
		return nil
	}
	return t
}
