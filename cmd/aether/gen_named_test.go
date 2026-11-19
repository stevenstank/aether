package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated named-result programs (Phase 8 increment 8.7, D-044).
//
// generateNamed writes a program of its own, from its own random stream,
// so that the other generators' programs stay byte for byte what they
// were. Its functions f0 to f3 have the named results (r int, s string,
// p P, a [2]int) and random bodies that assign them, directly, through a
// pointer, and from a function literal that captures them, and return
// early or at the end, bare or with values. Some functions defer calls
// that read and set the results, or defer print with the results' values
// at the defer statement; some also recover, and those may panic, by
// panic or with an index out of range. main calls each function with
// several arguments and prints the results. Bodies first seed some results
// from k, so that the operations see values other than zero.
//
// V-4: each statement makes at most one call, and its other operands are
// variables only that call cannot assign, so gc's order and Aether's
// agree.
//
// About one program in seven ends with a panic that is not recovered,
// from a function whose deferred call prints its named result.

// generateNamed returns the named-result program for seed.
func generateNamed(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0x4e44))}
	g.line(0, "package main\n")
	g.line(0, "type P struct {\n\tx, y int\n}\n")
	for i := range 4 {
		g.namedFunc(i)
	}
	boom := g.pick(7) == 0
	if boom {
		g.line(0, "func boom() (r int) {\n\tdefer func() { println(\"boom\", r) }()\n\tr = %s\n\tpanic(\"boom\")\n}\n", g.small())
	}
	g.line(0, "func main() {")
	g.line(1, "var r int")
	g.line(1, "var s string")
	g.line(1, "var p P")
	g.line(1, "var a [2]int")
	for range 6 + g.pick(6) {
		g.line(1, "r, s, p, a = f%d(%s)", g.pick(4), g.small())
		g.line(1, "println(r, s, p.x, p.y, a[0], a[1])")
	}
	if boom {
		g.line(1, "println(boom())")
	}
	g.line(0, "}")
	return g.b.String()
}

// namedFunc emits the function fi.
func (g *mgen) namedFunc(i int) {
	g.line(0, "func f%d(k int) (r int, s string, p P, a [2]int) {", i)
	recovers := g.pick(3) == 0
	if recovers {
		g.line(1, "defer func() {\n\t\tif e := recover(); e != nil {\n\t\t\tr = %s\n\t\t\ts += \"R\"\n\t\t}\n\t}()", g.nexpr(2))
	}
	for range g.pick(3) {
		switch g.pick(3) {
		case 0:
			g.line(1, "defer func() { r = %s }()", g.nexpr(3))
		case 1:
			g.line(1, "defer func() {\n\t\tp.%s += r\n\t\ta[%d] = %s\n\t\ts += \"d\"\n\t}()", []string{"x", "y"}[g.pick(2)], g.pick(2), g.nexpr(2))
		default:
			g.line(1, "defer println(\"at defer\", r, s, p.x, a[1])")
		}
	}
	// Results seeded from k, so that the operations below see values
	// other than zero.
	if g.pick(4) != 0 {
		g.line(1, "r = k*%d + %s", 2+g.pick(5), g.small())
	}
	if g.pick(2) == 0 {
		g.line(1, "a[%d], p.y = k-%d, %s", g.pick(2), g.pick(9), g.small())
	}
	for range 2 + g.pick(5) {
		g.namedStmt(recovers)
	}
	g.namedReturn(1)
	g.line(0, "}\n")
}

// nterm returns an int operand of a named-result function's body.
func (g *mgen) nterm() string {
	return []string{"k", "r", "r", "p.x", "p.y", "a[0]", "a[1]", "len(s)", g.small()}[g.pick(9)]
}

// nexpr returns an int expression of nterms.
func (g *mgen) nexpr(n int) string {
	e := g.nterm()
	for i := 1; i < n; i++ {
		e = fmt.Sprintf("(%s %s %s)", e, []string{"+", "-", "*", "^", "&", "|"}[g.pick(6)], g.nterm())
	}
	return e
}

// namedReturn emits a return statement, bare or with values, at depth.
func (g *mgen) namedReturn(depth int) {
	switch g.pick(4) {
	case 0, 1:
		g.line(depth, "return")
	case 2:
		g.line(depth, "return %s, s + \"v\", P{%s, %s}, [2]int{%s, %s}", g.nexpr(2), g.nexpr(2), g.nterm(), g.nterm(), g.nexpr(2))
	default:
		// The results' own values, read before any is assigned.
		g.line(depth, "return a[1], s, P{p.y, p.x}, [2]int{r, a[0]}")
	}
}

// namedStmt emits a statement of a named-result function's body; if
// recovers, the function recovers, and the statement may panic.
func (g *mgen) namedStmt(recovers bool) {
	in := strings.Repeat("\t", 2)
	switch g.pick(10) {
	case 0, 1:
		g.line(1, "r %s %s", []string{"=", "+=", "-=", "*="}[g.pick(4)], g.nexpr(3))
	case 2:
		g.line(1, "s += %q", []string{"a", "b", "cd"}[g.pick(3)])
	case 3:
		g.line(1, "p.%s = %s", []string{"x", "y"}[g.pick(2)], g.nexpr(2))
	case 4:
		g.line(1, "a[%d] = %s", g.pick(2), g.nexpr(2))
	case 5:
		g.line(1, "if k > %s {", g.small())
		g.line(2, "r += %s", g.nterm())
		g.namedReturn(2)
		g.line(1, "}")
	case 6:
		// A function literal that captures the results.
		g.line(1, "func(d int) {\n%sr += d\n%sp.y = %s\n\t}(%s)", in, in, g.nexpr(2), g.nterm())
	case 7:
		g.line(1, "{\n%sq := &%s\n%s*q += %s\n\t}", in, []string{"r", "p.x", "a[1]"}[g.pick(3)], in, g.nexpr(2))
	default:
		if !recovers {
			g.line(1, "p = P{%s, %s}", g.nterm(), g.nexpr(2))
			return
		}
		if g.pick(2) == 0 {
			g.line(1, "if k < %s {\n%spanic(\"p\")\n\t}", g.small(), in)
		} else {
			g.line(1, "r += a[k&3]")
		}
	}
}
