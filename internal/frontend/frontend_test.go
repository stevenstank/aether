package frontend

import (
	"go/ast"
	"go/constant"
	"go/token"
	"slices"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

// load parses and type-checks one file, failing the test on syntax errors.
func load(t *testing.T, src string) (*Package, []diag.Diagnostic) {
	t.Helper()
	fset := token.NewFileSet()
	f, ds := Parse(fset, "p.go", []byte(src))
	if len(ds) > 0 {
		t.Fatalf("unexpected syntax errors: %v", ds)
	}
	return TypeCheck(fset, []*ast.File{f})
}

func strs(ds []diag.Diagnostic) []string {
	diag.Sort(ds)
	var out []string
	for _, d := range ds {
		out = append(out, d.String())
	}
	return out
}

func TestParseValid(t *testing.T) {
	f, ds := Parse(token.NewFileSet(), "p.go", []byte("package main\n\nfunc main() {}\n"))
	if len(ds) != 0 || f == nil || f.Name.Name != "main" {
		t.Fatalf("Parse = %v, %v", f, ds)
	}
}

func TestParseKeepsDirectiveComments(t *testing.T) {
	src := "//go:build linux\n\npackage main\n\n//go:noinline\nfunc main() { /*line x.go:1*/ }\n"
	f, ds := Parse(token.NewFileSet(), "p.go", []byte(src))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	var got []string
	for _, g := range f.Comments {
		for _, c := range g.List {
			got = append(got, c.Text)
		}
	}
	want := []string{"//go:build linux", "//go:noinline", "/*line x.go:1*/"}
	if !slices.Equal(got, want) {
		t.Errorf("comments = %q, want %q (ParseComments must be set; E11)", got, want)
	}
}

func TestParseSyntaxErrors(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tx :=\n}\n\nfunc f( {}\n"
	f, ds := Parse(token.NewFileSet(), "p.go", []byte(src))
	if f != nil {
		t.Error("file returned despite syntax errors")
	}
	got := strs(ds)
	want := []string{
		"p.go:5:1: invalid: expected operand, found '}'",
		"p.go:7:12: invalid: expected ';', found 'EOF'",
	}
	if !slices.Equal(got, want) {
		t.Errorf("diagnostics:\n%q\nwant:\n%q", got, want)
	}
}

func TestTypeErrorsArePositionedAndDeterministic(t *testing.T) {
	src := `package main

func main() {
	var a int = "a"
	b := undefined
	println(a, b)
}
`
	var first []string
	for i := 0; i < 5; i++ {
		_, ds := load(t, src)
		got := strs(ds)
		if i == 0 {
			first = got
			continue
		}
		if !slices.Equal(got, first) {
			t.Fatalf("run %d differs: %q vs %q", i, got, first)
		}
	}
	want := []string{
		`p.go:4:14: invalid: cannot use "a" (untyped string constant) as int value in variable declaration`,
		"p.go:5:7: invalid: undefined: undefined",
	}
	if !slices.Equal(first, want) {
		t.Errorf("diagnostics:\n%q\nwant:\n%q", first, want)
	}
}

func TestProgramChecks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"main present", "package main\n\nfunc main() {}\n", nil},
		{"main missing", "package main\n\nfunc f() {}\n",
			[]string{"p.go:1:9: invalid: function main is undeclared in the main package"}},
		{"main is a variable", "package main\n\nvar main = 1\n",
			// Only the type checker's error: the program-level check must
			// not add a misleading "undeclared" diagnostic.
			[]string{"p.go:3:5: invalid: cannot declare main - must be func"}},
		{"library package", "package lib\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ds := load(t, tt.src)
			if got := strs(ds); !slices.Equal(got, tt.want) {
				t.Errorf("diagnostics:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// TestLineDirectiveDoesNotMovePositions checks that diagnostics use real
// positions even when a //line directive is present.
func TestLineDirectiveDoesNotMovePositions(t *testing.T) {
	src := "package main\n\n//line elsewhere.go:100\nfunc main() {\n\tvar x int = \"s\"\n\tprintln(x)\n}\n"
	_, ds := load(t, src)
	want := []string{`p.go:5:14: invalid: cannot use "s" (untyped string constant) as int value in variable declaration`}
	if got := strs(ds); !slices.Equal(got, want) {
		t.Errorf("diagnostics:\n%q\nwant:\n%q", got, want)
	}
}

// TestExactConstants checks that constant values come from go/types and
// go/constant with exact precision (D-002), including intermediate values
// that do not fit in int64 (case S1-05).
func TestExactConstants(t *testing.T) {
	src := `package main

func main() {
	const big = 9223372036854775807*4 - 3*9223372036854775807
	var x int = 1.0
	println(big, x)
}
`
	pkg, ds := load(t, src)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	values := map[string]string{}
	kinds := map[string]constant.Kind{}
	for e, tv := range pkg.Info.Types {
		if tv.Value != nil {
			s := sourceText(pkg, src, e)
			values[s] = tv.Value.ExactString()
			kinds[s] = tv.Value.Kind()
		}
	}
	if got := values["9223372036854775807*4"]; got != "36893488147419103228" {
		t.Errorf("intermediate value = %q, want 36893488147419103228", got)
	}
	if got := values["9223372036854775807*4 - 3*9223372036854775807"]; got != "9223372036854775807" {
		t.Errorf("constant value = %q, want 9223372036854775807", got)
	}
	// A float literal used as int is recorded as an exact Int (E8).
	if got, k := values["1.0"], kinds["1.0"]; got != "1" || k != constant.Int {
		t.Errorf("1.0 as int recorded as %s (kind %v), want 1 (Int)", got, k)
	}
}

// sourceText returns the source text of e, which was parsed from src.
func sourceText(pkg *Package, src string, e ast.Expr) string {
	return src[pkg.Fset.Position(e.Pos()).Offset:pkg.Fset.Position(e.End()).Offset]
}

// TestRedeclarationRecordedAsUse checks the go/types behavior the gate and
// lowering depend on: a redeclared name in := is a use, not a definition
// (E8, case S1-10).
func TestRedeclarationRecordedAsUse(t *testing.T) {
	src := "package main\n\nfunc main() {\n\ta, b := 1, 2\n\ta, c := 3, 4\n\tprintln(a, b, c)\n}\n"
	pkg, ds := load(t, src)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for id, obj := range pkg.Info.Defs {
		if id.Name == "a" && obj != nil && pkg.Fset.Position(id.Pos()).Line == 5 {
			t.Error("redeclared a recorded as a definition")
		}
	}
	found := false
	for id := range pkg.Info.Uses {
		p := pkg.Fset.Position(id.Pos())
		if id.Name == "a" && p.Line == 5 && p.Column == 2 {
			found = true
		}
	}
	if !found {
		t.Error("redeclared a not recorded as a use")
	}
}
