package main

import (
	"os"
	"strings"
	"testing"
)

// TestPreemption runs testdata/native/preempt.go, which TestNativePrograms
// compares with gc, in the interpreter, natively with and without -O0,
// and under ABI-R1, with its computations shortened for the interpreter
// (D-059, D-060): goroutines spinning in for, goto, and range loops and in
// recursion, and computations preempted with values in registers, let
// timers, the other goroutines, and the collector run. Before D-059 it
// never ended, nor did its recursion before D-060.
func TestPreemption(t *testing.T) {
	src, err := os.ReadFile("testdata/native/preempt.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, r := range [][2]string{{"nested(s, 4000)", "nested(s, 40)"}, {"hold(100000)", "hold(2000)"}, {"rounds*100000*280", "rounds*2000*280"}, {"rec(20, s", "rec(10, s"}} {
		if strings.Count(s, r[0]) == 0 {
			t.Fatalf("no %q in preempt.go", r[0])
		}
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	// The spinners take their time slices, of wall-clock time in the
	// interpreter too, whatever the steps: about 100 million a second.
	checkPinnedSteps(t, "preempt", []byte(s), result{stdout: "slept true\nstopped 5 true true true true true\nnested true true\nrecursion true\nhold true\ndone\n"}, 2_000_000_000)
}
