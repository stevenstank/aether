package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// layoutTypes are the array and struct types whose layout TestLayoutAgainstGc
// checks: padding between fields of every width, nesting, embedding, a blank
// field, zero-size fields (gc pads a struct that ends in one), and arrays of
// structs with tail padding.
var layoutTypes = []struct {
	name string
	def  string // the type literal
	// fields are the struct's non-blank fields and their types; for an
	// array, the element type, under the field name "[2]".
	fields [][2]string
}{
	{"T0", "struct{ a bool; b int64; c int8 }", [][2]string{{"a", "bool"}, {"b", "int64"}, {"c", "int8"}}},
	{"T1", "struct{ a int8; b int16; c int8; d int32; e int8; f float32; g int8; h float64 }",
		[][2]string{{"a", "int8"}, {"b", "int16"}, {"c", "int8"}, {"d", "int32"}, {"e", "int8"}, {"f", "float32"}, {"g", "int8"}, {"h", "float64"}}},
	{"T2", "struct{ s string; b bool; t string }", [][2]string{{"s", "string"}, {"b", "bool"}, {"t", "string"}}},
	{"T3", "struct{ x [3]int8; y int16 }", [][2]string{{"x", "[3]int8"}, {"y", "int16"}}},
	{"T4", "struct{ p T0; q [2]T1; r uint8 }", [][2]string{{"p", "T0"}, {"q", "[2]T1"}, {"r", "uint8"}}},
	{"T5", "[3]T0", [][2]string{{"[2]", "T0"}}},
	{"T6", "struct{ a int64; z struct{} }", [][2]string{{"a", "int64"}}},
	{"T7", "struct{ z [0]int64; a int8 }", [][2]string{{"a", "int8"}}},
	{"T8", "struct{ T0; f float64; _ int8; g uint16 }", [][2]string{{"T0", "T0"}, {"f", "float64"}, {"g", "uint16"}}},
	{"T9", "[3][2]T3", [][2]string{{"[2]", "[2]T3"}}},
	{"T10", "struct{ a bool; b [2]struct{ c int8; d int32 }; e bool }", [][2]string{{"a", "bool"}, {"b", "[2]struct{ c int8; d int32 }"}, {"e", "bool"}}},
	{"T11", "struct{}", nil},
	{"T12", "[5]bool", [][2]string{{"[2]", "bool"}}},
	{"T13", "struct{ u uint32; s [2]string; f float32 }", [][2]string{{"u", "uint32"}, {"s", "[2]string"}, {"f", "float32"}}},
	{"T14", "struct{ a int32; b struct{ c int8 }; d [0]int32 }", [][2]string{{"a", "int32"}, {"b", "struct{ c int8 }"}}},
	{"T15", "[7]uint16", [][2]string{{"[2]", "uint16"}}},
	{"T16", "struct{ a uint8; b uint64; c uint8; d uintptr; e uint; f int; g rune; h byte; i float32; j bool }",
		[][2]string{{"a", "uint8"}, {"b", "uint64"}, {"c", "uint8"}, {"d", "uintptr"}, {"e", "uint"}, {"f", "int"}, {"g", "rune"}, {"h", "byte"}, {"i", "float32"}, {"j", "bool"}}},
	{"T17", "[3]struct{ a int64; b int8 }", [][2]string{{"[2]", "struct{ a int64; b int8 }"}}},
}

// TestLayoutAgainstGc checks the layout Aether gives arrays and structs
// (D-034) against gc's: a gc-built program prints unsafe.Sizeof, Alignof,
// and Offsetof of each type of layoutTypes and the offset of an array's
// element 2, and Aether's must be the same. Aether's comes from the IR it
// emits, not from go/types: a global's size and alignment from its memory
// object, and a field's or element's offset as the lowest offset at which a
// function storing only that field or element writes into the global. A
// field of a type without scalars is never accessed, so it has no offset to
// check.
func TestLayoutAgainstGc(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	var decls, gcMain, aeFuncs strings.Builder
	for _, ty := range layoutTypes {
		fmt.Fprintf(&decls, "type %s %s\n\n", ty.name, ty.def)
		fmt.Fprintf(&gcMain, "\tvar x%s %s\n\tprintln(\"%s\", unsafe.Sizeof(x%s), unsafe.Alignof(x%s))\n", ty.name, ty.name, ty.name, ty.name, ty.name)
		fmt.Fprintf(&aeFuncs, "var g%s %s\n\n", ty.name, ty.name)
		for i, f := range ty.fields {
			if f[0] == "[2]" {
				fmt.Fprintf(&gcMain, "\tprintln(\"%s[2]\", uintptr(unsafe.Pointer(&x%s[2]))-uintptr(unsafe.Pointer(&x%s)))\n", ty.name, ty.name, ty.name)
			} else {
				fmt.Fprintf(&gcMain, "\tprintln(\"%s.%s\", unsafe.Offsetof(x%s.%s))\n", ty.name, f[0], ty.name, f[0])
			}
			sel := "." + f[0]
			if f[0] == "[2]" {
				sel = f[0]
			}
			fmt.Fprintf(&aeFuncs, "func set%s_%d() {\n\tvar h %s\n\tg%s%s = h\n}\n\n", ty.name, i, f[1], ty.name, sel)
		}
	}
	gcSrc := "package main\n\nimport \"unsafe\"\n\n" + decls.String() + "func main() {\n" + gcMain.String() + "}\n"
	want := gcRun(t, gobin, map[string][]byte{"layout": []byte(gcSrc)})["layout"]
	if want.exit != 0 {
		t.Fatalf("gc-built layout program: %+v", want)
	}
	aeSrc := "package main\n\n" + decls.String() + aeFuncs.String() + "func main() {}\n"
	prog, ds := compile([]source{{"layout.go", []byte(aeSrc)}})
	if len(ds) > 0 {
		t.Fatalf("Aether rejected the layout program: %v\n%s", ds, aeSrc)
	}
	var got []string
	mems := map[string]*ir.Mem{}
	for _, m := range prog.Mems {
		mems[m.Name] = m
	}
	funcs := map[string]*ir.Func{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	for _, ty := range layoutTypes {
		m := mems["g"+ty.name]
		if m == nil {
			t.Fatalf("no memory object for g%s", ty.name)
		}
		got = append(got, fmt.Sprintf("%s %d %d", ty.name, m.Size, m.Align))
		for i, f := range ty.fields {
			off, ok := lowestStore(t, funcs[fmt.Sprintf("set%s_%d", ty.name, i)], m)
			label := ty.name + "." + f[0]
			if f[0] == "[2]" {
				label = ty.name + "[2]"
			}
			if !ok {
				t.Fatalf("%s: no store into g%s", label, ty.name)
			}
			got = append(got, fmt.Sprintf("%s %d", label, off))
		}
	}
	wantLines := strings.Split(strings.TrimSuffix(want.stderr, "\n"), "\n")
	sort.Strings(wantLines)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(wantLines, "\n") {
		t.Errorf("layouts differ\nAether:\n%s\ngc:\n%s", strings.Join(got, "\n"), strings.Join(wantLines, "\n"))
	}
	if len(got) < 60 {
		t.Errorf("only %d layout facts checked", len(got))
	}
}

// lowestStore returns the lowest constant offset at which f stores into the
// global memory object m.
func lowestStore(t *testing.T, f *ir.Func, m *ir.Mem) (int64, bool) {
	t.Helper()
	low, found := int64(0), false
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if in.Op != ir.StM || in.Args[0].Op != ir.Addr || in.Args[0].Mem != m {
				continue
			}
			if in.Args[1].Op != ir.Const {
				t.Fatalf("func %s: %s has a computed offset", f.Name, in)
			}
			if !found || in.Args[1].Const < low {
				low, found = in.Args[1].Const, true
			}
		}
	}
	return low, found
}
