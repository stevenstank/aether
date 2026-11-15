package main

import (
	"fmt"
	"strings"
)

// Generated string operations (Phase 7 increment 7.3, D-037).
//
// From a seventh random stream, statements on the string variables in scope
// (stringOpStmt) are interleaved with the others: declarations and
// assignments of concatenations of two or three operands, among them
// slices of variables and results of conversions; += of such operands;
// slices with constant bounds under a test of the length, or with a
// variable's bounds unguarded, so that the program may panic; []byte(s),
// changed and printed with s, which must not change; append and copy of a
// string's bytes; and, since 7.3b, == and != of strings, also of equal
// bytes at different addresses, switches on strings whose case values may
// print, and == and != of arrays and structs that hold strings.
//
// Every string variable is kept to at most maxStrLen bytes: an assignment
// of a concatenation is followed by a cut of its tail, so a string cannot
// double in each iteration of nested loops.

// maxStrOpStmts bounds the string operations in a program, as maxPtrStmts
// does the pointer statements, so that main stays within the limits of
// promotion (TestBuildBackendLimits).
const maxStrOpStmts = 8

// maxStrLen bounds the length of a string an operation assigns.
const maxStrLen = 24

// strOperand returns an operand of a concatenation: a literal, a variable,
// a slice of one under its length, or a conversion's result.
func (g *generator) strOperand(vars []string) string {
	v := g.stringPick(vars)
	switch g.so.IntN(6) {
	case 0:
		return g.stringPick(stringLiterals)
	case 1:
		return v + "[len(" + v + ")/2:]"
	case 2:
		return "string([]byte(" + v + "))"
	case 3:
		return "string(" + v + ")"
	}
	return v
}

// concatOf returns a concatenation of two or three operands.
func (g *generator) concatOf(vars []string) string {
	ops := []string{g.strOperand(vars), g.strOperand(vars)}
	if g.so.IntN(2) == 0 {
		ops = append(ops, g.strOperand(vars))
	}
	if g.so.IntN(4) == 0 {
		return ops[0] + " + (" + strings.Join(ops[1:], " + ") + ")"
	}
	return strings.Join(ops, " + ")
}

// cut keeps the string variable s to at most maxStrLen bytes.
func (g *generator) cut(depth int, s string) {
	g.line(depth, "if len(%s) > %d {", s, maxStrLen)
	g.line(depth+1, "%s = %s[len(%s)-%d:]", s, s, s, maxStrLen)
	g.line(depth, "}")
}

func (g *generator) stringOpStmt(depth int) {
	vars := g.visible(func(e entry) bool { return e.k == strVar })
	if len(vars) == 0 {
		return
	}
	ints := g.visible(func(e entry) bool { return (e.k == variable || e.k == counter) && e.t != "bool" })
	s := g.stringPick(vars)
	switch k := g.so.IntN(11); {
	case k == 0:
		g.snames++
		name := fmt.Sprintf("s%d", g.snames)
		g.line(depth, "%s := %s", name, g.concatOf(vars))
		g.cut(depth, name)
		g.declare(name, entry{k: strVar, t: "string"})
	case k == 1:
		g.line(depth, "%s = %s", s, g.concatOf(vars))
		g.cut(depth, s)
	case k == 2:
		g.line(depth, "%s += %s", s, g.strOperand(vars))
		g.cut(depth, s)
	case k == 3:
		// Constant bounds under a test of the length.
		n := g.so.IntN(5)
		lo, hi := g.so.IntN(n+1), n
		g.line(depth, "if len(%s) >= %d {", s, n)
		g.line(depth+1, "println(%s[%d:%d], %s[%d:], %s[:%d], len(%s[%d:%d]))", s, lo, hi, s, lo, s, hi, s, lo, hi)
		g.line(depth, "}")
	case k == 4 && len(ints) > 0:
		// A variable's bound, of any integer type, guarded unless the
		// program may panic.
		i := g.stringPick(ints)
		if g.so.IntN(6) > 0 {
			g.line(depth, "if %s >= 0 && uint64(%s) <= uint64(len(%s)) {", i, i, s)
			g.line(depth+1, "println(%s[%s:], %s[:%s])", s, i, s, i)
			g.line(depth, "}")
		} else {
			g.line(depth, "println(%s[%s:])", s, i)
		}
	case k == 5:
		// A changed copy of the bytes, which s must not see.
		g.line(depth, "{")
		g.line(depth+1, "b := []byte(%s)", s)
		g.line(depth+1, "if len(b) > 0 {")
		g.line(depth+2, "b[0]++")
		g.line(depth+1, "}")
		g.line(depth+1, "println(string(b), %s, len(b))", s)
		g.line(depth, "}")
	case k == 6:
		t := g.stringPick(vars)
		g.line(depth, "{")
		g.line(depth+1, "b := append([]byte(%s), %s...)", s, g.strOperand(vars))
		g.line(depth+1, "n := copy(b, %s)", t)
		g.line(depth+1, "println(n, string(b))")
		// Not in one statement with string(b), which gc would evaluate
		// after the copy (V-4).
		g.line(depth+1, "n = copy(b[n:], %s)", g.strOperand(vars))
		g.line(depth+1, "println(n, string(b))")
		g.line(depth, "}")
	case k == 8:
		// Equal bytes at another address, prefixes, and other strings;
		// then a copy of s with one byte changed, of the same length.
		t := g.stringPick(vars)
		g.line(depth, "println(%s == %s, %s != string([]byte(%s)), %s[:len(%s)/2] == %s, %s == %s)",
			s, t, s, s, s, s, t, g.strOperand(vars), g.stringPick(stringLiterals))
		g.line(depth, "{")
		g.line(depth+1, "b := []byte(%s)", s)
		g.line(depth+1, "if len(b) > 0 {")
		g.line(depth+2, "b[%s] ^= 1", g.stringPick([]string{"0", "len(b) - 1", "len(b) / 2"}))
		g.line(depth+1, "}")
		g.line(depth+1, "println(string(b) == %s, %s != string(b), string(b[:len(b)/2]) == %s[:len(%s)/2])", s, s, s, s)
		g.line(depth, "}")
	case k == 9:
		g.stringSwitch(depth, s, vars)
	case k == 10:
		g.aggStringCompare(depth)
	default:
		g.line(depth, "println(%s, len(%s))", g.concatOf(vars), g.concatOf(vars))
	}
}

// stringSwitch emits a switch on s, or on a concatenation, whose case
// values are literals, variables, slices, and calls of strs, which prints
// its operand, so that the order in which they are evaluated shows.
func (g *generator) stringSwitch(depth int, s string, vars []string) {
	tag := s
	if g.so.IntN(3) == 0 {
		tag = g.concatOf(vars)
	}
	g.line(depth, "switch %s {", tag)
	cases := 1 + g.so.IntN(3)
	seen := map[string]bool{} // constant case values, which may not repeat
	dflt := g.so.IntN(cases + 1)
	for i := range cases + 1 {
		if i == dflt && g.so.IntN(2) == 0 {
			g.line(depth, "default:")
			g.line(depth+1, "println(\"default\")")
			continue
		}
		var vals []string
		for n := 1 + g.so.IntN(2); n > 0; n-- {
			v := g.strOperand(vars)
			if g.so.IntN(3) == 0 || seen[v] {
				v = "strs(" + v + ")"
			}
			if contains(stringLiterals, v) {
				seen[v] = true
			}
			vals = append(vals, v)
		}
		g.line(depth, "case %s:", strings.Join(vals, ", "))
		g.line(depth+1, "println(\"case\", %d)", i)
	}
	g.line(depth, "}")
}

// aggStringCompare compares two arrays or structs of one type that holds
// strings: the same variable, or two.
func (g *generator) aggStringCompare(depth int) {
	byType := map[string][]string{}
	var types []string
	for _, v := range g.visible(func(e entry) bool { return e.k == aggVar }) {
		e, _ := g.lookup(v)
		if !g.pointee(e.t).holdsString() {
			continue
		}
		if byType[e.t] == nil {
			types = append(types, e.t)
		}
		byType[e.t] = append(byType[e.t], v)
	}
	if len(types) == 0 {
		return
	}
	tn := g.stringPick(types)
	vs := byType[tn]
	a, b := g.stringPick(vs), g.stringPick(vs)
	g.line(depth, "println(%s == %s, %s != %s)", a, b, b, a)
	// b a copy of a whose strings are equal bytes at other addresses,
	// then with one of them longer.
	paths := stringPaths("", g.pointee(tn))
	if a == b || len(paths) == 0 {
		return
	}
	g.line(depth, "%s = %s", b, a)
	for _, p := range paths {
		g.line(depth, "%s%s = string([]byte(%s%s))", b, p, a, p)
	}
	g.line(depth, "println(%s == %s, %s != %s)", a, b, b, a)
	p := g.stringPick(paths)
	g.line(depth, "%s%s += %s", b, p, g.stringPick([]string{`"\x00"`, `"x"`, `""`}))
	g.line(depth, "println(%s == %s, %s != %s)", a, b, b, a)
}

// stringPaths returns the paths from a value of type t to every string in
// it, through non-blank fields and constant indices; at most 8.
func stringPaths(base string, t *gtype) []string {
	switch {
	case t.scalar:
		if t.name == "string" {
			return []string{base}
		}
		return nil
	case t.n >= 0:
		var out []string
		for i := 0; i < t.n && len(out) < 8; i++ {
			out = append(out, stringPaths(fmt.Sprintf("%s[%d]", base, i), t.elem)...)
		}
		return out[:min(len(out), 8)]
	}
	var out []string
	for _, f := range t.fields {
		if f.name != "_" {
			out = append(out, stringPaths(base+"."+f.name, f.t)...)
		}
	}
	return out[:min(len(out), 8)]
}
