package mono

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
)

// instantiate type-checks src and instantiates it.
func instantiate(t *testing.T, src string) (*frontend.Package, *frontend.Package, []diag.Diagnostic) {
	t.Helper()
	fset := token.NewFileSet()
	f, ds := frontend.Parse(fset, "p.go", []byte(src))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	pkg, ds := frontend.TypeCheck(fset, []*ast.File{f})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	out, _, ds := Instantiate(pkg, nil, false)
	return pkg, out, ds
}

// TestInstantiate checks the copies of generic code (D-045): which
// declarations the program has afterwards, and the rejections.
func TestInstantiate(t *testing.T) {
	// Without generic declarations, the package itself.
	in, out, ds := instantiate(t, "package main\n\ntype I interface{ M() }\n\nfunc main() {}\n")
	if len(ds) > 0 || out != in {
		t.Errorf("program without generics: %v, same package %v", ds, out == in)
	}
	// An interface that can only be a constraint is left out.
	in, out, ds = instantiate(t, "package main\n\ntype I interface{ M() }\n\ntype C interface{ ~int }\n\nfunc main() {}\n")
	if len(ds) > 0 || out == in || len(out.Files[0].Decls) != 2 || out.Types.Scope().Lookup("C") != nil {
		t.Errorf("constraint without generics: %v, %d declarations", ds, len(out.Files[0].Decls))
	}

	const src = `package main

type Num interface{ ~int | ~float64 }

type Box[T any] struct{ v T }

func (b *Box[T]) Set(v T) { b.v = v }

type Getter[T any] interface{ Get() T }

type Pair[A, B any] struct {
	a A
	b B
}

func Twice[T Num](x T) T { return x + x }

func Conv[T Num](x T) T { return x * T(3) }

func Wrap[T any](v T) Box[T] { return Box[T]{v} }

func main() {
	var b Box[int]
	b.Set(Twice(2))
	println(Twice[float64](1.5), Conv(2), Wrap(Pair[string, *Box[int]]{}).v.a)
}
`
	_, out, ds = instantiate(t, src)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	var names []string
	for _, d := range out.Files[0].Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil {
				name = types.ExprString(d.Recv.List[0].Type) + "." + name
			}
			names = append(names, name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				names = append(names, "type "+s.(*ast.TypeSpec).Name.Name)
			}
		}
	}
	got := strings.Join(names, ", ")
	want := "type Box[int], type Box[main.Pair[string,*main.Box[int]]], *Box[int].Set, *Box[main.Pair[string,*main.Box[int]]].Set, " +
		"type Pair[string,*main.Box[int]], Twice[int], Twice[float64], Conv[int], Wrap[main.Pair[string,*main.Box[int]]], main"
	if got != want {
		t.Errorf("declarations:\n%s\nwant\n%s", got, want)
	}
	// Conv[int]'s T(3) is not a constant.
	for _, d := range out.Files[0].Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "Conv[int]" {
			ret := fd.Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.BinaryExpr)
			ix, ok := ret.Y.(*ast.IndexExpr)
			if !ok {
				t.Fatalf("Conv[int]: T(3) is %s", types.ExprString(ret.Y))
			}
			lit := ix.X.(*ast.CompositeLit)
			if tv := out.Info.Types[ret.Y]; tv.Value != nil || types.ExprString(lit.Type) != "[1]int" || types.ExprString(lit.Elts[0]) != "int(3)" {
				t.Errorf("Conv[int]: T(3) is %s{%s}, constant %v", types.ExprString(lit.Type), types.ExprString(lit.Elts[0]), tv.Value)
			}
		}
	}

	for _, c := range []struct{ name, src, want string }{
		{"uninstantiated function", "package main\n\nfunc F[T any]() {}\n\nfunc main() {}\n", "p.go:3:6 generic-uninstantiated"},
		{"uninstantiated type", "package main\n\ntype B[T any] struct{ v T }\n\nfunc (b B[T]) M() {}\n\nfunc main() {}\n", "p.go:3:6 generic-uninstantiated"},
		{"only from uninstantiated code", "package main\n\nfunc F[T any]() {}\n\nfunc G[T any]() { F[T]() }\n\nfunc main() {}\n", "p.go:3:6 generic-uninstantiated, p.go:5:6 generic-uninstantiated"},
		{"generic alias", "package main\n\ntype A[T any] = []T\n\nfunc main() { var a A[int]; println(len(a)) }\n", "p.go:3:6 package-level-declaration"},
		{"channel argument", "package main\n\nfunc F[T any](x T) {}\n\nfunc main() { F[chan int](nil) }\n", "p.go:3:17 generic-instance"},
		{"tagged struct argument", "package main\n\nfunc F[T any](x T) {}\n\nfunc main() { F(struct{ a int `t` }{}) }\n", "p.go:3:17 generic-instance"},
	} {
		_, _, ds := instantiate(t, c.src)
		var got []string
		for _, d := range ds {
			got = append(got, fmt.Sprintf("%s:%d:%d %s", d.Pos.File, d.Pos.Line, d.Pos.Col, d.Feature))
		}
		if g := strings.Join(got, ", "); g != c.want {
			t.Errorf("%s: %s, want %s", c.name, g, c.want)
		}
	}
	// No generic interface is reported, nor a constraint.
	if _, _, ds := instantiate(t, "package main\n\ntype G[T any] interface{ M() T }\n\ntype C interface{ ~int; M() int }\n\nfunc main() {}\n"); len(ds) > 0 {
		t.Errorf("uninstantiated interfaces: %v", ds)
	}
}

// TestMaxInstances checks the bound on a program's instances.
func TestMaxInstances(t *testing.T) {
	prog := func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\nfunc F[T any]() {}\n\nfunc main() {\n")
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "\tF[[%d]int]()\n", i)
		}
		b.WriteString("}\n")
		return b.String()
	}
	if _, _, ds := instantiate(t, prog(maxInstances)); len(ds) > 0 {
		t.Errorf("%d instances: %v", maxInstances, ds[0])
	}
	_, _, ds := instantiate(t, prog(maxInstances+1))
	if len(ds) != 1 || ds[0].Feature != "generic-instance" || ds[0].Pos.Line != 6+maxInstances {
		t.Errorf("%d instances: %v", maxInstances+1, ds)
	}
}

// TestTypeName checks the names of instances against those gc's run time
// wrote on 2026-11-20 (D-045).
func TestTypeName(t *testing.T) {
	const src = `package main

type P struct{ x int }
type I interface{ M() }
type Box[T any] struct{ v T }
type Pair[K comparable, V any] struct {
	k K
	v V
}

var (
	v1 Box[int]
	v2 Box[P]
	v3 Pair[string, *P]
	v4 Box[Box[int]]
	v5 Box[[]int]
	v6 Box[struct{ a int }]
	v7 Box[map[string]int]
	v8 Box[func(int) bool]
	v9 Box[any]
	v10 Box[I]
	v11 Box[[2]P]
	v12 Box[error]
	v13 Box[byte]
	v14 Box[struct{ A int; b string; P }]
	v15 Box[interface{ M(); n() int }]
	v16 Box[func(...int) (int, error)]
	v17 Box[*Pair[int, Box[[]*P]]]
	v18 Box[struct{}]
	v19 Box[rune]
)
`
	want := []string{"main.Box[int]", "main.Box[main.P]", "main.Pair[string,*main.P]", "main.Box[main.Box[int]]",
		"main.Box[[]int]", "main.Box[struct { main.a int }]", "main.Box[map[string]int]", "main.Box[func(int) bool]",
		"main.Box[interface {}]", "main.Box[main.I]", "main.Box[[2]main.P]", "main.Box[error]", "main.Box[uint8]",
		"main.Box[struct { A int; main.b string; main.P }]", "main.Box[interface { M(); main.n() int }]",
		"main.Box[func(...int) (int, error)]", "main.Box[*main.Pair[int,main.Box[[]*main.P]]]", "main.Box[struct {}]", "main.Box[int32]"}
	fset := token.NewFileSet()
	f, ds := frontend.Parse(fset, "p.go", []byte(src))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{}).Check("main", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range want {
		v := pkg.Scope().Lookup(fmt.Sprintf("v%d", i+1))
		if got := typeName(v.Type(), false); got != w {
			t.Errorf("v%d: %s, want %s", i+1, got, w)
		}
	}
}
