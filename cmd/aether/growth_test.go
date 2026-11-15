package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// growthProgram prints the capacity of slices after each of a series of
// appends, for elements of 8, 1, 24, and 0 bytes, appends of several values
// and of another slice, and whether an append within the capacity Aether
// chose shares the array.
const growthProgram = `package main

type B struct{ a [3]int64 }

type E struct{}

func main() {
	var s []int
	for i := 0; i < 600; i++ {
		s = append(s, i)
		print(cap(s), " ")
	}
	println()
	var b []byte
	for i := 0; i < 300; i++ {
		b = append(b, 1)
		print(cap(b), " ")
	}
	println()
	var p []B
	for i := 0; i < 40; i++ {
		p = append(p, B{})
		print(cap(p), " ")
	}
	println()
	var z []E
	for i := 0; i < 5; i++ {
		z = append(z, E{}, E{})
		print(cap(z), " ")
	}
	println()
	t := make([]int, 3, 5)
	t = append(t, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)
	u := append(make([]int, 0, 1), 1, 2)
	v := append(make([]int, 2, 3), t...)
	println(cap(t), cap(u), cap(v))
	g := append([]int{1}, 2)
	h := append(g, 3)
	h[0] = 9
	println(cap(g), cap(h), g[0])
}
`

// growthWant is growthProgram's output under D-036's rule: the new
// capacity is the largest of the needed length, twice the old capacity,
// and 4, or for a zero-size element the needed length.
func growthWant() string {
	grow := func(c, need int, zero bool) int {
		if need <= c {
			return c
		}
		if zero {
			return need
		}
		return max(need, 2*c, 4)
	}
	var b strings.Builder
	for _, n := range []int{600, 300, 40} {
		c := 0
		for i := 1; i <= n; i++ {
			c = grow(c, i, false)
			fmt.Fprintf(&b, "%d ", c)
		}
		b.WriteString("\n")
	}
	c := 0
	for i := 1; i <= 5; i++ {
		c = grow(c, 2*i, true)
		fmt.Fprintf(&b, "%d ", c)
	}
	b.WriteString("\n")
	t := grow(5, 15, false)
	fmt.Fprintf(&b, "%d %d %d\n", t, grow(1, 2, false), grow(3, 17, false))
	g := grow(1, 2, false) // 4: room for h, which shares g's array
	fmt.Fprintf(&b, "%d %d %d\n", g, g, 9)
	return b.String()
}

// TestAppendGrowthRule checks Aether's own growth rule (D-036, V-6) in the
// interpreter, the default build, and -O0. gc's capacities differ by
// design, so this is not a differential test; the differential tests never
// observe a capacity after growth.
func TestAppendGrowthRule(t *testing.T) {
	want := growthWant()
	got, ds, err := interpret("growth.go", []byte(growthProgram))
	if len(ds) > 0 || err != nil || got.exit != 0 || got.stderr != want {
		t.Fatalf("interpreter: %v %v %+v\nwant %q", ds, err, got, want)
	}
	requireTools(t)
	dir := t.TempDir()
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(dir, fmt.Sprintf("growth-%v", o0))
		if ds, err := build([]source{{"growth.go", []byte(growthProgram)}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("-O0=%v: %v %v", o0, ds, err)
		}
		if got := runExe(t, exe); got.exit != 0 || got.stderr != want {
			t.Errorf("-O0=%v: %+v\nwant %q", o0, got, want)
		}
	}
}
