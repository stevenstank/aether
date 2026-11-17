package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated closure programs (Phase 8 increment 8.3, D-040).
//
// generateClosures writes a program of its own, from its own random
// stream, so that the other generators' programs stay byte for byte what
// they were. main declares variables of several kinds, an int pair, a
// string, an array, a struct, and a slice, and makes closures, of type
// func(int) int, whose random bodies read and assign them; closures made
// by other closures and by a function, holding variables of their own;
// closures made in a loop, each capturing its iteration's variable; and
// calls of them, directly and through a higher-order function. It prints
// the variables between the calls.
//
// V-4: a call of a closure is the only call of its statement, and every
// other operand of the statement is a constant, so whatever it assigns,
// gc's order and Aether's agree.
//
// About one program in seven ends with a closure indexing the slice out of
// range, or a call of a nil func value.

// generateClosures returns the closure program for seed.
func generateClosures(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0xc105))}
	g.line(0, "package main\n")
	g.line(0, "type P struct {\n\tx, y int\n}\n")
	g.line(0, "func mk(k int) func(int) int {\n\tacc := k\n\treturn func(d int) int {\n\t\tacc = %s\n\t\treturn acc\n\t}\n}\n",
		strings.NewReplacer("x", "acc", "y", "d").Replace(g.fexpr(3)))
	g.line(0, "func apply(f func(int) int, v int) int { return f(f(v)) }\n")
	g.line(0, "func main() {")
	g.line(1, "a, b := %s, %s", g.small(), g.small())
	g.line(1, "s := \"x\"")
	g.line(1, "arr := [2]int{%s, %s}", g.small(), g.small())
	g.line(1, "pt := P{%s, %s}", g.small(), g.small())
	g.line(1, "sl := []int{%s}", g.small())
	g.line(1, "var gs []func(int) int")
	for i := range 3 {
		g.line(1, "f%d := %s", i, g.literal(1))
	}
	for range 10 + g.pick(12) {
		g.cstmt()
	}
	g.line(1, "println(a, b, s, arr[0], arr[1], pt.x, pt.y, len(sl), len(gs), f0 != nil, f1 != nil, f2 != nil)")
	if g.pick(7) == 0 {
		if g.pick(2) == 0 {
			g.line(1, "println(func(k int) int { return sl[len(sl)+k] }(%d))", g.pick(3))
		} else {
			g.line(1, "var z func(int) int")
			g.line(1, "println(z(%s))", g.small())
		}
	}
	g.line(0, "}")
	return g.b.String()
}

// cterm returns an int operand of a closure's body, whose parameter is k.
func (g *mgen) cterm() string {
	return []string{"a", "b", "k", "k", "arr[0]", "arr[1]", "pt.x", "pt.y", "len(s)", "len(sl)", "sl[0]", g.small()}[g.pick(12)]
}

// cexpr returns an int expression of cterms.
func (g *mgen) cexpr(n int) string {
	e := g.cterm()
	for i := 1; i < n; i++ {
		e = fmt.Sprintf("(%s %s %s)", e, []string{"+", "-", "*", "^", "&", "|"}[g.pick(6)], g.cterm())
	}
	return e
}

// literal returns a function literal of type func(int) int, at depth
// tabs, whose body assigns and reads captured variables.
func (g *mgen) literal(depth int) string {
	var b strings.Builder
	in := strings.Repeat("\t", depth+1)
	b.WriteString("func(k int) int {\n")
	for range 1 + g.pick(3) {
		b.WriteString(in)
		switch g.pick(8) {
		case 0:
			fmt.Fprintf(&b, "a += %s", g.cexpr(2))
		case 1:
			fmt.Fprintf(&b, "b = %s", g.cexpr(2))
		case 2:
			fmt.Fprintf(&b, "arr[%d] -= %s", g.pick(2), g.cexpr(2))
		case 3:
			fmt.Fprintf(&b, "pt.%s = %s", []string{"x", "y"}[g.pick(2)], g.cexpr(2))
		case 4:
			b.WriteString("s += \"y\"")
		case 5:
			b.WriteString("sl = append(sl, k)")
		case 6:
			fmt.Fprintf(&b, "k = %s", g.cexpr(3))
		default:
			fmt.Fprintf(&b, "if k > %s {\n%s\ta = k\n%s}", g.small(), in, in)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%sreturn %s\n%s}", in, g.cexpr(3), strings.Repeat("\t", depth))
	return b.String()
}

// cstmt emits a statement of main.
func (g *mgen) cstmt() {
	f := fmt.Sprintf("f%d", g.pick(3))
	switch g.pick(10) {
	case 0, 1:
		g.line(1, "%s = %s", f, g.literal(1))
	case 2, 3:
		g.line(1, "println(%s(%s))", f, g.small())
	case 4:
		g.line(1, "println(apply(%s, %s))", f, g.small())
	case 5:
		g.line(1, "%s = mk(%s)", f, g.small())
	case 6:
		// A closure made by a closure, with a variable of its own.
		g.line(1, "%s = func() func(int) int {\n\t\tt := %s\n\t\treturn func(k int) int {\n\t\t\tt += k\n\t\t\ta++\n\t\t\treturn t\n\t\t}\n\t}()", f, g.small())
	case 7:
		// One closure for each iteration's variable of the body.
		g.line(1, "for i := 0; i < 3; i++ {\n\t\tj := i * %s\n\t\tgs = append(gs, func(k int) int {\n\t\t\tj += k\n\t\t\treturn j\n\t\t})\n\t}", g.small())
		g.line(1, "println(gs[len(gs)-1](%s), gs[0](%s))", g.small(), g.small())
	case 8:
		g.line(1, "%s = %s", []string{"a", "b", "pt.x", "arr[1]"}[g.pick(4)], g.small())
	default:
		g.line(1, "println(a, b, s, arr[0], arr[1], pt.x, pt.y, len(sl))")
	}
}
