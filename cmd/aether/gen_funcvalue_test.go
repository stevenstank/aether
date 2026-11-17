package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated func value programs (Phase 8 increment 8.2, D-039).
//
// generateFuncValues writes a program of its own, from its own random
// stream, so that the other generators' programs stay byte for byte what
// they were. Its functions F0 to F4, of type func(int, int) int, have
// random bodies; S holds an int and a func value, with a value method Val
// and a pointer method Bump, and W embeds S and holds another func value.
// main keeps func values in variables, a slice, an array, struct fields,
// and results of calls, makes method values and method expressions of
// Val and Bump, directly and promoted through W, and calls them all,
// directly and through higher-order functions.
//
// V-4: a call that writes a variable, Bump's, is the only operand of its
// statement, and its argument a constant; every other call is of a
// function that writes nothing, and a method value is made in a statement
// of its own.
//
// About one program in seven ends with a call of a nil func value, or a
// method value of Val through a nil *S, which both panic.

// generateFuncValues returns the func value program for seed.
func generateFuncValues(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0xf00d))}
	g.line(0, "package main\n")
	for i := range 5 {
		g.line(0, "func F%d(x, y int) int { return %s }\n", i, g.fexpr(3))
	}
	g.line(0, "type S struct {\n\tx int\n\tf func(int, int) int\n}\n")
	g.line(0, "func (s S) Val(k int) int { return %s }\n", strings.NewReplacer("x", "s.x", "y", "k").Replace(g.fexpr(2)))
	g.line(0, "func (s *S) Bump(k int) int {\n\ts.x += k\n\treturn s.x\n}\n")
	g.line(0, "type W struct {\n\tS\n\tg func(int, int) int\n}\n")
	g.line(0, "func apply(f func(int, int) int, a, b int) int { return f(a, b) }\n")
	g.line(0, "func twice(f func(int) int, v int) int { return f(f(v)) }\n")
	g.line(0, "func choose(i int) func(int, int) int {\n\tswitch i %% 3 {\n\tcase 0:\n\t\treturn F%d\n\tcase 1:\n\t\treturn F%d\n\t}\n\treturn F%d\n}\n",
		g.pick(5), g.pick(5), g.pick(5))
	g.line(0, "var gf = F%d\n", g.pick(5))
	g.line(0, "func main() {")
	g.line(1, "fs := []func(int, int) int{F0, F1, F2, F3, F4}")
	g.line(1, "arr := [3]func(int, int) int{F%d, F%d, F%d}", g.pick(5), g.pick(5), g.pick(5))
	g.line(1, "s := S{%s, F%d}", g.small(), g.pick(5))
	g.line(1, "w := W{S{%s, F%d}, F%d}", g.small(), g.pick(5), g.pick(5))
	g.line(1, "v0, v1 := F%d, F%d", g.pick(5), g.pick(5))
	g.line(1, "sv, wv := s.Val, w.Val")
	g.line(1, "bs, bw := s.Bump, w.Bump")
	g.line(1, "acc := %s", g.small())
	g.line(1, "println(acc, len(fs), v0 != nil, gf == nil)")
	for range 12 + g.pick(12) {
		g.fstmt()
	}
	g.line(1, "println(v0 != nil, v1(1, 2), sv(3), wv(4), bs != nil, bw != nil, arr[0] != nil)")
	if g.pick(7) == 0 {
		if g.pick(2) == 0 {
			g.line(1, "var z func(int, int) int")
			g.line(1, "println(z(%s, %s))", g.small(), g.small())
		} else {
			g.line(1, "var np *S")
			g.line(1, "nv := np.Val")
			g.line(1, "println(nv(1))")
		}
	}
	g.line(0, "}")
	return g.b.String()
}

// small returns a small int constant.
func (g *mgen) small() string { return fmt.Sprint(g.pick(41) - 20) }

// fexpr returns an int expression of x, y, and constants.
func (g *mgen) fexpr(n int) string {
	t := func() string { return []string{"x", "y", "x", "y", g.small()}[g.pick(5)] }
	e := t()
	for i := 1; i < n; i++ {
		e = fmt.Sprintf("(%s %s %s)", e, []string{"+", "-", "*", "^", "&", "|"}[g.pick(6)], t())
	}
	return e
}

// funcExpr returns an expression of type func(int, int) int that is not nil.
func (g *mgen) funcExpr() string {
	switch g.pick(9) {
	case 0:
		return fmt.Sprintf("F%d", g.pick(5))
	case 1:
		return fmt.Sprintf("choose(%d)", g.pick(9))
	case 2:
		return fmt.Sprintf("fs[%d]", g.pick(5))
	case 3:
		return fmt.Sprintf("arr[%d]", g.pick(3))
	case 4:
		return []string{"s.f", "w.f", "w.g", "w.S.f"}[g.pick(4)]
	case 5:
		return "gf"
	}
	return []string{"v0", "v1"}[g.pick(2)]
}

// operand returns an int operand of a call: a constant or acc.
func (g *mgen) operand() string {
	if g.pick(3) == 0 {
		return "acc"
	}
	return g.small()
}

// pureCall returns a call that writes nothing, of type int.
func (g *mgen) pureCall() string {
	a, b := g.operand(), g.operand()
	switch g.pick(10) {
	case 0:
		return fmt.Sprintf("apply(%s, %s, %s)", g.funcExpr(), a, b)
	case 1:
		return fmt.Sprintf("%s(%s)", []string{"sv", "wv", "s.Val", "w.Val", "w.S.Val"}[g.pick(5)], a)
	case 2:
		if g.pick(2) == 0 {
			return fmt.Sprintf("S.Val(s, %s)", a)
		}
		return fmt.Sprintf("(*S).Val(&s, %s)", a)
	case 3:
		return fmt.Sprintf("W.Val(w, %s)", a)
	case 4:
		return fmt.Sprintf("twice(%s, %s)", []string{"sv", "wv", "s.Val", "w.Val"}[g.pick(4)], a)
	}
	return fmt.Sprintf("%s(%s, %s)", g.funcExpr(), a, b)
}

// fstmt emits a statement of main.
func (g *mgen) fstmt() {
	switch g.pick(12) {
	case 0, 1:
		g.line(1, "%s = %s", []string{"v0", "v1", "gf", "s.f", "w.g", "w.f", "fs[" + fmt.Sprint(g.pick(5)) + "]", "arr[" + fmt.Sprint(g.pick(3)) + "]"}[g.pick(8)], g.funcExpr())
	case 2, 3, 4:
		g.line(1, "println(%s, %s)", g.pureCall(), g.pureCall())
	case 5:
		g.line(1, "acc = %s", g.pureCall())
	case 6:
		// Method values, made in statements of their own (V-4).
		g.line(1, "%s", []string{"sv = s.Val", "wv = w.Val", "sv = w.S.Val", "bs = s.Bump", "bw = w.Bump", "bs = (&w).Bump", "sv = (&s).Val"}[g.pick(7)])
	case 7:
		// The only call of its statement, which writes s.x or w.x.
		g.line(1, "println(%s)", []string{
			fmt.Sprintf("bs(%s)", g.small()), fmt.Sprintf("bw(%s)", g.small()),
			fmt.Sprintf("(*S).Bump(&s, %s)", g.small()), fmt.Sprintf("s.Bump(%s)", g.small()),
			fmt.Sprintf("(*W).Bump(&w, %s)", g.small())}[g.pick(5)])
	case 8:
		g.line(1, "%s = %s", []string{"s.x", "w.x", "w.S.x"}[g.pick(3)], g.small())
	case 9:
		g.line(1, "for i := 0; i < len(fs); i++ {\n\t\tacc = fs[i](acc, i) ^ arr[i%%3](i, %s)\n\t}", g.small())
	case 10:
		g.line(1, "println(%s == nil, %s != nil, s.x, w.x)", g.funcExpr(), g.funcExpr())
	default:
		g.line(1, "if %s > %s {\n\t\tacc = %s\n\t} else {\n\t\tacc = acc + 1\n\t}", g.pureCall(), g.operand(), g.pureCall())
	}
}
