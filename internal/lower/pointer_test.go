package lower

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestHasPointers checks which types lowering allocates noscan (D-047):
// any that may hold a pointer word must not be.
func TestHasPointers(t *testing.T) {
	const src = `package main
type P struct{ a int; b [2]float64; c bool }
type Q struct{ P; s string }
type E struct{}
var (
	n1 int
	n2 [4]uint8
	n3 P
	n4 E
	n5 [0]*int
	n6 struct{ x [3]P }
	n7 float32
	y1 string
	y2 *int
	y3 []int
	y4 map[int]int
	y5 func()
	y6 any
	y7 Q
	y8 [2]struct{ p *P }
	y9 error
)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{}).Check("main", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range pkg.Scope().Names() {
		v, ok := pkg.Scope().Lookup(name).(*types.Var)
		if !ok {
			continue
		}
		if want := name[0] == 'y'; hasPointers(v.Type()) != want {
			t.Errorf("%s %s: hasPointers %v, want %v", name, v.Type(), !want, want)
		}
	}
}
