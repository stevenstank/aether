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
	if ds := Precheck(fset, f); len(ds) > 0 {
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
		"empty main":                ``,
		"var without initializer":   `var x int; println(x)`,
		"var with type and value":   `var x int = 1; println(x)`,
		"var with inferred type":    `var x = 1; println(x)`,
		"var list":                  `var a, b int = 1, 2; println(a, b)`,
		"grouped var":               "var (\n a int\n b = 2\n)\nprintln(a, b)",
		"short declaration":         `x := 1; println(x)`,
		"short declaration list":    `a, b := 1, 2; println(a, b)`,
		"redeclaration":             `a, b := 1, 2; a, c := 3, 4; println(a, b, c)`,
		"untyped const":             `const c = 3; println(c)`,
		"typed const":               `const c int = 3; println(c)`,
		"grouped const":             "const (\n a = 1\n b int = a * 2\n)\nprintln(a, b)",
		"const from const":          `const a = 2; const b = a * a; println(b)`,
		"oversized intermediate":    `const big = 9223372036854775807*4 - 3*9223372036854775807; println(big)`,
		"min int literal":           `x := -9223372036854775808; println(x)`,
		"assignment":                `x := 1; x = 2; println(x)`,
		"tuple assignment":          `a, b := 1, 2; a, b = b, a; println(a, b)`,
		"compound assignments":      `x := 1; x += 2; x -= 3; x *= 4; println(x)`,
		"increment and decrement":   `x := 1; x++; x--; println(x)`,
		"binary operators":          `x := 1; y := x + x - x*x; println(y)`,
		"unary operators":           `x := 1; y := -x + +x; println(y)`,
		"parentheses":               `x := 1; y := (x + 1) * (x - (1)); println(y)`,
		"integer literal forms":     `println(0x1F, 0X1f, 0o17, 0O17, 017, 0b101, 0B101, 1_000_000, 0)`,
		"nested blocks":             `x := 1; { x := x + 1; { println(x) } }; println(x)`,
		"empty statements":          `;;`,
		"println no arguments":      `println()`,
		"println constant argument": `println(1 + 2)`,
		"builtin names as vars":     `len := 1; print := 2; int := 3; println(len, print, int)`,
		"return in main":            `println(1); return`,
		"bool variables":            `var a bool; b := true; var c bool = !b; a = b && c || !a; println(a, b, c)`,
		"bool constants":            `const t = true; const f bool = !t; const c = 1 < 2; println(t, f, c, false)`,
		"comparisons":               `x := 1; println(x == 1, x != 2, x < 3, x <= 4, x > 5, x >= 6, true == (x > 0))`,
		"if, else if, else":         `x := 1; if x > 0 { println(1) } else if y := x; y < 0 { println(y) } else { println(2) }`,
		"three-clause for":          `for i := 0; i < 3; i++ { println(i) }`,
		"condition-only for":        `i := 0; for i < 3 { i++ }; println(i)`,
		"infinite for":              `for { break }`,
		"break and continue":        `for i := 0; i < 9; i++ { if i == 2 { continue }; if i > 5 { break }; println(i) }`,
		"for with empty clauses":    `i := 0; for ; i < 3; { i++ }; println(i)`,
		"recursive main":            `main()`,
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
	}
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
		{"division", `x := 7; println(x / 2)`, []string{"operator-div"}},
		{"remainder", `x := 7; println(x % 2)`, []string{"operator-rem"}},
		{"shifts", `x := 7; println(x<<1, x>>1)`, []string{"operator-shl", "operator-shr"}},
		{"bitwise", `x := 7; println(x&1, x|1, x^1, x&^1)`, []string{"operator-and", "operator-and-not", "operator-or", "operator-xor"}},
		{"constant division", `const c = 7 / 2; println(c)`, []string{"operator-div"}},
		{"constant shift", `const c = 1 << 3; println(c)`, []string{"operator-shl"}},
		{"compound division", `x := 8; x /= 2; println(x)`, []string{"operator-div"}},
		{"compound shift", `x := 8; x <<= 1; println(x)`, []string{"operator-shl"}},
		{"complement", `x := 1; println(^x)`, []string{"operator-complement"}},
		{"float literal as int", `var x int = 1.0; println(x)`, []string{"float-literal"}},
		{"hex float literal as int", `var x int = 0x1p4; println(x)`, []string{"float-literal"}},
		// The 3 is an integer literal of type untyped float here, because
		// the constant expression around it is a float expression.
		{"float literal in int constant", `const c int = 2.0 * 3; println(c)`, []string{"float-literal", "non-int-type"}},
		{"untyped float constant", `const c = 2.5 * 2; var y int = c; println(y)`, []string{"non-int-type"}},
		{"rune literal as int", `var x int = 'a'; println(x)`, []string{"rune-literal"}},
		{"imaginary literal as int", `var x int = 0i; println(x)`, []string{"imaginary-literal"}},
		{"float variable", `x := 1.5; println(x)`, []string{"println-argument-type", "float-literal"}},
		{"sized integer", `var x int64 = 1; println(x)`, []string{"non-int-type", "println-argument-type"}},
		{"byte", `var x byte; println(x)`, []string{"non-int-type", "println-argument-type"}},
		{"string", `s := "x"; println(len(s))`, []string{"builtin", "string-literal"}},
		{"println rune", `println('a')`, []string{"println-argument-type"}},
		{"println float", `println(1.5)`, []string{"println-argument-type"}},
		{"println string", `println("hi")`, []string{"println-argument-type"}},
		{"print", `print(1)`, []string{"print"}},
		{"builtin constant", `x := len([3]int{}); println(x)`, []string{"builtin"}},
		{"conversion", `x := int(1); println(x)`, []string{"conversion"}},
		{"function literal", `f := func() {}; f()`, []string{"call", "function-literal"}},
		{"iota", `const c = iota; println(c)`, []string{"predeclared-identifier"}},
		{"blank assignment", `x := 1; _ = x`, []string{"blank-identifier"}},
		{"blank var", `var _ = 1`, []string{"blank-identifier"}},
		{"blank typed var", `var _ int`, []string{"blank-identifier"}},
		{"blank const", `const _ = 1`, []string{"blank-identifier"}},
		{"blank in short declaration", `a, _ := 1, 2; println(a)`, []string{"blank-identifier"}},
		{"implicit const value", "const (\n a = 1\n b\n)\nprintln(a, b)", []string{"const-implicit-value"}},
		{"local type", `type T int`, []string{"local-type-declaration"}},
		{"parenthesized target", `x := 1; (x) = 2; println(x)`, []string{"assignment-target"}},
		{"range", `for range 3 {}`, []string{"for-range"}},
		{"switch", `switch {}`, []string{"switch"}},
		{"select", `select {}`, []string{"select"}},
		{"go", `go println()`, []string{"go"}},
		{"defer", `defer println()`, []string{"defer"}},
		{"goto and label", "goto L\nL:", []string{"goto", "label"}},
		{"labeled break", "L:\nfor {\nbreak L\n}", []string{"label"}},
		{"labeled continue", "L:\nfor i := 0; i < 1; i++ {\ncontinue L\n}", []string{"label"}},
		{"uint comparison", `var x uint = 1; println(x < 2)`, []string{"non-int-type"}},
		{"address and deref", `x := 1; p := &x; println(*p)`, []string{"operator-address", "pointer-dereference"}},
		{"composite literal", `a := [1]int{1}; println(a[0])`, []string{"composite-literal", "index-expression"}},
		{"selector", `var s struct{ f int }; println(s.f)`, []string{"non-int-type", "selector"}},
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
		{"import", "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(0) }\n", []string{"import"}},
		{"cgo", "package main\n\nimport \"C\"\n\nfunc main() {}\n", []string{"cgo"}},
		{"go directive", "package main\n\n//go:noinline\nfunc main() {}\n", []string{"directive"}},
		{"build constraint", "//go:build linux\n\npackage main\n\nfunc main() {}\n", []string{"directive"}},
		{"line directive", "package main\n\n//line x.go:1\nfunc main() {}\n", []string{"directive"}},
		{"empty result list", "package main\n\nfunc main() () {}\n", []string{"empty-result-list"}},
		{"empty result list on init", "package main\n\nfunc init() () {}\n\nfunc main() {}\n", []string{"empty-result-list"}},
		{"package var", "package main\n\nvar g int\n\nfunc main() { g = 1 }\n", []string{"package-level-declaration"}},
		{"package const", "package main\n\nconst k = 1\n\nfunc main() { println(k) }\n", []string{"package-level-declaration"}},
		{"two results", "package main\n\nfunc f() (int, int) { return 1, 2 }\n\nfunc main() { a, b := f(); println(a, b) }\n", []string{"function-declaration", "multi-value-assignment"}},
		{"named result", "package main\n\nfunc f() (r int) { return }\n\nfunc main() { println(f()) }\n", []string{"function-declaration"}},
		{"generic function", "package main\n\nfunc f[T any]() {}\n\nfunc main() { f[int]() }\n", []string{"call", "function-declaration"}},
		{"non-int parameter", "package main\n\nfunc f(b byte) {}\n\nfunc main() { f(1) }\n", []string{"non-int-type"}},
		{"variadic function", "package main\n\nfunc f(xs ...int) {}\n\nfunc main() { f(1, 2) }\n", []string{"non-int-type"}},
		{"function without body", "package main\n\nfunc f()\n\nfunc main() { f() }\n", []string{"function-without-body"}},
		{"function named _", "package main\n\nfunc _() {}\n\nfunc main() {}\n", []string{"blank-identifier"}},
		{"function value", "package main\n\nfunc f() {}\n\nfunc main() { g := f; g() }\n", []string{"call"}},
		{"local of another function", "package main\n\nfunc f() int { const k = 1; return k }\n\nfunc main() { println(f()) }\n", nil},
		{"init", "package main\n\nfunc init() {}\n\nfunc main() {}\n", []string{"init-function"}},
		{"method", "package main\n\ntype T int\n\nfunc (T) m() {}\n\nfunc main() {}\n", []string{"method", "package-level-declaration"}},
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
	ds := Files([]string{"a.go", "b.go", "c.go"})
	if len(ds) != 1 || ds[0].Feature != FeatMultipleFiles || ds[0].Pos.File != "b.go" || !strings.Contains(ds[0].Message, "3 source files") {
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
