package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// generate returns a random program determined by seed. It uses every S1
// construct (var, :=, and const declarations, redeclaration, shadowing in
// nested blocks, plain, tuple, and compound assignment, ++ and --, binary
// + - *, unary + -, parentheses, empty statements, and println), the
// constructs of increment 4.1 (bool variables and constants, comparisons
// with negative and boundary operands, ! && ||, if/else trees, and bounded
// loops in all three for forms with break and continue), and those of
// increment 4.4 (variables of every integer type, conversions between them,
// & | ^ &^ and unary ^ with their compound assignments, and print), and
// those of increment 4.5 (/ % << >> with their compound assignments).
// Literals include values that make run-time arithmetic wrap. A
// non-constant divisor or signed shift count may be zero or negative, so
// some programs panic. Since increment 4.6, a program may begin with
// package-level var and const declarations, whose initializers refer to
// other ones; they are printed in shuffled order, so the initialization
// order (types.Info.InitOrder) differs from the declaration order. There is
// no function besides main, so no call assigns a global while an
// expression reads it (SEMANTICS.md V-4). main assigns globals like locals
// and prints them all at its end.
//
// Every program is valid Go except that a constant expression may overflow
// its type, which go/types (and gc) reject; callers skip such programs.
// Every variable is printed at the end of its block, so none is unused.
// Every loop runs at most 3 times: its counter is never a target of
// assignment, and nested loops are bounded in depth.
func generate(seed uint64) string {
	g := &generator{r: rand.New(rand.NewPCG(seed, 0x5eed))}
	g.line(0, "package main")
	g.line(0, "")
	// The package scope. Each declaration may use the ones generated
	// before it, which makes a cycle impossible in any order.
	g.scopes, g.order = []map[string]entry{{}}, [][]string{nil}
	var decls []string
	for n := g.r.IntN(5); n > 0; n-- {
		decls = append(decls, g.capture(func() {
			if g.r.IntN(3) == 0 {
				g.constDecl(0)
			} else {
				g.varDecl(0)
			}
		}))
	}
	g.r.Shuffle(len(decls), func(i, j int) { decls[i], decls[j] = decls[j], decls[i] })
	for _, d := range decls {
		g.b.WriteString(d)
		g.line(0, "")
	}
	g.line(0, "func main() {")
	g.block(1)
	if globals := g.visible(entry.isVar); len(globals) > 0 {
		g.line(1, "println(%s)", strings.Join(globals, ", "))
	}
	g.line(0, "}")
	return g.b.String()
}

// kind is what a name denotes.
type kind uint8

const (
	intConst  kind = iota // an untyped integer constant
	boolConst             // an untyped bool constant
	variable              // a variable of the entry's type
	counter               // a loop counter: an int variable that is never assigned
)

// entry is a declared name: what it denotes and, for a variable, its type.
type entry struct {
	k kind
	t string // "bool" or an integer type name; "int" for a counter
}

func (e entry) isVar() bool { return e.k == variable || e.k == counter }

// intTypes are the integer types variables are declared with. byte and
// rune are uint8 and int32 under other names.
var intTypes = []string{
	"int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16",
	"uint32", "uint64", "uintptr", "byte", "rune",
}

// same reports whether two type names denote the same type.
func same(a, b string) bool {
	alias := map[string]string{"byte": "uint8", "rune": "int32"}
	if x, ok := alias[a]; ok {
		a = x
	}
	if x, ok := alias[b]; ok {
		b = x
	}
	return a == b
}

func unsigned(t string) bool { return strings.HasPrefix(t, "u") || t == "byte" }

// intLiterals are the literals of an int expression; maxLiteral gives each
// other type its maximum, so its arithmetic wraps too.
var intLiterals = []string{
	"0", "1", "2", "3", "7", "10", "100", "1_000", "0x7f", "0o17", "017", "0b101",
	"3037000500", "4294967296", "9223372036854775807",
}

var maxLiteral = map[string]string{
	"int8": "127", "int16": "32767", "int32": "2147483647", "rune": "2147483647",
	"int64": "9223372036854775807", "uint8": "255", "byte": "255", "uint16": "65535",
	"uint32": "4294967295", "uint": "18446744073709551615", "uint64": "18446744073709551615",
	"uintptr": "18446744073709551615",
}

type generator struct {
	r      *rand.Rand
	b      strings.Builder
	scopes []map[string]entry
	order  [][]string // declaration order, per scope
	names  int
	loops  int // enclosing loops
}

// capture returns what emit writes, instead of writing it.
func (g *generator) capture(emit func()) string {
	saved := g.b
	g.b = strings.Builder{}
	emit()
	out := g.b.String()
	g.b = saved
	return out
}

func (g *generator) line(depth int, format string, args ...any) {
	g.b.WriteString(strings.Repeat("\t", depth))
	fmt.Fprintf(&g.b, format, args...)
	g.b.WriteString("\n")
}

// block emits statements in a new scope, then prints its variables.
func (g *generator) block(depth int) {
	g.scopes = append(g.scopes, map[string]entry{})
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
func (g *generator) lookup(name string) (entry, bool) {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if e, ok := g.scopes[i][name]; ok {
			return e, true
		}
	}
	return entry{}, false
}

// visible returns the visible names for which want is true, in a
// deterministic order.
func (g *generator) visible(want func(entry) bool) []string {
	var out []string
	for i := range g.order {
		for _, name := range g.order[i] {
			if e, _ := g.lookup(name); want(e) && !contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// vars returns the visible variables of type t, counters included for int.
func (g *generator) vars(t string) []string {
	return g.visible(func(e entry) bool { return e.isVar() && same(e.t, t) })
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
	if outer := g.visible(func(entry) bool { return true }); len(outer) > 0 && g.r.IntN(4) == 0 {
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

func (g *generator) declare(name string, e entry) {
	g.scopes[len(g.scopes)-1][name] = e
	g.order[len(g.order)-1] = append(g.order[len(g.order)-1], name)
}

// intType returns an integer type: int half the time, and otherwise
// usually a type with a visible variable, since an expression of a narrow
// type without one is constant and likely to overflow it.
func (g *generator) intType() string {
	if g.r.IntN(2) == 0 {
		return "int"
	}
	var typed []string
	for _, t := range intTypes {
		if len(g.vars(t)) > 0 {
			typed = append(typed, t)
		}
	}
	if len(typed) > 0 && g.r.IntN(4) > 0 {
		return g.pick(typed)
	}
	return g.pick(intTypes)
}

// value returns a bool or integer expression and its type. A constant
// integer expression has type int, the default type of its untyped value.
func (g *generator) value() (string, string) {
	if g.r.IntN(3) == 0 {
		return g.boolExpr(3, true), "bool"
	}
	t := g.intType()
	e, isConst := g.intExpr(3, true, t)
	if isConst {
		t = "int"
	}
	return e, t
}

// typed returns an expression of type t.
func (g *generator) typed(t string, useVars bool) string {
	if t == "bool" {
		return g.boolExpr(3, useVars)
	}
	e, _ := g.intExpr(3, useVars, t)
	return e
}

func (g *generator) stmt(depth int) {
	assignable := g.visible(func(e entry) bool { return e.k == variable })
	ints := g.visible(func(e entry) bool { return e.k == variable && e.t != "bool" })
	switch k := g.r.IntN(19); {
	case k <= 1 && depth < 5:
		g.line(depth, "{")
		g.block(depth + 1)
		g.line(depth, "}")
	case k == 2:
		g.line(depth, ";")
	case k == 3:
		g.constDecl(depth)
	case k <= 5:
		g.varDecl(depth)
	case k <= 7 || len(assignable) == 0:
		// := with one or two names, redeclaring a variable of this scope
		// alongside a new one where possible.
		var lhs, vals, typs []string
		var local []string
		for _, name := range g.order[len(g.order)-1] {
			if g.scopes[len(g.scopes)-1][name].k == variable {
				local = append(local, name)
			}
		}
		if len(local) > 0 && g.r.IntN(3) == 0 {
			name := g.pick(local)
			e, _ := g.lookup(name)
			lhs, vals, typs = append(lhs, name), append(vals, g.typed(e.t, true)), append(typs, e.t)
		}
		for _, name := range g.newNames(1 + g.r.IntN(2)) {
			v, t := g.value()
			lhs, vals, typs = append(lhs, name), append(vals, v), append(typs, t)
		}
		g.line(depth, "%s := %s", strings.Join(lhs, ", "), strings.Join(vals, ", "))
		for i, name := range lhs {
			if !contains(local, name) {
				g.declare(name, entry{k: variable, t: typs[i]})
			}
		}
	case k == 8:
		// Tuple assignment; a target may repeat, and the last store wins.
		var lhs, vals []string
		for n := 1 + g.r.IntN(3); n > 0; n-- {
			name := g.pick(assignable)
			e, _ := g.lookup(name)
			lhs, vals = append(lhs, name), append(vals, g.typed(e.t, true))
		}
		g.line(depth, "%s = %s", strings.Join(lhs, ", "), strings.Join(vals, ", "))
	case k == 9 && len(ints) > 0:
		name := g.pick(ints)
		e, _ := g.lookup(name)
		op := g.pick([]string{"+=", "-=", "*=", "&=", "|=", "^=", "&^=", "/=", "%=", "<<=", ">>="})
		var rhs string
		switch op {
		case "/=", "%=":
			rhs, _ = g.divisor(2, true, e.t)
		case "<<=", ">>=":
			rhs, _ = g.count(2, true)
		default:
			rhs, _ = g.intExpr(2, true, e.t)
		}
		g.line(depth, "%s %s %s", name, op, rhs)
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
			v, _ := g.value()
			args = append(args, v)
		}
		g.line(depth, "%s(%s)", g.pick([]string{"println", "println", "print"}), strings.Join(args, ", "))
	}
}

// constDecl emits an untyped bool or integer constant declaration.
func (g *generator) constDecl(depth int) {
	name := g.newName(nil)
	if g.r.IntN(3) == 0 {
		g.line(depth, "const %s = %s", name, g.boolExpr(2, false))
		g.declare(name, entry{k: boolConst})
	} else {
		e, _ := g.intExpr(2, false, "int")
		g.line(depth, "const %s = %s", name, e)
		g.declare(name, entry{k: intConst})
	}
}

// varDecl emits a var declaration with or without values, and with or
// without a type. Without one, the type is bool or int, which the values'
// untyped constants default to.
func (g *generator) varDecl(depth int) {
	n := 1 + g.r.IntN(2)
	names := g.newNames(n)
	t := "bool"
	if g.r.IntN(3) > 0 {
		t = g.intType()
	}
	var vals []string
	for range n {
		vals = append(vals, g.typed(t, true))
	}
	switch r := g.r.IntN(3); {
	case r == 0:
		g.line(depth, "var %s %s", strings.Join(names, ", "), t)
	case r == 1 || t != "bool" && t != "int":
		g.line(depth, "var %s %s = %s", strings.Join(names, ", "), t, strings.Join(vals, ", "))
	default:
		g.line(depth, "var %s = %s", strings.Join(names, ", "), strings.Join(vals, ", "))
	}
	for _, name := range names {
		g.declare(name, entry{k: variable, t: t})
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
		g.scopes = append(g.scopes, map[string]entry{i: {k: counter, t: "int"}})
		g.order = append(g.order, []string{i})
		g.block(depth + 1)
		g.scopes, g.order = g.scopes[:len(g.scopes)-1], g.order[:len(g.order)-1]
		g.line(depth, "}")
		return
	case 1:
		g.line(depth, "%s := 0", i)
		g.declare(i, entry{k: counter, t: "int"})
		g.line(depth, "for %s < %d {", i, n)
		g.line(depth+1, "%s++", i)
	default:
		g.line(depth, "%s := 0", i)
		g.declare(i, entry{k: counter, t: "int"})
		g.line(depth, "for {")
		g.line(depth+1, "%s++", i)
		g.line(depth+1, "if %s > %d {", i, n)
		g.line(depth+2, "break")
		g.line(depth+1, "}")
	}
	g.block(depth + 1)
	g.line(depth, "}")
}

// intExpr returns an expression of integer type t of at most the given
// depth, and whether it is constant. If useVars is false, it uses only
// literals and constants. A constant is never negated or complemented in an
// unsigned type, and only variables are converted, since either would make
// the program invalid for a reason other than overflow.
func (g *generator) intExpr(depth int, useVars bool, t string) (string, bool) {
	if depth <= 0 || g.r.IntN(3) == 0 {
		if vars := g.vars(t); useVars && len(vars) > 0 && g.r.IntN(3) > 0 {
			return g.pick(vars), false
		}
		if consts := g.visible(func(e entry) bool { return e.k == intConst }); !useVars && len(consts) > 0 && g.r.IntN(3) == 0 {
			return g.pick(consts), true
		}
		if same(t, "int") {
			return g.pick(intLiterals), true
		}
		return g.pick([]string{"0", "1", "2", "3", "7", "100", maxLiteral[t]}), true
	}
	switch g.r.IntN(5) {
	case 0:
		// A space keeps "- -x" from scanning as the -- operator.
		x, isConst := g.intExpr(depth-1, useVars, t)
		ops := []string{"-", "+", "^"}
		if isConst && unsigned(t) {
			ops = []string{"+"}
		}
		return g.pick(ops) + " " + x, isConst
	case 1:
		x, isConst := g.intExpr(depth-1, useVars, t)
		return "(" + x + ")", isConst
	case 2:
		// A conversion of a variable of another integer type.
		from := g.pick(intTypes)
		if vars := g.vars(from); useVars && len(vars) > 0 && !same(from, t) {
			return t + "(" + g.pick(vars) + ")", false
		}
	}
	x, cx := g.intExpr(depth-1, useVars, t)
	switch g.r.IntN(7) {
	case 0:
		y, cy := g.divisor(depth-1, useVars, t)
		return x + " " + g.pick([]string{"/", "%"}) + " " + y, cx && cy
	case 1:
		// The shifted operand is parenthesized, so that the count applies
		// to all of it. A constant one is shifted by a small constant
		// count: with a variable count, its type would come from the
		// context rather than be t.
		if cx && !same(t, "int") {
			break
		}
		y, cy := "", true
		if cx {
			y = g.pick([]string{"0", "1", "2", "3", "7", "10"})
		} else {
			y, cy = g.count(depth-1, useVars)
		}
		return "(" + x + ") " + g.pick([]string{"<<", ">>"}) + " " + y, cx && cy
	}
	y, cy := g.intExpr(depth-1, useVars, t)
	ops := []string{"+", "-", "*", "&", "|", "^", "&^"}
	if cx && cy && !same(t, "int") {
		// Bitwise operations keep constants of t in range; arithmetic on
		// them mostly overflows a narrow type.
		ops = ops[3:]
	}
	return x + " " + g.pick(ops) + " " + y, cx && cy
}

// divisor returns a divisor of type t and whether it is constant. A constant
// divisor is a nonzero literal, since go/types rejects a constant division
// by zero. A variable divisor is usually or-ed with 1, so that most programs
// do not panic, and is otherwise any parenthesized expression of type t,
// which may be zero.
func (g *generator) divisor(depth int, useVars bool, t string) (string, bool) {
	if vars := g.vars(t); useVars && len(vars) > 0 {
		switch g.r.IntN(4) {
		case 0:
			return "(" + g.pick(vars) + " | 1)", false
		case 1:
			if e, isConst := g.intExpr(depth, true, t); !isConst && g.r.IntN(2) == 0 {
				return "(" + e + ")", false
			}
		}
	}
	lits := []string{"1", "2", "3", "7", "10", "9223372036854775807"}
	if m, ok := maxLiteral[t]; ok {
		lits[len(lits)-1] = m
	}
	if !unsigned(t) {
		lits = append(lits, "-1", "-2", "-7")
	}
	return g.pick(lits), true
}

// count returns a shift count and whether it is constant. A constant count
// is a nonnegative literal, since go/types rejects a negative one. A
// variable count is a parenthesized expression of any integer type, which
// may be negative or at least the width of the shifted type.
func (g *generator) count(depth int, useVars bool) (string, bool) {
	if useVars && g.r.IntN(2) == 0 {
		if e, isConst := g.intExpr(depth, true, g.pick(intTypes)); !isConst {
			return "(" + e + ")", false
		}
	}
	return g.pick([]string{"0", "1", "2", "3", "7", "8", "15", "16", "31", "32", "63", "64", "100"}), true
}

// boolExpr returns a bool expression of at most the given depth, with every
// operand of an operator parenthesized. If useVars is false, it is
// constant.
func (g *generator) boolExpr(depth int, useVars bool) string {
	if depth <= 0 || g.r.IntN(4) == 0 {
		if vars := g.vars("bool"); useVars && len(vars) > 0 && g.r.IntN(2) == 0 {
			return g.pick(vars)
		}
		if consts := g.visible(func(e entry) bool { return e.k == boolConst }); len(consts) > 0 && g.r.IntN(3) == 0 {
			return g.pick(consts)
		}
		return g.pick([]string{"true", "false"})
	}
	switch g.r.IntN(5) {
	case 0:
		return "!(" + g.boolExpr(depth-1, useVars) + ")"
	case 1, 2:
		// Operands of one integer type, often negated, so negative and
		// boundary values are compared. They are shallow: a constant
		// comparison of deep operands mostly overflows, which makes the
		// program invalid.
		op := g.pick([]string{"==", "!=", "<", "<=", ">", ">="})
		d := min(depth-1, 1)
		t := g.intType()
		x, _ := g.intExpr(d, useVars, t)
		y, cy := g.intExpr(d, useVars, t)
		if !cy || !unsigned(t) {
			y = g.pick([]string{"- ", ""}) + y
		}
		return "(" + x + ") " + op + " (" + y + ")"
	case 3:
		return "(" + g.boolExpr(depth-1, useVars) + ") " + g.pick([]string{"==", "!="}) + " (" + g.boolExpr(depth-1, useVars) + ")"
	}
	return "(" + g.boolExpr(depth-1, useVars) + ") " + g.pick([]string{"&&", "||"}) + " (" + g.boolExpr(depth-1, useVars) + ")"
}
