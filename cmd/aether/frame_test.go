package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
	"github.com/stevenstank/aether/internal/ir"
	"github.com/stevenstank/aether/internal/lower"
	"github.com/stevenstank/aether/internal/mono"
)

// TestFrameBound checks the gate's bound on the stack a function's arrays
// and structs need (gate.FrameBytes, feature frame-size) against what
// lowering gives each function, on every corpus program expected to run
// and on the generated programs: the bound must never be below the bytes of
// its memory objects laid out with their alignment, so that no function the
// gate accepts can exceed the backend's frame limit (D-034).
func TestFrameBound(t *testing.T) {
	type program struct {
		name string
		src  []byte
	}
	var progs []program
	for _, path := range corpus(t) {
		if readExpectation(t, path).class != "run" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs = append(progs, program{path, src})
	}
	// Operands of an index or selector that lowering evaluates into a
	// temporary rather than using in place: a literal, a call's result,
	// a conversion, and a type assertion's value.
	for i, body := range []string{
		"i := 1; println([2]int{1, 2}[i], [3]int8{}[2])",
		"println(f()[1], f()[0])",
		"q := Q{1}; println(P(q).x, P{2}.x)",
		"println([2]*P{{1}, nil}[0].x)",
		"var e any = [2]int{5, 6}; println(e.([2]int)[1], e.(Q).x)",
	} {
		src := "package main\n\ntype P struct{ x int }\n\ntype Q struct{ x int }\n\nfunc f() [2]int { return [2]int{3, 4} }\n\nfunc main() {\n" + body + "\n}\n"
		progs = append(progs, program{fmt.Sprintf("operand%d.go", i), []byte(src)})
	}
	for seed := uint64(1); seed <= 400; seed++ {
		progs = append(progs, program{fmt.Sprintf("gen%04d.go", seed), []byte(generate(seed))})
	}
	for seed := uint64(1); seed <= 100; seed++ {
		progs = append(progs, program{fmt.Sprintf("slc%04d.go", seed), []byte(generateSlices(seed))})
		progs = append(progs, program{fmt.Sprintf("mth%04d.go", seed), []byte(generateMethods(seed))})
		progs = append(progs, program{fmt.Sprintf("fnv%04d.go", seed), []byte(generateFuncValues(seed))})
		progs = append(progs, program{fmt.Sprintf("cls%04d.go", seed), []byte(generateClosures(seed))})
		progs = append(progs, program{fmt.Sprintf("ifc%04d.go", seed), []byte(generateIfaces(seed))})
		progs = append(progs, program{fmt.Sprintf("map%04d.go", seed), []byte(generateMaps(seed))})
		progs = append(progs, program{fmt.Sprintf("nmd%04d.go", seed), []byte(generateNamed(seed))})
		progs = append(progs, program{fmt.Sprintf("gnr%04d.go", seed), []byte(generateGenerics(seed))})
		progs = append(progs, program{fmt.Sprintf("chn%04d.go", seed), []byte(generateChans(seed))})
	}
	checked, slack := 0, int64(0)
	for _, p := range progs {
		fset := token.NewFileSet()
		file, ds := frontend.Parse(fset, p.name, p.src)
		if len(ds) > 0 {
			t.Fatalf("%s: %v", p.name, ds)
		}
		pkg, ds := frontend.TypeCheck(fset, []*ast.File{file})
		if len(ds) > 0 {
			continue // a generated program with an overflowing constant
		}
		// Generic code is checked in its instances' copies (D-045).
		if pkg, ds = mono.Instantiate(pkg); len(ds) > 0 {
			t.Fatalf("%s: %v", p.name, ds)
		}
		file = pkg.Files[0]
		if ds := gate.Check(pkg); len(ds) > 0 {
			t.Fatalf("%s: %v", p.name, ds)
		}
		prog, ds := lower.Lower(pkg)
		if len(ds) > 0 {
			t.Fatalf("%s: %v", p.name, ds)
		}
		funcs := map[string]*ir.Func{}
		for _, f := range prog.Funcs {
			funcs[f.Name] = f
		}
		check := func(what string, bound int64, f *ir.Func) {
			got := memBytes(f)
			if bound < got {
				t.Errorf("%s: %s: bound %d below the %d bytes of its memory objects", p.name, what, bound, got)
			}
			checked++
			if got > 0 {
				slack = max(slack, bound-got)
			}
		}
		var initBound int64
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				// A method's function is T.M (D-038).
				name := d.Name.Name
				if d.Recv != nil {
					name = types.ExprString(d.Recv.List[0].Type)
					name = strings.TrimPrefix(name, "*") + "." + d.Name.Name
				}
				if funcs[name] == nil {
					t.Fatalf("%s: no IR function %s", p.name, name)
				}
				check("func "+name, gate.FuncBytes(pkg, d.Type, d.Body), funcs[name])
			case *ast.GenDecl:
				if d.Tok == token.VAR {
					for _, spec := range d.Specs {
						for _, v := range spec.(*ast.ValueSpec).Values {
							initBound += gate.FrameBytes(pkg, v)
						}
					}
				}
			}
		}
		if prog.Init != nil {
			check("init", initBound, prog.Init)
		}
		// Each function literal is the function .litN, numbered in source
		// order (D-040), with a bound of its own.
		n := 0
		ast.Inspect(file, func(node ast.Node) bool {
			if lit, ok := node.(*ast.FuncLit); ok {
				n++
				name := fmt.Sprintf(".lit%d", n)
				if funcs[name] == nil {
					t.Fatalf("%s: no IR function %s", p.name, name)
				}
				check("func "+name, gate.FuncBytes(pkg, lit.Type, lit.Body), funcs[name])
			}
			return true
		})
	}
	t.Logf("%d functions checked; largest excess of the bound %d bytes", checked, slack)
}

// memBytes returns the bytes f's memory objects take laid out one after the
// other, each aligned, as the backend lays them out after its cells, which
// end at a multiple of 8.
func memBytes(f *ir.Func) int64 {
	var n int64
	for _, m := range f.Mems {
		n += m.Size
		n = (n + m.Align - 1) &^ (m.Align - 1)
	}
	return (n + 7) &^ 7
}

// TestFrameSizeDiagnostic checks that a function, or package
// initialization, whose arrays and structs may need more stack than the
// frame-size limit is unsupported, and that one just under it builds.
func TestFrameSizeDiagnostic(t *testing.T) {
	// 31 local arrays of 16 MiB and their 30 copies need 61 objects, 976 MiB.
	var b strings.Builder
	b.WriteString("package main\n\ntype Big [1 << 21]int64\n\nfunc main() {\n\tvar a0 Big\n\ta0[0] = 1\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "\ta%d := a%d\n", i, i-1)
	}
	b.WriteString("\tprintln(a30[0])\n}\n")
	tests := []struct {
		name, src, want string
	}{
		{"function", b.String(), "frame-size.go:5:6: unsupported: function main, whose arrays and structs may need"},
		// 40 package-level variables initialized by calls: 40 results of
		// 16 MiB in Init's frame.
		{"init", "package main\n\ntype Big [1 << 21]int64\n\nfunc f() Big { return Big{} }\n\n" + gens(40) + "func main() {}\n",
			"unsupported: package initialization, whose arrays and structs may need"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := check([]source{{"frame-size.go", []byte(tt.src)}})
			if len(ds) != 1 || ds[0].Feature != gate.FeatFrameSize || !strings.Contains(ds[0].String(), tt.want) {
				t.Errorf("got %v, want one frame-size diagnostic containing %q", ds, tt.want)
			}
		})
	}
	// Under the limit: 16 objects of 16 MiB build and run.
	requireTools(t)
	var u strings.Builder
	u.WriteString("package main\n\ntype Big [1 << 21]int64\n\nfunc main() {\n\tvar a0 Big\n\ta0[1<<21-1] = 7\n")
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&u, "\ta%d := a%d\n", i, i-1)
	}
	u.WriteString("\tprintln(a7[1<<21-1])\n}\n")
	dir := t.TempDir()
	src, exe := dir+"/under.go", dir+"/under"
	if err := os.WriteFile(src, []byte(u.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if stderr, code := runAether("build", "-o", exe, src); code != 0 {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	if got := runExe(t, exe); got != (result{stderr: "7\n"}) {
		t.Errorf("got %+v", got)
	}
}

// gens returns n package-level variables, each initialized by a call of f,
// which returns a 16 MiB array.
func gens(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "var g%d = f()\n\n", i)
	}
	return b.String()
}
