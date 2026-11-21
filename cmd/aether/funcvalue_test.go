package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFuncValuePrograms runs the func value corpus programs (s5_f*) and
// generated func value programs (generateFuncValues) under gc, the
// interpreter, aether build with and without -O0, and ABI-R1: all must
// agree (D-039).
func TestFuncValuePrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if !strings.HasPrefix(filepath.Base(path), "s5_f") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	if len(progs) == 0 {
		t.Fatal("no s5_f corpus programs")
	}
	for seed := uint64(1); seed <= 30; seed++ {
		progs[fmt.Sprintf("fnv%04d", seed)] = []byte(generateFuncValues(seed))
	}
	checkR1Programs(t, progs)
}

// TestNilMethodExpression pins, without gc, what the method expression
// (*T).M of a method with a value receiver does with a nil pointer, in the
// interpreter and under both backends: it panics as gc's wrapper does,
// with "value method main.T.Get called using nil *T pointer" and exit
// status 2 (V-7, resolved in Phase 11; s5_i07 compares it with gc).
func TestNilMethodExpression(t *testing.T) {
	src := []byte(`package main

type T struct{ n int }

func (t T) Get() int { return t.n }

type U struct{ *T }

func main() {
	get := (*T).Get
	t := T{3}
	println(get(&t), U.Get(U{&t}))
	var p *T
	println("before")
	println(get(p))
}
`)
	want := "3 3\nbefore\npanic: value method main.T.Get called using nil *T pointer\n"
	got, ds, err := interpret("nilexpr.go", src)
	if len(ds) > 0 || err != nil {
		t.Fatalf("not interpreted: %v %v", ds, err)
	}
	if !strings.HasPrefix(got.stderr, want) || got.exit != 2 {
		t.Errorf("interpreter: exit %d, stderr %q; want exit 2, %q", got.exit, got.stderr, want)
	}
	requireTools(t)
	dir := t.TempDir()
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(dir, fmt.Sprint("nilexpr", o0))
		if ds, err := build([]source{{"nilexpr.go", src}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("-O0 %v: not built: %v %v", o0, ds, err)
		}
		if n := runExe(t, exe); n != got {
			t.Errorf("-O0 %v: native differs from the interpreter:\n%s", o0, diffResults(n, got))
		}
	}
	if r1 := runR1(t, "nilexpr", emitR1(t, "nilexpr.go", src), dir); r1 != got {
		t.Errorf("ABI-R1 differs from the interpreter:\n%s", diffResults(r1, got))
	}
}
