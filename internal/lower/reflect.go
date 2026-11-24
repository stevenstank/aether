package lower

import (
	"fmt"
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
//
// 96 and 104 are written only in a program that reaches
// internal/reflectlite's implements, for Type.Implements, AssignableTo,
// and Value.Set (D-050). A channel's word 64 is its direction, as gc
// numbers them (1 receive, 2 send, 3 both).
//
// The descriptors of the element, key, and field types are made with it,
// so reflect reaches every type a value holds.
const (
	reflectDescSize = 112
	fieldRecSize    = 56
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
	}
	if !l.implements {
		return
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
	switch u := d.t.Underlying().(type) {
	case *types.Array:
		word(a, 64, c(u.Len()))
	case *types.Chan:
		word(a, 64, c(map[types.ChanDir]int64{types.RecvOnly: 1, types.SendOnly: 2, types.SendRecv: 3}[u.Dir()]))
	case *types.Interface:
		word(a, 64, c(int64(u.NumMethods())))
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
				path := f.Pkg().Path()
				word(fa, r+40, l.fn.Str(b, l.prog.Intern(path), pos))
				word(fa, r+48, c(int64(len(path))))
			}
		}
	}
}

// fieldName returns f's name as gc gives it. Monomorphization names an
// embedded field of an instance type as it names the instance, E[int];
// its name is E.
func fieldName(f *types.Var) string {
	name := f.Name()
	if f.Embedded() {
		name, _, _ = strings.Cut(name, "[")
	}
	return name
}
