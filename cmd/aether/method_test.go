package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMethodPrograms runs the method corpus programs (s5_m*) and generated
// method programs (generateMethods) under gc, the interpreter, aether build
// with and without -O0, and ABI-R1: all must agree (D-038).
func TestMethodPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if !strings.HasPrefix(filepath.Base(path), "s5_m") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	if len(progs) == 0 {
		t.Fatal("no s5_m corpus programs")
	}
	for seed := uint64(1); seed <= 30; seed++ {
		progs[fmt.Sprintf("mth%04d", seed)] = []byte(generateMethods(seed))
	}
	checkR1Programs(t, progs)
}

// TestMethodReceiverOrder pins Aether's order for a method's receiver
// without gc, in the interpreter and under both backends: the receiver is
// evaluated before the arguments (D-024), so a value receiver is copied,
// and a nil pointer to it checked, before a call among the arguments runs.
// gc does both after such a call; both orders are allowed (V-4).
func TestMethodReceiverOrder(t *testing.T) {
	src := []byte(`package main

type T struct{ a int }

func (t T) Get(x int) int { return t.a*100 + x }

func (t *T) PGet(x int) int { return t.a*100 + x }

var g T

func set(v int) int {
	g.a = v
	return v
}

func main() {
	g.a = 1
	println(g.Get(set(5)))
	g.a = 1
	println(g.PGet(set(7)))
	var p *T
	println(p.Get(set(3)))
}
`)
	want := "105\n707\npanic: runtime error: invalid memory address or nil pointer dereference\n"
	got, ds, err := interpret("order.go", src)
	if len(ds) > 0 || err != nil {
		t.Fatalf("not interpreted: %v %v", ds, err)
	}
	if !strings.HasPrefix(got.stderr, want) || got.exit != 2 {
		t.Errorf("interpreter: exit %d, stderr %q; want exit 2, %q", got.exit, got.stderr, want)
	}
	requireTools(t)
	dir := t.TempDir()
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(dir, fmt.Sprint("order", o0))
		if ds, err := build([]source{{"order.go", src}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("-O0 %v: not built: %v %v", o0, ds, err)
		}
		if n := runExe(t, exe); n != got {
			t.Errorf("-O0 %v: native differs from the interpreter:\n%s", o0, diffResults(n, got))
		}
	}
	if r1 := runR1(t, "order", emitR1(t, "order.go", src), dir); r1 != got {
		t.Errorf("ABI-R1 differs from the interpreter:\n%s", diffResults(r1, got))
	}
}
