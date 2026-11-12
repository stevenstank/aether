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
// order (types.Info.InitOrder) differs from the declaration order. No
// function assigns a global (the only ones besides main are those of
// gen_agg_test.go), so no call assigns a global while an expression reads
// it (SEMANTICS.md V-4). main assigns globals like locals
// and prints them all at its end. Since increment 4.7, a program may
// contain switch statements on bool and integer tags, or without a tag,
// with an optional init statement, several values per case, a default
// clause anywhere, empty clauses, and break in a clause; case values may
// have side effects only through a panic.
//
// Since Phase 6 increment 6.1, statements on float64 and float32 values
// (floatStmt) are interleaved with the others, and float globals follow the
// integer ones. Their choices come from a second random stream, so the rest
// of the program is what the same seed generated before. Since increment
// 6.2, string statements (stringStmt) and globals are added the same way,
// from a third stream, and since increment 6.3 array and struct types,
// functions, globals, and statements (gen_agg_test.go) from a fourth.
//
// Every program is valid Go except that a constant expression may overflow
// its type, which go/types (and gc) reject; callers skip such programs.
// Every variable is printed at the end of its block, so none is unused.
// Every loop runs at most 3 times: its counter is never a target of
// assignment, and nested loops are bounded in depth.
func generate(seed uint64) string {
	g := &generator{r: rand.New(rand.NewPCG(seed, 0x5eed)), f: rand.New(rand.NewPCG(seed, 0xf10a7)), s: rand.New(rand.NewPCG(seed, 0x57217)),
		a: rand.New(rand.NewPCG(seed, 0xa66))}
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
	g.floatGlobals()
	g.stringGlobals()
	g.aggDecls()
	g.line(0, "func main() {")
	// Float variables that are never constant: zeros, for the special
	// values of division, and nonzero values.
	g.scopes, g.order = append(g.scopes, map[string]entry{}), append(g.order, nil)
	for _, v := range []struct{ name, t, decl string }{
		{"fz", "float64", "var fz float64"}, {"fo", "float64", "fo := 0.1"},
		{"gz", "float32", "var gz float32"}, {"gt", "float32", "gt := float32(1) / 3"},
	} {
		g.line(1, "%s", v.decl)
		g.declare(v.name, entry{k: floatVar, t: v.t})
	}
	g.block(1)
	g.line(1, "println(fz, fo, gz, gt)")
	g.scopes, g.order = g.scopes[:len(g.scopes)-1], g.order[:len(g.order)-1]
	if globals := g.visible(entry.isVar); len(globals) > 0 {
		g.line(1, "println(%s)", strings.Join(g.uses(globals), ", "))
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
	floatVar              // a float64 or float32 variable, which only floatStmt uses
	strVar                // a string variable, which only stringStmt uses
	aggVar                // an array or struct variable, which only aggStmt uses
)

// entry is a declared name: what it denotes and, for a variable, its type.
type entry struct {
	k kind
	t string // "bool" or an integer type name; "int" for a counter; an aggVar's type name
}

func (e entry) isVar() bool {
	return e.k == variable || e.k == counter || e.k == floatVar || e.k == strVar || e.k == aggVar
}

// uses returns expressions that use the variables names: each itself, or
// for an array or struct one of its scalars (aggUse).
func (g *generator) uses(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = name
		if e, _ := g.lookup(name); e.k == aggVar {
			out[i] = g.aggUse(name, g.typeOf(e.t))
		}
	}
	return out
}

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
	r        *rand.Rand
	f        *rand.Rand // the float statements' choices
	fnames   int
	s        *rand.Rand // the string statements' choices
	snames   int
	a        *rand.Rand // the array and struct choices
	anames   int
	aggStmts int
	aggTypes []*gtype
	b        strings.Builder
	scopes   []map[string]entry
	order    [][]string // declaration order, per scope
	names    int
	loops    int // enclosing loops
	breaks   int // enclosing loops and switches
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

// block emits 2 to 8 statements in a new scope, then prints its variables.
func (g *generator) block(depth int) { g.stmts(depth, 2+g.r.IntN(7)) }

// stmts emits n statements in a new scope, then prints its variables.
func (g *generator) stmts(depth, n int) {
	g.scopes = append(g.scopes, map[string]entry{})
	g.order = append(g.order, nil)
	for ; n > 0; n-- {
		g.stmt(depth)
	}
	var vars []string
	for _, name := range g.order[len(g.order)-1] {
		if g.scopes[len(g.scopes)-1][name].isVar() {
			vars = append(vars, name)
		}
	}
	if len(vars) > 0 {
		g.line(depth, "println(%s)", strings.Join(g.uses(vars), ", "))
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
	if outer := g.visible(func(e entry) bool { return e.k != floatVar && e.k != strVar && e.k != aggVar }); len(outer) > 0 && g.r.IntN(4) == 0 {
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
	if g.f.IntN(3) == 0 {
		g.floatStmt(depth)
	}
	if g.s.IntN(4) == 0 {
		g.stringStmt(depth)
	}
	if g.a.IntN(5) == 0 && g.aggStmts < maxAggStmts {
		g.aggStmts++
		g.aggStmt(depth)
	}
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
	case k == 14 && g.breaks > 0:
		jumps := []string{"break"}
		if g.loops > 0 {
			jumps = append(jumps, "continue")
		}
		g.line(depth, "if %s {", g.boolExpr(2, true))
		g.line(depth+1, "%s", g.pick(jumps))
		g.line(depth, "}")
	case k == 15 && depth < 4:
		g.switchStmt(depth)
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
	g.breaks++
	defer func() { g.loops--; g.breaks-- }()
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

// switchStmt emits a switch statement: on a bool or integer tag, which
// may be a variable declared by the init statement, or without a tag. go/types
// rejects duplicate integer constants among the case values, so a constant
// integer case value is a literal not used before in the switch, which
// fits every integer type.
func (g *generator) switchStmt(depth int) {
	g.scopes = append(g.scopes, map[string]entry{})
	g.order = append(g.order, nil)
	g.breaks++
	defer func() {
		g.scopes, g.order = g.scopes[:len(g.scopes)-1], g.order[:len(g.order)-1]
		g.breaks--
	}()
	var head, t string
	switch g.r.IntN(4) {
	case 0:
		head = "switch "
	case 1:
		// An init statement declaring the tag, in the switch's scope.
		name := g.newName(nil)
		v, vt := g.value()
		head, t = fmt.Sprintf("switch %s := %s; %s ", name, v, name), vt
		g.declare(name, entry{k: variable, t: vt})
	case 2:
		head, t = "switch "+g.boolExpr(2, true)+" ", "bool"
	default:
		t = g.intType()
		e, isConst := g.intExpr(2, true, t)
		if isConst {
			t = "int"
		}
		head = "switch " + e + " "
	}
	g.line(depth, "%s{", head)
	// At most 3 clauses besides default, each with at most 3 values, so
	// the literals never run out.
	lits := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "100", "127"}
	if t != "" && t != "bool" && !unsigned(t) {
		lits = append(lits, "-1")
	}
	g.r.Shuffle(len(lits), func(i, j int) { lits[i], lits[j] = lits[j], lits[i] })
	value := func() string {
		switch t {
		case "", "bool":
			return g.boolExpr(2, true)
		}
		if e, isConst := g.intExpr(2, true, t); !isConst && g.r.IntN(2) == 0 {
			return e
		}
		lit := lits[0]
		lits = lits[1:]
		return lit
	}
	clauses := g.r.IntN(4)
	dflt := -1
	if g.r.IntN(2) == 0 {
		dflt = g.r.IntN(clauses + 1)
		clauses++
	}
	for i := range clauses {
		if i == dflt {
			g.line(depth, "default:")
		} else {
			var vals []string
			for n := 1 + g.r.IntN(3); n > 0; n-- {
				vals = append(vals, value())
			}
			g.line(depth, "case %s:", strings.Join(vals, ", "))
		}
		// A break here skips the rest of the clause, and leaves only the
		// switch, not an enclosing loop.
		if g.r.IntN(4) == 0 {
			g.line(depth+1, "if %s {", g.boolExpr(2, true))
			g.line(depth+2, "break")
			g.line(depth+1, "}")
		}
		// A short body, possibly empty, keeps programs small.
		g.stmts(depth+1, g.r.IntN(4))
	}
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

// floatTypes are the float types; floatLiterals gives each its literals,
// none zero, so that none is a constant divisor of zero, and each
// representable, so that no constant overflows. Zeros, infinities, and NaN
// come from arithmetic on variables.
var (
	floatTypes    = []string{"float64", "float32"}
	floatLiterals = map[string][]string{
		"float64": {"0.1", "1.5", "3", "1e308", "5e-324", "0x1p-1022", "1e-7", "16777217", "9.3e18", "2.5", "1e18", "0.3"},
		"float32": {"0.1", "1.5", "3", "3.4e38", "1e-45", "1e-38", "16777217", "2.5", "9.3e18", "0.3"},
	}
)

// floatGlobals emits up to three package-level float variables, in shuffled
// order, each initialized from the ones generated before it or a literal.
func (g *generator) floatGlobals() {
	var decls []string
	for n := g.f.IntN(4); n > 0; n-- {
		g.fnames++
		name, t := fmt.Sprintf("gf%d", g.fnames), g.floatPick(floatTypes)
		e, _ := g.floatExpr(2, t)
		decls = append(decls, fmt.Sprintf("var %s %s = %s\n\n", name, t, e))
		g.declare(name, entry{k: floatVar, t: t})
	}
	g.f.Shuffle(len(decls), func(i, j int) { decls[i], decls[j] = decls[j], decls[i] })
	for _, d := range decls {
		g.b.WriteString(d)
	}
}

func (g *generator) floatPick(list []string) string { return list[g.f.IntN(len(list))] }

// floatStmt emits a statement on float values: a declaration, an
// assignment, a compound assignment, ++ or --, or a print of float values,
// comparisons, and conversions to and from integers.
func (g *generator) floatStmt(depth int) {
	t := g.floatPick(floatTypes)
	vars := g.visible(func(e entry) bool { return e.k == floatVar && e.t == t })
	switch k := g.f.IntN(8); {
	case k <= 1:
		g.fnames++
		name := fmt.Sprintf("f%d", g.fnames)
		// A constant may be an integer literal, which := would give type
		// int, so a constant initializer has the type written out.
		e, isConst := g.floatExpr(3, t)
		if t == "float64" && !isConst && g.f.IntN(2) == 0 {
			g.line(depth, "%s := %s", name, e)
		} else {
			g.line(depth, "var %s %s = %s", name, t, e)
		}
		g.declare(name, entry{k: floatVar, t: t})
	case k == 2 && len(vars) > 0:
		e, _ := g.floatExpr(3, t)
		g.line(depth, "%s = %s", g.floatPick(vars), e)
	case k == 3 && len(vars) > 0:
		e, _ := g.floatExpr(2, t)
		g.line(depth, "%s %s %s", g.floatPick(vars), g.floatPick([]string{"+=", "-=", "*=", "/="}), e)
	case k == 4 && len(vars) > 0:
		g.line(depth, "%s%s", g.floatPick(vars), g.floatPick([]string{"++", "--"}))
	case k == 5:
		// Comparisons, NaN with itself included.
		x, _ := g.floatExpr(2, t)
		y, _ := g.floatExpr(2, t)
		var args []string
		for _, op := range []string{"==", "!=", "<", "<=", ">", ">="} {
			args = append(args, "("+x+") "+op+" ("+y+")")
		}
		g.line(depth, "println(%s)", strings.Join(args, ", "))
	case k == 6:
		// Conversions of a non-constant value to every integer type: out of
		// range, the result is gc's on amd64 (D-032).
		x := g.floatVarExpr(t)
		var args []string
		for _, it := range []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64"} {
			args = append(args, it+"("+x+")")
		}
		g.line(depth, "println(%s)", strings.Join(args, ", "))
	default:
		var args []string
		for n := 1 + g.f.IntN(3); n > 0; n-- {
			e, _ := g.floatExpr(3, g.floatPick(floatTypes))
			args = append(args, e)
		}
		g.line(depth, "%s(%s)", g.floatPick([]string{"println", "print"}), strings.Join(args, ", "))
	}
}

// floatVarExpr returns a non-constant expression of float type t, where a
// variable of type t is in scope (main's own always are).
func (g *generator) floatVarExpr(t string) string {
	if e, isConst := g.floatExpr(2, t); !isConst {
		return e
	}
	return g.floatPick(g.visible(func(e entry) bool { return e.k == floatVar && e.t == t }))
}

// floatExpr returns an expression of float type t of at most the given
// depth, and whether it is constant. At most one operand of a binary
// operator is constant, so no constant expression can overflow, and a
// constant is converted only implicitly.
func (g *generator) floatExpr(depth int, t string) (string, bool) {
	vars := g.visible(func(e entry) bool { return e.k == floatVar && e.t == t })
	if depth <= 0 || g.f.IntN(4) == 0 {
		switch {
		case len(vars) > 0 && g.f.IntN(3) > 0:
			return g.floatPick(vars), false
		case g.f.IntN(4) == 0:
			// A conversion from an integer or the other float type.
			ints := g.visible(func(e entry) bool { return e.k == variable && e.t != "bool" || e.k == counter })
			other := "float32"
			if t == other {
				other = "float64"
			}
			others := g.visible(func(e entry) bool { return e.k == floatVar && e.t == other })
			if len(ints) > 0 && g.f.IntN(2) == 0 {
				return t + "(" + g.floatPick(ints) + ")", false
			}
			if len(others) > 0 {
				return t + "(" + g.floatPick(others) + ")", false
			}
		}
		return g.floatPick(floatLiterals[t]), true
	}
	switch g.f.IntN(4) {
	case 0:
		x, isConst := g.floatExpr(depth-1, t)
		return "- (" + x + ")", isConst
	case 1:
		x, isConst := g.floatExpr(depth-1, t)
		return "(" + x + ")", isConst
	}
	x, cx := g.floatExpr(depth-1, t)
	y, cy := g.floatExpr(depth-1, t)
	if cx && cy {
		// Two constants: replace one with a variable, or with none in
		// scope, as at package level before any float global, keep one.
		if len(vars) == 0 {
			return x, true
		}
		y = g.floatPick(vars)
	}
	return "(" + x + ") " + g.floatPick([]string{"+", "-", "*", "/"}) + " (" + y + ")", false
}

// stringLiterals are the string constants: empty, ASCII, multibyte UTF-8,
// NUL bytes, and invalid UTF-8.
var stringLiterals = []string{`""`, `"a"`, `"abc"`, `"héllo"`, `"日本"`, `"x\x00y"`, `"\xff\xfe"`, `"The end."`, "`raw\\`"}

func (g *generator) stringPick(list []string) string { return list[g.s.IntN(len(list))] }

// stringGlobals emits up to three package-level string variables, in
// shuffled order, each initialized from a literal or a global generated
// before it.
func (g *generator) stringGlobals() {
	var decls []string
	for n := g.s.IntN(4); n > 0; n-- {
		g.snames++
		name := fmt.Sprintf("gs%d", g.snames)
		decls = append(decls, fmt.Sprintf("var %s = %s\n\n", name, g.stringExpr()))
		g.declare(name, entry{k: strVar, t: "string"})
	}
	g.s.Shuffle(len(decls), func(i, j int) { decls[i], decls[j] = decls[j], decls[i] })
	for _, d := range decls {
		g.b.WriteString(d)
	}
}

// stringExpr returns a string expression: a literal or a variable,
// perhaps parenthesized.
func (g *generator) stringExpr() string {
	e := g.stringPick(stringLiterals)
	if vars := g.visible(func(e entry) bool { return e.k == strVar }); len(vars) > 0 && g.s.IntN(2) == 0 {
		e = g.stringPick(vars)
	}
	if g.s.IntN(4) == 0 {
		e = "(" + e + ")"
	}
	return e
}

// stringStmt emits a statement on strings: a declaration, an assignment, a
// print of strings and lengths, or an index of a string. An index is
// usually guarded by a test of the length, and otherwise may be out of
// range, of either sign, so that the program panics. Some prints mix in
// integer operands that branch, which the strings' words must survive.
func (g *generator) stringStmt(depth int) {
	vars := g.visible(func(e entry) bool { return e.k == strVar })
	ints := g.visible(func(e entry) bool { return (e.k == variable || e.k == counter) && e.t != "bool" })
	switch k := g.s.IntN(7); {
	case k <= 1 || len(vars) == 0:
		g.snames++
		name := fmt.Sprintf("s%d", g.snames)
		switch g.s.IntN(3) {
		case 0:
			g.line(depth, "%s := %s", name, g.stringExpr())
		case 1:
			g.line(depth, "var %s string = %s", name, g.stringExpr())
		default:
			g.line(depth, "var %s string", name)
		}
		g.declare(name, entry{k: strVar, t: "string"})
	case k == 2:
		g.line(depth, "%s = %s", g.stringPick(vars), g.stringExpr())
	case k == 3:
		// Two strings, swapped.
		a, b := g.stringPick(vars), g.stringPick(vars)
		if a != b {
			g.line(depth, "%s, %s = %s, %s", a, b, b, a)
		}
	case k == 4 && len(ints) > 0:
		// An index by a variable of any integer type, guarded unless
		// the program may panic.
		s, i := g.stringPick(vars), g.stringPick(ints)
		if g.s.IntN(8) > 0 {
			g.line(depth, "if %s >= 0 && uint64(%s) < uint64(len(%s)) {", i, i, s)
			g.line(depth+1, "println(%s[%s], %s)", s, i, s)
			g.line(depth, "}")
		} else {
			g.line(depth, "println(%s[%s])", s, i)
		}
	case k == 5:
		// Strings around operands that branch: && and a division.
		s := g.stringPick(vars)
		args := []string{s, "len(" + s + ")", "(len(" + s + ") > 0) && (len(" + s + ") < 4)", s}
		if len(ints) > 0 {
			i := g.stringPick(ints)
			args = append(args, i+" / ("+i+" | 1)", s)
		}
		g.line(depth, "%s(%s)", g.stringPick([]string{"println", "print"}), strings.Join(args, ", "))
	default:
		var args []string
		for n := 1 + g.s.IntN(3); n > 0; n-- {
			e := g.stringExpr()
			if g.s.IntN(3) == 0 {
				e = "len(" + e + ")"
			}
			args = append(args, e)
		}
		g.line(depth, "%s(%s)", g.stringPick([]string{"println", "print"}), strings.Join(args, ", "))
	}
}
