package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated generic programs (Phase 8 increment 8.8, D-045).
//
// generateGenerics writes a program of its own, from its own random
// stream, so that the other generators' programs stay byte for byte what
// they were. Its generic functions have random bodies over operands of a
// type parameter: i0 to i2 over the integer types (Int), with + - * & | ^
// &^ and shifts by constants; n0 and n1 over every number type (Num), with
// + - * and comparisons; f0 over the float types, with constants such as
// T(0.1), which must round at run time (D-045). Each body converts
// constants to the type parameter, assigns, branches, and loops, and may
// call another instance. Generic types Box and Pair, with methods, hold
// the results; Apply and Fold take func values of instances. main calls
// each function with random type arguments, explicit or inferred, on
// values that wrap around in the narrow types, and prints the results.
//
// V-4: each statement makes at most one call, and its other operands are
// variables only that call cannot assign, so gc's order and Aether's
// agree.

// genericInts and genericNums are the type arguments of Int and Num.
var (
	genericInts = []string{"int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64"}
	genericNums = append(append([]string{}, genericInts...), "float32", "float64")
)

// generateGenerics returns the generic program for seed.
func generateGenerics(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0x6e72))}
	g.line(0, "package main\n")
	g.line(0, "type Int interface {\n\t~%s\n}\n", strings.Join(genericInts, " | ~"))
	g.line(0, "type Num interface {\n\tInt | ~float32 | ~float64\n}\n")
	g.line(0, "type Box[T any] struct {\n\tv T\n\tn int\n}\n")
	g.line(0, "func (b *Box[T]) Put(v T) {\n\tb.v = v\n\tb.n++\n}\n")
	g.line(0, "func (b Box[T]) Get() T { return b.v }\n")
	g.line(0, "type Pair[A, B any] struct {\n\ta A\n\tb B\n}\n")
	g.line(0, "func MkPair[A, B any](a A, b B) Pair[A, B] { return Pair[A, B]{a, b} }\n")
	g.line(0, "func Apply[T any](xs []T, f func(T, T) T) []T {\n\tr := make([]T, len(xs))\n\tfor i := 0; i < len(xs); i++ {\n\t\tr[i] = f(xs[i], xs[len(xs)-1-i])\n\t}\n\treturn r\n}\n")
	g.line(0, "func Fold[T, A any](xs []T, a A, f func(A, T) A) A {\n\tfor i := 0; i < len(xs); i++ {\n\t\ta = f(a, xs[i])\n\t}\n\treturn a\n}\n")
	for i := range 3 {
		g.genericFunc(fmt.Sprintf("i%d", i), "Int", []string{"+", "-", "*", "&", "|", "^", "&^"}, i > 0)
	}
	for i := range 2 {
		g.genericFunc(fmt.Sprintf("n%d", i), "Num", []string{"+", "-", "*"}, false)
	}
	g.genericFunc("f0", "~float32 | ~float64", []string{"+", "-", "*", "/"}, false)
	g.line(0, "func main() {")
	g.line(1, "var b Box[int64]")
	// Each function and each form of call at least once: a generic
	// function never instantiated is unsupported.
	for i, fn := range genericFuncs {
		g.genericCall(fn, i%5)
	}
	for range 2 + g.pick(8) {
		g.genericCall(genericFuncs[g.pick(len(genericFuncs))], g.pick(5))
	}
	g.line(1, "println(b.Get(), b.n)")
	g.line(0, "}")
	return g.b.String()
}

// genericConst returns a constant that every type of the constraint of
// the function being generated can represent; floats only in f0.
func (g *mgen) genericConst(float bool) string {
	if float && g.pick(2) == 0 {
		return []string{"0.1", "0.2", "1.5", "0.3", "2.25"}[g.pick(5)]
	}
	return fmt.Sprint(g.pick(101))
}

// genericFunc emits name[T c](x, y T) T, whose body uses the operators
// ops; if calls, it may call i0, an instance of the same type argument.
func (g *mgen) genericFunc(name, c string, ops []string, calls bool) {
	float := name == "f0"
	g.line(0, "func %s[T %s](x, y T) T {", name, c)
	g.line(1, "z := T(%s)", g.genericConst(float))
	term := func() string {
		switch g.pick(4) {
		case 0:
			return fmt.Sprintf("T(%s)", g.genericConst(float))
		case 1:
			return "x"
		case 2:
			return "y"
		}
		return "z"
	}
	expr := func() string {
		e := term()
		for range 1 + g.pick(2) {
			e = fmt.Sprintf("(%s %s %s)", e, ops[g.pick(len(ops))], term())
		}
		return e
	}
	for range 2 + g.pick(4) {
		v := []string{"x", "y", "z"}[g.pick(3)]
		switch g.pick(6) {
		case 0, 1:
			g.line(1, "%s = %s", v, expr())
		case 2:
			g.line(1, "if %s %s %s {\n\t\t%s = %s\n\t}", term(), []string{"<", ">", "==", "!="}[g.pick(4)], term(), v, expr())
		case 3:
			g.line(1, "for i := 0; i < %d; i++ {\n\t\t%s = %s\n\t}", 1+g.pick(4), v, expr())
		case 4:
			if calls {
				g.line(1, "%s = i0(%s, %s)", v, term(), term())
				continue
			}
			g.line(1, "%s += %s", v, term())
		default:
			if name[0] == 'i' {
				g.line(1, "%s = %s %s %d", v, term(), []string{"<<", ">>"}[g.pick(2)], g.pick(70))
				continue
			}
			g.line(1, "%s -= %s", v, term())
		}
	}
	g.line(1, "return %s + z", expr())
	g.line(0, "}\n")
}

// genericFuncs are the generic functions with random bodies.
var genericFuncs = []string{"i0", "i1", "i2", "n0", "n1", "f0"}

// genericCall emits a loop of main that calls fn in the form kind.
func (g *mgen) genericCall(fn string, kind int) {
	targs := genericInts
	switch fn[0] {
	case 'n':
		targs = genericNums
	case 'f':
		targs = []string{"float32", "float64"}
	}
	t := targs[g.pick(len(targs))]
	arg := func() string {
		return fmt.Sprintf("%s(k*%d + %d)", t, 1+g.pick(300), g.pick(50))
	}
	inst := fn
	if g.pick(2) == 0 {
		inst = fmt.Sprintf("%s[%s]", fn, t)
	}
	g.line(1, "for k := 0; k < %d; k++ {", 1+g.pick(4))
	switch kind {
	case 0:
		g.line(2, "println(%s(%s, %s))", inst, arg(), arg())
	case 1:
		g.line(2, "bx := Box[%s]{}", t)
		g.line(2, "bx.Put(%s(%s, %s))", inst, arg(), arg())
		g.line(2, "p := MkPair(bx, bx.Get())")
		g.line(2, "println(p.a.v, p.a.n, p.b)")
	case 2:
		g.line(2, "r := Apply([]%s{%s, %s, %s}, %s[%s])", t, arg(), arg(), arg(), fn, t)
		g.line(2, "println(r[0], r[1], r[2], len(r))")
	case 3:
		// A comparison, not a conversion: int64 of a float out of its
		// range is not specified.
		g.line(2, "s := Fold([]%s{%s, %s}, 0, func(a int64, v %s) int64 {\n\t\t\tif %s(v, v) > v {\n\t\t\t\treturn a*3 + 1\n\t\t\t}\n\t\t\treturn a*3 + 2\n\t\t})", t, arg(), arg(), t, inst)
		g.line(2, "b.Put(s)")
		g.line(2, "println(s)")
	default:
		g.line(2, "f := %s[%s]", fn, t)
		g.line(2, "x := f(%s, %s)", arg(), arg())
		g.line(2, "println(x, f(x, x))")
	}
	g.line(1, "}")
}
