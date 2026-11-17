package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// streqStrings are the strings TestStringEquality compares: empty, NUL
// bytes, invalid UTF-8, prefixes of one another, and strings that differ
// only in their first or last byte.
var streqStrings = []string{"", "\x00", "a", "ab", "abc", "abd", "bbc", "a\x00c", "a\x00", "\xff", "\xff\xfe", "\xc3\xa9", "é"}

// TestStringEquality checks == and != of every pair of streqStrings, each
// once as a constant and once as bytes made at run time, at another
// address, against Go's own == (D-037), in the interpreter, the default
// backend, and -O0.
func TestStringEquality(t *testing.T) {
	var src, want strings.Builder
	src.WriteString("package main\n\nfunc mk(s string) string { return string([]byte(s)) }\n\nfunc main() {\n")
	for _, x := range streqStrings {
		for _, y := range streqStrings {
			fmt.Fprintf(&src, "\tprintln(%s == %s, mk(%s) == %s, %s != mk(%s), mk(%s) == mk(%s))\n",
				strconv.Quote(x), strconv.Quote(y), strconv.Quote(x), strconv.Quote(y),
				strconv.Quote(x), strconv.Quote(y), strconv.Quote(x), strconv.Quote(y))
			fmt.Fprintf(&want, "%t %t %t %t\n", x == y, x == y, x != y, x == y)
		}
	}
	src.WriteString("}\n")
	w := result{stderr: want.String()}
	got, ds, err := interpret("eq.go", []byte(src.String()))
	if len(ds) > 0 || err != nil || got != w {
		t.Fatalf("interpreter: %v %v\n%+v", ds, err, got)
	}
	requireTools(t)
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(t.TempDir(), "eq")
		if ds, err := build([]source{{"eq.go", []byte(src.String())}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("build -O0=%v: %v %v", o0, ds, err)
		}
		if got := runExe(t, exe); got != w {
			t.Errorf("native -O0=%v: %+v", o0, got)
		}
	}
}

// TestStreqFunc checks that lowering adds .streq only to a program
// that compares strings, and once however many comparisons it has.
func TestStreqFunc(t *testing.T) {
	tests := []struct {
		src  string
		want int
	}{
		{`func main() { s := "a"; println(s + s) }`, 0},
		{`func main() { var a, b [2]int; println(a == b) }`, 0},
		{`func main() { s := "a"; println(s == s, s != "b") }`, 1},
		{`var g = "a" + "" == "a"; func main() { switch g { case true: } }`, 0}, // a constant
		{`type T struct{ s string }; func main() { var a, b T; println(a == b) }`, 1},
		{`func f(s string) { switch s { case "x", "y": } }; func main() { f(""); println([1]string{} == [1]string{"a"}) }`, 1},
	}
	for _, tt := range tests {
		prog, ds := compile([]source{{"p.go", []byte("package main\n" + tt.src + "\n")}})
		if len(ds) > 0 {
			t.Errorf("%s: %v", tt.src, ds)
			continue
		}
		n := 0
		for _, f := range prog.Funcs {
			if f.Name == ".streq" {
				n++
			}
		}
		if n != tt.want {
			t.Errorf("%s: %d .streq functions, want %d", tt.src, n, tt.want)
		}
	}
}
