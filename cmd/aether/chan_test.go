package main

import (
	"fmt"
	"io"
	"path/filepath"
	"testing"
)

// checkPinned runs src in the interpreter, natively with and without -O0,
// and under ABI-R1, and requires the result want of each, worked out
// without gc.
func checkPinned(t *testing.T, name string, src []byte, want result) {
	t.Helper()
	got, ds, err := interpret(name+".go", src)
	if len(ds) > 0 || err != nil {
		t.Fatalf("not interpreted: %v %v", ds, err)
	}
	if got != want {
		t.Errorf("interpreter:\n%s", diffResults(got, want))
	}
	requireTools(t)
	dir := t.TempDir()
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(dir, fmt.Sprint(name, o0))
		if ds, err := build([]source{{name + ".go", src}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("-O0 %v: not built: %v %v", o0, ds, err)
		}
		if n := runExe(t, exe); n != want {
			t.Errorf("-O0 %v:\n%s", o0, diffResults(n, want))
		}
	}
	if r1 := runR1(t, name, emitR1(t, name+".go", src), dir); r1 != want {
		t.Errorf("ABI-R1:\n%s", diffResults(r1, want))
	}
}

// TestScheduleOrder pins Aether's schedule (D-048), which programs
// compared with gc never depend on: gc's single-P order, with a new or
// woken goroutine run next and the others in turn, and close waking its
// waiters in gc's order. The output is the one gc-built go1.26.1 binaries
// wrote with GOMAXPROCS=1, the same in five runs (2026-11-20).
func TestScheduleOrder(t *testing.T) {
	src := []byte(`package main

func main() {
	c := make(chan int)
	for i := 0; i < 4; i++ {
		go func(i int) { print("g", i, " "); c <- i }(i)
	}
	for i := 0; i < 4; i++ {
		print("r", <-c, " ")
	}
	println()
	b := make(chan int, 2)
	for i := 0; i < 3; i++ {
		go func(i int) { b <- i; print("s", i, " ") }(i)
	}
	for i := 0; i < 3; i++ {
		print("rb", <-b, " ")
	}
	println()
	d := make(chan int)
	done := make(chan int, 10)
	for i := 0; i < 3; i++ {
		go func(i int) { v, ok := <-d; print("w", i, v, ok, " "); done <- i }(i)
	}
	go func() { print("closer "); close(d) }()
	for i := 0; i < 4; i++ {
		go func(i int) { print("x", i, " ") }(i)
	}
	for i := 0; i < 3; i++ {
		<-done
	}
	println()
	e := make(chan int)
	for i := 0; i < 3; i++ {
		go func(i int) { e <- i }(i)
	}
	for i := 0; i < 3; i++ {
		print("e", <-e, " ")
	}
	println()
}
`)
	want := "g3 r3 g0 r0 g1 r1 g2 r2 \ns2 rb2 s0 rb0 s1 rb1 \nx3 closer w00false x0 x1 x2 w20false w10false \ne2 e0 e1 \n"
	checkPinned(t, "sched", src, result{stderr: want})
}

// TestGoNilFuncArgs pins go of a nil func value with arguments, which gc
// evaluates at the statement and calls in the new goroutine: there it
// panics, while main waits (D-048).
func TestGoNilFuncArgs(t *testing.T) {
	src := []byte(`package main

func main() {
	var f func(int)
	c := make(chan int)
	go f(1)
	println("after")
	<-c
}
`)
	checkPinned(t, "gonil", src, result{stderr: "after\npanic: runtime error: invalid memory address or nil pointer dereference\n", exit: 2})
}

// TestSelectChoice pins Aether's choice among ready select cases (D-048,
// increment 10.3): uniform, from a xorshift generator of fixed seed, so
// the same in every run, in the interpreter and natively. Three cases
// always ready, 3000 times: the counts, near 1000 each, and the first 20
// running weighted sums are Aether's, measured 2026-11-20.
func TestSelectChoice(t *testing.T) {
	src := []byte(`package main

func main() {
	a, b, c := make(chan int, 1), make(chan int, 1), make(chan int, 1)
	na, nb, nc := 0, 0, 0
	for i := 0; i < 3000; i++ {
		a <- 1
		b <- 2
		c <- 3
		select {
		case <-a:
			na++
			<-b
			<-c
		case <-b:
			nb++
			<-a
			<-c
		case <-c:
			nc++
			<-a
			<-b
		}
		if i < 20 {
			print(na+2*nb+3*nc, " ")
		}
	}
	println()
	println(na, nb, nc)
}
`)
	want := "1 2 5 8 9 12 15 17 18 20 22 24 25 26 28 31 32 35 36 39 \n1025 1015 960\n"
	checkPinned(t, "choice", src, result{stderr: want})
}
