package rangefunc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
)

func reject(pos diag.Pos, feature, what string) diag.Diagnostic {
	return diag.Diagnostic{Class: diag.Unsupported, Pos: pos, Feature: feature, Message: what}
}

func check(t *testing.T, src string) *frontend.Package {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pkg, ds := frontend.CheckPackage(fset, "main", []*ast.File{f}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return pkg
}

// TestRewrite checks that every range over a function is rewritten, the
// nested ones too, and that each node the rewrite makes is recorded as
// go/types would record it: every expression has a type, and every
// identifier other than a field's or a label's has an object.
func TestRewrite(t *testing.T) {
	pkg := check(t, `package main

func seq(yield func(int, string) bool) {}

func f() (int, error) {
	var k int
	var v string
outer:
	for i, s := range seq {
		for j := range func(yield func(int) bool) {} {
			if j > i {
				continue outer
			}
			if s == "" {
				return j, nil
			}
		}
		for k, v = range seq {
			break outer
		}
	}
	_, _ = k, v
	return 0, nil
}

func main() { f() }
`)
	if ds := Rewrite(pkg, reject); len(ds) > 0 {
		t.Fatal(ds)
	}
	ast.Inspect(pkg.Files[0], func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.RangeStmt:
			t.Errorf("range statement left at %v", pkg.Fset.Position(n.Pos()))
		case *ast.Ident:
			if pkg.Info.ObjectOf(n) == nil && n.Name != "_" && n.Name != "main" && n.Name != "outer" {
				// Field names and the package name have no object.
				if _, ok := pkg.Info.Types[n]; !ok {
					t.Errorf("identifier %s at %v has no object or type", n.Name, pkg.Fset.Position(n.Pos()))
				}
			}
		case *ast.BinaryExpr, *ast.CallExpr, *ast.FuncLit, *ast.BasicLit:
			if _, ok := pkg.Info.Types[n.(ast.Expr)]; !ok {
				t.Errorf("%T at %v has no type", n, pkg.Fset.Position(n.Pos()))
			}
		}
		return true
	})
	// The new variables are locals, declared before their loop: a
	// literal captures them.
	for id, obj := range pkg.Info.Defs {
		if v, ok := obj.(*types.Var); ok && strings.HasPrefix(id.Name, "#") && v.Parent() == nil {
			t.Errorf("variable %s has no scope", id.Name)
		}
	}
}

// TestUnsupported checks that a body the rewrite cannot handle is left as
// it is and reported.
func TestUnsupported(t *testing.T) {
	pkg := check(t, `package main

func seq(yield func(int) bool) {}

func main() {
	for i := range seq {
		defer println(i)
	}
}
`)
	ds := Rewrite(pkg, reject)
	if len(ds) != 1 || ds[0].Feature != Feature || !strings.Contains(ds[0].Message, "defer") || ds[0].Pos.Line != 7 {
		t.Fatalf("diagnostics %v, want one for-range at line 7 about defer", ds)
	}
	found := false
	ast.Inspect(pkg.Files[0], func(n ast.Node) bool {
		_, ok := n.(*ast.RangeStmt)
		found = found || ok
		return true
	})
	if !found {
		t.Error("the loop was rewritten anyway")
	}
}

// TestNoRangeOverFunc checks that a package without such a loop is left
// alone, without the proto check.
func TestNoRangeOverFunc(t *testing.T) {
	pkg := check(t, "package main\n\nfunc main() {\n\tfor i := range 3 {\n\t\tprintln(i)\n\t}\n}\n")
	if ds := Rewrite(pkg, reject); len(ds) > 0 {
		t.Fatal(ds)
	}
}
