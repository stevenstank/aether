package reflect

import "unsafe"

// Value is the reflection interface to a Go value. Aether's holds the
// address of the value, of type typ, whether it is stored in a variable
// or element (addressable) or in a copy of its own.
type Value struct {
	typ  *rtype
	ptr  unsafe.Pointer
	flag flag
}

type flag int

const (
	flagRO   flag = 1 // obtained through an unexported field
	flagAddr flag = 2 // addressable
)

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
// pointer, rather than the address of a copy (Aether's D-041).
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

func (v Value) mustBe(k Kind, method string) {
	if v.typ == nil || v.typ.kind != k {
		panic(&ValueError{"reflect.Value." + method, v.Kind()})
	}
}

// IsValid reports whether v represents a value.
func (v Value) IsValid() bool { return v.typ != nil }

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
		panic(&ValueError{"reflect.Value.Type", Invalid})
	}
	return v.typ
}

// CanAddr reports whether the value's address can be obtained.
func (v Value) CanAddr() bool { return v.flag&flagAddr != 0 }

// CanInterface reports whether Interface can be used without panicking:
// whether v was not obtained through an unexported field.
func (v Value) CanInterface() bool {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.CanInterface", Invalid})
	}
	return v.flag&flagRO == 0
}

// Bool returns v's underlying value.
func (v Value) Bool() bool {
	v.mustBe(Bool, "Bool")
	return *(*bool)(v.ptr)
}

// Int returns v's underlying value, as an int64.
func (v Value) Int() int64 {
	switch v.Kind() {
	case Int:
		return int64(*(*int)(v.ptr))
	case Int8:
		return int64(*(*int8)(v.ptr))
	case Int16:
		return int64(*(*int16)(v.ptr))
	case Int32:
		return int64(*(*int32)(v.ptr))
	case Int64:
		return *(*int64)(v.ptr)
	}
	panic(&ValueError{"reflect.Value.Int", v.Kind()})
}

// Uint returns v's underlying value, as a uint64.
func (v Value) Uint() uint64 {
	switch v.Kind() {
	case Uint:
		return uint64(*(*uint)(v.ptr))
	case Uint8:
		return uint64(*(*uint8)(v.ptr))
	case Uint16:
		return uint64(*(*uint16)(v.ptr))
	case Uint32:
		return uint64(*(*uint32)(v.ptr))
	case Uint64:
		return *(*uint64)(v.ptr)
	case Uintptr:
		return uint64(*(*uintptr)(v.ptr))
	}
	panic(&ValueError{"reflect.Value.Uint", v.Kind()})
}

// Float returns v's underlying value, as a float64.
func (v Value) Float() float64 {
	switch v.Kind() {
	case Float32:
		return float64(*(*float32)(v.ptr))
	case Float64:
		return *(*float64)(v.ptr)
	}
	panic(&ValueError{"reflect.Value.Float", v.Kind()})
}

// Complex returns v's underlying value, as a complex128.
func (v Value) Complex() complex128 {
	switch v.Kind() {
	case Complex64:
		return complex128(*(*complex64)(v.ptr))
	case Complex128:
		return *(*complex128)(v.ptr)
	}
	panic(&ValueError{"reflect.Value.Complex", v.Kind()})
}

// String returns the string v's underlying value, as a string, or for a
// Value of another kind a string of the form "<T Value>".
func (v Value) String() string {
	switch v.Kind() {
	case Invalid:
		return "<invalid Value>"
	case String:
		return *(*string)(v.ptr)
	}
	return "<" + v.typ.str + " Value>"
}

// Bytes returns v's underlying value, a slice of bytes.
func (v Value) Bytes() []byte {
	v.mustBe(Slice, "Bytes")
	if v.typ.elem.kind != Uint8 {
		panic("reflect.Value.Bytes of non-byte slice")
	}
	return *(*[]byte)(v.ptr)
}

// word returns the word at v's address: a pointer, map, channel, func, or
// slice's array.
func (v Value) word() unsafe.Pointer { return *(*unsafe.Pointer)(v.ptr) }

// Len returns v's length.
func (v Value) Len() int {
	switch v.Kind() {
	case Slice, String:
		return *(*int)(unsafe.Add(v.ptr, 8))
	case Array:
		return v.typ.n
	case Map, Chan:
		// The first word of a map's and a channel's header (D-042,
		// D-048) is its count.
		if h := v.word(); h != nil {
			if v.typ.kind == Chan {
				return *(*int)(unsafe.Add(h, 16))
			}
			return *(*int)(h)
		}
		return 0
	}
	panic(&ValueError{"reflect.Value.Len", v.Kind()})
}

// Cap returns v's capacity.
func (v Value) Cap() int {
	switch v.Kind() {
	case Slice:
		return *(*int)(unsafe.Add(v.ptr, 16))
	case Array:
		return v.typ.n
	case Chan:
		if h := v.word(); h != nil {
			return *(*int)(unsafe.Add(h, 8))
		}
		return 0
	}
	panic(&ValueError{"reflect.Value.Cap", v.Kind()})
}

// Index returns v's i'th element.
func (v Value) Index(i int) Value {
	switch v.Kind() {
	case Slice:
		if uint(i) >= uint(v.Len()) {
			panic("reflect: slice index out of range")
		}
		e := v.typ.elem
		return Value{typ: e, ptr: unsafe.Add(v.word(), uintptr(i)*e.size), flag: v.flag&flagRO | flagAddr}
	case Array:
		if uint(i) >= uint(v.typ.n) {
			panic("reflect: array index out of range")
		}
		e := v.typ.elem
		return Value{typ: e, ptr: unsafe.Add(v.ptr, uintptr(i)*e.size), flag: v.flag}
	case String:
		if uint(i) >= uint(v.Len()) {
			panic("reflect: string index out of range")
		}
		b := new(byte)
		*b = (*(*string)(v.ptr))[i]
		return Value{typ: (*rtype)((*[2]unsafe.Pointer)(unsafe.Pointer(&byteType))[0]), ptr: unsafe.Pointer(b), flag: v.flag & flagRO}
	}
	panic(&ValueError{"reflect.Value.Index", v.Kind()})
}

// byteType holds a byte, for the descriptor of type byte.
var byteType any = byte(0)

// NumField returns the number of fields in the struct v.
func (v Value) NumField() int {
	v.mustBe(Struct, "NumField")
	return v.typ.n
}

// Field returns the i'th field of the struct v.
func (v Value) Field(i int) Value {
	v.mustBe(Struct, "Field")
	r := v.typ.field(i)
	fl := v.flag
	if r.flags&1 == 0 {
		fl |= flagRO
	}
	return Value{typ: r.typ, ptr: unsafe.Add(v.ptr, r.offset), flag: fl}
}

// Elem returns the value that the interface v contains or that the
// pointer v points to, or the zero Value if v is nil.
func (v Value) Elem() Value {
	switch v.Kind() {
	case Interface:
		w := (*[2]unsafe.Pointer)(v.ptr)
		t := (*rtype)(w[0])
		if t != nil && v.typ.n > 0 {
			t = *(**rtype)(w[0]) // an itab's descriptor
		}
		x := fromIface(t, w[1])
		x.flag |= v.flag & flagRO
		return x
	case Pointer:
		p := v.word()
		if p == nil {
			return Value{}
		}
		return Value{typ: v.typ.elem, ptr: p, flag: v.flag&flagRO | flagAddr}
	}
	panic(&ValueError{"reflect.Value.Elem", v.Kind()})
}

// IsNil reports whether v is nil. v must be a chan, func, interface, map,
// pointer, or slice value.
func (v Value) IsNil() bool {
	switch v.Kind() {
	case Chan, Func, Map, Pointer, UnsafePointer, Slice:
		return v.word() == nil
	case Interface:
		return (*[2]unsafe.Pointer)(v.ptr)[0] == nil
	}
	panic(&ValueError{"reflect.Value.IsNil", v.Kind()})
}

// Pointer returns v's value as a uintptr: the address of a pointer,
// channel, map, slice's array, or func's closure.
func (v Value) Pointer() uintptr {
	switch v.Kind() {
	case Chan, Func, Map, Pointer, UnsafePointer, Slice:
		return uintptr(v.word())
	}
	panic(&ValueError{"reflect.Value.Pointer", v.Kind()})
}

// UnsafePointer returns v's value as an unsafe.Pointer.
func (v Value) UnsafePointer() unsafe.Pointer { return unsafe.Pointer(v.Pointer()) }

// Interface returns v's current value as an interface{}.
func (v Value) Interface() any {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.Interface", Invalid})
	}
	if v.flag&flagRO != 0 {
		panic("reflect.Value.Interface: cannot return value obtained from unexported field or method")
	}
	if v.typ.kind == Interface {
		// The interface's own dynamic value, or nil.
		e := v.Elem()
		if !e.IsValid() {
			return nil
		}
		return e.Interface()
	}
	var data unsafe.Pointer
	if v.typ.direct() {
		data = v.word()
	} else {
		// A copy of its own, never written again (D-041), in words the
		// collector scans.
		words := make([]unsafe.Pointer, (v.typ.size+7)/8+1)
		data = unsafe.Pointer(&words[0])
		copy(unsafe.Slice((*byte)(data), v.typ.size), unsafe.Slice((*byte)(v.ptr), v.typ.size))
	}
	var i any
	w := (*[2]unsafe.Pointer)(unsafe.Pointer(&i))
	w[0], w[1] = unsafe.Pointer(v.typ), data
	return i
}

// A MapIter is an iterator for ranging over a map.
type MapIter struct {
	m    Value
	i    int            // the next entry's position
	cur  unsafe.Pointer // the current entry
	done bool
}

// MapRange returns a range iterator for a map.
func (v Value) MapRange() *MapIter {
	v.mustBe(Map, "MapRange")
	return &MapIter{m: v}
}

// Next advances the map iterator and reports whether there is another
// entry. Aether's maps keep their entries in order of insertion, each its
// key's hash, whether it is live, the key, and the element (D-042).
func (it *MapIter) Next() bool {
	h := it.m.word()
	if h == nil || it.done {
		return false
	}
	entries := *(*unsafe.Pointer)(unsafe.Add(h, 8))
	used := *(*int)(unsafe.Add(h, 16))
	for it.i < used {
		e := unsafe.Add(entries, 8+uintptr(it.i)*it.m.typ.esize)
		it.i++
		if *(*bool)(unsafe.Add(e, 8)) {
			it.cur = e
			return true
		}
	}
	it.done = true
	return false
}

// Key returns the key of the iterator's current map entry.
func (it *MapIter) Key() Value {
	return Value{typ: it.m.typ.key, ptr: unsafe.Add(it.cur, 16), flag: it.m.flag & flagRO}
}

// Value returns the value of the iterator's current map entry.
func (it *MapIter) Value() Value {
	return Value{typ: it.m.typ.elem, ptr: unsafe.Add(it.cur, it.m.typ.voff), flag: it.m.flag & flagRO}
}

// mustBeAssignable panics if v cannot be set: it is not addressable or
// was obtained through an unexported field.
func (v Value) mustBeAssignable(method string) {
	if v.flag&flagRO != 0 {
		panic("reflect: " + method + " using value obtained using unexported field")
	}
	if v.flag&flagAddr == 0 {
		panic("reflect: " + method + " using unaddressable value")
	}
}

// Set assigns x to the value v: Aether's assigns a value of v's type, or
// any value to an empty interface.
func (v Value) Set(x Value) {
	v.mustBeAssignable("reflect.Value.Set")
	if x.flag&flagRO != 0 {
		panic("reflect: reflect.Value.Set using value obtained using unexported field")
	}
	switch {
	case x.typ == v.typ:
		copy(unsafe.Slice((*byte)(v.ptr), v.typ.size), unsafe.Slice((*byte)(x.ptr), v.typ.size))
	case v.typ.kind == Interface && v.typ.n == 0:
		*(*any)(v.ptr) = x.Interface()
	default:
		panic("reflect.Set: Aether does not assign a value of type " + x.typ.str + " to type " + v.typ.str)
	}
}

// SetBool sets v's underlying value.
func (v Value) SetBool(x bool) {
	v.mustBeAssignable("reflect.Value.SetBool")
	v.mustBe(Bool, "SetBool")
	*(*bool)(v.ptr) = x
}

// SetInt sets v's underlying value.
func (v Value) SetInt(x int64) {
	v.mustBeAssignable("reflect.Value.SetInt")
	switch v.Kind() {
	case Int:
		*(*int)(v.ptr) = int(x)
	case Int8:
		*(*int8)(v.ptr) = int8(x)
	case Int16:
		*(*int16)(v.ptr) = int16(x)
	case Int32:
		*(*int32)(v.ptr) = int32(x)
	case Int64:
		*(*int64)(v.ptr) = x
	default:
		panic(&ValueError{"reflect.Value.SetInt", v.Kind()})
	}
}

// SetUint sets v's underlying value.
func (v Value) SetUint(x uint64) {
	v.mustBeAssignable("reflect.Value.SetUint")
	switch v.Kind() {
	case Uint:
		*(*uint)(v.ptr) = uint(x)
	case Uint8:
		*(*uint8)(v.ptr) = uint8(x)
	case Uint16:
		*(*uint16)(v.ptr) = uint16(x)
	case Uint32:
		*(*uint32)(v.ptr) = uint32(x)
	case Uint64:
		*(*uint64)(v.ptr) = x
	case Uintptr:
		*(*uintptr)(v.ptr) = uintptr(x)
	default:
		panic(&ValueError{"reflect.Value.SetUint", v.Kind()})
	}
}

// SetFloat sets v's underlying value.
func (v Value) SetFloat(x float64) {
	v.mustBeAssignable("reflect.Value.SetFloat")
	switch v.Kind() {
	case Float32:
		*(*float32)(v.ptr) = float32(x)
	case Float64:
		*(*float64)(v.ptr) = x
	default:
		panic(&ValueError{"reflect.Value.SetFloat", v.Kind()})
	}
}

// SetComplex sets v's underlying value.
func (v Value) SetComplex(x complex128) {
	v.mustBeAssignable("reflect.Value.SetComplex")
	switch v.Kind() {
	case Complex64:
		*(*complex64)(v.ptr) = complex64(x)
	case Complex128:
		*(*complex128)(v.ptr) = x
	default:
		panic(&ValueError{"reflect.Value.SetComplex", v.Kind()})
	}
}

// SetString sets v's underlying value.
func (v Value) SetString(x string) {
	v.mustBeAssignable("reflect.Value.SetString")
	v.mustBe(String, "SetString")
	*(*string)(v.ptr) = x
}

// MakeSlice creates a new zero-initialized slice value for the specified
// slice type, length, and capacity.
func MakeSlice(typ Type, len, cap int) Value
