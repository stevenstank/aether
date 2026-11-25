package main

import (
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
	"github.com/stevenstank/aether/internal/mono"
)

// hostedTree is a module in which two packages, u1 and u2, that do not
// import each other, and main, instantiate gen's generic code with shape's
// type: instances shape hosts (D-052). shape has a generic type of the
// same name as gen's, instantiated with the same argument. Package
// initializers print, and u1's calls a hosted method.
var hostedTree = map[string]string{
	"go.mod": "module m.test/x\n\ngo 1.26\n",
	"gen/gen.go": `package gen

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(x T) { s.items = append(s.items, x) }
func (s *Stack[T]) Len() int  { return len(s.items) }

func Count[T comparable](xs []T, x T) (n int) {
	for _, y := range xs {
		if y == x {
			n++
		}
	}
	return n
}

var _ = trace("gen")

func trace(s string) int { println("init", s); return 0 }
`,
	"shape/shape.go": `package shape

type Point struct{ X int }

// Stack is shape's own generic type, named as gen's.
type Stack[T any] struct{ top T }

func (s Stack[T]) Top() T { return s.top }

func Own() Stack[Point] { return Stack[Point]{Point{9}} }

var _ = func() int { println("init shape"); return 0 }()
`,
	"u1/u1.go": `package u1

import (
	"m.test/x/gen"
	"m.test/x/shape"
)

var early = func() int {
	var s gen.Stack[shape.Point]
	s.Push(shape.Point{X: 5})
	println("init u1", s.Len())
	return s.Len()
}()

func A() int {
	var s gen.Stack[shape.Point]
	s.Push(shape.Point{X: 1})
	return early + s.Len() + gen.Count([]shape.Point{{X: 1}, {X: 2}, {X: 1}}, shape.Point{X: 1})
}
`,
	"u2/u2.go": `package u2

import (
	"m.test/x/gen"
	"m.test/x/shape"
)

func B() int {
	var s gen.Stack[shape.Point]
	s.Push(shape.Point{X: 1})
	s.Push(shape.Point{X: 3})
	return s.Len() + gen.Count([]shape.Point{{X: 3}}, shape.Point{X: 3})
}
`,
	"main.go": `package main

import (
	"m.test/x/gen"
	"m.test/x/shape"
	"m.test/x/u1"
	"m.test/x/u2"
)

func main() {
	var s gen.Stack[shape.Point]
	s.Push(shape.Own().Top())
	println(u1.A(), u2.B(), s.Len(), shape.Own().Top().X)
}
`,
}

// hostedVariant returns hostedTree with the files of edits replaced.
func hostedVariant(edits map[string]string) map[string]string {
	t := maps.Clone(hostedTree)
	maps.Copy(t, edits)
	return t
}

// hostedPermuted is hostedTree with every import list reversed.
var hostedPermuted = hostedVariant(map[string]string{
	"main.go": strings.Replace(hostedTree["main.go"], `	"m.test/x/gen"
	"m.test/x/shape"
	"m.test/x/u1"
	"m.test/x/u2"`, `	"m.test/x/u2"
	"m.test/x/u1"
	"m.test/x/shape"
	"m.test/x/gen"`, 1),
	"u1/u1.go": strings.Replace(hostedTree["u1/u1.go"], `	"m.test/x/gen"
	"m.test/x/shape"`, `	"m.test/x/shape"
	"m.test/x/gen"`, 1),
})

// hostedMore is hostedTree with two more packages: aa, first in path
// order among the packages without generic code, which wants the same
// instances, and z, which wants gen's
// instances of its own.
var hostedMore = hostedVariant(map[string]string{
	"aa/aa.go": `package aa

import (
	"m.test/x/gen"
	"m.test/x/shape"
)

func C() int {
	var s gen.Stack[shape.Point]
	s.Push(shape.Point{})
	return s.Len()
}
`,
	"z/z.go": `package z

import "m.test/x/gen"

func Z() int { var s gen.Stack[int]; s.Push(1); return s.Len() + gen.Count([]int{1}, 1) }
`,
	"main.go": strings.Replace(strings.Replace(hostedTree["main.go"], `	"m.test/x/gen"`, `	"m.test/x/aa"
	"m.test/x/gen"`, 1), `	"m.test/x/u2"
)`, `	"m.test/x/u2"
	"m.test/x/z"
)`, 1) + "\nfunc init() { println(aa.C(), z.Z()) }\n",
})

func writeTree(t *testing.T, dir string, tree map[string]string) {
	t.Helper()
	for name, data := range tree {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, name), []byte(data))
	}
}

// hostedBuild compiles tree, written to a new directory, and returns its
// assembly, the IR text of each function by name, and the paths of the
// program's packages in initialization order with, for each, the paths of
// the generic code it hosts.
func hostedBuild(t *testing.T, tree map[string]string) (asm string, funcs map[string]string, order []string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, tree)
	main := filepath.Join(dir, "main.go")
	prog, ds := compile([]source{{main, []byte(tree["main.go"])}})
	if len(ds) > 0 {
		t.Fatalf("not compiled: %v", ds)
	}
	b, err := emitProgram(prog)
	if err != nil {
		t.Fatal(err)
	}
	funcs = map[string]string{}
	// The IR's positions name the source files, in the test's directory.
	for _, chunk := range strings.Split(strings.ReplaceAll(prog.String(), dir, "$DIR"), "\nfunc ")[1:] {
		name, _, _ := strings.Cut(chunk, "(")
		if _, dup := funcs[name]; dup {
			t.Errorf("function %s twice", name)
		}
		funcs[name] = chunk
	}
	program, ds, err := frontend.Load(token.NewFileSet(), []frontend.Source{{Name: main, Data: []byte(tree["main.go"])}},
		frontend.Stages{Precheck: gate.Precheck, Prepare: mono.Instantiate, Reject: gate.Reject})
	if err != nil || len(ds) > 0 {
		t.Fatal(err, ds)
	}
	for _, p := range program.Packages {
		path := p.Types.Path()
		if strings.HasPrefix(path, "aether.view/") {
			t.Errorf("view %s among the program's packages", path)
		}
		homes := map[string]bool{}
		for obj, home := range p.Homes {
			if obj.Pkg() != p.Types {
				t.Errorf("%s records the home of %v, of %s", path, obj, obj.Pkg().Path())
			}
			homes[home.Path()] = true
		}
		order = append(order, path+" hosts "+strings.Join(slices.Sorted(maps.Keys(homes)), ","))
	}
	return string(b), funcs, order
}

// TestHostedInvariants checks D-052's hosting for determinism (import order
// and unrelated importers do not change an instance's host or code),
// duplicates (one copy of an instance however many packages want it, and
// none colliding with the host's own instance of a generic of the same
// name), initialization order (the views add no package and change no
// order), and reproducible output (two builds from different directories
// are the same).
func TestHostedInvariants(t *testing.T) {
	asm, funcs, order := hostedBuild(t, hostedTree)
	wantOrder := []string{"m.test/x/gen hosts ", "m.test/x/shape hosts m.test/x/gen", "m.test/x/u1 hosts ", "m.test/x/u2 hosts ", "main hosts "}
	if !slices.Equal(order, wantOrder) {
		t.Errorf("packages and hosts:\n%s\nwant\n%s", strings.Join(order, "\n"), strings.Join(wantOrder, "\n"))
	}
	hosted := []string{
		"m.test/x/gen.Stack[m.test/x/shape.Point].Push", "m.test/x/gen.Stack[m.test/x/shape.Point].Len",
		"m.test/x/gen.Count[m.test/x/shape.Point]", "m.test/x/shape.Stack[m.test/x/shape.Point].Top",
	}
	for _, name := range hosted {
		if funcs[name] == "" {
			t.Errorf("no function %s", name)
		}
	}

	t.Run("reproducible", func(t *testing.T) {
		again, _, _ := hostedBuild(t, hostedTree)
		if again != asm {
			t.Error("two builds of one program, in different directories, differ")
		}
	})
	t.Run("import order", func(t *testing.T) {
		got, _, gotOrder := hostedBuild(t, hostedPermuted)
		if got != asm || !slices.Equal(gotOrder, order) {
			t.Errorf("reversing the import lists changes the output (order %v)", gotOrder)
		}
	})
	t.Run("more importers", func(t *testing.T) {
		_, got, gotOrder := hostedBuild(t, hostedMore)
		// aa, first in path order, comes after the packages it imports.
		want := []string{"m.test/x/gen hosts ", "m.test/x/shape hosts m.test/x/gen", "m.test/x/aa hosts ",
			"m.test/x/u1 hosts ", "m.test/x/u2 hosts ", "m.test/x/z hosts ", "main hosts "}
		if !slices.Equal(gotOrder, want) {
			t.Errorf("packages and hosts:\n%s\nwant\n%s", strings.Join(gotOrder, "\n"), strings.Join(want, "\n"))
		}
		for _, name := range hosted {
			if got[name] != funcs[name] {
				t.Errorf("%s differs with more importers:\n%s\nwas\n%s", name, got[name], funcs[name])
			}
		}
		if got["m.test/x/gen.Stack[int].Push"] == "" {
			t.Error("no instance of gen's for z")
		}
	})
}

// TestHostedInitOrder checks that a program whose package initializers
// call hosted code prints what gc's does, natively.
func TestHostedInitOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	for name, tree := range map[string]map[string]string{"base": hostedTree, "more": hostedMore} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tree)
			gcExe := filepath.Join(dir, "gc")
			cmd := exec.Command(gobin, "build", "-o", gcExe, ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=", "GOAMD64=v1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("gc build: %v\n%s", err, out)
			}
			want := runExe(t, gcExe)
			out := filepath.Join(dir, "ae")
			if stderr, code := runAether("build", "-o", out, filepath.Join(dir, "main.go")); code != 0 {
				t.Fatalf("aether build: exit %d\n%s", code, stderr)
			}
			if got := runExe(t, out); got != want {
				t.Errorf("aether %+v\ngc %+v", got, want)
			}
		})
	}
}
