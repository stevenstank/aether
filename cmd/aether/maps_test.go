package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMapPrograms runs the map corpus programs (s5_k*) and
// generated map programs (generateMaps) under gc, the interpreter,
// aether build with and without -O0, and ABI-R1: all must agree (D-042).
func TestMapPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if !strings.HasPrefix(filepath.Base(path), "s5_k") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	if len(progs) == 0 {
		t.Fatal("no s5_k corpus programs")
	}
	for seed := uint64(1); seed <= 30; seed++ {
		progs[fmt.Sprintf("map%04d", seed)] = []byte(generateMaps(seed))
	}
	checkR1Programs(t, progs)
}

// TestMapRangeOrder pins Aether's order of a range over a map, which gc
// randomizes (V-8, D-042), in the interpreter, both backends, and ABI-R1:
// insertion order, with a deleted and reinserted key last; once the map
// grows during the range, the rest of the old entries with their current
// elements, deleted keys skipped and new keys not visited; a NaN key
// visited after growth, but not after a clear; and keys added without
// growth visited.
func TestMapRangeOrder(t *testing.T) {
	src := []byte(`package main

func main() {
	m := map[int]int{}
	for i := 5; i > 0; i-- {
		m[i] = i
	}
	delete(m, 3)
	m[3] = 33
	m[1] = 11
	for k, v := range m {
		print(k, ":", v, " ")
	}
	println()
	g := map[int]int{1: 1, 2: 2, 3: 3}
	for k, v := range g {
		print(k, "=", v, " ")
		if k == 1 {
			for i := 10; i < 30; i++ {
				g[i] = i
			}
			delete(g, 3)
			g[2] = 22
		}
	}
	println(len(g))
	nan := 0.0
	nan /= nan
	f := map[float64]int{}
	f[nan] = 1
	f[1] = 2
	for _, v := range f {
		print(v, " ")
		if v == 1 {
			for i := 0; i < 20; i++ {
				f[float64(i+10)] = i
			}
		}
	}
	println(len(f))
	f2 := map[float64]int{}
	f2[nan] = 1
	f2[nan] = 2
	f2[0] = 3
	for _, v := range f2 {
		print(v, " ")
		if v == 1 {
			clear(f2)
			for i := 0; i < 20; i++ {
				f2[float64(i)] = 100 + i
			}
		}
	}
	println(len(f2))
	h := map[string]int{"a": 1}
	for k, v := range h {
		print(k, v, " ")
		if v < 4 {
			h[k+"a"] = v + 1
		}
	}
	println(len(h))
}
`)
	want := "5:5 4:4 2:2 1:11 3:33 \n1=1 2=22 10=10 11=11 12=12 13=13 14=14 22\n1 2 0 1 2 3 4 5 22\n1 101 102 103 104 105 106 107 20\na1 aa2 aaa3 aaaa4 4\n"
	got, ds, err := interpret("order.go", src)
	if len(ds) > 0 || err != nil {
		t.Fatalf("not interpreted: %v %v", ds, err)
	}
	if got != (result{stderr: want}) {
		t.Errorf("interpreter: exit %d, stderr %q; want %q", got.exit, got.stderr, want)
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
