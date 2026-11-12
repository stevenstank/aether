package main

import (
	"fmt"
	"strings"
)

// Generated array and struct programs (Phase 6 increment 6.3).
//
// From a fourth random stream, a program declares up to three array or
// struct types of the scalar types and the types declared before it, a
// function for each that changes its parameter's copy and returns it, and up
// to two globals of those types. Statements on them (aggStmt) are
// interleaved with the others: declarations, whole and tuple assignments,
// stores to and arithmetic on fields and elements, comparisons, calls, and
// prints. An element is selected by constant indices, or by a variable
// index under a test of the length, without which it may be out of range,
// so that the program panics. Arrays are sometimes longer than the lowering
// unrolls, so copies and comparisons loop. The functions assign no global,
// so no call changes a value an expression reads (SEMANTICS.md V-4).

// gtype is a type a generated program uses.
type gtype struct {
	name   string // a scalar's name, or the declared type's
	scalar bool
	fields []gfield // a struct's
	n      int      // an array's length; -1 for a struct
	elem   *gtype   // an array's
}

type gfield struct {
	name string
	t    *gtype
}

// holdsString reports whether a value of t contains a string, so that
// comparing it is unsupported.
func (t *gtype) holdsString() bool {
	switch {
	case t.scalar:
		return t.name == "string"
	case t.n >= 0:
		return t.elem.holdsString()
	}
	for _, f := range t.fields {
		if f.t.holdsString() {
			return true
		}
	}
	return false
}

// leaves returns the number of scalar values of t.
func (t *gtype) leaves() int {
	switch {
	case t.scalar:
		return 1
	case t.n >= 0:
		return t.n * t.elem.leaves()
	}
	n := 0
	for _, f := range t.fields {
		if f.name != "_" {
			n += f.t.leaves()
		}
	}
	return n
}

// maxAggStmts bounds the statements on arrays and structs in a program,
// whose copies and comparisons are long, so that main stays within the
// limits on promotion, as TestBuildBackendLimits requires of the S2 suite.
const maxAggStmts = 30

// aggScalars are the scalar types of fields and elements.
var aggScalars = []string{"int", "int8", "uint8", "int16", "uint32", "int64", "uint64", "bool", "float64", "float32", "string"}

// aggLengths are the array lengths: 17 and 33 exceed the lowering's
// unrolling, so arrays of them are copied and compared in loops.
var aggLengths = []int{0, 1, 2, 3, 4, 17, 33}

func (g *generator) aggPick(n int) int { return g.a.IntN(n) }

// aggDecls emits the program's array and struct types, a function for
// each, and its globals.
func (g *generator) aggDecls() {
	for n := g.aggPick(4); n > 0; n-- {
		g.aggType()
	}
	for _, t := range g.aggTypes {
		// cpT changes a leaf of its parameter, which the caller's value
		// must not see, and returns the copy.
		g.line(0, "func cp%s(v %s, n int) (%s, int) {", t.name, t.name, t.name)
		if path, leaf, ok := g.leafPath("v", t); ok {
			g.line(1, "%s = %s", path, g.scalarValue(leaf))
		}
		g.line(1, "return v, n + 1")
		g.line(0, "}")
		g.line(0, "")
	}
	for n := g.aggPick(3); n > 0 && len(g.aggTypes) > 0; n-- {
		g.anames++
		name, t := fmt.Sprintf("ga%d", g.anames), g.aggTypes[g.aggPick(len(g.aggTypes))]
		if g.aggPick(2) == 0 {
			g.line(0, "var %s %s", name, t.name)
		} else {
			g.line(0, "var %s = %s", name, g.literal(t))
		}
		g.line(0, "")
		g.declare(name, entry{k: aggVar, t: t.name})
	}
}

// aggType declares a new array or struct type.
func (g *generator) aggType() {
	elem := func() *gtype {
		if len(g.aggTypes) > 0 && g.aggPick(3) == 0 {
			return g.aggTypes[g.aggPick(len(g.aggTypes))]
		}
		return &gtype{name: aggScalars[g.aggPick(len(aggScalars))], scalar: true}
	}
	t := &gtype{name: fmt.Sprintf("A%d", len(g.aggTypes)+1), n: -1}
	if g.aggPick(3) == 0 {
		t.elem = elem()
		for {
			t.n = aggLengths[g.aggPick(len(aggLengths))]
			if t.leaves() <= 32 {
				break
			}
		}
		g.line(0, "type %s [%d]%s", t.name, t.n, t.elem.name)
	} else {
		var lines []string
		for i := g.aggPick(5); i >= 0; i-- {
			f := gfield{name: fmt.Sprintf("f%d", len(t.fields)), t: elem()}
			if g.aggPick(8) == 0 && !f.t.holdsString() {
				// Blank: never compared, never selected. Not of a string,
				// whose comparison the gate rejects even in a blank field.
				f.name = "_"
			}
			if t.leaves()+f.t.leaves() > 32 {
				break
			}
			t.fields = append(t.fields, f)
			lines = append(lines, fmt.Sprintf("\t%s %s", f.name, f.t.name))
		}
		g.line(0, "type %s struct {", t.name)
		for _, l := range lines {
			g.line(0, "%s", l)
		}
		g.line(0, "}")
	}
	g.line(0, "")
	g.aggTypes = append(g.aggTypes, t)
}

// typeOf returns the declared type named name.
func (g *generator) typeOf(name string) *gtype {
	for _, t := range g.aggTypes {
		if t.name == name {
			return t
		}
	}
	panic("unknown type " + name)
}

// leafPath returns a path from base, of type t, to a scalar, through
// non-blank fields and constant indices, and the scalar's type; ok is false
// if t has no scalar.
func (g *generator) leafPath(base string, t *gtype) (path string, leaf *gtype, ok bool) {
	for !t.scalar {
		if t.n >= 0 {
			if t.n == 0 || t.elem.leaves() == 0 {
				return "", nil, false
			}
			base += fmt.Sprintf("[%d]", g.aggPick(t.n))
			t = t.elem
			continue
		}
		var fs []gfield
		for _, f := range t.fields {
			if f.name != "_" && f.t.leaves() > 0 {
				fs = append(fs, f)
			}
		}
		if len(fs) == 0 {
			return "", nil, false
		}
		f := fs[g.aggPick(len(fs))]
		base += "." + f.name
		t = f.t
	}
	return base, t, true
}

// arrayPath returns a path from base, of type t, to an array with scalars,
// through non-blank fields and constant indices, and the array's type; ok
// is false if there is none.
func (g *generator) arrayPath(base string, t *gtype) (string, *gtype, bool) {
	for depth := 0; !t.scalar && depth < 4; depth++ {
		if t.n >= 0 {
			if t.n > 0 && t.elem.leaves() > 0 {
				return base, t, true
			}
			return "", nil, false
		}
		var fs []gfield
		for _, f := range t.fields {
			if f.name != "_" && f.t.leaves() > 0 && !f.t.scalar {
				fs = append(fs, f)
			}
		}
		if len(fs) == 0 {
			return "", nil, false
		}
		f := fs[g.aggPick(len(fs))]
		base += "." + f.name
		t = f.t
	}
	return "", nil, false
}

// scalarValue returns an expression of the scalar type t: a literal, or a
// visible variable of the type.
func (g *generator) scalarValue(t *gtype) string {
	if vars := g.vars(t.name); len(vars) > 0 && g.aggPick(3) == 0 {
		return vars[g.aggPick(len(vars))]
	}
	switch t.name {
	case "bool":
		return []string{"true", "false"}[g.aggPick(2)]
	case "string":
		return stringLiterals[g.aggPick(len(stringLiterals))]
	case "float64", "float32":
		return []string{"0", "0.5", "-1.25", "1e10", "3"}[g.aggPick(5)]
	}
	lits := []string{"0", "1", "7", maxLiteral[t.name]}
	if t.name == "int" {
		lits[3] = "9223372036854775807"
	}
	if !unsigned(t.name) {
		lits = append(lits, "-1", "-"+lits[3])
	}
	return lits[g.aggPick(len(lits))]
}

// literal returns a composite literal of t: keyed or positional, with some
// elements left out, and an element of an array of arrays or structs
// sometimes with its type elided.
func (g *generator) literal(t *gtype) string {
	return t.name + g.literalBody(t)
}

func (g *generator) literalBody(t *gtype) string {
	value := func(e *gtype, elide bool) string {
		if e.scalar {
			return g.scalarValue(e)
		}
		if elide && g.aggPick(2) == 0 {
			return g.literalBody(e)
		}
		return g.literal(e)
	}
	var elts []string
	if t.n >= 0 {
		keyed := g.aggPick(2) == 0
		for i := 0; i < t.n && len(elts) < 4; i++ {
			if g.aggPick(3) == 0 {
				if !keyed {
					break // a positional literal lists a prefix
				}
				continue
			}
			v := value(t.elem, true)
			if keyed {
				v = fmt.Sprintf("%d: %s", i, v)
			}
			elts = append(elts, v)
		}
		return "{" + strings.Join(elts, ", ") + "}"
	}
	positional := g.aggPick(3) == 0
	for _, f := range t.fields {
		if f.name == "_" {
			positional = false // a blank field cannot be listed
		}
	}
	for _, f := range t.fields {
		switch {
		case positional:
			elts = append(elts, value(f.t, false))
		case f.name != "_" && g.aggPick(2) == 0:
			elts = append(elts, f.name+": "+value(f.t, false))
		}
	}
	return "{" + strings.Join(elts, ", ") + "}"
}

// aggUse returns an expression that uses the aggregate variable name: one
// of its scalars, or for a type without any a comparison with itself; or,
// if it holds strings, which the gate does not compare, the length of an
// empty array in it (blank fields are never strings).
func (g *generator) aggUse(name string, t *gtype) string {
	if path, _, ok := g.leafPath(name, t); ok {
		return path
	}
	if !t.holdsString() {
		return name + " == " + name
	}
	for !t.scalar && t.n < 0 {
		for _, f := range t.fields {
			if f.name != "_" && f.t.holdsString() {
				name, t = name+"."+f.name, f.t
				break
			}
		}
	}
	return "len(" + name + ")"
}

// aggStmt emits a statement on arrays and structs.
func (g *generator) aggStmt(depth int) {
	if len(g.aggTypes) == 0 {
		return
	}
	vars := g.visible(func(e entry) bool { return e.k == aggVar })
	ints := g.visible(func(e entry) bool { return (e.k == variable || e.k == counter) && e.t != "bool" })
	k := g.aggPick(10)
	if len(vars) == 0 {
		k = 0
	}
	counters := g.visible(func(e entry) bool { return e.k == counter })
	if k != 0 && len(counters) > 0 && g.aggPick(3) == 0 {
		k = 4 + g.aggPick(2) // an element, often by the boundary probe below
	}
	var name string
	var t *gtype
	if len(vars) > 0 {
		name = vars[g.aggPick(len(vars))]
		e, _ := g.lookup(name)
		t = g.typeOf(e.t)
	}
	same := func() string {
		var out []string
		for _, v := range vars {
			if e, _ := g.lookup(v); e.t == t.name {
				out = append(out, v)
			}
		}
		return out[g.aggPick(len(out))]
	}
	switch k {
	case 0:
		g.anames++
		nt := g.aggTypes[g.aggPick(len(g.aggTypes))]
		v := fmt.Sprintf("ag%d", g.anames)
		switch g.aggPick(3) {
		case 0:
			g.line(depth, "var %s %s", v, nt.name)
		case 1:
			g.line(depth, "%s := %s", v, g.literal(nt))
		default:
			src := g.literal(nt)
			for _, w := range vars {
				if e, _ := g.lookup(w); e.t == nt.name && g.aggPick(2) == 0 {
					src = w
				}
			}
			g.line(depth, "var %s = %s", v, src)
		}
		g.declare(v, entry{k: aggVar, t: nt.name})
	case 1:
		// A whole value: another variable's, a literal, or a swap.
		other := same()
		switch {
		case other != name && g.aggPick(2) == 0:
			g.line(depth, "%s, %s = %s, %s", name, other, other, name)
		case g.aggPick(2) == 0:
			g.line(depth, "%s = %s", name, other)
		default:
			g.line(depth, "%s = %s", name, g.literal(t))
		}
	case 2, 3:
		// A scalar, assigned or changed in place.
		path, leaf, ok := g.leafPath(name, t)
		if !ok {
			return
		}
		switch {
		case leaf.name == "bool" || leaf.name == "string" || g.aggPick(2) == 0:
			g.line(depth, "%s = %s", path, g.scalarValue(leaf))
		case g.aggPick(3) == 0:
			g.line(depth, "%s%s", path, []string{"++", "--"}[g.aggPick(2)])
		case leaf.name == "float64" || leaf.name == "float32":
			g.line(depth, "%s %s %s", path, []string{"+=", "-=", "*="}[g.aggPick(3)], g.scalarValue(leaf))
		default:
			g.line(depth, "%s %s %s", path, []string{"+=", "-=", "*=", "|=", "^="}[g.aggPick(5)], g.scalarValue(leaf))
		}
	case 4, 5:
		// An element selected by a variable index, guarded unless the
		// program may panic; read or assigned.
		arr, at, ok := g.arrayPath(name, t)
		if !ok || len(ints) == 0 {
			return
		}
		i := ints[g.aggPick(len(ints))]
		path, leaf, ok := g.leafPath(arr+"["+i+"]", at.elem)
		if !ok {
			return
		}
		stmt := fmt.Sprintf("println(%s, len(%s))", path, arr)
		if k == 5 {
			stmt = fmt.Sprintf("%s = %s", path, g.scalarValue(leaf))
		}
		// Unguarded: by c + n-2, where c is a loop counter, from 0 to 2,
		// and n the array's length, which reaches n, the first index out
		// of range, if the loop runs to its end (or -1 for n = 1); or
		// rarely by any variable.
		if len(counters) > 0 && g.aggPick(2) == 0 {
			c := fmt.Sprintf("%s + %d", counters[g.aggPick(len(counters))], at.n-2)
			g.line(depth, "%s", strings.Replace(stmt, "["+i+"]", "["+c+"]", 1))
			return
		}
		if g.aggPick(10) == 0 {
			g.line(depth, "%s", stmt)
			return
		}
		g.line(depth, "if %s >= 0 && uint64(%s) < uint64(len(%s)) {", i, i, arr)
		g.line(depth+1, "%s", stmt)
		g.line(depth, "}")
	case 6:
		// A comparison, of two variables or with a literal.
		if t.holdsString() {
			return
		}
		g.line(depth, "println(%s == %s, %s != %s)", name, same(), name, g.literal(t))
	case 7:
		// A call: the argument is a copy.
		g.anames++
		v, n := fmt.Sprintf("ag%d", g.anames), fmt.Sprintf("an%d", g.anames)
		g.line(depth, "%s, %s := cp%s(%s, %d)", v, n, t.name, name, g.aggPick(5))
		g.declare(v, entry{k: aggVar, t: t.name})
		g.declare(n, entry{k: variable, t: "int"})
		g.line(depth, "println(%s, %s)", g.aggUse(name, t), g.aggUse(v, t))
	default:
		var args []string
		for n := 1 + g.aggPick(3); n > 0; n-- {
			args = append(args, g.aggUse(name, t))
		}
		if arr, _, ok := g.arrayPath(name, t); ok {
			args = append(args, "len("+arr+")")
		}
		g.line(depth, "%s(%s)", []string{"println", "print"}[g.aggPick(2)], strings.Join(args, ", "))
	}
}
