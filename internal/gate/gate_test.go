package gate

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
)

// classify runs the frontend and the gate on the body of func main, in the
// order used by the aether command, and returns the features of all
// unsupported diagnostics. It fails the test on invalid or internal results.
func classify(t *testing.T, body string) []string {
	t.Helper()
	return classifyFile(t, "package main\n\nfunc main() {\n"+body+"\n}\n")
}

func classifyFile(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, ds := frontend.Parse(fset, "p.go", []byte(src))
	if len(ds) > 0 {
		t.Fatalf("syntax error: %v", ds)
	}
	if ds := Precheck(fset, f, true, false); len(ds) > 0 {
		return features(t, ds)
	}
	pkg, ds := frontend.TypeCheck(fset, []*ast.File{f})
	if len(ds) > 0 {
		t.Fatalf("invalid Go (test cases must be valid): %v", ds)
	}
	return features(t, Check(pkg))
}

func features(t *testing.T, ds []diag.Diagnostic) []string {
	t.Helper()
	var fs []string
	for _, d := range ds {
		if d.Class != diag.Unsupported {
			t.Fatalf("unexpected %s diagnostic: %s", d.Class, d)
		}
		if d.Pos.Line == 0 {
			t.Errorf("diagnostic without a line: %s", d)
		}
		fs = append(fs, d.Feature)
	}
	slices.Sort(fs)
	return slices.Compact(fs)
}

// TestAccepted lists the S1 constructs (docs/SEMANTICS.md §Subset S1)
// individually; each must pass the gate with no diagnostics.
func TestAccepted(t *testing.T) {
	tests := map[string]string{
		"empty main":                   ``,
		"complex values":               `var c complex128 = 1 + 2i; d := complex64(c); var a any = c; println(real(c), imag(d), c == complex(1, 2), a == any(c), complex64(2) == 0, 0i == 0)`,
		"var without initializer":      `var x int; println(x)`,
		"var with type and value":      `var x int = 1; println(x)`,
		"var with inferred type":       `var x = 1; println(x)`,
		"var list":                     `var a, b int = 1, 2; println(a, b)`,
		"grouped var":                  "var (\n a int\n b = 2\n)\nprintln(a, b)",
		"short declaration":            `x := 1; println(x)`,
		"short declaration list":       `a, b := 1, 2; println(a, b)`,
		"redeclaration":                `a, b := 1, 2; a, c := 3, 4; println(a, b, c)`,
		"untyped const":                `const c = 3; println(c)`,
		"typed const":                  `const c int = 3; println(c)`,
		"grouped const":                "const (\n a = 1\n b int = a * 2\n)\nprintln(a, b)",
		"const from const":             `const a = 2; const b = a * a; println(b)`,
		"oversized intermediate":       `const big = 9223372036854775807*4 - 3*9223372036854775807; println(big)`,
		"min int literal":              `x := -9223372036854775808; println(x)`,
		"assignment":                   `x := 1; x = 2; println(x)`,
		"tuple assignment":             `a, b := 1, 2; a, b = b, a; println(a, b)`,
		"compound assignments":         `x := 1; x += 2; x -= 3; x *= 4; println(x)`,
		"increment and decrement":      `x := 1; x++; x--; println(x)`,
		"binary operators":             `x := 1; y := x + x - x*x; println(y)`,
		"unary operators":              `x := 1; y := -x + +x; println(y)`,
		"parentheses":                  `x := 1; y := (x + 1) * (x - (1)); println(y)`,
		"integer literal forms":        `println(0x1F, 0X1f, 0o17, 0O17, 017, 0b101, 0B101, 1_000_000, 0)`,
		"nested blocks":                `x := 1; { x := x + 1; { println(x) } }; println(x)`,
		"empty statements":             `;;`,
		"println no arguments":         `println()`,
		"println constant argument":    `println(1 + 2)`,
		"builtin names as vars":        `len := 1; print := 2; int := 3; println(len, print, int)`,
		"return in main":               `println(1); return`,
		"bool variables":               `var a bool; b := true; var c bool = !b; a = b && c || !a; println(a, b, c)`,
		"bool constants":               `const t = true; const f bool = !t; const c = 1 < 2; println(t, f, c, false)`,
		"comparisons":                  `x := 1; println(x == 1, x != 2, x < 3, x <= 4, x > 5, x >= 6, true == (x > 0))`,
		"if, else if, else":            `x := 1; if x > 0 { println(1) } else if y := x; y < 0 { println(y) } else { println(2) }`,
		"three-clause for":             `for i := 0; i < 3; i++ { println(i) }`,
		"condition-only for":           `i := 0; for i < 3 { i++ }; println(i)`,
		"infinite for":                 `for { break }`,
		"break and continue":           `for i := 0; i < 9; i++ { if i == 2 { continue }; if i > 5 { break }; println(i) }`,
		"for with empty clauses":       `i := 0; for ; i < 3; { i++ }; println(i)`,
		"sized integers":               `var a int8 = -1; var b uint16 = 65535; c := uint64(a) &^ uint64(b); var d byte = 1; var e rune = 2; var f uintptr = 3; var g uint = 4; println(a, b, c, d, e, f, g, ^a, a&a|a^a)`,
		"conversions":                  `x := 300; println(int8(x), uint8(x), (int16)(x), uint32(-x), int64(uint64(x)), bool(x > 0))`,
		"bit assignments":              `x := uint32(5); x &= 3; x |= 8; x ^= 1; x &^= 2; println(x)`,
		"print":                        `print(1, true, uint8(2)); print(); println()`,
		"division and shifts":          `x, y := 7, 2; var u uint8 = 3; println(x/y, x%y, x<<y, x>>u, 1<<u, -x>>1); x /= y; x %= 3; x <<= u; x >>= 1; const c = 7 / 2 << 3; println(x, c)`,
		"recursive main":               `main()`,
		"switch":                       `x := 2; switch y := x + 1; y { case 1, 2: println(1); case 3: default: break }; switch {}; switch { case x > 1: }; switch int8(x) { case -1: }`,
		"break and continue in switch": `for i := 0; i < 3; i++ { switch i { case 0: continue; case 1: break }; println(i) }`,
		// Floats (Phase 6 increment 6.1).
		"float variables":            `var a float64; b := 1.5; var c float32 = 0.1; println(a, b, c)`,
		"float literal forms":        `println(1.5, .5, 1., 1e3, 1E-3, 0x1p-2, 0X1.8P1, 1_000.5)`,
		"float arithmetic":           `x, y := 1.5, 2.5; x += y; x -= 1; x *= y; x /= 3; x++; x--; println(x+y, x-y, x*y, x/y, -x, +x)`,
		"float comparisons":          `x, y := float32(1), float32(2); println(x == y, x != y, x < y, x <= y, x > y, x >= y)`,
		"float conversions":          `x := 3; f := float64(x); g := float32(f); println(int(f), uint8(g), float64(g), int64(g))`,
		"float constants":            `const a = 1.0 / 3; const b float32 = a; const c float64 = 2.5 * 4; println(a, b, c)`,
		"float literal as int":       `var x int = 1.0; var y uint8 = 0x1p4; const c int = 2.0 * 3; println(x, y, c)`,
		"untyped float constant int": `const c = 2.5 * 2; var y int = c; println(y)`,
		"float switch":               `x := 1.5; switch x { case 0.5, 1.5: println(1) }`,
		// Strings (Phase 6 increment 6.2).
		"string variables":  "var a string; b := \"x\"; var c string = `raw\\n`; a = b; println(a, b, c, \"\", \"\\x00é\")",
		"string constants":  `const c = "héllo"; const d string = c; const n = len(c); println(c, d, n)`,
		"string len":        `s := "abc"; n := len(s); var m uint8 = uint8(len(s)); println(n, m, len("xy"))`,
		"string index":      `s := "abc"; i := 1; var u uint8 = 2; println(s[0], s[i], s[u], "xyz"[i], s[len(s)-1])`,
		"string print":      `s := "x"; print(s, 1, "y"); println(s)`,
		"string tuple":      `a, b := "x", "y"; a, b = b, a; println(a, b)`,
		"index in a switch": `s := "ab"; switch s[0] { case 97, 0x20: }`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if got := classify(t, body); len(got) != 0 {
				t.Errorf("rejected with %v", got)
			}
		})
	}
	// Functions (increment 4.2).
	files := map[string]string{
		"parameters and result":     "func f(a, b int, c int) int { return a - b*c }\nfunc main() { println(f(1, 2, 3)) }",
		"call statement":            "func f(x int) { println(x) }\nfunc main() { f(1) }",
		"discarded result":          "func f() int { return 1 }\nfunc main() { f() }",
		"blank and unnamed":         "func f(_ int) int { return 2 }\nfunc g(int, int) {}\nfunc main() { g(f(1), 1) }",
		"parenthesized callee":      "func f() int { return 1 }\nfunc main() { println((f)()) }",
		"nested calls":              "func f(x int) int { return x }\nfunc main() { println(f(f(1) + f(2))) }",
		"recursion":                 "func f(x int) int { return f(x-1) + 1 }\nfunc main() { println(f(1)) }",
		"mutual recursion":          "func f(x int) int { return g(x) }\nfunc g(x int) int { return f(x) }\nfunc main() { f(0) }",
		"code after return":         "func f() { return; println(2) }\nfunc main() { f() }",
		"return in a block":         "func f() int { { return 1 } }\nfunc main() { println(f()) }",
		"empty result list":         "func f() () { return }\nfunc main() { f() }",
		"bool parameter and result": "func f(b bool, x int) bool { return !b && x > 0 }\nfunc main() { println(f(true, 1)) }",
		"multiple results":          "func f(x int) (int, bool, int) { return x, x > 0, -x }\nfunc g() (int, bool, int) { return f(2) }\nfunc h(a int, b bool, c int) { println(a, b, c) }\nfunc main() { a, b, c := g(); var d, e, f2 = f(1); a, b, c = f(3); h(g()); f(4); println(a, b, c, d, e, f2); println(f(5)) }",
		// Arrays and structs (increment 6.3).
		"aggregate types":      "type P struct{ x, y int }\ntype Q struct { P; s string; a [2]P; _ float32 }\ntype A [3]Q\nvar g A\nfunc main() { type L [2]struct{ b bool }; var l L; l[1].b = true; println(l[0].b) }",
		"aggregate parameters": "type P struct{ x, y int }\nfunc f(p P, a [2]int8) (P, [2]int8, int) { return p, a, 1 }\nfunc main() { p, a, n := f(P{1, 2}, [2]int8{3}); println(p.x, a[1], n) }",
		"aggregate literals":   "type P struct{ x, y int }\nfunc main() { a := [...]P{{1, 2}, 4: {y: 3}}; b := struct{ p P }{p: P{}}; c := [2][2]int{{1}, {}}; println(a[4].y, b.p.x, c[1][0], len(a), len([2]int{})) }",
		"aggregate targets":    "type P struct{ x, y int; a [3]int }\nvar g [2]P\nfunc main() { i := 1; g[i].a[i] = 2; g[0].x += 3; g[1].y++; g[i] = P{}; println(g[1].a[1]) }",
		"aggregate comparison": "type P struct{ x float64; _ int; b bool }\nfunc main() { var a, b [2]P; println(a == b, a[0] != b[1], P{} == P{x: 1}) }",
		"aggregate conversion": "type P struct{ x int }\ntype Q struct{ x int }\nfunc main() { q := Q(P{1}); println(q.x, [2]int([2]int{}) == [2]int{}) }",
		"promoted field":       "type P struct{ x int }\ntype Q struct{ P }\nfunc main() { var q Q; q.x = 1; println(q.x, q.P.x) }",
		"zero-size aggregates": "type E struct{}\nfunc main() { var a [0]int; var e [3]E; println(len(a), e[1] == E{}) }",
		// Pointers (Phase 7 increment 7.1).
		"pointer types":            "type N struct{ v int; next *N; s *string }\ntype M struct{ a *[2]M; b **N }\nvar g *N\nfunc f(p *int, q **float64) *[3]bool { return nil }\nfunc main() { var m M; println(m.a == nil, g == nil, f(nil, nil) == nil) }",
		"address and deref":        "type P struct{ x, y int; a [2]int }\nvar g P\nfunc main() { x := 1; p := &x; *p = 2; *p += 1; *p++; q := &g.a[x]; r := &P{1, 2, [2]int{}}; println(*p, *q, r.y, (*r).x, &g == &g) }",
		"targets through pointers": "type P struct{ x int; a [2]int; n *P }\nfunc f() *P { return new(P) }\nfunc main() { p := f(); p.x = 1; p.a[1]++; p.n = p; p.n.n.x += 2; f().x = 3; (*p).a[0] = 4; pa := &p.a; pa[0] = 5; println(p.x, len(pa)) }",
		"embedded pointer":         "type I struct{ a int }\ntype O struct{ *I; b int }\nfunc main() { o := O{&I{1}, 2}; o.a = 3; println(o.a, o.I.a) }",
		"elided address":           "type P struct{ x int }\nfunc main() { a := [2]*P{{1}, nil}; s := struct{ p [1]*P }{[1]*P{{2}}}; println(a[0].x, s.p[0].x) }",
		"pointer comparison":       "type P struct{ x int }\nfunc main() { p, q := new(P), new(P); switch p { case q, nil: }; var a, b struct{ p *P }; println(p == q, p != nil, nil == q, a == b) }",
		"pointer conversion":       "type P struct{ x int }\ntype Q struct{ x int }\nfunc main() { p := &P{}; q := (*Q)(p); println(q.x, (*int)(nil) == nil) }",
		"address of a parameter":   "func f(x int, a [2]int) *int { a[0] = x; return &a[0] }\nfunc main() { println(*f(1, [2]int{})) }",
		"address in an if init":    "func main() { if x := 1; &x != nil { println(x) } }",
		// Slices (increment 7.2).
		"slice types":           "type P struct{ s []int; a [2][]string }\nvar g [][]P\nfunc f(s []int, p *[]int) ([]bool, []int) { return nil, s }\nfunc main() { var t []float32; var a [3][]int; println(len(t), len(a), g == nil) }",
		"slice operations":      "func main() { s := make([]int, 2, 5); t := []int{1, 2, 3: 4}; s = append(s, 7); s = append(s, t...); n := copy(s, t[1:]); s[0] += n; s[1]++; println(len(s), cap(s), s[0], s == nil, nil != t, len(s[1:2:3]), cap(make([]int8, 3))) }",
		"slicing arrays":        "var g [4]int\nfunc main() { var a [3]int; p := &[2]int{}; s, t, u := a[:], p[1:], g[1:3]; println(len(s), len(t), cap(u), cap(a), cap(p)) }",
		"slice elided literals": "type P struct{ x int }\nfunc main() { s := [][]P{{{1}}, nil}; t := []*P{{2}}; println(s[0][0].x, t[0].x) }",
		"slice conversion":      "func main() { s := []int(nil); t := []int(s); println(len(t)) }",
		"copy statement":        "func main() { s := []int{1}; copy(s, s) }",
		// Strings (increment 7.3, D-037).
		"string concatenation": "var g = \"g\"\nfunc f() string { return \"f\" }\nfunc main() { s := \"x\"; s += f(); const c = \"x\" + \"y\"; t := s + c + g + s; println(t, len(s+t)) }",
		"string slicing":       "func main() { s := \"xyz\"; t := s[1:]; println(s[:2], t[0], s[1:2], \"ab\"[1:], s[:]) }",
		"byte conversions":     "type P struct{ b []byte }\nfunc main() { s := \"x\"; b := []byte(s); p := P{[]uint8(\"yz\")}; println(string(b), string(p.b[1:]), string(s), len([]byte(\"\"))) }",
		"string equality":      "type P struct{ s string; t [2]string }\nvar g = \"g\" == \"h\"\nfunc main() { s := \"x\"; var p, q P; println(s == \"x\", s != s+s, p == q, p.t != q.t, g) }",
		"string switch":        "func f() string { return \"a\" }\nfunc main() { switch f() { case \"a\", \"b\": println(1); default: }; switch s := f(); s { case f(): } }",
		"string bytes":         "func main() { b := []byte{}; b = append(b, \"xy\"...); s := \"z\"; b = append(b, s...); println(copy(b, \"xy\"), copy(b, s), len(b)) }",
		"string order":         "func main() { s := \"x\"; println(\"a\" < s, s >= s, s > s+s, s <= \"y\") }",
		"rune literals":        "const c = 'a'\nvar g = [2]rune{'\\n', '世'}\nfunc main() { var x int = 'a'; println('a', x+c, g[1], '\\U0001F600') }",
		"rune conversions":     "type P struct{ r []rune }\nfunc main() { s := \"x\"; r := []rune(s); var u8 uint8 = 1; p := P{[]int32(\"y\")}; println(string(r), string(r[0]), string(s[0]), string(u8), string(p.r), len([]rune(\"\"))) }",
	}
	// Defer and panic (D-043).
	files["defer forms"] = "type T struct{ x int }\nfunc (t T) m(a int) { println(t.x, a) }\ntype I interface{ m(int) }\nfunc f() (int, string) { return 1, \"a\" }\nfunc g(int, string) {}\nfunc main() { var i I = T{}; v := func() {}; defer g(f()); defer T{}.m(1); defer i.m(2); defer v(); defer func() { println() }(); defer print(f()); defer println(\"x\", 1); c := make(chan int); defer close(c); m := map[string]int{}; defer delete(m, \"k\"); defer h(1, 2); defer h(); go h(3); defer h([]int{4}...) }\nfunc h(xs ...int) {}"
	files["recover"] = "func main() { defer func() { r := recover(); if e, ok := r.(error); ok { println(e.Error()) }; recover() }(); println(recover() == nil) }"
	files["panic values"] = "type E struct{}\nfunc main() { var e any = E{}; if e == nil { panic(nil) }; if e != nil { panic(e) }; panic(\"x\" + \"y\") }"
	for name, src := range files {
		t.Run(name, func(t *testing.T) {
			if got := classifyFile(t, "package main\n\n"+src+"\n"); len(got) != 0 {
				t.Errorf("rejected with %v", got)
			}
		})
	}
}

// TestRejected lists valid Go programs outside S1 with the features the gate
// must report. Every one is valid Go: classify fails the test otherwise.
func TestRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		// Complex numbers are values without arithmetic (D-049, 11.8).
		{"complex arithmetic", `var c complex128; c = c + 1; println(c == 0)`, []string{"complex-arithmetic"}},
		{"complex negation", `var c complex64; println(-c == 0)`, []string{"complex-arithmetic"}},
		{"complex compound assignment", `var c complex64; c *= 2; println(c == 0)`, []string{"complex-arithmetic"}},
		{"complex increment", `var c complex64; c++; println(c == 0)`, []string{"complex-arithmetic"}},
		{"rune slice to byte slice", `r := []rune{65}; println(len([]byte(string(r))), len([4]rune(r)))`, []string{"conversion"}},
		{"slice to array", `s := make([]int, 2); a := [2]int(s); println(a[0])`, []string{"conversion"}},
		{"slice to array pointer", `s := make([]int, 2); p := (*[2]int)(s); println(p[0])`, []string{"conversion"}},
		{"println of a slice", `s := []int{1}; println(s)`, []string{"println-argument-type"}},
		{"range over a slice", `for i := range []int{1} { println(i) }`, nil},
		{"slice of a for variable", `for a := [2]int{}; a[0] < 2; a[0]++ { println(len(a[:])) }`, []string{"loop-variable-address"}},
		{"slice type declaration", `type S []int; var s S; println(len(s))`, nil},
		{"slice of large elements", `var s [][1 << 22]int; println(len(s))`, []string{"non-int-type"}},
		{"range over string", `for i := range "ab" { println(i) }`, nil},
		{"println complex", `println(1i)`, []string{"println-argument-type"}},
		{"builtin constant", `x := min(1, 2); println(x)`, nil},
		{"min of floats", `x, y := 1.5, 2.5; println(min(x, y))`, []string{"builtin"}},
		{"math function", `x := 1.5; println(min(x, 2))`, []string{"builtin"}},
		{"function literal", `f := func() {}; f()`, nil},
		{"closures", `x := 1; f := func(k int) int { x += k; return x }; g := func() func() int { y := x; return func() int { y++; return y + x } }(); println(f(2), g(), g(), x)`, nil},
		{"closure of a loop variable", `for i := 0; i < 2; i++ { f := func() int { return i }; println(f()) }`, []string{"loop-variable-address"}},
		{"closure assigning a loop variable", `for i := 0; i < 2; i++ { func() { i++ }() }`, []string{"loop-variable-address"}},
		{"loop variable of a closure", `f := func() { for i := 0; i < 2; i++ { g := func() int { return 1 }; println(i, g()) } }; f()`, nil},
		{"closure with named results", `f := func() (r int) { defer func() { r++ }(); return 1 }; println(f())`, nil},
		{"iota", `const c = iota; println(c)`, nil},
		{"blank assignment", `x := 1; _ = x`, nil},
		{"blank var", `var _ = 1`, nil},
		{"blank typed var", `var _ int`, nil},
		{"blank const", `const _ = 1`, nil},
		{"blank in short declaration", `a, _ := 1, 2; println(a)`, nil},
		{"implicit const value", "const (\n a = 1\n b\n)\nprintln(a, b)", nil},
		{"local type", `type T int`, nil},
		{"parenthesized target", `x := 1; (x) = 2; println(x)`, []string{"assignment-target"}},
		{"range", `for range 3 {}`, nil},
		{"fallthrough", `switch { case true: fallthrough; default: }`, nil},
		{"labeled switch", "L:\nswitch {\ndefault:\nbreak L\n}", nil},
		{"switch with a rejected case body", `switch { case true: defer recover() }`, []string{"defer"}},
		{"select", `L: for { select { default: break L } }`, nil},
		{"go", `go panic(1)`, []string{"go"}},
		{"defer of recover", `defer recover()`, []string{"defer"}},
		{"defer of panic", `defer panic(1)`, []string{"defer"}},
		{"defer of copy", `s := []int{}; defer copy(s, s)`, []string{"defer"}},
		{"panic of an unsupported value", `var c struct{ error }; panic(c)`, []string{"non-int-type"}},
		{"defer of an unsupported call", `var c struct{ error }; defer println(c)`, []string{"non-int-type", "println-argument-type"}},
		{"goto and label", "goto L\nL:", nil},
		{"labeled break", "L:\nfor {\nbreak L\n}", nil},
		{"labeled continue", "L:\nfor i := 0; i < 1; i++ {\ncontinue L\n}", nil},
		{"address of a for variable", `for i := 0; i < 2; i++ { p := &i; println(*p) }`, []string{"loop-variable-address"}},
		{"address of a for variable's element", `for a := [2]int{}; a[0] < 2; a[0]++ { p := &a[1]; println(*p) }`, []string{"loop-variable-address"}},
		{"println of a pointer", `x := 1; println(&x)`, []string{"println-argument-type"}},
		{"new of a value", `p := new(1); println(*p)`, []string{"builtin"}},
		{"pointer to a large array", `var p *[1 << 22]int; println(p == nil)`, []string{"non-int-type"}},
		{"pointer type declaration", `type P *int; var p P; println(p == nil)`, nil},
		// Maps (increment 8.5, D-042).
		{"maps", `m := make(map[string][]int, 3); n := map[[2]int]any{{1, 2}: 3}; m["a"] = append(m["a"], 1); m["b"] = nil; v, ok := n[[2]int{1, 2}]; var w any; w, ok = n[[2]int{}]; m["a"][0]++; delete(m, "b"); clear(n); println(len(m), len(n), v == w, ok, m == nil, n[[2]int{}] != nil)`, nil},
		{"map literals", `type P struct{ x int }; m := map[P]*P{{1}: {2}}; var e map[string]map[int]bool = map[string]map[int]bool{"a": {1: true}}; var a = []map[int]int{{1: 2}}; println(m[P{1}].x, e["a"][1], a[0][1])`, nil},
		{"println of a map", `m := map[int]int{}; println(m)`, []string{"println-argument-type"}},
		{"range over a map", `m := map[[2]int]string{}; var k [2]int; var v string; for k, v := range m { m[k] = v + "x" }; for _, v := range m { println(v) }; for k, v = range m { }; for k = range m { k[0]++ }; for range m { break }; println(k[0], v)`, nil},
		{"range over a map, blank elsewhere", `for k := range map[int]int{} { _ = k }`, nil},
		{"range over a map of an unsupported type", `var m map[int]struct{ error }; for k := range m { println(k) }`, []string{"non-int-type"}},
		{"map type declaration", `type M map[int]int; var m M; println(len(m))`, nil},
		{"map of an unsupported type", `var m map[int]struct{ error }; println(len(m))`, []string{"non-int-type"}},
		{"map of large elements", `var m map[int][1 << 22]int; println(len(m))`, []string{"non-int-type"}},
		{"clear of a slice", `s := []int{1}; clear(s); type T []string; clear(T(nil)[:0])`, nil}, // 11.7
		{"array too large", `var a [1 << 22]int; a[0] = 1`, []string{"non-int-type"}},
		{"println of an array", `var a [2]int; println(a)`, []string{"println-argument-type"}},
		{"println of a struct", `print(struct{ x int }{})`, []string{"println-argument-type"}},
		{"switch on a struct", `var s struct{ x int }; switch s { case s: }`, nil},
		{"switch on an array with interface cases", `var a [2]int; var i any; switch a { case [2]int{}, i: }`, nil},
		{"range over an array", `var a [2]int; for i := range a { println(i) }`, nil},
		{"range over a func", `for range func(func() bool) {} {}`, []string{"for-range"}},
		{"fallthrough", "switch {\ncase true:\nfallthrough\ndefault:\n}", nil},
		{"local type alias", `type A = [2]int; var a A; a[0] = 1`, []string{"local-type-declaration"}},
		{"parenthesized field target", `var s struct{ x int }; (s).x = 1`, []string{"assignment-target"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(t, tt.body)
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("features %v, want %v", got, want)
			}
		})
	}
}

func TestRejectedFileLevel(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"package name", "package lib\n\nfunc main() {}\n", []string{"package-name"}},
		{"cgo", "package main\n\nimport \"C\"\n\nfunc main() {}\n", []string{"cgo"}},
		{"go directive", "package main\n\n//go:nosplit\nfunc main() {}\n", []string{"directive"}},
		{"unknown go directive", "package main\n\n//go:foobar\nfunc main() {}\n", []string{"directive"}},
		{"function directives", "package main\n\n//go:noinline\n//go:norace\n\n// f is a function.\n//go:nocheckptr\n//go:registerparams\nfunc f() {}\n\nfunc main() { f() }\n", nil},
		{"build constraint", "//go:build windows\n// +build windows\n\npackage main\n\nfunc main() {}\n", nil},
		{"build constraint after package", "package main\n\n//go:build linux\nfunc main() {}\n", []string{"directive"}},
		{"misplaced noinline before var", "package main\n\n//go:noinline\nvar x = 1\n\nfunc main() { println(x) }\n", []string{"directive"}},
		{"misplaced noinline in body", "package main\n\nfunc main() {\n//go:noinline\n\tprintln()\n}\n", []string{"directive"}},
		{"misplaced noinline after comment", "package main\n\n/* c */ //go:noinline\nfunc main() {}\n", []string{"directive"}},
		{"noinline at end of file", "package main\n\nfunc main() {}\n\n//go:noinline\n", []string{"directive"}},
		{"noinline before function without body", "package main\n\n//go:noinline\nfunc f()\n\nfunc main() {}\n", []string{"directive"}},
		{"line directive", "package main\n\n//line x.go:1\nfunc main() {}\n", []string{"directive"}},
		{"empty result list", "package main\n\nfunc main() () {}\n", []string{"empty-result-list"}},
		{"empty result list on init", "package main\n\nfunc init() () {}\n\nfunc main() {}\n", []string{"empty-result-list"}},
		{"package var", "package main\n\nvar g int\n\nfunc main() { g = 1 }\n", nil},
		{"package const", "package main\n\nconst k = 1\n\nfunc main() { println(k) }\n", nil},
		{"package vars from a call", "package main\n\nvar a, b = f()\n\nfunc f() (int, bool) { return 1, true }\n\nfunc main() { println(a, b) }\n", nil},
		{"package type", "package main\n\ntype T int\n\nfunc main() {}\n", nil},
		{"use of a rejected type", "package main\n\ntype T float64\n\nfunc f(t T) {}\n\nfunc main() { var x [2]T; f(x[0]) }\n", nil},
		{"generic struct", "package main\n\ntype T[E any] struct{ e E }\n\nfunc main() { var t T[int]; t.e = 1 }\n", []string{"non-int-type", "package-level-declaration"}},
		{"package array alias", "package main\n\ntype A = [2]int\n\nfunc main() { var a A; a[0] = 1 }\n", []string{"package-level-declaration"}},
		{"type named _", "package main\n\ntype _ struct{}\n\nfunc main() {}\n", []string{"blank-identifier"}},
		{"println of aggregate results", "package main\n\nfunc f() ([1]int, int) { return [1]int{}, 1 }\n\nfunc main() { println(f()) }\n", []string{"println-argument-type"}},
		{"package var of an unsupported type", "package main\n\nvar c struct{ error }\n\nfunc main() {}\n", []string{"non-int-type"}},
		{"package blank var", "package main\n\nvar _ = 1\n\nfunc main() {}\n", nil},
		{"package const without value", "package main\n\nconst (\n\ta = 1\n\tb\n)\n\nfunc main() { println(a, b) }\n", nil},
		{"named result", "package main\n\nfunc f() (r int) { return }\n\nfunc main() { println(f()) }\n", nil},
		{"named aggregate results", "package main\n\ntype P struct{ a [2]int }\n\nfunc f() (p P, _ [3]string, q *P) { q = &p; defer func() { p.a[0]++ }(); return }\n\nfunc main() { p, s, q := f(); println(p.a[0], q.a[0], s[0]) }\n", nil},
		{"named result of an unsupported type", "package main\n\nfunc f() (c struct{ error }) { return }\n\nfunc main() { f() }\n", []string{"non-int-type"}},
		{"named results", "package main\n\nfunc f() (a, b int) { return 1, 2 }\n\nfunc main() { x, y := f(); println(x, y) }\n", nil},
		{"blank result target", "package main\n\nfunc f() (int, int) { return 1, 2 }\n\nfunc main() { x, _ := f(); println(x) }\n", nil},
		{"generic function", "package main\n\nfunc f[T any]() {}\n\nfunc main() { f[int]() }\n", []string{"function-declaration", "index-expression"}},
		{"parameter of an unsupported type", "package main\n\nfunc f(b struct{ error }) {}\n\nfunc main() { f(struct{ error }{}) }\n", []string{"composite-literal", "non-int-type"}},
		{"variadic function", "package main\n\nfunc f(xs ...int) {}\n\nfunc main() { f(1, 2); f(); f([]int{1}...) }\n", nil},
		{"variadic method", "package main\n\ntype T int\n\nfunc (T) f(xs ...int) {}\n\nfunc main() { T(0).f(1) }\n", []string{"non-int-type"}},
		{"variadic defer of results", "package main\n\nfunc f(xs ...int) {}\n\nfunc g() (int, int) { return 1, 2 }\n\nfunc main() { defer f(g()) }\n", []string{"variadic-call"}},
		{"variadic go of results", "package main\n\nfunc f(xs ...int) {}\n\nfunc g() (int, int) { return 1, 2 }\n\nfunc main() { go f(g()) }\n", []string{"variadic-call"}},
		{"defer of copy", "package main\n\nfunc main() { s := []int{1}; defer copy(s, s) }\n", []string{"defer"}},
		{"go of close", "package main\n\nfunc main() { c := make(chan int); go close(c) }\n", []string{"go"}},
		{"variadic func value", "package main\n\nfunc f(xs ...int) {}\n\nfunc main() { g := f; g(1) }\n", []string{"non-int-type", "variadic-call"}},
		{"variadic with results", "package main\n\nfunc f(xs ...int) {}\n\nfunc two() (int, int) { return 1, 2 }\n\nfunc main() { f(two()) }\n", []string{"variadic-call"}},
		{"function without body", "package main\n\nfunc f()\n\nfunc main() { f() }\n", []string{"function-without-body"}},
		{"function named _", "package main\n\nfunc _() {}\n\nfunc main() {}\n", []string{"blank-identifier"}},
		{"function value", "package main\n\nfunc f() {}\n\nfunc main() { g := f; g() }\n", nil},
		{"func values", "package main\n\ntype T struct{ f func(int) [2]int }\n\nfunc f(x int) [2]int { return [2]int{x, x} }\n\nfunc g() func(int) [2]int { return f }\n\nvar h = f\n\nfunc main() { var t T; t.f = g(); fs := []func(int) [2]int{f, h, nil}; println(t.f(1)[0], fs[0](2)[1], g()(3)[0], fs[2] == nil, nil != h); fs[1] = (func(int) [2]int)(nil) }\n", nil},
		{"variadic func type", "package main\n\nfunc main() { var f func(...int); _ = f }\n", []string{"non-int-type"}},
		{"func type of an unsupported type", "package main\n\nfunc main() { var f func(struct{ error }); f(struct{ error }{}) }\n", []string{"non-int-type"}},
		{"println of a func value", "package main\n\nfunc f() {}\n\nfunc main() { println(f) }\n", []string{"println-argument-type"}},
		{"method value of a loop variable", "package main\n\ntype T struct{ x int }\n\nfunc (t *T) m() {}\n\nfunc main() { for t := (T{}); t.x < 1; t.x++ { f := t.m; f() } }\n", []string{"loop-variable-address"}},
		{"local of another function", "package main\n\nfunc f() int { const k = 1; return k }\n\nfunc main() { println(f()) }\n", nil},
		{"init", "package main\n\nfunc init() {}\n\nfunc main() {}\n", nil},
		{"method of a rejected type", "package main\n\ntype T int\n\nfunc (T) m() {}\n\nfunc main() {}\n", nil},
		{"methods", "package main\n\ntype T struct{ x int }\n\nfunc (t T) v(k int) int { return t.x + k }\n\nfunc (t *T) p() { t.x++ }\n\nfunc (T) u() {}\n\nfunc (_ *T) b() {}\n\nfunc main() { var t T; t.p(); (&t).p(); println(t.v(1), (&t).v(2)); t.u(); T{}.u() }\n", nil},
		{"promoted methods", "package main\n\ntype T struct{ x int }\n\nfunc (t *T) p() int { return t.x }\n\ntype U struct {\n\tT\n\tq *T\n}\n\ntype V struct{ *T }\n\nfunc main() { var u U; var v V; println(u.p(), v.p(), u.q.p()) }\n", nil},
		{"interfaces", "package main\n\ntype I interface{ M(int) [2]int }\n\ntype J interface {\n\tI\n\tN() string\n}\n\ntype T struct{}\n\nfunc (T) M(k int) [2]int { return [2]int{k} }\n\nfunc (*T) N() string { return \"\" }\n\nfunc main() { var i I = T{}; var j J = &T{}; var a any = i; var e error; i = j; v, ok := a.(J); w := a.(I); switch x := a.(type) { case nil, int: println(x == nil); case I: println(x.M(1)[0]); default: } ; println(i.M(2)[1], v == nil, ok, w != nil, e == nil, a == any(1), I.M(i, 3)[0]); f := i.M; println(f(4)[0]) }\n", nil},
		{"constraint interface", "package main\n\ntype C interface{ ~int }\n\nfunc main() {}\n", []string{"non-int-type"}},
		{"interface embedded in a struct", "package main\n\ntype I interface{ M() }\n\ntype T struct{ I }\n\nfunc main() { var t T; t.M() }\n", []string{"non-int-type"}},
		{"switch on an interface", "package main\n\nfunc main() { var a any = 1; switch a { case 1, nil: } }\n", nil},
		{"interface case of another interface", "package main\n\nfunc main() { var e error; switch e { case any(1): } }\n", []string{"non-int-type"}},
		{"interface case value", "package main\n\nfunc main() { var a any = 1; switch 1 { case a: }; switch { case a: } }\n", nil},
		{"interface case value not assignable to the tag", "package main\n\nfunc main() { var a any; var e error; switch e { case a: } }\n", []string{"non-int-type"}},
		{"println of an interface", "package main\n\nfunc main() { var a any; println(a) }\n", []string{"println-argument-type"}},
		{"interface of an unsupported method", "package main\n\ntype I interface{ M(struct{ error }) }\n\nfunc main() {}\n", []string{"non-int-type"}},
		{"method of type aether", "package main\n\ntype aether struct{}\n\nfunc (aether) streq() {}\n\nfunc main() {}\n", nil},
		{"method of type aether in parentheses", "package main\n\ntype aether struct{}\n\nfunc (a (*aether)) strcmp() {}\n\nfunc main() {}\n", nil},
		{"method named _", "package main\n\ntype T struct{}\n\nfunc (T) _() {}\n\nfunc main() {}\n", []string{"blank-identifier"}},
		{"method with named results", "package main\n\ntype T struct{}\n\nfunc (T) m() (r int) { return }\n\nfunc main() {}\n", nil},
		{"variadic method call", "package main\n\ntype T struct{}\n\nfunc (T) m(xs ...int) {}\n\nfunc main() { var t T; t.m([]int{1}...) }\n", []string{"non-int-type", "variadic-call"}},
		{"method value", "package main\n\ntype T struct{}\n\nfunc (T) m() {}\n\nfunc main() { var t T; f := t.m; f() }\n", nil},
		{"method expression", "package main\n\ntype T struct{}\n\nfunc (T) m() {}\n\nfunc main() { f := T.m; f(T{}) }\n", nil},
		{"method expression of a struct literal type", "package main\n\ntype T int\n\nfunc (T) m() {}\n\nfunc main() { f := struct{ T }.m; f(struct{ T }{}) }\n", []string{"selector"}},
		{"pointer method expression", "package main\n\ntype T struct{}\n\nfunc (*T) m() {}\n\nfunc main() { f := (*T).m; f(nil) }\n", nil},
		{"method of loop variable", "package main\n\ntype T struct{ x int }\n\nfunc (t *T) p() {}\n\nfunc main() { for t := (T{}); t.x < 1; t.x++ { t.p() } }\n", []string{"loop-variable-address"}},
		{"value method of loop variable", "package main\n\ntype T struct{ x int }\n\nfunc (t T) v() {}\n\nfunc main() { for t := (T{}); t.x < 1; t.x++ { t.v() } }\n", nil},
		{"method argument", "package main\n\ntype T struct{}\n\nfunc (T) m(x int) {}\n\nfunc main() { var t T; t.m(func() int { return 1 }()) }\n", nil},
		{"main without body", "package main\n\nfunc main()\n", []string{"function-without-body"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyFile(t, tt.src); !slices.Equal(got, tt.want) {
				t.Errorf("features %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFiles(t *testing.T) {
	if ds := Files([]string{"a.go"}); len(ds) != 0 {
		t.Errorf("one file rejected: %v", ds)
	}
	if ds := Files([]string{"d/a.go", "d/b.go", "d/c.go"}); len(ds) != 0 {
		t.Errorf("files of one directory rejected: %v", ds)
	}
	ds := Files([]string{"a.go", "d/b.go", "c.go"})
	if len(ds) != 1 || ds[0].Feature != FeatMultipleFiles || ds[0].Pos.File != "d/b.go" || !strings.Contains(ds[0].Message, "more than one directory") {
		t.Errorf("Files = %v", ds)
	}
}

func TestDirective(t *testing.T) {
	tests := map[string]bool{
		"//go:noinline":         true,
		"//go:build linux":      true,
		"//go:linkname a b":     true,
		"// +build linux":       true,
		"//\t+build linux":      true,
		"//line x.go:1":         true,
		"/*line x.go:1:1*/":     true,
		"// go:noinline":        false,
		"// aether:expect run":  false,
		"// line comment":       false,
		"//lineup":              false,
		"/* go:noinline */":     false,
		"// a +build in a text": false,
	}
	for text, want := range tests {
		if _, got := directive(text); got != want {
			t.Errorf("directive(%q) = %v, want %v", text, got, want)
		}
	}
}

// TestMissingTypeInformationIsInternal checks that a broken invariant is
// reported as an internal error, not as a problem with the program.
func TestMissingTypeInformationIsInternal(t *testing.T) {
	fset := token.NewFileSet()
	f, ds := frontend.Parse(fset, "p.go", []byte("package main\n\nfunc main() {\n\tx := 1\n\tprintln(x)\n}\n"))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	pkg, ds := frontend.TypeCheck(fset, []*ast.File{f})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	pkg.Info.Types = map[ast.Expr]types.TypeAndValue{}
	ds = Check(pkg)
	if len(ds) == 0 {
		t.Fatal("no diagnostics")
	}
	for _, d := range ds {
		if d.Class != diag.Internal {
			t.Errorf("got %s, want only internal diagnostics", d)
		}
	}
}

func TestPrecheckStd(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{"package p\n\nimport _ \"unsafe\"\n\n//go:linkname x runtime.x\nvar x error\n", []string{"directive"}},
		{"package p\n\nimport _ \"unsafe\"\n\n//go:linkname x\nvar x error\n", nil},
		{"package p\n\nimport _ \"unsafe\"\n\n//go:linkname x p.x\nvar x error\n", nil},
		{"package p\n\nimport _ \"unsafe\"\n\n//go:linkname f runtime.f\nfunc f()\n", nil},
		{"package p\n\n//go:nosplit\nfunc f() {}\n\n//go:noescape\nfunc g(p *int)\n", nil},
	}
	for _, tt := range tests {
		fset := token.NewFileSet()
		f, ds := frontend.Parse(fset, "p.go", []byte(tt.src))
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if got := features(t, Precheck(fset, f, false, true)); !slices.Equal(got, tt.want) {
			t.Errorf("%q: features %v, want %v", tt.src, got, tt.want)
		}
	}
}
