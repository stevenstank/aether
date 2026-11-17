package main

import (
	"fmt"
	"math/rand/v2"
)

// Generated interface programs (Phase 8 increment 8.4, D-041).
//
// generateIfaces writes a program of its own, from its own random stream,
// so that the other generators' programs stay byte for byte what they
// were. Its types are fixed: S, a struct with value methods; P, a struct
// whose pointer methods include Inc, which writes it; and A, an array type
// with value methods. Its interfaces are Getter, Tagger, Both embedding
// them, and Incer. main converts values of these and of predeclared types
// to the interfaces and to any, calls methods through them, converts
// between interfaces, compares interface values, and asserts and switches
// on their types, printing what it finds.
//
// V-4: a call of Inc, the only method that writes, is the only call of its
// statement.
//
// About one program in seven ends with a failed assertion, a method call
// on a nil interface, or a comparison of uncomparable values.

// generateIfaces returns the interface program for seed.
func generateIfaces(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0x1f4c))}
	g.line(0, "package main\n")
	g.line(0, "type Getter interface{ Get() int }\n")
	g.line(0, "type Tagger interface{ Tag() string }\n")
	g.line(0, "type Both interface {\n\tGetter\n\tTagger\n}\n")
	g.line(0, "type Incer interface{ Inc() int }\n")
	g.line(0, "type S struct {\n\ta int\n\ts string\n}\n")
	g.line(0, "func (v S) Get() int     { return %s }", g.ifexpr("v.a"))
	g.line(0, "func (v S) Tag() string  { return \"S\" + v.s }\n")
	g.line(0, "type P struct{ n int }\n")
	g.line(0, "func (p *P) Get() int    { return %s }", g.ifexpr("p.n"))
	g.line(0, "func (p *P) Tag() string { return \"P\" }")
	g.line(0, "func (p *P) Inc() int    {\n\tp.n += %d\n\treturn p.n\n}\n", 1+g.pick(5))
	g.line(0, "type A [2]int\n")
	g.line(0, "func (a A) Get() int { return a[0] - a[1] }\n")
	g.line(0, "func main() {")
	g.line(1, "p := &P{%s}", g.small())
	g.line(1, "var gt Getter = S{%s, \"x\"}", g.small())
	g.line(1, "var tg Tagger = p")
	g.line(1, "var bo Both = S{%s, \"y\"}", g.small())
	g.line(1, "var in Incer = p")
	g.line(1, "var x, y any = %s, \"s0\"", g.small())
	g.line(1, "xs := []any{%s, %s, %s}", g.anyValue(), g.anyValue(), g.anyValue())
	for range 12 + g.pick(12) {
		g.istmt()
	}
	g.line(1, "println(gt.Get(), tg.Tag(), bo.Tag(), x == nil, y == nil, len(xs), p.n)")
	if g.pick(7) == 0 {
		switch g.pick(3) {
		case 0:
			g.line(1, "x = %d", g.pick(9))
			g.line(1, "println(x.(string))")
		case 1:
			g.line(1, "var z Getter")
			g.line(1, "println(z.Get())")
		default:
			g.line(1, "x, y = []int{1}, []int{1}")
			g.line(1, "println(x == y)")
		}
	}
	g.line(1, "println(in.Inc())")
	g.line(0, "}")
	return g.b.String()
}

// ifexpr returns an int expression of the field f and constants.
func (g *mgen) ifexpr(f string) string {
	return fmt.Sprintf("%s %s %s", f, []string{"+", "-", "*", "^"}[g.pick(4)], g.small())
}

// anyValue returns a value of a random type, for an any.
func (g *mgen) anyValue() string {
	switch g.pick(12) {
	case 0:
		return g.small()
	case 1:
		return fmt.Sprintf("int8(%s)", g.small())
	case 2:
		return fmt.Sprintf("\"s%d\"", g.pick(3))
	case 3:
		return fmt.Sprintf("S{%s, \"%d\"}", g.small(), g.pick(2))
	case 4:
		return "p"
	case 5:
		return fmt.Sprintf("A{%s, %s}", g.small(), g.small())
	case 6:
		return []string{"true", "false"}[g.pick(2)]
	case 7:
		return "nil"
	case 8:
		return []string{"gt", "bo", "tg"}[g.pick(3)]
	case 9:
		return fmt.Sprintf("%d.5", g.pick(3))
	case 10:
		return "&P{1}"
	}
	return "x"
}

// itype returns a type to assert or switch to, with an expression of the
// asserted value v of that type that println can print.
func (g *mgen) itype() (string, string) {
	switch g.pick(11) {
	case 0:
		return "int", "v"
	case 1:
		return "int8", "v"
	case 2:
		return "string", "len(v)"
	case 3:
		return "S", "v.a"
	case 4:
		return "*P", "v == p"
	case 5:
		return "A", "v[1]"
	case 6:
		return "bool", "v"
	case 7:
		return "Getter", "v != nil"
	case 8:
		return "Both", "v != nil"
	case 9:
		return "float64", "v"
	}
	return "Incer", "v != nil"
}

// istmt emits a statement of main.
func (g *mgen) istmt() {
	switch g.pick(11) {
	case 0, 1:
		g.line(1, "%s = %s", []string{"x", "y", "xs[0]", "xs[2]"}[g.pick(4)], g.anyValue())
	case 2:
		g.line(1, "gt = %s", []string{"S{1, \"g\"}", "p", "A{4, 1}", "bo", "Getter(bo)"}[g.pick(5)])
	case 3:
		g.line(1, "bo = %s", []string{"S{2, \"b\"}", "p"}[g.pick(2)])
	case 4:
		g.line(1, "println(gt.Get(), tg.Tag(), bo.Get(), bo.Tag())")
	case 5:
		g.line(1, "println(%s)", []string{"in.Inc()", "p.Inc()", "Incer.Inc(in)"}[g.pick(3)])
	case 6:
		// Comparisons of values whose types are comparable, or differ.
		g.line(1, "println(x == y, x == %s, gt == Getter(bo), any(gt) == x, xs[1] != nil)", []string{g.small(), "\"s1\"", "S{1, \"0\"}", "p", "nil"}[g.pick(5)])
	case 7, 8:
		t, use := g.itype()
		g.line(1, "if v, ok := %s.(%s); ok {\n\t\tprintln(\"is\", %q, %s)\n\t} else {\n\t\tprintln(\"not\", %q)\n\t}", []string{"x", "y", "xs[1]", "any(gt)"}[g.pick(4)], t, t, use, t)
	case 9:
		g.line(1, "switch v := %s.(type) {", []string{"x", "y", "xs[0]"}[g.pick(3)])
		seen := map[string]bool{}
		for range 1 + g.pick(4) {
			t, use := g.itype()
			if seen[t] {
				continue // a type may be a case once
			}
			seen[t] = true
			g.line(1, "case %s:\n\t\tprintln(%q, %s)", t, t, use)
		}
		if g.pick(2) == 0 {
			g.line(1, "case nil:\n\t\tprintln(\"nil\", v == nil)")
		}
		g.line(1, "default:\n\t\tprintln(\"default\", v != nil)\n\t}")
	default:
		g.line(1, "{\n\t\tf := %s\n\t\tprintln(f())\n\t}", []string{"gt.Get", "bo.Tag", "tg.Tag"}[g.pick(3)])
	}
}
