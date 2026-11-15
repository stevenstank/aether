package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated slice programs (Phase 7 increment 7.2, D-036).
//
// generateSlices writes a program of its own, from its own random stream,
// so that generate's programs stay byte for byte what they were. Its main
// is straight-line code (loops only fill elements), so the generator knows
// the length and capacity of every slice at every point, and which slices
// share an array (a group): it keeps every index and bound in range, except
// in one deliberate panic at the end of some programs.
//
// V-6: after an append grows a slice, gc's capacity and Aether's differ.
// Such a capacity is uncertain: it is never printed, a bound is checked
// against the length instead, and the slice, or one sliced from it, is
// appended to only when no other variable shares its array, so that whether
// the append grows cannot show. A three-index slice makes a capacity certain
// again. Appending within a certain capacity shares the array, as the spec
// requires, and the programs observe that.
//
// V-4: a call or copy, which may write a slice's elements, is the only
// operand of its statement, or println's only argument.

// sliceElems are the element types of generated slices: E is a zero-size
// struct, and P a struct of an int8 and a string.
var sliceElems = []string{"int", "uint8", "float64", "string", "P", "E"}

// slc is a slice variable as the generator models it.
type slc struct {
	elem     string
	len, cap int
	certain  bool // cap is the same under gc and Aether (V-6)
	group    int  // the array it shares
}

type sgen struct {
	r      *rand.Rand
	b      strings.Builder
	vars   map[string]*slc
	order  []string
	arrays []string // local [6]int arrays, and the global ga
	groups int
	names  int
}

func (g *sgen) pick(n int) int { return g.r.IntN(n) }

func (g *sgen) line(depth int, format string, args ...any) {
	g.b.WriteString(strings.Repeat("\t", depth))
	fmt.Fprintf(&g.b, format, args...)
	g.b.WriteString("\n")
}

func (g *sgen) name(prefix string) string {
	g.names++
	return fmt.Sprintf("%s%d", prefix, g.names)
}

func (g *sgen) group() int {
	g.groups++
	return g.groups
}

// value returns an expression of the element type t.
func (g *sgen) value(t string) string {
	switch t {
	case "int":
		return []string{"0", "1", "-7", "42", "9223372036854775807", "-9223372036854775808"}[g.pick(6)]
	case "uint8":
		return []string{"0", "1", "200", "255"}[g.pick(4)]
	case "float64":
		return []string{"0", "0.5", "-2.25", "1e10"}[g.pick(4)]
	case "string":
		return []string{`""`, `"a"`, `"xyz"`, `"héllo"`}[g.pick(4)]
	case "P":
		return fmt.Sprintf("P{%s, %s}", []string{"0", "-1", "127"}[g.pick(3)], g.value("string"))
	}
	return "E{}"
}

// show returns printable expressions for the element e of type t.
func show(e, t string) []string {
	switch t {
	case "P":
		return []string{e + ".a", e + ".s"}
	case "E":
		return nil
	}
	return []string{e}
}

// declare adds the slice variable name.
func (g *sgen) declare(name string, s slc) {
	g.vars[name] = &s
	g.order = append(g.order, name)
}

// pickVar returns a slice variable for which ok holds, or "".
func (g *sgen) pickVar(ok func(*slc) bool) string {
	var fit []string
	for _, n := range g.order {
		if ok(g.vars[n]) {
			fit = append(fit, n)
		}
	}
	if len(fit) == 0 {
		return ""
	}
	return fit[g.pick(len(fit))]
}

// alone reports whether no variable but name shares its array.
func (g *sgen) alone(name string) bool {
	for _, n := range g.order {
		if n != name && g.vars[n].group == g.vars[name].group {
			return false
		}
	}
	return true
}

// top is the largest bound a slice expression of s may use: its capacity if
// certain, and otherwise its length.
func top(s *slc) int {
	if s.certain {
		return s.cap
	}
	return s.len
}

// sliceOf returns a slice expression of s, named n, with bounds in range,
// and its model.
func (g *sgen) sliceOf(n string, s *slc) (string, slc) {
	lo := g.pick(s.len + 1)
	if g.pick(3) == 0 {
		hi := lo + g.pick(top(s)-lo+1)
		m := hi + g.pick(top(s)-hi+1)
		return fmt.Sprintf("%s[%d:%d:%d]", n, lo, hi, m), slc{s.elem, hi - lo, m - lo, true, s.group}
	}
	hi := lo + g.pick(top(s)-lo+1)
	r := slc{s.elem, hi - lo, s.cap - lo, s.certain, s.group}
	switch {
	case hi == s.len && g.pick(2) == 0:
		return fmt.Sprintf("%s[%d:]", n, lo), r
	case lo == 0 && g.pick(2) == 0:
		return fmt.Sprintf("%s[:%d]", n, hi), r
	}
	return fmt.Sprintf("%s[%d:%d]", n, lo, hi), r
}

// appended returns the model of s after k elements are appended to it: the
// array is shared if the capacity is certain and enough, and otherwise new,
// with an uncertain capacity (V-6).
func (g *sgen) appended(s *slc, k int) slc {
	r := *s
	r.len += k
	if !s.certain || r.len > s.cap {
		r.cap, r.certain, r.group = r.len, false, g.group()
	}
	return r
}

// generateSlices returns a slice program for seed.
func generateSlices(seed uint64) string {
	g := &sgen{r: rand.New(rand.NewPCG(seed, 0x511ce)), vars: map[string]*slc{}}
	g.line(0, "package main\n\ntype P struct {\n\ta int8\n\ts string\n}\n\ntype E struct{}\n\ntype H struct {\n\tn int\n\ts []int\n}\n")
	g.line(0, "var gs = []int{3, 1, 2}\n\nvar ga [6]int\n")
	g.declare("gs", slc{"int", 3, 3, true, g.group()})
	g.arrays = append(g.arrays, "ga")
	g.line(0, "func sum(s []int) int {\n\tt := 0\n\tfor i := 0; i < len(s); i++ {\n\t\tt += s[i]\n\t}\n\treturn t\n}\n")
	for _, t := range sliceElems {
		g.line(0, "func fill%s(s []%s, v %s) {\n\tfor i := 0; i < len(s); i++ {\n\t\ts[i] = v\n\t}\n}\n", t, t, t)
		g.line(0, "func grow%s(s []%s, v %s, k int) []%s {\n\tfor i := 0; i < k; i++ {\n\t\ts = append(s, v)\n\t}\n\treturn s\n}\n", t, t, t, t)
	}
	g.line(0, "func main() {")
	g.line(1, "var h H")
	g.declare("h.s", slc{"int", 0, 0, true, g.group()})
	for n := 10 + g.pick(30); n > 0; n-- {
		g.stmt()
	}
	if g.pick(5) == 0 {
		g.panicStmt()
	}
	var uses []string
	for _, n := range g.order {
		uses = append(uses, fmt.Sprintf("len(%s)", n))
	}
	for _, a := range g.arrays {
		uses = append(uses, a+"[5]")
	}
	g.line(1, "println(h.n, %s)", strings.Join(uses, ", "))
	g.line(0, "}")
	return g.b.String()
}

// stmt emits a statement of main.
func (g *sgen) stmt() {
	switch k := g.pick(12); {
	case k <= 2 || len(g.order) < 3:
		g.declStmt()
	case k == 3:
		// An element written, or changed in place.
		n := g.pickVar(func(s *slc) bool { return s.len > 0 })
		if n == "" {
			return
		}
		s := g.vars[n]
		i := fmt.Sprint(g.pick(s.len))
		if g.pick(3) == 0 {
			i = fmt.Sprintf("len(%s)-1", n)
		}
		switch {
		case s.elem == "int" && g.pick(2) == 0:
			g.line(1, "%s[%s] %s %s", n, i, []string{"+=", "*=", "^="}[g.pick(3)], g.value("int"))
		case s.elem == "uint8" && g.pick(2) == 0:
			g.line(1, "%s[%s]++", n, i)
		default:
			g.line(1, "%s[%s] = %s", n, i, g.value(s.elem))
		}
	case k <= 5:
		g.appendStmt()
	case k == 6:
		// copy, perhaps between overlapping parts of one array.
		d := g.pickVar(func(*slc) bool { return true })
		ds := g.vars[d]
		src := g.pickVar(func(s *slc) bool { return s.elem == ds.elem })
		de, _ := g.sliceOf(d, ds)
		se, _ := g.sliceOf(src, g.vars[src])
		c := g.name("c")
		g.line(1, "%s := copy(%s, %s)", c, de, se)
		g.line(1, "println(%s)", c)
	case k == 7:
		// fill, a call that writes the elements, alone in its statement.
		n := g.pickVar(func(*slc) bool { return true })
		g.line(1, "fill%s(%s, %s)", g.vars[n].elem, n, g.value(g.vars[n].elem))
	case k == 8:
		n := g.pickVar(func(s *slc) bool { return s.elem == "int" })
		g.line(1, "println(sum(%s))", n)
	case k == 9 && g.vars["h.s"].len < 20:
		// The slice in a struct: assigned another, or appended to.
		if n := g.pickVar(func(s *slc) bool { return s.elem == "int" }); n != "h.s" && g.pick(2) == 0 {
			g.line(1, "h.s = %s", n)
			*g.vars["h.s"] = *g.vars[n]
		} else if g.vars["h.s"].certain || g.alone("h.s") {
			v := g.value("int")
			g.line(1, "h.s = append(h.s, %s)", v)
			*g.vars["h.s"] = g.appended(g.vars["h.s"], 1)
		}
		g.line(1, "h.n += len(h.s)")
	default:
		g.printStmt()
	}
}

// declStmt declares a slice: nil, empty, a literal, made, sliced from
// another, an array, or a pointer to one, or appended to another.
func (g *sgen) declStmt() {
	n := g.name("s")
	t := sliceElems[g.pick(len(sliceElems))]
	switch g.pick(8) {
	case 0:
		if g.pick(2) == 0 {
			g.line(1, "var %s []%s", n, t)
		} else {
			g.line(1, "%s := []%s(nil)", n, t)
		}
		g.declare(n, slc{t, 0, 0, true, g.group()})
	case 1:
		var elts []string
		k := g.pick(6)
		for i := 0; i < k; i++ {
			elts = append(elts, g.value(t))
		}
		if k > 0 && g.pick(3) == 0 {
			elts[k-1] = fmt.Sprintf("%d: %s", k+1, elts[k-1])
			k += 2
		}
		g.line(1, "%s := []%s{%s}", n, t, strings.Join(elts, ", "))
		g.declare(n, slc{t, k, k, true, g.group()})
	case 2:
		l := g.pick(8)
		c := l + g.pick(6)
		if g.pick(2) == 0 {
			g.line(1, "%s := make([]%s, %d)", n, t, l)
			c = l
		} else {
			g.line(1, "%s := make([]%s, %d, %d)", n, t, l, c)
		}
		g.declare(n, slc{t, l, c, true, g.group()})
	case 3, 4:
		src := g.pickVar(func(*slc) bool { return true })
		if src == "" {
			g.line(1, "var %s []%s", n, t)
			g.declare(n, slc{t, 0, 0, true, g.group()})
			return
		}
		e, s := g.sliceOf(src, g.vars[src])
		g.line(1, "%s := %s", n, e)
		g.declare(n, s)
	case 5:
		// An array, local or global, perhaps through a pointer: the
		// variable shares it, at a constant offset.
		a := g.arrays[g.pick(len(g.arrays))]
		if g.pick(3) == 0 {
			a = g.name("a")
			g.line(1, "var %s [6]int", a)
			g.arrays = append(g.arrays, a)
		}
		base := slc{"int", 6, 6, true, 0}
		for i, x := range g.arrays {
			if x == a {
				base.group = -1 - i
			}
		}
		at := a
		if g.pick(2) == 0 {
			at = "(&" + a + ")"
		}
		e, s := g.sliceOf(at, &base)
		g.line(1, "%s := %s", n, e)
		g.declare(n, s)
	default:
		src := g.pickVar(func(s *slc) bool { return s.certain })
		if src == "" {
			return
		}
		s := g.vars[src]
		k := 1 + g.pick(3)
		if g.pick(2) == 0 {
			g.line(1, "%s := grow%s(%s, %s, %d)", n, s.elem, src, g.value(s.elem), k)
		} else {
			var vs []string
			for i := 0; i < k; i++ {
				vs = append(vs, g.value(s.elem))
			}
			g.line(1, "%s := append(%s, %s)", n, src, strings.Join(vs, ", "))
		}
		g.declare(n, g.appended(s, k))
	}
}

// appendStmt appends to a slice variable, assigning it the result: values,
// another slice's elements, or by grow.
func (g *sgen) appendStmt() {
	n := g.pickVar(func(s *slc) bool { return s.len < 40 })
	if n == "" || n == "h.s" || !g.vars[n].certain && !g.alone(n) {
		return
	}
	s := g.vars[n]
	switch g.pick(3) {
	case 0:
		src := g.pickVar(func(o *slc) bool { return o.elem == s.elem })
		e, o := g.sliceOf(src, g.vars[src])
		g.line(1, "%s = append(%s, %s...)", n, n, e)
		*s = g.appended(s, o.len)
	case 1:
		k := 1 + g.pick(4)
		g.line(1, "%s = grow%s(%s, %s, %d)", n, s.elem, n, g.value(s.elem), k)
		*s = g.appended(s, k)
	default:
		var vs []string
		for i := g.pick(4); i >= 0; i-- {
			vs = append(vs, g.value(s.elem))
		}
		g.line(1, "%s = append(%s, %s)", n, n, strings.Join(vs, ", "))
		*s = g.appended(s, len(vs))
	}
}

// printStmt prints a slice's length, its capacity if certain, whether it
// is nil, and up to three elements.
func (g *sgen) printStmt() {
	n := g.pickVar(func(*slc) bool { return true })
	s := g.vars[n]
	args := []string{fmt.Sprintf("len(%s)", n), n + " == nil"}
	if s.certain {
		args = append(args, fmt.Sprintf("cap(%s)", n))
	}
	for i := 0; i < min(3, s.len); i++ {
		args = append(args, show(fmt.Sprintf("%s[%d]", n, g.pick(s.len)), s.elem)...)
	}
	g.line(1, "println(%s)", strings.Join(args, ", "))
}

// panicStmt ends main with a run-time error of slices: an index, a slice
// expression's bounds, make's length or capacity, a nil pointer to an
// array, or growth beyond the largest int. Its operands are variables, so
// that go/types cannot reject a constant one.
func (g *sgen) panicStmt() {
	n := g.pickVar(func(*slc) bool { return true })
	s := g.vars[n]
	k := g.name("k")
	switch g.pick(8) {
	case 0:
		g.line(1, "%s := %d", k, s.len+g.pick(3))
		if e := show(fmt.Sprintf("%s[%s]", n, k), s.elem); e != nil {
			g.line(1, "println(%s)", e[0])
		} else {
			g.line(1, "%s[%s] = E{}", n, k)
		}
	case 1:
		g.line(1, "%s := -1", k)
		g.line(1, "%s[%s] = %s", n, k, g.value(s.elem))
	case 2:
		if !s.certain {
			return
		}
		g.line(1, "%s := %d", k, s.cap+1+g.pick(2))
		g.line(1, "println(len(%s[:%s]))", n, k)
	case 3:
		g.line(1, "%s := %d", k, s.len+1)
		g.line(1, "println(len(%s[%s:]))", n, k)
	case 4:
		if !s.certain {
			return
		}
		g.line(1, "%s := %d", k, s.cap+1)
		g.line(1, "println(len(%s[0:0:%s]))", n, k)
	case 5:
		g.line(1, "%s := %s", k, []string{"-3", "1 << 60"}[g.pick(2)])
		g.line(1, "println(len(make([]int, %s)))", k)
	case 6:
		g.line(1, "%s := 5", k)
		g.line(1, "println(len(make([]%s, %s, 4)))", s.elem, k)
	default:
		if g.pick(2) == 0 {
			g.line(1, "var %s *[3]int", k)
			g.line(1, "println(len(%s[1:]))", k)
			return
		}
		g.line(1, "%s := make([]E, 1<<63-1)", k)
		g.line(1, "%s = append(%s, E{})", k, k)
		g.line(1, "println(len(%s))", k)
	}
}
