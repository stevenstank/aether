package reflect

// Maps by their descriptors (v2 evaluation). A map is a Ptr to Aether's
// header (D-042); a map type's descriptor holds static closures of its
// access, assign, and delete functions, which take the map and a key's
// address (lower/reflect.go). Access returns the address of the element,
// or of the zero object .mzero for a missing key, which a nil map's access
// gives.

import "unsafe"

type mapFunc func(m, key unsafe.Pointer) unsafe.Pointer

func (t *rtype) mapFn(fn *unsafe.Pointer) mapFunc {
	if *fn == nil {
		panic("reflect: Aether has no map functions for " + t.str)
	}
	return *(*mapFunc)(unsafe.Pointer(fn))
}

// mapHeaderSize is the size of Aether's map header (D-042).
const mapHeaderSize = 64

// MakeMap creates a new map with the specified type.
func MakeMap(typ Type) Value { return MakeMapWithSize(typ, 0) }

// MakeMapWithSize creates a new map with the specified type and initial
// space for approximately n elements; Aether's maps grow as needed.
func MakeMapWithSize(typ Type, n int) Value {
	if typ.Kind() != Map {
		panic("reflect.MakeMapWithSize of non-map type")
	}
	t := typ.(*rtype)
	h := make([]unsafe.Pointer, mapHeaderSize/8)
	p := new(unsafe.Pointer)
	*p = unsafe.Pointer(&h[0])
	return Value{typ: t, ptr: unsafe.Pointer(p)}
}

// keyAddr returns the address of key's value, of the map's key type.
func (v Value) keyAddr(key Value, method string) unsafe.Pointer {
	key.mustBeExported(method)
	if key.typ == v.typ.key {
		return key.ptr
	}
	if v.typ.key.kind == Interface && v.typ.key.n == 0 {
		k := new(any)
		*k = key.Interface()
		return unsafe.Pointer(k)
	}
	panic("reflect.Value." + method + ": value of type " + key.typ.str + " is not assignable to type " + v.typ.key.str)
}

// MapIndex returns the value associated with key in the map v, or the
// zero Value if key is not found in the map or if v represents a nil map.
func (v Value) MapIndex(key Value) Value {
	v.mustBe(Map, "MapIndex")
	m := v.word()
	k := v.keyAddr(key, "MapIndex")
	access := v.typ.mapFn(&v.typ.mapAccess)
	e := access(m, k)
	if e == access(nil, k) { // .mzero: not found
		return Value{}
	}
	// A copy: the map may change.
	c := zeroed(v.typ.elem)
	copy(unsafe.Slice((*byte)(c), v.typ.elem.size), unsafe.Slice((*byte)(e), v.typ.elem.size))
	return Value{typ: v.typ.elem, ptr: c, flag: (v.flag | key.flag) & flagRO}
}

// MapKeys returns a slice containing all the keys present in the map, in
// the order Aether's maps keep them.
func (v Value) MapKeys() []Value {
	v.mustBe(Map, "MapKeys")
	var keys []Value
	it := v.MapRange()
	for it.Next() {
		k := it.Key()
		c := zeroed(k.typ)
		copy(unsafe.Slice((*byte)(c), k.typ.size), unsafe.Slice((*byte)(k.ptr), k.typ.size))
		keys = append(keys, Value{typ: k.typ, ptr: c, flag: k.flag})
	}
	return keys
}

// SetMapIndex sets the element associated with key in the map v to elem.
// If elem is the zero Value, SetMapIndex deletes the key from the map.
func (v Value) SetMapIndex(key, elem Value) {
	v.mustBe(Map, "SetMapIndex")
	v.mustBeExported("reflect.Value.SetMapIndex")
	m := v.word()
	k := v.keyAddr(key, "SetMapIndex")
	if !elem.IsValid() {
		if m != nil {
			v.typ.mapFn(&v.typ.mapDelete)(m, k)
		}
		return
	}
	elem.mustBeExported("reflect.Value.SetMapIndex")
	if m == nil {
		panic("assignment to entry in nil map")
	}
	e := v.typ.mapFn(&v.typ.mapAssign)(m, k)
	dst := Value{typ: v.typ.elem, ptr: e, flag: flagAddr}
	dst.Set(elem)
}
