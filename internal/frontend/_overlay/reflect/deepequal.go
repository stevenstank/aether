package reflect

// DeepEqual and conversions (v2 evaluation). DeepEqual is gc's algorithm
// over Aether's Values.

import "unsafe"

// Call and MakeFunc are declared without a body: Aether cannot call or
// make a function of a signature known only at run time, so a program
// that reaches either is unsupported (function-without-body). Since
// D-055's pruning, a method is reached only where code names it, such
// as net/http/httptrace's ClientTrace.compose (D-057).
func (v Value) Call(in []Value) []Value

func MakeFunc(typ Type, fn func(args []Value) (results []Value)) Value

// visit records a pair of values deepValueEqual is comparing, so that
// cyclic structures compare equal.
type visit struct {
	a1  unsafe.Pointer
	a2  unsafe.Pointer
	typ *rtype
}

func deepValueEqual(v1, v2 Value, visited map[visit]bool) bool {
	if !v1.IsValid() || !v2.IsValid() {
		return v1.IsValid() == v2.IsValid()
	}
	if v1.typ != v2.typ {
		return false
	}
	hard := func(v1, v2 Value) bool {
		switch v1.Kind() {
		case Pointer, Map, Slice, Interface:
			return !v1.IsNil() && !v2.IsNil()
		}
		return false
	}
	if hard(v1, v2) {
		var addr1, addr2 unsafe.Pointer
		if v1.Kind() == Interface {
			addr1, addr2 = v1.ptr, v2.ptr
		} else {
			addr1, addr2 = v1.word(), v2.word()
		}
		if uintptr(addr1) > uintptr(addr2) {
			addr1, addr2 = addr2, addr1
		}
		v := visit{addr1, addr2, v1.typ}
		if visited[v] {
			return true
		}
		visited[v] = true
	}
	switch v1.Kind() {
	case Array:
		for i := range v1.Len() {
			if !deepValueEqual(v1.Index(i), v2.Index(i), visited) {
				return false
			}
		}
		return true
	case Slice:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.word() == v2.word() {
			return true
		}
		for i := range v1.Len() {
			if !deepValueEqual(v1.Index(i), v2.Index(i), visited) {
				return false
			}
		}
		return true
	case Interface:
		if v1.IsNil() || v2.IsNil() {
			return v1.IsNil() == v2.IsNil()
		}
		return deepValueEqual(v1.Elem(), v2.Elem(), visited)
	case Pointer:
		if v1.word() == v2.word() {
			return true
		}
		return deepValueEqual(v1.Elem(), v2.Elem(), visited)
	case Struct:
		for i := range v1.NumField() {
			if !deepValueEqual(v1.Field(i), v2.Field(i), visited) {
				return false
			}
		}
		return true
	case Map:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.word() == v2.word() {
			return true
		}
		it := v1.MapRange()
		for it.Next() {
			val1 := it.Value()
			val2 := v2.MapIndex(it.Key())
			if !val1.IsValid() || !val2.IsValid() || !deepValueEqual(val1, val2, visited) {
				return false
			}
		}
		return true
	case Func:
		if v1.IsNil() && v2.IsNil() {
			return true
		}
		return false // Can't do better than this.
	case Int, Int8, Int16, Int32, Int64:
		return v1.Int() == v2.Int()
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return v1.Uint() == v2.Uint()
	case String:
		return v1.String() == v2.String()
	case Bool:
		return v1.Bool() == v2.Bool()
	case Float32, Float64:
		return v1.Float() == v2.Float()
	case Complex64, Complex128:
		return v1.Complex() == v2.Complex()
	case Chan, UnsafePointer:
		return v1.word() == v2.word()
	}
	return false
}

// DeepEqual reports whether x and y are "deeply equal", as gc's.
func DeepEqual(x, y any) bool {
	if x == nil || y == nil {
		return x == y
	}
	v1 := ValueOf(x)
	v2 := ValueOf(y)
	if v1.typ != v2.typ {
		return false
	}
	return deepValueEqual(v1, v2, make(map[visit]bool))
}

// conversion returns how a value of type src converts to type dst, as
// Aether converts it: 0 if it does not; 1 identical layout (the same
// underlying type, or pointers to the same underlying type, or a
// bidirectional channel); 2 numeric; 3 to an interface.
func conversion(dst, src *rtype) int {
	switch {
	case directlyAssignable(dst, src) || underlying(dst) == underlying(src):
		return 1
	case dst.kind == Pointer && src.kind == Pointer && dst.under == nil && src.under == nil &&
		underlying(dst.elem) == underlying(src.elem):
		return 1
	case isNumeric(dst.kind) && isNumeric(src.kind):
		return 2
	case dst.kind == Interface && implementsType(dst, src):
		return 3
	}
	return 0
}

func underlying(t *rtype) *rtype {
	if t.under != nil {
		return t.under
	}
	return t
}

func isNumeric(k Kind) bool { return k >= Int && k <= Float64 }

// ConvertibleTo reports whether a value of the type is convertible to type
// u, for the conversions Aether's reflect makes: between types of the same
// underlying type, between numeric types, and to an implemented interface.
// It panics for other kinds of conversion that Go has.
func (t *rtype) ConvertibleTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.ConvertibleTo")
	}
	uu := u.(*rtype)
	if conversion(uu, t) != 0 {
		return true
	}
	if uu.kind == String || t.kind == String || uu.kind == Array || uu.kind == Complex64 || uu.kind == Complex128 {
		panic("reflect: Aether does not decide whether " + t.str + " converts to " + uu.str)
	}
	return false
}

// Convert returns the value v converted to type t, for the conversions
// ConvertibleTo reports.
func (v Value) Convert(t Type) Value {
	tt := t.(*rtype)
	switch conversion(tt, v.typ) {
	case 1:
		c := zeroed(tt)
		copy(unsafe.Slice((*byte)(c), tt.size), unsafe.Slice((*byte)(v.ptr), tt.size))
		return Value{typ: tt, ptr: c, flag: v.flag & flagRO}
	case 2:
		n := New(t).Elem()
		switch {
		case v.Kind() >= Float32:
			f := v.Float()
			switch {
			case tt.kind >= Float32:
				n.SetFloat(f)
			case tt.kind >= Uint:
				n.SetUint(uint64(f))
			default:
				n.SetInt(int64(f))
			}
		case v.Kind() >= Uint:
			u := v.Uint()
			switch {
			case tt.kind >= Float32:
				n.SetFloat(float64(u))
			case tt.kind >= Uint:
				n.SetUint(u)
			default:
				n.SetInt(int64(u))
			}
		default:
			i := v.Int()
			switch {
			case tt.kind >= Float32:
				n.SetFloat(float64(i))
			case tt.kind >= Uint:
				n.SetUint(uint64(i))
			default:
				n.SetInt(i)
			}
		}
		n.flag = v.flag & flagRO
		return n
	case 3:
		n := New(t).Elem()
		n.Set(v)
		n.flag = v.flag & flagRO
		return n
	}
	panic("reflect.Value.Convert: value of type " + v.typ.str + " cannot be converted to type " + tt.str)
}
