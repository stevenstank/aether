package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// generate returns a random S1 program determined by seed. It uses every S1
// construct: var, :=, and const declarations, redeclaration, shadowing in
// nested blocks, plain, tuple, and compound assignment, ++ and --, binary
// + - *, unary + -, parentheses, empty statements, and println. Literals
// include values that make run-time arithmetic wrap.
//
// Every program is valid Go except that a constant expression may overflow
// int, which go/types (and gc) reject; callers skip such programs. Every
// variable is printed at the end of its block, so none is unused.
func generate(seed uint64) string {
	g := &generator{r: rand.New(rand.NewPCG(seed, 0x5eed))}
	g.line(0, "package main")
	g.line(0, "")
	g.line(0, "func main() {")
	g.block(1)
	g.line(0, "}")
	return g.b.String()
}

type generator struct {
	r      *rand.Rand
	b      strings.Builder
	scopes []map[string]bool // name → is a variable (false: constant)
	order  [][]string        // declaration order, per scope
	names  int
}

func (g *generator) line(depth int, format string, args ...any) {
	g.b.WriteString(strings.Repeat("\t", depth))
	fmt.Fprintf(&g.b, format, args...)
	g.b.WriteString("\n")
}

func (g *generator) block(depth int) {
	g.scopes = append(g.scopes, map[string]bool{})
	g.order = append(g.order, nil)
	for n := 2 + g.r.IntN(8); n > 0; n-- {
		g.stmt(depth)
	}
	var vars []string
	for _, name := range g.order[len(g.order)-1] {
		if g.scopes[len(g.scopes)-1][name] {
			vars = append(vars, name)
		}
	}
	if len(vars) > 0 {
		g.line(depth, "println(%s)", strings.Join(vars, ", "))
	}
	g.scopes = g.scopes[:len(g.scopes)-1]
	g.order = g.order[:len(g.order)-1]
}

// lookup reports whether name is visible and, if so, whether it is a
// variable.
func (g *generator) lookup(name string) (isVar, ok bool) {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if v, ok := g.scopes[i][name]; ok {
			return v, true
		}
	}
	return false, false
}

// visible returns the visible names, of variables only if varsOnly is set,
// in a deterministic order.
func (g *generator) visible(varsOnly bool) []string {
	var out []string
	for i := range g.order {
		for _, name := range g.order[i] {
			if isVar, _ := g.lookup(name); (isVar || !varsOnly) && !contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// newName returns a name not declared in the current scope: usually a fresh
// one, sometimes one from an outer scope, which the declaration shadows.
func (g *generator) newName(taken []string) string {
	if outer := g.visible(false); len(outer) > 0 && g.r.IntN(4) == 0 {
		name := outer[g.r.IntN(len(outer))]
		if _, inScope := g.scopes[len(g.scopes)-1][name]; !inScope && !contains(taken, name) {
			return name
		}
	}
	g.names++
	return fmt.Sprintf("v%d", g.names)
}

func (g *generator) declare(name string, isVar bool) {
	g.scopes[len(g.scopes)-1][name] = isVar
	g.order[len(g.order)-1] = append(g.order[len(g.order)-1], name)
}

func (g *generator) stmt(depth int) {
	vars := g.visible(true)
	switch k := g.r.IntN(13); {
	case k <= 1 && depth < 4:
		g.line(depth, "{")
		g.block(depth + 1)
		g.line(depth, "}")
	case k == 2:
		g.line(depth, ";")
	case k == 3:
		name, e := g.newName(nil), g.expr(2, false)
		g.declare(name, false)
		g.line(depth, "const %s = %s", name, e)
	case k <= 5:
		// var declaration with or without values, and with or without int.
		n := 1 + g.r.IntN(2)
		names, vals := g.names2(n), g.exprs(n)
		typ := ""
		if g.r.IntN(2) == 0 {
			typ = " int"
		}
		switch {
		case g.r.IntN(3) == 0:
			g.line(depth, "var %s int", strings.Join(names, ", "))
		default:
			g.line(depth, "var %s%s = %s", strings.Join(names, ", "), typ, strings.Join(vals, ", "))
		}
		for _, name := range names {
			g.declare(name, true)
		}
	case k <= 7 || len(vars) == 0:
		// := with one or two names, redeclaring a variable of this scope
		// alongside a new one where possible.
		var lhs []string
		var local []string
		for _, name := range g.order[len(g.order)-1] {
			if g.scopes[len(g.scopes)-1][name] {
				local = append(local, name)
			}
		}
		if len(local) > 0 && g.r.IntN(3) == 0 {
			lhs = append(lhs, local[g.r.IntN(len(local))])
		}
		lhs = append(lhs, g.names2(1+g.r.IntN(2))...)
		g.line(depth, "%s := %s", strings.Join(lhs, ", "), strings.Join(g.exprs(len(lhs)), ", "))
		for _, name := range lhs {
			if !contains(local, name) {
				g.declare(name, true)
			}
		}
	case k == 8:
		// Tuple assignment; a target may repeat, and the last store wins.
		n := 1 + g.r.IntN(3)
		lhs := make([]string, n)
		for i := range lhs {
			lhs[i] = vars[g.r.IntN(len(vars))]
		}
		g.line(depth, "%s = %s", strings.Join(lhs, ", "), strings.Join(g.exprs(n), ", "))
	case k == 9:
		ops := []string{"+=", "-=", "*="}
		g.line(depth, "%s %s %s", vars[g.r.IntN(len(vars))], ops[g.r.IntN(3)], g.expr(2, true))
	case k == 10:
		ops := []string{"++", "--"}
		g.line(depth, "%s%s", vars[g.r.IntN(len(vars))], ops[g.r.IntN(2)])
	default:
		g.line(depth, "println(%s)", strings.Join(g.exprs(g.r.IntN(4)), ", "))
	}
}

// names2 returns n distinct names new to the current scope.
func (g *generator) names2(n int) []string {
	var out []string
	for len(out) < n {
		out = append(out, g.newName(out))
	}
	return out
}

func (g *generator) exprs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = g.expr(3, true)
	}
	return out
}

var literals = []string{
	"0", "1", "2", "3", "7", "10", "100", "1_000", "0x7f", "0o17", "017", "0b101",
	"3037000500", "4294967296", "9223372036854775807",
}

// expr returns an int expression of at most the given depth. If useVars is
// false, it uses only literals and constants, so it is constant.
func (g *generator) expr(depth int, useVars bool) string {
	if depth == 0 || g.r.IntN(3) == 0 {
		if names := g.visible(false); useVars && len(names) > 0 && g.r.IntN(3) > 0 {
			return names[g.r.IntN(len(names))]
		}
		if consts := g.constants(); !useVars && len(consts) > 0 && g.r.IntN(3) == 0 {
			return consts[g.r.IntN(len(consts))]
		}
		return literals[g.r.IntN(len(literals))]
	}
	switch g.r.IntN(4) {
	case 0:
		// A space keeps "- -x" from scanning as the -- operator.
		return []string{"-", "+"}[g.r.IntN(2)] + " " + g.expr(depth-1, useVars)
	case 1:
		return "(" + g.expr(depth-1, useVars) + ")"
	}
	op := []string{"+", "-", "*"}[g.r.IntN(3)]
	return g.expr(depth-1, useVars) + " " + op + " " + g.expr(depth-1, useVars)
}

func (g *generator) constants() []string {
	var out []string
	for _, name := range g.visible(false) {
		if isVar, _ := g.lookup(name); !isVar {
			out = append(out, name)
		}
	}
	return out
}
