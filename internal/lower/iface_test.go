package lower

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestGcName checks the names of types that panics write against gc's
// (go1.26.1, D-041): reflect's String of each, which gc's run time uses.
func TestGcName(t *testing.T) {
	const src = `package main
type T struct{ a int }
type I interface{ M() int }
var (
	v1 int
	v2 byte
	v3 rune
	v4 *T
	v5 []string
	v6 [3]*int
	v7 struct{ a int; b []string }
	v8 struct{}
	v9 func(int, string) (bool, error)
	v10 func(...int)
	v11 func()
	v12 interface{}
	v13 any
	v14 interface{ M(int) int; N() }
	v15 I
	v16 error
	v17 map[string][]*T
	v18 func(int) int
	v19 uintptr
	v20 struct{ T; x float32 }
)
`
	want := []string{"int", "uint8", "int32", "*main.T", "[]string", "[3]*int", "struct { a int; b []string }", "struct {}",
		"func(int, string) (bool, error)", "func(...int)", "func()", "interface {}", "interface {}",
		"interface { M(int) int; N() }", "main.I", "error", "map[string][]*main.T", "func(int) int", "uintptr", "struct { main.T; x float32 }"}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{}).Check("main", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range want {
		v := pkg.Scope().Lookup("v" + itoa(i+1))
		if got := gcName(v.Type()); got != w {
			t.Errorf("v%d: gcName = %q, want %q", i+1, got, w)
		}
	}
}
