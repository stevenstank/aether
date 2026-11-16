package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated method programs (Phase 8 increment 8.1, D-038).
//
// generateMethods writes a program of its own, from its own random stream,
// so that the other generators' programs stay byte for byte what they were.
// Its types are fixed: A, a struct of an int, an int8, a string, and an
// array; B, embedding A; C, embedding *A; D, embedding B; and Arr, an array
// type. The bodies of their methods are random: value receivers that read
// fields, pointer receivers that write them, value receivers that change
// their own copy and return it, methods calling others on their receiver,
// promoted or reached by the implicit &. main calls them on every kind of
// receiver expression: variables, pointers, *p, embedded fields at every
// depth, through an embedded pointer, array and slice elements, literals,
// and calls.
//
// V-4: a call that may write the program's variables is the only operand of
// its statement, or println's only argument; a method argument is a
// constant or a call of a method that writes nothing.

type mgen struct {
	r     *rand.Rand
	b     strings.Builder
	names int
}

func (g *mgen) pick(n int) int { return g.r.IntN(n) }

func (g *mgen) line(depth int, format string, args ...any) {
	g.b.WriteString(strings.Repeat("\t", depth))
	fmt.Fprintf(&g.b, format, args...)
	g.b.WriteString("\n")
}

func (g *mgen) name(prefix string) string {
	g.names++
	return fmt.Sprintf("%s%d", prefix, g.names)
}

// methodGetters and methodSetters are how many getters GetN (value
// receivers that write nothing) and setters SetN (pointer receivers) A has.
const methodGetters, methodSetters = 3, 3

// constant returns an int constant: small, negative, or near the limits.
func (g *mgen) constant() string {
	switch g.pick(6) {
	case 0:
		return fmt.Sprint(-1 - g.pick(100))
	case 1:
		return []string{"9223372036854775807", "-9223372036854775808", "1 << 40", "255", "-128"}[g.pick(5)]
	}
	return fmt.Sprint(g.pick(20))
}

// term returns an int operand in a method of A whose receiver is a and
// whose parameter is p. Its constants are small, so that no constant
// expression of them overflows, and they fit int8 and int16.
func (g *mgen) term() string {
	return []string{"a.x", "int(a.y)", "int(a.v[0])", "int(a.v[1])", "len(a.s)", "p", "p", fmt.Sprint(g.pick(120) - 100)}[g.pick(8)]
}

// intExpr returns an int expression of terms.
func (g *mgen) intExpr(n int) string {
	e := g.term()
	for i := 1; i < n; i++ {
		e = fmt.Sprintf("(%s %s %s)", e, []string{"+", "-", "*", "^", "&", "|"}[g.pick(6)], g.term())
	}
	return e
}

// methods emits the types and their methods.
func (g *mgen) methods() {
	g.line(0, "type A struct {\n\tx int\n\ty int8\n\ts string\n\tv [2]int16\n}\n")
	g.line(0, "type B struct {\n\tA\n\tk uint8\n}\n")
	g.line(0, "type C struct {\n\t*A\n\tn int\n}\n")
	g.line(0, "type D struct {\n\tB\n\tm int\n}\n")
	g.line(0, "type Arr [3]int\n")
	for i := 0; i < methodGetters; i++ {
		g.line(0, "func (a A) Get%d(p int) int { return %s }\n", i, g.intExpr(1+g.pick(4)))
	}
	for i := 0; i < methodSetters; i++ {
		g.line(0, "func (a *A) Set%d(p int) int {", i)
		for n := 1 + g.pick(3); n > 0; n-- {
			switch g.pick(5) {
			case 0:
				g.line(1, "a.y += int8(%s)", g.term())
			case 1:
				g.line(1, "a.v[%d] ^= int16(%s)", g.pick(2), g.term())
			case 2:
				if g.pick(2) == 0 {
					g.line(1, "a.s = a.s + %q", string(rune('a'+g.pick(26))))
				} else {
					g.line(1, "a.s = a.s[len(a.s)/2:]")
				}
			default:
				g.line(1, "a.x = %s", g.intExpr(1+g.pick(3)))
			}
		}
		g.line(1, "return a.x")
		g.line(0, "}\n")
	}
	// A value receiver is its own variable: changing it, even through the
	// implicit & of a pointer method, leaves the caller's value alone.
	g.line(0, "func (a A) Copy(p int) A {\n\ta.x += p\n\ta.Set%d(p)\n\treturn a\n}\n", g.pick(methodSetters))
	g.line(0, "func (a A) Chain(p int) int {\n\tr := a.Get%d(p)\n\tr += a.Set%d(p)\n\treturn r + a.x\n}\n", g.pick(methodGetters), g.pick(methodSetters))
	// Without the implicit &, the receiver stays the caller's copy (D-034),
	// which the method writes.
	g.line(0, "func (a A) Twist(p int) int {\n\ta.x += p\n\ta.v[%d]++\n\ta.s = a.s + \"t\"\n\treturn a.x + int(a.v[0]) + len(a.s)\n}\n", g.pick(2))
	g.line(0, "func (a *A) Nil() bool { return a == nil }\n")
	g.line(0, "func (b *B) Bump(p int) int {\n\tb.k++\n\tb.Set%d(p)\n\treturn b.Get%d(int(b.k))\n}\n", g.pick(methodSetters), g.pick(methodGetters))
	g.line(0, "func (d D) Total(p int) int {\n\tt := d.Get%d(p)\n\treturn t + d.m + int(d.k)\n}\n", g.pick(methodGetters))
	g.line(0, "func (c C) Through(p int) int { return c.Set%d(p) + c.n }\n", g.pick(methodSetters))
	g.line(0, "func (r Arr) Sum() int { return r[0] - r[1] + r[2] }\n")
	g.line(0, "func (r *Arr) Rot(p int) {\n\tr[0], r[1], r[2] = r[1], r[2], r[0]+p\n}\n")
	g.line(0, "func mkA(x int) A { return A{x: x, y: int8(x), s: \"mk\"} }\n")
}

// aLit returns an A literal.
func (g *mgen) aLit() string {
	return fmt.Sprintf("A{%s, %d, %q, [2]int16{%d, %d}}", g.constant(), g.pick(256)-128, strings.Repeat("s", g.pick(4)),
		g.pick(1000)-500, g.pick(1000)-500)
}

// receiver returns an expression whose method set has A's value methods,
// and if addr, its pointer methods too.
func (g *mgen) receiver(addr bool) string {
	addressable := []string{"a", "pa", "(*pa)", "b", "b.A", "pb", "c", "c.A", "d", "d.B", "d.B.A", "pd", "pd.A",
		"as[1]", "as[i]", "aa[0]", "aa[i]", "pc", "ad[i].B"}
	if !addr && g.pick(5) == 0 {
		return []string{g.aLit(), "mkA(" + g.constant() + ")", "B{A: " + g.aLit() + "}", "D{}"}[g.pick(4)]
	}
	return addressable[g.pick(len(addressable))]
}

// arg returns a method argument that writes nothing.
func (g *mgen) arg() string {
	if g.pick(4) == 0 {
		return fmt.Sprintf("%s.Get%d(%s)", g.receiver(false), g.pick(methodGetters), g.constant())
	}
	return g.constant()
}

// stmt emits a statement of main.
func (g *mgen) stmt() {
	switch g.pick(11) {
	case 0, 1:
		g.line(1, "println(%s.Get%d(%s))", g.receiver(false), g.pick(methodGetters), g.arg())
	case 2, 3:
		call := fmt.Sprintf("%s.Set%d(%s)", g.receiver(true), g.pick(methodSetters), g.arg())
		if g.pick(2) == 0 {
			g.line(1, "%s", call)
			return
		}
		t := g.name("t")
		g.line(1, "%s := %s", t, call)
		g.line(1, "println(%s)", t)
	case 4:
		cp := g.name("cp")
		g.line(1, "%s := %s.Copy(%s)", cp, g.receiver(false), g.arg())
		g.line(1, "println(%s.x, %s.y, %s.s, %s.v[0], %s.v[1])", cp, cp, cp, cp, cp)
	case 5:
		g.line(1, "println(%s.%s(%s))", g.receiver(false), []string{"Chain", "Twist"}[g.pick(2)], g.arg())
	case 6:
		g.line(1, "println(%s.Bump(%s))", []string{"b", "pb", "d", "d.B", "pd", "ad[i]"}[g.pick(6)], g.arg())
	case 7:
		g.line(1, "println(%s.Total(%s))", []string{"d", "pd", "(*pd)", "ad[0]", "D{}", "D{m: 3}"}[g.pick(6)], g.arg())
	case 8:
		g.line(1, "println(%s.Through(%s))", []string{"c", "(*pc)", "C{&a, 7}", "C{A: &as[0]}"}[g.pick(4)], g.arg())
	case 9:
		if g.pick(2) == 0 {
			g.line(1, "%s.Rot(%s)", []string{"arr", "(*parr)", "parr"}[g.pick(3)], g.arg())
		} else {
			g.line(1, "println(%s.Sum())", []string{"arr", "(*parr)", "parr", "Arr{1, 2, 3}"}[g.pick(4)])
		}
	default:
		g.line(1, "println(np.Nil(), %s.Nil(), c.Nil(), b.Nil())", g.receiver(true))
	}
}

// generateMethods returns a method program for seed.
func generateMethods(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0x3e7d))}
	g.line(0, "package main\n")
	g.methods()
	g.line(0, "func main() {")
	g.line(1, "i := 1")
	g.line(1, "a := %s", g.aLit())
	g.line(1, "pa := &a")
	g.line(1, "b := B{%s, %d}", g.aLit(), g.pick(256))
	g.line(1, "pb := &b")
	g.line(1, "c := C{pa, %s}", g.constant())
	g.line(1, "pc := &c")
	g.line(1, "d := D{B{%s, %d}, %s}", g.aLit(), g.pick(256), g.constant())
	g.line(1, "pd := &d")
	g.line(1, "as := []A{%s, %s}", g.aLit(), g.aLit())
	g.line(1, "var aa [2]A")
	g.line(1, "ad := make([]D, 2)")
	g.line(1, "var arr Arr")
	g.line(1, "parr := &arr")
	g.line(1, "var np *A")
	for n := 10 + g.pick(30); n > 0; n-- {
		g.stmt()
	}
	if g.pick(6) == 0 {
		// A deliberate nil dereference: a value method through a nil
		// pointer, or through a nil embedded pointer.
		g.line(1, "println(\"end\")")
		if k := g.pick(4); k == 0 {
			g.line(1, "println(np.Get0(1))")
		} else {
			g.line(1, "var cz C")
			g.line(1, "println(%s)", []string{"cz.Get1(2)", "cz.Set0(3)", "cz.Chain(4)"}[k-1])
		}
	}
	for _, v := range []string{"a", "b.A", "d.B.A", "as[0]", "as[1]", "aa[0]", "aa[1]", "ad[0].A", "ad[1].A"} {
		g.line(1, "println(%s.x, %s.y, %s.s, %s.v[0], %s.v[1])", v, v, v, v, v)
	}
	g.line(1, "println(b.k, d.k, d.m, c.n, arr[0], arr[1], arr[2], ad[1].k)")
	g.line(1, "println(pa == nil, pb == nil, pc == nil, pd == nil, parr == nil, np == nil, i)")
	g.line(0, "}")
	return g.b.String()
}
