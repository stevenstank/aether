package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// generate returns a random program determined by seed. It uses every S1
// construct (var, :=, and const declarations, redeclaration, shadowing in
// nested blocks, plain, tuple, and compound assignment, ++ and --, binary
// + - *, unary + -, parentheses, empty statements, and println) and the
// constructs of increment 4.1: bool variables and constants, comparisons
// with negative and boundary operands, ! && ||, if/else trees, and bounded
// loops in all three for forms with break and continue. Literals include
// values that make run-time arithmetic wrap.
//
// Every program is valid Go except that a constant expression may overflow
// int, which go/types (and gc) reject; callers skip such programs. Every
// variable is printed at the end of its block, so none is unused. Every
// loop runs at most 3 times: its counter is never a target of assignment,
// and nested loops are bounded in depth.
func generate(seed uint64) string {
	g := &generator{r: rand.New(rand.NewPCG(seed, 0x5eed))}
	g.line(0, "package main")
	g.line(0, "")
	g.line(0, "func main() {")
	g.block(1)
	g.line(0, "}")
	return g.b.String()
}

// kind is what a name denotes.
type kind uint8

const (
	intConst kind = iota
	boolConst
	intVar
	boolVar
	counter // a loop counter: an int variable that is never assigned
)

func (k kind) isVar() bool { return k >= intVar }

type generator struct {
	r      *rand.Rand
	b      strings.Builder
	scopes []map[string]kind
	order  [][]string // declaration order, per scope
	names  int
	loops  int // enclosing loops
}

func (g *generator) line(depth int, format string, args ...any) {
	g.b.WriteString(strings.Repeat("\t", depth))
	fmt.Fprintf(&g.b, format, args...)
	g.b.WriteString("\n")
}

// block emits statements in a new scope, then prints its variables.
func (g *generator) block(depth int) {
	g.scopes = append(g.scopes, map[string]kind{})
	g.order = append(g.order, nil)
	for n := 2 + g.r.IntN(7); n > 0; n-- {
		g.stmt(depth)
	}
	var vars []string
	for _, name := range g.order[len(g.order)-1] {
		if g.scopes[len(g.scopes)-1][name].isVar() {
			vars = append(vars, name)
		}
	}
	if len(vars) > 0 {
		g.line(depth, "println(%s)", strings.Join(vars, ", "))
	}
	g.scopes = g.scopes[:len(g.scopes)-1]
	g.order = g.order[:len(g.order)-1]
}

// lookup returns what a visible name denotes.
func (g *generator) lookup(name string) (kind, bool) {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if k, ok := g.scopes[i][name]; ok {
			return k, true
		}
	}
	return 0, false
}

// visible returns the visible names for which want is true, in a
// deterministic order.
func (g *generator) visible(want func(kind) bool) []string {
	var out []string
	for i := range g.order {
		for _, name := range g.order[i] {
			if k, _ := g.lookup(name); want(k) && !contains(out, name) {
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

func (g *generator) pick(list []string) string { return list[g.r.IntN(len(list))] }

// newName returns a name not declared in the current scope: usually a fresh
// one, sometimes one from an outer scope, which the declaration shadows.
func (g *generator) newName(taken []string) string {
	if outer := g.visible(func(kind) bool { return true }); len(outer) > 0 && g.r.IntN(4) == 0 {
		name := g.pick(outer)
		if _, inScope := g.scopes[len(g.scopes)-1][name]; !inScope && !contains(taken, name) {
			return name
		}
	}
	g.names++
	return fmt.Sprintf("v%d", g.names)
}

// newNames returns n distinct names new to the current scope.
func (g *generator) newNames(n int) []string {
	var out []string
	for len(out) < n {
		out = append(out, g.newName(out))
	}
	return out
}

func (g *generator) declare(name string, k kind) {
	g.scopes[len(g.scopes)-1][name] = k
	g.order[len(g.order)-1] = append(g.order[len(g.order)-1], name)
}

// value returns an int or bool expression, and whether it is bool.
func (g *generator) value(useVars bool) (string, bool) {
	if g.r.IntN(3) == 0 {
		return g.boolExpr(3, useVars), true
	}
	return g.intExpr(3, useVars), false
}

func (g *generator) stmt(depth int) {
	ints := g.visible(func(k kind) bool { return k == intVar })
	bools := g.visible(func(k kind) bool { return k == boolVar })
	switch k := g.r.IntN(18); {
	case k <= 1 && depth < 5:
		g.line(depth, "{")
		g.block(depth + 1)
		g.line(depth, "}")
	case k == 2:
		g.line(depth, ";")
	case k == 3:
		name := g.newName(nil)
		if g.r.IntN(3) == 0 {
			g.line(depth, "const %s = %s", name, g.boolExpr(2, false))
			g.declare(name, boolConst)
		} else {
			g.line(depth, "const %s = %s", name, g.intExpr(2, false))
			g.declare(name, intConst)
		}
	case k <= 5:
		// var declaration with or without values, and with or without a
		// type.
		n := 1 + g.r.IntN(2)
		names := g.newNames(n)
		isBool := g.r.IntN(3) == 0
		typ, kd := "int", intVar
		if isBool {
			typ, kd = "bool", boolVar
		}
		var vals []string
		for range n {
			vals = append(vals, g.typed(isBool, true))
		}
		switch r := g.r.IntN(3); r {
		case 0:
			g.line(depth, "var %s %s", strings.Join(names, ", "), typ)
		case 1:
			g.line(depth, "var %s %s = %s", strings.Join(names, ", "), typ, strings.Join(vals, ", "))
		default:
			g.line(depth, "var %s = %s", strings.Join(names, ", "), strings.Join(vals, ", "))
		}
		for _, name := range names {
			g.declare(name, kd)
		}
	case k <= 7 || len(ints)+len(bools) == 0:
		// := with one or two names, redeclaring a variable of this scope
		// alongside a new one where possible.
		var lhs, vals []string
		var kinds []kind
		var local []string
		for _, name := range g.order[len(g.order)-1] {
			if k := g.scopes[len(g.scopes)-1][name]; k == intVar || k == boolVar {
				local = append(local, name)
			}
		}
		if len(local) > 0 && g.r.IntN(3) == 0 {
			name := g.pick(local)
			kd, _ := g.lookup(name)
			lhs, vals, kinds = append(lhs, name), append(vals, g.typed(kd == boolVar, true)), append(kinds, kd)
		}
		for _, name := range g.newNames(1 + g.r.IntN(2)) {
			v, isBool := g.value(true)
			kd := intVar
			if isBool {
				kd = boolVar
			}
			lhs, vals, kinds = append(lhs, name), append(vals, v), append(kinds, kd)
		}
		g.line(depth, "%s := %s", strings.Join(lhs, ", "), strings.Join(vals, ", "))
		for i, name := range lhs {
			if !contains(local, name) {
				g.declare(name, kinds[i])
			}
		}
	case k == 8:
		// Tuple assignment; a target may repeat, and the last store wins.
		var lhs, vals []string
		for n := 1 + g.r.IntN(3); n > 0; n-- {
			if len(bools) > 0 && (len(ints) == 0 || g.r.IntN(3) == 0) {
				lhs, vals = append(lhs, g.pick(bools)), append(vals, g.boolExpr(3, true))
			} else {
				lhs, vals = append(lhs, g.pick(ints)), append(vals, g.intExpr(3, true))
			}
		}
		g.line(depth, "%s = %s", strings.Join(lhs, ", "), strings.Join(vals, ", "))
	case k == 9 && len(ints) > 0:
		g.line(depth, "%s %s %s", g.pick(ints), g.pick([]string{"+=", "-=", "*="}), g.intExpr(2, true))
	case k == 10 && len(ints) > 0:
		g.line(depth, "%s%s", g.pick(ints), g.pick([]string{"++", "--"}))
	case k <= 12 && depth < 5:
		g.ifStmt(depth, 0)
	case k == 13 && depth < 5 && g.loops < 3:
		g.loop(depth)
	case k == 14 && g.loops > 0:
		g.line(depth, "if %s {", g.boolExpr(2, true))
		g.line(depth+1, "%s", g.pick([]string{"break", "continue"}))
		g.line(depth, "}")
	default:
		var args []string
		for n := g.r.IntN(4); n > 0; n-- {
			v, _ := g.value(true)
			args = append(args, v)
		}
		g.line(depth, "println(%s)", strings.Join(args, ", "))
	}
}

// ifStmt emits an if statement with an optional else or else-if chain.
func (g *generator) ifStmt(depth, chain int) {
	g.line(depth, "if %s {", g.boolExpr(3, true))
	g.block(depth + 1)
	switch r := g.r.IntN(3); {
	case r == 0 && chain < 2:
		g.b.WriteString(strings.Repeat("\t", depth) + "} else ")
		// Continue the chain on the same line.
		var rest strings.Builder
		saved := g.b
		g.b = rest
		g.ifStmt(depth, chain+1)
		out := g.b.String()
		g.b = saved
		g.b.WriteString(strings.TrimLeft(out, "\t"))
		return
	case r == 1:
		g.line(depth, "} else {")
		g.block(depth + 1)
	}
	g.line(depth, "}")
}

// loop emits a for statement of one of the three forms that runs at most
// 3 times. Its counter is a fresh variable that no statement assigns, so
// that the bound holds; the counter advances before the body, or in the
// post statement, so continue cannot skip it.
func (g *generator) loop(depth int) {
	g.names++
	i := fmt.Sprintf("i%d", g.names)
	n := g.r.IntN(4)
	g.loops++
	defer func() { g.loops-- }()
	switch g.r.IntN(3) {
	case 0:
		g.line(depth, "for %s := 0; %s < %d; %s++ {", i, i, n, i)
		g.scopes = append(g.scopes, map[string]kind{i: counter})
		g.order = append(g.order, []string{i})
		g.block(depth + 1)
		g.scopes, g.order = g.scopes[:len(g.scopes)-1], g.order[:len(g.order)-1]
		g.line(depth, "}")
		return
	case 1:
		g.line(depth, "%s := 0", i)
		g.declare(i, counter)
		g.line(depth, "for %s < %d {", i, n)
		g.line(depth+1, "%s++", i)
	default:
		g.line(depth, "%s := 0", i)
		g.declare(i, counter)
		g.line(depth, "for {")
		g.line(depth+1, "%s++", i)
		g.line(depth+1, "if %s > %d {", i, n)
		g.line(depth+2, "break")
		g.line(depth+1, "}")
	}
	g.block(depth + 1)
	g.line(depth, "}")
}

var literals = []string{
	"0", "1", "2", "3", "7", "10", "100", "1_000", "0x7f", "0o17", "017", "0b101",
	"3037000500", "4294967296", "9223372036854775807",
}

// typed returns an expression of the given kind.
func (g *generator) typed(isBool, useVars bool) string {
	if isBool {
		return g.boolExpr(3, useVars)
	}
	return g.intExpr(3, useVars)
}

// intExpr returns an int expression of at most the given depth. If useVars
// is false, it uses only literals and constants, so it is constant.
func (g *generator) intExpr(depth int, useVars bool) string {
	if depth <= 0 || g.r.IntN(3) == 0 {
		vars := g.visible(func(k kind) bool { return k == intVar || k == counter })
		if useVars && len(vars) > 0 && g.r.IntN(3) > 0 {
			return g.pick(vars)
		}
		if consts := g.visible(func(k kind) bool { return k == intConst }); !useVars && len(consts) > 0 && g.r.IntN(3) == 0 {
			return g.pick(consts)
		}
		return g.pick(literals)
	}
	switch g.r.IntN(4) {
	case 0:
		// A space keeps "- -x" from scanning as the -- operator.
		return g.pick([]string{"-", "+"}) + " " + g.intExpr(depth-1, useVars)
	case 1:
		return "(" + g.intExpr(depth-1, useVars) + ")"
	}
	return g.intExpr(depth-1, useVars) + " " + g.pick([]string{"+", "-", "*"}) + " " + g.intExpr(depth-1, useVars)
}

// boolExpr returns a bool expression of at most the given depth, with every
// operand of an operator parenthesized. If useVars is false, it is
// constant.
func (g *generator) boolExpr(depth int, useVars bool) string {
	if depth <= 0 || g.r.IntN(4) == 0 {
		vars := g.visible(func(k kind) bool { return k == boolVar })
		if useVars && len(vars) > 0 && g.r.IntN(2) == 0 {
			return g.pick(vars)
		}
		if consts := g.visible(func(k kind) bool { return k == boolConst }); len(consts) > 0 && g.r.IntN(3) == 0 {
			return g.pick(consts)
		}
		return g.pick([]string{"true", "false"})
	}
	switch g.r.IntN(5) {
	case 0:
		return "!(" + g.boolExpr(depth-1, useVars) + ")"
	case 1, 2:
		// Operands are often negated, so negative and boundary values are
		// compared. They are shallow: a constant comparison of deep
		// operands mostly overflows, which makes the program invalid.
		op := g.pick([]string{"==", "!=", "<", "<=", ">", ">="})
		d := min(depth-1, 1)
		return "(" + g.intExpr(d, useVars) + ") " + op + " (" + g.pick([]string{"- ", ""}) + g.intExpr(d, useVars) + ")"
	case 3:
		return "(" + g.boolExpr(depth-1, useVars) + ") " + g.pick([]string{"==", "!="}) + " (" + g.boolExpr(depth-1, useVars) + ")"
	}
	return "(" + g.boolExpr(depth-1, useVars) + ") " + g.pick([]string{"&&", "||"}) + " (" + g.boolExpr(depth-1, useVars) + ")"
}
