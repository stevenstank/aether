package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated map programs (Phase 8 increment 8.5, D-042).
//
// generateMaps writes a program of its own, from its own random stream, so
// that the other generators' programs stay byte for byte what they were.
// main makes maps of int, string, struct, interface, and float64 keys,
// with int, string, slice, and nested map elements, then runs random
// statements on them: assignments, op=, ++ and --, comma-ok reads,
// deletes, clears, tuple assignments, loops that grow a map and delete
// from it, and calls of a function that changes a map it is passed. It
// prints lengths and values between them, and at the end every entry,
// found by looking up each key of the map's small key domain: without
// range, nothing printed depends on the iteration order (V-8).
//
// V-4: a call is the only call of its statement, and its arguments read
// no map it changes.
//
// About one program in seven ends with an assignment to a nil map or a
// lookup of an unhashable key.

// mapStrs are the string keys, which mapStr combines.
var mapStrs = []string{`""`, `"a"`, `"b"`, `"ab"`, `"ba"`}

// generateMaps returns the map program for seed.
func generateMaps(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0x3a95))}
	g.line(0, "package main\n")
	g.line(0, "type P struct {\n\tx int\n\ts string\n}\n")
	g.line(0, "func bump(m map[string]int, k string, d int) {\n\tm[k] += d\n}\n")
	g.line(0, "func fresh(n int) map[int]string {\n\tm := make(map[int]string, n)\n\tfor i := 0; i < n; i++ {\n\t\tm[i] = string(rune('a' + i%%26))\n\t}\n\treturn m\n}\n")
	g.line(0, "func main() {")
	g.line(1, "a, b := %s, %s", g.small(), g.small())
	var lit []string
	used := map[int]bool{}
	for range g.pick(5) {
		if k := g.pick(64); !used[k] {
			used[k] = true // go/types rejects a repeated constant key
			lit = append(lit, fmt.Sprintf("%d: %s", k, g.small()))
		}
	}
	g.line(1, "ints := map[int]int{%s}", strings.Join(lit, ", "))
	g.line(1, "strs := map[string]int{%s: 1}", g.mapStr())
	g.line(1, "ps := map[P][]int{}")
	g.line(1, "anys := map[any]int{%s: 2}", g.anyKey())
	g.line(1, "fl := map[float64]string{}")
	g.line(1, "nest := map[string]map[int]string{\"x\": fresh(%d)}", g.pick(20))
	g.line(1, "var nm map[int]int")
	g.line(1, "z := 0.0")
	for range 12 + g.pick(20) {
		g.mapStmt()
	}
	g.line(1, "{")
	g.line(2, "s, c := 0, 0")
	g.line(2, "for i := 0; i < 64; i++ {\n\t\t\tif v, ok := ints[i]; ok {\n\t\t\t\ts += v * (i + 1)\n\t\t\t\tc++\n\t\t\t}\n\t\t}")
	g.line(2, "println(s, c, len(ints), a, b)")
	g.line(1, "}")
	var keys []string
	for _, s := range mapStrs {
		keys = append(keys, fmt.Sprintf("strs[%s]", s))
	}
	g.line(1, "println(%s, len(strs))", strings.Join(keys, ", "))
	g.line(1, "for i := 0; i < 4; i++ {")
	for _, s := range mapStrs {
		g.line(2, "if v, ok := ps[P{i, %s}]; ok {\n\t\t\tprintln(i, %s, len(v), v[0], v[len(v)-1])\n\t\t}", s, s)
	}
	g.line(2, "println(anys[i], anys[int8(i)], anys[P{i, \"a\"}], fl[float64(i)*0.5], fl[-float64(i)*0.5])")
	g.line(1, "}")
	keys = keys[:0]
	for _, s := range mapStrs {
		keys = append(keys, fmt.Sprintf("anys[%s]", s))
	}
	g.line(1, "println(%s, len(anys), len(ps), len(fl), len(nest), len(nest[\"x\"]), nest[\"x\"][3], len(nm), fl[z], fl[-z])", strings.Join(keys, ", "))
	if g.pick(7) == 0 {
		switch g.pick(3) {
		case 0:
			g.line(1, "nm[%s] = 1", g.mapInt())
		case 1:
			g.line(1, "println(anys[[]int{%s}])", g.small())
		default:
			g.line(1, "delete(anys, P{1, \"a\"})\n\tanys[map[int]int{}]++")
		}
	}
	g.line(0, "}")
	return g.b.String()
}

// mapInt returns an int expression: of a and b, constants, reads of ints,
// and lengths.
func (g *mgen) mapInt() string {
	terms := func() string {
		return []string{"a", "b", "a", "b", g.small(), g.small(), fmt.Sprintf("ints[%d]", g.pick(64)), "len(ints)", "len(strs)", "strs[\"a\"]", "anys[1]"}[g.pick(11)]
	}
	e := terms()
	for range g.pick(3) {
		e = fmt.Sprintf("(%s %s %s)", e, []string{"+", "-", "*", "^", "&", "|"}[g.pick(6)], terms())
	}
	return e
}

// mapKey returns a key of ints, in 0 to 63.
func (g *mgen) mapKey() string {
	if g.pick(3) == 0 {
		return fmt.Sprint(g.pick(64))
	}
	return fmt.Sprintf("(%s) & 63", g.mapInt())
}

// mapStr returns a string key, one of mapStrs, perhaps made at run time.
func (g *mgen) mapStr() string {
	if g.pick(3) == 0 {
		return []string{`"a" + "b"`, `string("ba"[0]) + "a"`, `"ab"[:1]`, `"ab"[1:]`, `"ab"[2:]`}[g.pick(5)]
	}
	return mapStrs[g.pick(len(mapStrs))]
}

// anyKey returns a key of anys: an int, an int8, a string, or a P.
func (g *mgen) anyKey() string {
	switch g.pick(4) {
	case 0:
		return fmt.Sprintf("int8(%d)", g.pick(4))
	case 1:
		return g.mapStr()
	case 2:
		return fmt.Sprintf("P{%d, \"a\"}", g.pick(4))
	}
	return fmt.Sprint(g.pick(4))
}

// mapStmt emits a random statement on the maps.
func (g *mgen) mapStmt() {
	switch g.pick(16) {
	case 0, 1:
		g.line(1, "ints[%s] = %s", g.mapKey(), g.mapInt())
	case 2:
		g.line(1, "ints[%s] %s= %s", g.mapKey(), []string{"+", "-", "*", "^", "|"}[g.pick(5)], g.mapInt())
	case 3:
		g.line(1, "ints[%s]%s", g.mapKey(), []string{"++", "--"}[g.pick(2)])
	case 4:
		g.line(1, "delete(ints, %s)", g.mapKey())
	case 5:
		g.line(1, "if v, ok := ints[%s]; ok || v != 0 {\n\t\tprintln(\"found\", v)\n\t}", g.mapKey())
	case 6:
		// Grow, then delete some.
		g.line(1, "for i := 0; i < %d; i++ {\n\t\tints[(i*%d+a)&63] = i ^ b\n\t}", 10+g.pick(80), 1+g.pick(9))
		g.line(1, "for i := 0; i < 64; i += %d {\n\t\tif ints[i] > %s {\n\t\t\tdelete(ints, i)\n\t\t}\n\t}", 1+g.pick(4), g.small())
	case 7:
		g.line(1, "strs[%s] = %s", g.mapStr(), g.mapInt())
	case 8:
		g.line(1, "bump(strs, %s, %s)", g.mapStr(), g.small())
	case 9:
		k := fmt.Sprintf("P{%d, %s}", g.pick(4), mapStrs[g.pick(len(mapStrs))])
		g.line(1, "ps[%s] = append(ps[%s], %s)", k, k, g.mapInt())
	case 10:
		g.line(1, "anys[%s] %s", g.anyKey(), []string{"= " + g.mapInt(), "+= 3", "++"}[g.pick(3)])
	case 11:
		if g.pick(3) == 0 {
			g.line(1, "fl[-z] = %s\n\tfl[z] += \"0\"", mapStrs[g.pick(len(mapStrs))])
		} else {
			g.line(1, "fl[%sfloat64(%d)*0.5] += %s", []string{"", "-"}[g.pick(2)], g.pick(4), mapStrs[g.pick(len(mapStrs))])
		}
	case 12:
		g.line(1, "println(len(ints), len(strs), len(anys), len(fl), nm[%s], len(nm), nm == nil, ints != nil)", g.mapInt())
	case 13:
		if g.pick(4) == 0 {
			g.line(1, "clear(%s)", []string{"strs", "ints", "anys", "nm", "fl"}[g.pick(5)])
		} else {
			g.line(1, "delete(%s, %s)", []string{"strs", "anys"}[g.pick(2)], g.mapStr())
		}
	case 14:
		k1, k2 := g.mapKey(), g.mapKey()
		g.line(1, "ints[%s], ints[%s] = ints[%s], len(ints)", k1, k2, k2)
	case 15:
		g.line(1, "nest[%s] = fresh(%d)\n\tnest[\"x\"][%d] += %s\n\tdelete(nest[%s], %d)",
			mapStrs[g.pick(len(mapStrs))], g.pick(12), g.pick(30), mapStrs[g.pick(len(mapStrs))], mapStrs[g.pick(len(mapStrs))], g.pick(12))
	}
}
