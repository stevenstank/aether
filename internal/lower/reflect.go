package lower

import (
	"fmt"
	"github.com/stevenstank/aether/internal/frontend"
	"go/types"
	"strings"

	"github.com/stevenstank/aether/internal/ir"
)

// Type descriptors for package reflect (D-049, increment 11.8). In a
// program with package reflect or internal/reflectlite (R.6), a descriptor (iface.go) continues with the
// words reflect's rtype reads (the overlay's reflect/type.go):
//
//	32 the kind, as reflect numbers them
//	40 the size of a value
//	48 the descriptor of the element type: of a pointer, slice, array,
//	   channel, or map
//	56 the descriptor of a map's key type
//	64 an array's length, a struct's fields, an interface's methods
//	72 a struct's field records: each its name, its type's descriptor,
//	   its offset, its flags (1 exported, 2 embedded), and the path of
//	   its package if it is not exported (empty otherwise)
//	80 a map entry's size, 88 the element's offset in it (maps.go)
//	96 for a non-empty interface, the code address of its .findK
//	   (iface.go): given a descriptor twice, the itab of its type and the
//	   interface, or 0 if the type does not implement it
//	104 for a defined type, the descriptor of its underlying type (its
//	   own for a predeclared one); 0 for a type literal
//	112 the descriptor of the pointer type *T, if the program has one;
//	   reflect's New and PointerTo use it, so that a pointer they make
//	   has the type the rest of the program uses
//	120 a defined type's name, as reflect's Name gives it (a string);
//	   empty for a type literal
//	136 a defined type's package path (a string); empty for a
//	   predeclared type or a type literal
//	152 the number of methods reflect's NumMethod counts: an interface's
//	   methods, or the exported methods of another type's method set
//	160, 168, 176 for a map type, static closures of its functions
//	   access, assign, and delete, which take the map and the address of
//	   a key, as the map's own do its words (D-042); access and assign
//	   return the element's address, a missing key's that of .mzero
//
// A struct's field record continues with its tag (a string) at 56, and an
// interface's word 72 holds its methods' records (methodRecSize bytes
// each: the name, the package path of an unexported one, and the
// descriptor of its signature) (v2 evaluation).
//
// 96 and 104 are written only in a program that reaches
// internal/reflectlite's implements, for Type.Implements, AssignableTo,
// and Value.Set (D-050). A channel's word 64 is its direction, as gc
// numbers them (1 receive, 2 send, 3 both).
//
// The descriptors of the element, key, and field types are made with it,
// so reflect reaches every type a value holds.
const (
	reflectDescSize = 184
	fieldRecSize    = 72
	methodRecSize   = 40
)

// reflectKind returns reflect's Kind of t.
func reflectKind(t types.Type) int64 {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		k := map[types.BasicKind]int64{types.Bool: 1, types.Int: 2, types.Int8: 3, types.Int16: 4, types.Int32: 5, types.Int64: 6,
			types.Uint: 7, types.Uint8: 8, types.Uint16: 9, types.Uint32: 10, types.Uint64: 11, types.Uintptr: 12,
			types.Float32: 13, types.Float64: 14, types.Complex64: 15, types.Complex128: 16,
			types.String: 24, types.UnsafePointer: 26}[u.Kind()]
		return k
	case *types.Array:
		return 17
	case *types.Chan:
		return 18
	case *types.Signature:
		return 19
	case *types.Interface:
		return 20
	case *types.Map:
		return 21
	case *types.Pointer:
		return 22
	case *types.Slice:
		return 23
	case *types.Struct:
		return 25
	}
	return 0
}

// reflectDesc makes the descriptors d refers to, and its field records.
func (l *lowerer) reflectDesc(d *tdesc, pos ir.Pos) {
	switch u := d.t.Underlying().(type) {
	case *types.Pointer:
		d.elem = l.desc(u.Elem())
	case *types.Slice:
		d.elem = l.desc(u.Elem())
	case *types.Array:
		d.elem = l.desc(u.Elem())
	case *types.Chan:
		d.elem = l.desc(u.Elem())
	case *types.Map:
		d.elem, d.key = l.desc(u.Elem()), l.desc(u.Key())
	case *types.Struct:
		d.fields = l.prog.NewMem(fmt.Sprintf("%sf%d", genPrefix, d.id), max(fieldRecSize*int64(u.NumFields()), 8), 8, l.fn.File, pos)
		for f := range u.Fields() {
			d.fieldDescs = append(d.fieldDescs, l.desc(f.Type()))
		}
	case *types.Interface:
		// Its methods' records, for reflect's Implements of an interface
		// type (v2 evaluation): each its name, its package's path if it
		// is not exported, and its signature's descriptor.
		d.fields = l.prog.NewMem(fmt.Sprintf("%sf%d", genPrefix, d.id), max(methodRecSize*int64(u.NumMethods()), 8), 8, l.fn.File, pos)
		for m := range u.Methods() {
			s := m.Type().(*types.Signature)
			d.fieldDescs = append(d.fieldDescs, l.desc(types.NewSignatureType(nil, nil, nil, s.Params(), s.Results(), s.Variadic())))
		}
	}
	if u, ok := d.t.Underlying().(*types.Map); ok {
		d.mapFns = l.reflectMapFns(d.t, u, pos)
	}
	if !l.implements {
		return
	}
	// A defined type whose pointer type has more methods: the pointer
	// type's descriptor, so that reflect's PointerTo finds it, and with it
	// the interfaces it implements (v2 evaluation).
	if named, ok := types.Unalias(d.t).(*types.Named); ok && !isIface(d.t) {
		if types.NewMethodSet(types.NewPointer(named)).Len() > types.NewMethodSet(named).Len() {
			l.desc(types.NewPointer(named))
		}
	}
	switch {
	case isIface(d.t):
		if !isEmptyIface(d.t) {
			d.find = l.findFunc(d.t, pos)
		}
	case isDefined(d.t):
		d.under = l.desc(d.t.Underlying())
	}
}

// isDefined reports whether t is a defined type, which reflect says has a
// name: a named type or a predeclared one.
func isDefined(t types.Type) bool {
	switch types.Unalias(t).(type) {
	case *types.Named, *types.Basic:
		return true
	}
	return false
}

// initReflectDesc writes, in Init's entry block b, the words of the
// descriptor d and its field records that reflect reads.
func (l *lowerer) initReflectDesc(b *ir.Block, d *tdesc, pos ir.Pos) {
	a := l.fn.Addr(b, d.mem, pos)
	word := func(p *ir.Instr, off int64, v *ir.Instr) { l.fn.StM(b, p, l.fn.Const(b, off, pos), v, pos) }
	c := func(v int64) *ir.Instr { return l.fn.Const(b, v, pos) }
	word(a, 32, c(reflectKind(d.t)))
	word(a, 40, c(sizes.Sizeof(d.t)))
	if d.elem != nil {
		word(a, 48, l.fn.Addr(b, d.elem.mem, pos))
	}
	if d.key != nil {
		word(a, 56, l.fn.Addr(b, d.key.mem, pos))
	}
	if d.find != nil {
		word(a, 96, l.fn.FuncAddr(b, d.find, pos))
	}
	if d.under != nil {
		word(a, 104, l.fn.Addr(b, d.under.mem, pos))
	}
	if _, ok := types.Unalias(d.t).(*types.Pointer); ok && d.elem != nil {
		word(l.fn.Addr(b, d.elem.mem, pos), 112, a)
	}
	strWord := func(p *ir.Instr, off int64, s string) {
		if s != "" {
			word(p, off, l.fn.Str(b, l.prog.Intern(s), pos))
			word(p, off+8, c(int64(len(s))))
		}
	}
	name, path := l.reflectName(d.t)
	strWord(a, 120, name)
	strWord(a, 136, path)
	if it, ok := d.t.Underlying().(*types.Interface); ok {
		word(a, 152, c(int64(it.NumMethods())))
	} else {
		n := 0
		ms := types.NewMethodSet(d.t)
		for i := range ms.Len() {
			if ms.At(i).Obj().Exported() {
				n++
			}
		}
		word(a, 152, c(int64(n)))
	}
	for i, m := range d.mapFns {
		word(a, 160+8*int64(i), l.fn.Addr(b, m, pos))
	}
	switch u := d.t.Underlying().(type) {
	case *types.Array:
		word(a, 64, c(u.Len()))
	case *types.Chan:
		word(a, 64, c(map[types.ChanDir]int64{types.RecvOnly: 1, types.SendOnly: 2, types.SendRecv: 3}[u.Dir()]))
	case *types.Interface:
		word(a, 64, c(int64(u.NumMethods())))
		fa := l.fn.Addr(b, d.fields, pos)
		word(a, 72, fa)
		i := 0
		for m := range u.Methods() {
			r := methodRecSize * int64(i)
			strWord(fa, r, m.Name())
			if !m.Exported() {
				strWord(fa, r+16, l.home(m).Path())
			}
			word(fa, r+32, l.fn.Addr(b, d.fieldDescs[i].mem, pos))
			i++
		}
	case *types.Map:
		mt := l.mapType(d.t)
		word(a, 80, c(mt.esize))
		word(a, 88, c(mt.voff))
	case *types.Struct:
		word(a, 64, c(int64(u.NumFields())))
		fa := l.fn.Addr(b, d.fields, pos)
		word(a, 72, fa)
		var fields []*types.Var
		for f := range u.Fields() {
			fields = append(fields, f)
		}
		offs := sizes.Offsetsof(fields)
		for i, f := range fields {
			r := fieldRecSize * int64(i)
			name := fieldName(f)
			word(fa, r, l.fn.Str(b, l.prog.Intern(name), pos))
			word(fa, r+8, c(int64(len(name))))
			word(fa, r+16, l.fn.Addr(b, d.fieldDescs[i].mem, pos))
			word(fa, r+24, c(offs[i]))
			flags := int64(0)
			if f.Exported() {
				flags |= 1
			}
			if f.Embedded() {
				flags |= 2
			}
			word(fa, r+32, c(flags))
			if !f.Exported() {
				path := l.home(f).Path()
				word(fa, r+40, l.fn.Str(b, l.prog.Intern(path), pos))
				word(fa, r+48, c(int64(len(path))))
			}
			strWord(fa, r+56, u.Tag(i))
		}
	}
}

// fieldName returns f's name as gc gives it. Monomorphization names an
// embedded field of an instance type as it names the instance, E[int];
// its name is E.
func fieldName(f *types.Var) string {
	name := f.Name()
	if f.Embedded() {
		name, _, _ = strings.Cut(frontend.SourceName(name), "[")
	}
	return name
}

// reflectName returns the name and package path that reflect's Name and
// PkgPath give of t: those of a defined type, gc's for an instance of a
// generic type (D-045), and empty for a type literal; a predeclared type
// has no package path.
func (l *lowerer) reflectName(t types.Type) (string, string) {
	switch t := types.Unalias(t).(type) {
	case *types.Basic:
		if t.Kind() == types.UnsafePointer {
			return "Pointer", "unsafe"
		}
		return t.Name(), ""
	case *types.Named:
		obj := t.Obj()
		name := frontend.SourceName(obj.Name())
		if obj.Pkg() == nil {
			return name, "" // error, comparable
		}
		return name, l.home(obj).Path()
	}
	return "", ""
}

// reflectMapFns returns the static closures of reflect's access, assign,
// and delete functions of the map type t: each takes the map and the
// address of a key and calls the map's own function with the key's words.
func (l *lowerer) reflectMapFns(t types.Type, u *types.Map, pos ir.Pos) []*ir.Mem {
	mt := l.mapType(t)
	var mems []*ir.Mem
	for kind, name := range []string{"raccess", "rassign", "rdelete"} {
		var results []ir.Type
		if kind != mapDelete {
			results = []ir.Type{ir.Ptr}
		}
		w := l.wrapper(name+itoa(mt.id), []ir.Type{ir.Ptr, ir.Ptr, ir.Ptr}, results, pos, func(w *ir.Func) {
			ps := l.enter(w, pos)
			var key []*ir.Instr
			if isAggregate(u.Key()) {
				key = []*ir.Instr{ps[2]()} // an array or struct is passed as its Ptr (D-034)
			} else {
				key = l.loadPlace(place{base: ps[2], t: u.Key()}, pos)
			}
			call := l.fn.Call(l.b, l.mapFunc(mt, kind, pos), append([]*ir.Instr{ps[1]()}, key...), pos)
			if kind == mapDelete {
				l.fn.Ret(l.b, pos)
			} else {
				l.fn.Ret(l.b, pos, call)
			}
			prune(l.fn)
		})
		mems = append(mems, l.staticClosure(w))
	}
	return mems
}
