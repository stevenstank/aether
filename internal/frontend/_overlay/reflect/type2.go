package reflect

// Type information that encoding/json, encoding/xml, and encoding/gob use
// (v2 evaluation): names, struct tags, method counts, interface checks,
// fields by index and name, and overflow tests. Struct tags and FieldByName
// are gc's code.

import (
	"strconv"
	"unsafe"
)

// Name returns a defined type's name within its package, or "" for a
// type literal.
func (t *rtype) Name() string { return t.name }

// PkgPath returns a defined type's package path, or "" for a predeclared
// type or a type literal.
func (t *rtype) PkgPath() string { return t.pkgPath }

// NumMethod returns the number of methods of an interface type, or the
// number of exported methods in another type's method set.
func (t *rtype) NumMethod() int { return t.nmethod }

// Implements reports whether the type implements the interface type u.
func (t *rtype) Implements(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.Implements")
	}
	if u.Kind() != Interface {
		panic("reflect: non-interface type passed to Type.Implements")
	}
	return implementsType(u.(*rtype), t)
}

// AssignableTo reports whether a value of the type is assignable to type u.
func (t *rtype) AssignableTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.AssignableTo")
	}
	uu := u.(*rtype)
	return directlyAssignable(uu, t) || implementsType(uu, t)
}

// method returns the record of an interface type's i'th method; they are
// in the order of their names, as gc's.
func (t *rtype) method(i int) *methodRec {
	return (*methodRec)(unsafe.Add(t.fields, uintptr(i)*unsafe.Sizeof(methodRec{})))
}

// implementsType reports whether the type V implements the interface type
// T: every type implements an empty interface; a non-interface type, by
// T's .findK, which lists every type of the program that implements T
// (D-050); an interface type, if its methods include each of T's, of the
// same name, package, and signature, whose descriptors are canonical.
func implementsType(T, V *rtype) bool {
	if T.kind != Interface {
		return false
	}
	if T.n == 0 {
		return true
	}
	if V.kind != Interface {
		return implements(unsafe.Pointer(T), unsafe.Pointer(V)) != nil
	}
	j := 0
	for i := range T.n {
		tm := T.method(i)
		for ; j < V.n; j++ {
			vm := V.method(j)
			if vm.name == tm.name && vm.pkgPath == tm.pkgPath && vm.typ == tm.typ {
				break
			}
		}
		if j == V.n {
			return false
		}
		j++
	}
	return true
}

// implements returns the itab of the type t and the non-empty interface
// type iface, or nil if t does not implement iface. It is an intrinsic,
// as internal/reflectlite's.
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

// OverflowInt reports whether the int64 x cannot be represented by the
// type, which must be an Int kind.
func (t *rtype) OverflowInt(x int64) bool {
	switch t.kind {
	case Int, Int8, Int16, Int32, Int64:
		bitSize := t.size * 8
		trunc := (x << (64 - bitSize)) >> (64 - bitSize)
		return x != trunc
	}
	panic("reflect: OverflowInt of non-int type " + t.String())
}

// OverflowUint reports whether the uint64 x cannot be represented by the
// type, which must be a Uint kind.
func (t *rtype) OverflowUint(x uint64) bool {
	switch t.kind {
	case Uint, Uintptr, Uint8, Uint16, Uint32, Uint64:
		bitSize := t.size * 8
		trunc := (x << (64 - bitSize)) >> (64 - bitSize)
		return x != trunc
	}
	panic("reflect: OverflowUint of non-uint type " + t.String())
}

// OverflowFloat reports whether the float64 x cannot be represented by the
// type, which must be a Float kind.
func (t *rtype) OverflowFloat(x float64) bool {
	switch t.kind {
	case Float32:
		return overflowFloat32(x)
	case Float64:
		return false
	}
	panic("reflect: OverflowFloat of non-float type " + t.String())
}

func overflowFloat32(x float64) bool {
	if x < 0 {
		x = -x
	}
	return 3.40282346638528859811704183484516925440e+38 < x && x <= 1.79769313486231570814527423731704356798070e+308
}

// FieldByIndex returns the nested field corresponding to index.
func (t *rtype) FieldByIndex(index []int) StructField {
	var f StructField
	var tt Type = t
	for i, x := range index {
		if i > 0 {
			if tt.Kind() == Pointer && tt.Elem().Kind() == Struct {
				tt = tt.Elem()
			}
		}
		f = tt.Field(x)
		tt = f.Type
	}
	if len(index) > 1 {
		f.Index = append([]int(nil), index...)
	}
	return f
}

// fieldScan is a struct type to scan, as gc's FieldByNameFunc.
type fieldScan struct {
	typ   *rtype
	index []int
}

// FieldByName returns the struct field with the given name, as gc's: the
// shallowest field of that name, embedded fields searched breadth first,
// none if two at that depth have it.
func (t *rtype) FieldByName(name string) (StructField, bool) {
	if t.kind != Struct {
		panic("reflect: FieldByName of non-struct type " + t.String())
	}
	// Fast path: a field of the struct itself, if none is embedded.
	hasEmbeds := false
	if name != "" {
		for i := range t.n {
			r := t.field(i)
			if r.name == name {
				return t.Field(i), true
			}
			if r.flags&2 != 0 {
				hasEmbeds = true
			}
		}
	}
	if !hasEmbeds {
		return StructField{}, false
	}
	return t.fieldByNameFunc(func(s string) bool { return s == name })
}

// fieldByNameFunc is gc's structType.FieldByNameFunc over Aether's
// descriptors.
func (t *rtype) fieldByNameFunc(match func(string) bool) (result StructField, ok bool) {
	current := []fieldScan{}
	next := []fieldScan{{typ: t}}
	var nextCount map[*rtype]int
	visited := map[*rtype]bool{}
	for len(next) > 0 {
		current, next = next, current[:0]
		count := nextCount
		nextCount = nil
		for _, scan := range current {
			st := scan.typ
			if visited[st] {
				continue
			}
			visited[st] = true
			for i := range st.n {
				f := st.field(i)
				var fname string
				var ntyp *rtype
				if f.flags&2 != 0 {
					ntyp = f.typ
					if ntyp.kind == Pointer {
						ntyp = ntyp.elem
					}
				}
				fname = f.name
				if match(fname) {
					if count[st] > 1 || ok {
						return StructField{}, false
					}
					result = st.Field(i)
					result.Index = nil
					result.Index = append(result.Index, scan.index...)
					result.Index = append(result.Index, i)
					ok = true
					continue
				}
				if ok || ntyp == nil || ntyp.kind != Struct {
					continue
				}
				if nextCount[ntyp] > 0 {
					nextCount[ntyp] = 2
					continue
				}
				if nextCount == nil {
					nextCount = map[*rtype]int{}
				}
				nextCount[ntyp] = 1
				if count[st] > 1 {
					nextCount[ntyp] = 2
				}
				var index []int
				index = append(index, scan.index...)
				index = append(index, i)
				next = append(next, fieldScan{ntyp, index})
			}
		}
		if ok {
			break
		}
	}
	return
}

// A StructTag is the tag string in a struct field.
type StructTag string

// Get returns the value associated with key in the tag string.
func (tag StructTag) Get(key string) string {
	v, _ := tag.Lookup(key)
	return v
}

// Lookup returns the value associated with key in the tag string, and
// whether the key is present (gc's code).
func (tag StructTag) Lookup(key string) (value string, ok bool) {
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := string(tag[:i])
		tag = tag[i+1:]
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		qvalue := string(tag[:i+1])
		tag = tag[i+1:]
		if key == name {
			value, err := strconv.Unquote(qvalue)
			if err != nil {
				break
			}
			return value, true
		}
	}
	return "", false
}

// TypeFor returns the Type that represents the type argument T (gc's
// code).
func TypeFor[T any]() Type {
	var v T
	if t := TypeOf(v); t != nil {
		return t // optimize for T being a non-interface kind
	}
	return TypeOf((*T)(nil)).Elem() // only for an interface kind
}
