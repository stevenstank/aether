package main

import "fmt"

// Generated pointer programs (Phase 7 increment 7.1, D-035).
//
// From a fifth random stream, a program declares a list node type PN, a
// function pinc that writes through its pointer parameter and returns the
// address of its other parameter, a global list pl, and up to two pointer
// globals. Statements on pointers (ptrStmt) are interleaved with the
// others: pointer declarations from &x of a variable, field, or element,
// new(T), &T{…}, nil, or another pointer; reassignments and swaps; reads
// and writes through pointers, op=, ++, and --, auto-dereferencing fields
// and elements; comparisons and switches on pointers; pinc calls; a pointer
// to a pointer; writes through a pointer to a variable, or to a scalar in
// one, followed by a direct read of it; and pushes onto, walks of, and
// copies and comparisons of nodes of the list, whose literals elide &PN.
// Accesses are usually under a nil test, without which the program may
// panic. Taking an address puts the
// variable on the heap, so the other statements' reads and writes of it go
// through memory too.
//
// Rules that keep programs in the subset and away from V-4: no address of a
// loop counter, declared in a for statement's init; pinc, the only function
// that writes through a pointer, is called alone, as "q := pinc(p, k)",
// where nothing else is read; and a pointer is never printed, only compared.
// A walk of the list is the one loop that may run more than 3 times: the
// list is finite, as each push makes a new head.
//
// ptrStmt and ptrDecls swap the stream into g.a, so that the array and
// struct helpers (scalarValue, literal, leafPath, aggUse) draw from it
// rather than from the array and struct choices.

// maxPtrStmts bounds the pointer statements in a program, as maxAggStmts
// does the array and struct ones.
const maxPtrStmts = 30

// ptrScalars are the scalar types new(T) and nil pointers point to.
var ptrScalars = []string{"int", "int8", "uint16", "int32", "uint64", "bool", "float64", "float32", "string"}

// onPtrStream runs f with the pointer stream as g.a.
func (g *generator) onPtrStream(f func()) {
	g.a, g.p = g.p, g.a
	defer func() { g.a, g.p = g.p, g.a }()
	f()
}

// pointee returns the type named name: a declared array or struct type, or
// a scalar.
func (g *generator) pointee(name string) *gtype {
	for _, t := range g.aggTypes {
		if t.name == name {
			return t
		}
	}
	return &gtype{name: name, scalar: true}
}

// ptrDecls emits PN, pinc, the list pl, and the pointer globals.
func (g *generator) ptrDecls() {
	g.line(0, "type PN struct {\n\tv    int\n\tnext *PN\n}")
	g.line(0, "")
	g.line(0, "var pl *PN")
	g.line(0, "")
	g.line(0, "func pinc(p *int, n int) *int {\n\t*p += n\n\tq := &n\n\t*q *= 2\n\treturn q\n}")
	g.line(0, "")
	g.onPtrStream(func() {
		for n := g.aggPick(3); n > 0; n-- {
			g.pnames++
			name := fmt.Sprintf("gp%d", g.pnames)
			src, t := g.ptrSource(nil)
			g.line(0, "var %s = %s", name, src)
			g.line(0, "")
			g.declare(name, entry{k: ptrVar, t: "*" + t})
		}
	})
}

// ptrSource returns an expression of a pointer, never nil, and the type it
// points to: if want is set, of that type. It is &x of a visible variable
// other than a loop counter, or of a field or element of one; new(T); &T{…};
// or another pointer variable.
func (g *generator) ptrSource(want *gtype) (string, string) {
	k := g.aggPick(5)
	if k <= 1 {
		addressable := g.visible(func(e entry) bool {
			return e.k == variable || e.k == floatVar || e.k == strVar || e.k == aggVar
		})
		var fit []string
		for _, v := range addressable {
			e, _ := g.lookup(v)
			switch {
			case want == nil || e.t == want.name || want.scalar && same(e.t, want.name):
				fit = append(fit, v)
			}
		}
		if len(fit) > 0 {
			v := fit[g.aggPick(len(fit))]
			e, _ := g.lookup(v)
			if e.k == aggVar && (want == nil || want.scalar) && g.aggPick(2) == 0 {
				// A scalar inside it, of the wanted type if any.
				if path, leaf, ok := g.leafPath(v, g.typeOf(e.t)); ok && (want == nil || leaf.name == want.name) {
					return "&" + path, leaf.name
				}
			}
			return "&" + v, e.t
		}
	}
	if k == 2 {
		if ps := g.ptrVars(want); len(ps) > 0 {
			p := ps[g.aggPick(len(ps))]
			e, _ := g.lookup(p)
			return p, e.t[1:]
		}
	}
	t := want
	if t == nil {
		if len(g.aggTypes) > 0 && g.aggPick(3) == 0 {
			t = g.aggTypes[g.aggPick(len(g.aggTypes))]
		} else {
			t = &gtype{name: ptrScalars[g.aggPick(len(ptrScalars))], scalar: true}
		}
	}
	if !t.scalar && g.aggPick(2) == 0 {
		return "&" + g.literal(t), t.name
	}
	return "new(" + t.name + ")", t.name
}

// ptrVars returns the visible pointer variables, to a value of want if it is
// set.
func (g *generator) ptrVars(want *gtype) []string {
	return g.visible(func(e entry) bool {
		return e.k == ptrVar && (want == nil || e.t == "*"+want.name)
	})
}

// ptrStmt emits a statement on pointers.
func (g *generator) ptrStmt(depth int) {
	g.onPtrStream(func() { g.ptrStmtOn(depth) })
}

func (g *generator) ptrStmtOn(depth int) {
	ptrs := g.ptrVars(nil)
	k := g.aggPick(13)
	if len(ptrs) == 0 && k < 9 {
		k = 0
	}
	if k == 12 {
		g.alias(depth)
		return
	}
	var p string
	var t *gtype
	if len(ptrs) > 0 {
		p = ptrs[g.aggPick(len(ptrs))]
		e, _ := g.lookup(p)
		t = g.pointee(e.t[1:])
	}
	// guard emits stmt under a test that p is not nil, or rarely without
	// one, so that the program may panic.
	guard := func(stmt ...string) {
		if g.aggPick(10) == 0 {
			for _, s := range stmt {
				g.line(depth, "%s", s)
			}
			return
		}
		g.line(depth, "if %s != nil {", p)
		for _, s := range stmt {
			g.line(depth+1, "%s", s)
		}
		g.line(depth, "}")
	}
	switch k {
	case 0, 1:
		g.pnames++
		name := fmt.Sprintf("p%d", g.pnames)
		src, pt := g.ptrSource(nil)
		switch g.aggPick(4) {
		case 0:
			g.line(depth, "var %s *%s", name, pt) // nil
		case 1:
			g.line(depth, "var %s *%s = %s", name, pt, src)
		default:
			g.line(depth, "%s := %s", name, src)
		}
		g.declare(name, entry{k: ptrVar, t: "*" + pt})
	case 2:
		// Another pointer to the same type, a swap, or rarely nil.
		switch q := g.ptrVars(t); {
		case g.aggPick(8) == 0:
			g.line(depth, "%s = nil", p)
		case len(q) > 1 && g.aggPick(2) == 0:
			g.line(depth, "%s, %s = %s, %s", p, q[0], q[0], p)
		default:
			src, _ := g.ptrSource(t)
			g.line(depth, "%s = %s", p, src)
		}
	case 3, 4:
		// A write through p: to *p, or to a scalar in it.
		target, leaf := "*"+p, t
		if !t.scalar {
			path, l, ok := g.leafPath(p, t)
			if !ok || g.aggPick(4) == 0 {
				src := g.literal(t)
				if vs := g.visible(func(e entry) bool { return e.k == aggVar && e.t == t.name }); len(vs) > 0 && g.aggPick(2) == 0 {
					src = vs[g.aggPick(len(vs))]
				}
				guard(fmt.Sprintf("*%s = %s", p, src))
				return
			}
			target, leaf = path, l
		}
		switch {
		case leaf.name == "bool" || leaf.name == "string" || g.aggPick(2) == 0:
			guard(fmt.Sprintf("%s = %s", target, g.scalarValue(leaf)))
		case g.aggPick(3) == 0:
			guard(fmt.Sprintf("%s%s", target, []string{"++", "--"}[g.aggPick(2)]))
		case leaf.name == "float64" || leaf.name == "float32":
			guard(fmt.Sprintf("%s %s %s", target, []string{"+=", "-=", "*="}[g.aggPick(3)], g.scalarValue(leaf)))
		default:
			guard(fmt.Sprintf("%s %s %s", target, []string{"+=", "-=", "*=", "|=", "^="}[g.aggPick(5)], g.scalarValue(leaf)))
		}
	case 5:
		// A read through p, and comparisons.
		read := "*" + p
		if !t.scalar {
			read = g.aggUse("(*"+p+")", t)
			if path, _, ok := g.leafPath(p, t); ok && g.aggPick(2) == 0 {
				read = path // through an implicit indirection
			}
		}
		q := g.ptrVars(t)
		guard(fmt.Sprintf("println(%s, %s == %s, %s != nil)", read, p, q[g.aggPick(len(q))], p))
	case 6:
		// A switch on p.
		q := g.ptrVars(t)
		g.line(depth, "switch %s {", p)
		g.line(depth, "case %s:", q[g.aggPick(len(q))])
		g.line(depth+1, "println(1)")
		g.line(depth, "case nil:")
		g.line(depth+1, "println(2)")
		g.line(depth, "default:")
		g.line(depth+1, "println(3)")
		g.line(depth, "}")
	case 7:
		// pinc, alone in its statement (V-4), or a pointer to p.
		if t.name == "int" {
			g.pnames++
			q := fmt.Sprintf("p%d", g.pnames)
			guard(fmt.Sprintf("%s := pinc(%s, %d)", q, p, g.aggPick(5)), fmt.Sprintf("println(*%s, *%s)", q, p))
			return
		}
		src, _ := g.ptrSource(t)
		g.line(depth, "{")
		g.line(depth+1, "pp := &%s", p)
		g.line(depth+1, "*pp = %s", src)
		g.line(depth+1, "println(*pp == %s, pp != nil)", p)
		g.line(depth, "}")
	case 8:
		// A copy of what p points to, compared with it.
		g.pnames++
		c := fmt.Sprintf("c%d", g.pnames)
		guard(fmt.Sprintf("%s := *%s", c, p), fmt.Sprintf("println(%s)", g.ptrCopyCompare(c, p, t)))
	case 9:
		// A push onto the list, sometimes with an elided &PN.
		v := g.scalarValue(&gtype{name: "int", scalar: true})
		if g.aggPick(2) == 0 {
			g.line(depth, "pl = &PN{%s, pl}", v)
		} else {
			g.line(depth, "pl = [2]*PN{{v: %s, next: pl}, nil}[0]", v)
		}
	case 10:
		// A walk of the list.
		g.line(depth, "for n := pl; n != nil; n = n.next {")
		g.line(depth+1, "print(n.v, \" \")")
		g.line(depth, "}")
		g.line(depth, "println()")
	default:
		// A change to the head node, and a copy of it compared with it.
		g.line(depth, "if pl != nil {")
		g.line(depth+1, "nd := *pl")
		g.line(depth+1, "pl.v %s %s", []string{"=", "+=", "*="}[g.aggPick(3)], g.scalarValue(&gtype{name: "int", scalar: true}))
		g.line(depth+1, "println(nd == *pl, nd.next == pl.next, nd.v)")
		g.line(depth, "}")
	}
}

// alias emits a write through a pointer to a visible variable, or to a
// scalar in one, then reads the variable directly: the two must alias.
func (g *generator) alias(depth int) {
	vars := g.visible(func(e entry) bool { return e.k == variable || e.k == floatVar || e.k == strVar || e.k == aggVar })
	if len(vars) == 0 {
		return
	}
	path := vars[g.aggPick(len(vars))]
	e, _ := g.lookup(path)
	leaf := &gtype{name: e.t, scalar: true}
	if e.k == aggVar {
		var ok bool
		if path, leaf, ok = g.leafPath(path, g.typeOf(e.t)); !ok {
			return
		}
	}
	g.line(depth, "{")
	g.line(depth+1, "q := &%s", path)
	g.line(depth+1, "*q = %s", g.scalarValue(leaf))
	g.line(depth+1, "println(%s, q == &%s)", path, path)
	g.line(depth, "}")
}

// ptrCopyCompare returns an expression that compares c, a copy of what p,
// a pointer to t, points to, with it, or for a type that holds a string,
// which the gate does not compare, reads c.
func (g *generator) ptrCopyCompare(c, p string, t *gtype) string {
	switch {
	case !t.holdsString():
		return fmt.Sprintf("%s == *%s", c, p)
	case t.scalar:
		return "len(" + c + ")"
	}
	return g.aggUse(c, t)
}
