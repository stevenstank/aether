package lower

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
	"github.com/stevenstank/aether/internal/ir"
)

// typeCheck parses and type-checks src, which must be valid Go.
func typeCheck(t *testing.T, src string) *frontend.Package {
	t.Helper()
	fset := token.NewFileSet()
	f, ds := frontend.Parse(fset, "p.go", []byte(src))
	if len(ds) > 0 {
		t.Fatalf("parse: %v", ds)
	}
	pkg, ds := frontend.TypeCheck(fset, []*ast.File{f})
	if len(ds) > 0 {
		t.Fatalf("type check: %v", ds)
	}
	return pkg
}

// lowerBody lowers "package main; func main() {body}" after the subset gate
// has accepted it, and returns the instructions of its entry block.
func lowerBody(t *testing.T, body string) string {
	t.Helper()
	pkg := typeCheck(t, "package main\n\nfunc main() {\n"+body+"\n}\n")
	if ds := gate.Check(pkg); len(ds) > 0 {
		t.Fatalf("gate: %v", ds)
	}
	prog, ds := Lower(pkg)
	if len(ds) > 0 {
		t.Fatalf("lower: %v", ds)
	}
	if errs := ir.Verify(prog); len(errs) > 0 {
		t.Fatalf("verify: %v", errs)
	}
	var lines []string
	for _, in := range prog.Funcs[0].Blocks[0].Instrs {
		lines = append(lines, in.String())
	}
	return strings.Join(lines, "; ")
}

// TestLowerConstructs checks the lowering rule of each S1 construct not
// already pinned down by the corpus IR goldens in cmd/aether.
func TestLowerConstructs(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"empty statements and blocks", "; {}; {;}", "ret"},
		{"zero value for each name", "var a, b int; println(a, b)",
			"v0 = const 0; store s0, v0; v1 = const 0; store s1, v1; v2 = load s0; v3 = load s1; println v2, v3; ret"},
		{"constant expression uses the exact value", "const k = 9223372036854775807*3 - 2*9223372036854775807 - 9223372036854775800; println(k*k - 1)",
			"v0 = const 48; println v0; ret"},
		{"constant operand of a variable expression", "x := 1; println(x + 2*3)",
			"v0 = const 1; store s0, v0; v1 = load s0; v2 = const 6; v3 = add v1, v2; println v3; ret"},
		{"unary plus and parentheses are identity", "x := 1; println(+(x), ((x)))",
			"v0 = const 1; store s0, v0; v1 = load s0; v2 = load s0; println v1, v2; ret"},
		{"operands left to right", "x, y := 1, 2; println(x - y*x)",
			"v0 = const 1; v1 = const 2; store s0, v0; store s1, v1; v2 = load s0; v3 = load s1; v4 = load s0; v5 = mul v3, v4; v6 = sub v2, v5; println v6; ret"},
		{"compound assignment loads, then evaluates", "x := 2; x *= x + 1; println(x)",
			"v0 = const 2; store s0, v0; v1 = load s0; v2 = load s0; v3 = const 1; v4 = add v2, v3; v5 = mul v1, v4; store s0, v5; v6 = load s0; println v6; ret"},
		{"redeclared variable is assigned", "a := 1; a, b := 2, a; println(a, b)",
			"v0 = const 1; store s0, v0; v1 = const 2; v2 = load s0; store s0, v1; store s1, v2; v3 = load s0; v4 = load s1; println v3, v4; ret"},
		{"shadowing initializer reads the outer variable", "x := 1; { x := x; println(x) }",
			"v0 = const 1; store s0, v0; v1 = load s0; store s1, v1; v2 = load s1; println v2; ret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowerBody(t, tt.body); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestLowerPositions checks that each instruction carries the position of
// the source construct it implements.
func TestLowerPositions(t *testing.T) {
	pkg := typeCheck(t, "package main\n\nfunc main() {\n\tx := 1\n\tx -= -x\n\tx--\n\tprintln(x)\n}\n")
	prog, ds := Lower(pkg)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	f := prog.Funcs[0]
	if f.File != "p.go" || f.Pos != (ir.Pos{Line: 3, Col: 6}) || f.Slots[0].Pos != (ir.Pos{Line: 4, Col: 2}) {
		t.Errorf("function or slot position: %s %v %v", f.File, f.Pos, f.Slots[0].Pos)
	}
	var got []string
	for _, in := range f.Blocks[0].Instrs {
		got = append(got, in.String()+" @"+in.Pos.String())
	}
	want := []string{
		"v0 = const 1 @4:7", "store s0, v0 @4:2", // x := 1
		"v1 = load s0 @5:2", "v2 = load s0 @5:8", "v3 = neg v2 @5:7", "v4 = sub v1, v3 @5:4", "store s0, v4 @5:2", // x -= -x
		"v5 = load s0 @6:2", "v6 = const 1 @6:3", "v7 = sub v5, v6 @6:3", "store s0, v7 @6:2", // x--
		"v8 = load s0 @7:10", "println v8 @7:2", // println(x)
		"ret @8:1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestLowerUnexpected calls Lower on valid Go that the subset gate would
// reject. Lowering must never see such a program; if it does, it must report
// an internal error at the offending construct instead of emitting IR.
func TestLowerUnexpected(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"statement", "func main() { x := 1; switch x { }; println(x) }", "p.go:2:23: internal: lowering: unexpected statement *ast.SwitchStmt"},
		{"operator", "func main() { x := 1; println(x / 2) }", "p.go:2:31: internal: lowering: unexpected expression x / 2"},
		{"type", "func main() { var s string; println(s) }", "p.go:2:19: internal: lowering: unexpected type string"},
		{"named results", "func f() (a, b int) { return 1, 2 }\nfunc main() { f() }", "p.go:2:1: internal: lowering: unexpected result var a int"},
		{"function value", "func f() {}\nfunc main() { g := f; g() }", "p.go:3:20: internal: lowering: unexpected type func()"},
		{"package variable", "var g int\nfunc main() { println(g) }", "p.go:2:1: internal: lowering: unexpected top-level declaration"},
		{"blank target", "func main() { _ = 1 }", "p.go:2:15: internal: lowering: unexpected assignment target _"},
		{"builtin call", "func main() { _ = len([1]int{}) }", "p.go:2:15: internal: lowering: unexpected assignment target _"},
		{"builtin call statement", "func main() { panic(1) }", "p.go:2:15: internal: lowering: unexpected call of panic"},
		{"float variable", "func main() { var x float64 = 1; println(x > 0) }", "p.go:2:31: internal: lowering: unexpected type float64"},
		{"no body", "func main()", "p.go:2:1: internal: lowering: unexpected top-level declaration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := typeCheck(t, "package main\n"+tt.src+"\n")
			prog, ds := Lower(pkg)
			if prog != nil {
				t.Errorf("IR produced:\n%s", prog)
			}
			if len(ds) != 1 || ds[0].Class != diag.Internal || ds[0].String() != strings.Replace(tt.want, "p.go:2:", "p.go:2:", 1) && ds[0].String() != tt.want {
				t.Errorf("got %v, want %s", ds, tt.want)
			}
		})
	}
}
