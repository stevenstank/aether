package main

import (
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// ssaFunc compiles src, promotes it, and returns its function name.
func ssaFunc(t *testing.T, src, name string) *ir.Func {
	t.Helper()
	prog, ds := compile([]source{{"p.go", []byte(src)}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if err := promoteProgram(prog, &promoteStats{}); err != nil {
		t.Fatal(err)
	}
	for _, f := range prog.Funcs {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no function %s", name)
	return nil
}

// pressureSrc has one small function for each property analyze measures.
const pressureSrc = `package main

func sum4(a, b, c, d int) int { return a + b + c + d }

func scale(a int) int { return a*3 + 5 }

func one() int { return 1 }

func cross(a, b int) int {
	x := one()
	return a + b + x
}

func two() (int, int) { return 1, 2 }

func pair(a int) int {
	x, y := two()
	return a + x + y
}

func loop(n int) int {
	t := 0
	for i := 0; i < n; i++ {
		t += i
	}
	return t
}

func main() { println(sum4(1, 2, 3, 4), scale(1), cross(1, 2), pair(1), loop(3)) }
`

// TestBenchPressure checks analyze against counts worked out by hand from
// the promoted IR. The pressure at an instruction is what is live before
// it plus its result, so operands that die there still count: in sum4,
// the first add sees four parameters and its result.
func TestBenchPressure(t *testing.T) {
	for _, c := range []struct {
		name                                              string
		maxLive, maxLiveNC, crossing, maxAcrossCall, phis int
	}{
		{"sum4", 5, 5, 0, 0, 0},
		{"scale", 3, 2, 0, 0, 0}, // the constants 3 and 5 are excluded from NC
		{"cross", 4, 4, 2, 2, 0}, // a and b live across the call
		{"pair", 5, 5, 1, 1, 0},  // the tuple counts two words
		{"loop", 5, 4, 0, 0, 2},  // phis for t and i
	} {
		p := analyze(ssaFunc(t, pressureSrc, c.name), nil)
		if p.maxLive != c.maxLive || p.maxLiveNC != c.maxLiveNC || p.crossingNC != c.crossing ||
			p.maxAcrossCall != c.maxAcrossCall || p.phis != c.phis {
			t.Errorf("%s: got %+v, want maxLive %d, maxLiveNC %d, crossingNC %d, maxAcrossCall %d, phis %d",
				c.name, p, c.maxLive, c.maxLiveNC, c.crossing, c.maxAcrossCall, c.phis)
		}
		if p.instrExecs != 0 || p.callsDyn != 0 || len(p.hist) != 0 {
			t.Errorf("%s: dynamic figures without frequencies: %+v", c.name, p)
		}
	}
}

// TestBenchPressureWeighted checks the frequency-weighted figures with the
// block counts of loop(3) and of one call of cross.
func TestBenchPressureWeighted(t *testing.T) {
	f := ssaFunc(t, pressureSrc, "loop")
	if len(f.Blocks) != 5 {
		t.Fatalf("loop has %d blocks, want 5", len(f.Blocks))
	}
	// b0 entry, b1 header, b2 body, b3 exit, b4 increment.
	p := analyze(f, []int64{1, 4, 3, 1, 3})
	var sum int64
	for _, n := range p.hist {
		sum += n
	}
	// Non-φ instructions: 4 in b0, 2 in b1, 2 in b2, 1 in b3, 3 in b4.
	if want := int64(4 + 2*4 + 2*3 + 1 + 3*3); p.instrExecs != want || sum != want {
		t.Errorf("loop: %d instruction executions, histogram sum %d, want %d", p.instrExecs, sum, want)
	}
	if p.twoAddrDyn != 0 {
		t.Errorf("loop: %d two-address copies, want 0: no add's first operand outlives it", p.twoAddrDyn)
	}
	p = analyze(ssaFunc(t, pressureSrc, "cross"), []int64{5})
	if p.callsDyn != 5 || p.callCrossDyn != 10 {
		t.Errorf("cross: %d calls with %d values across them, want 5 and 10", p.callsDyn, p.callCrossDyn)
	}
}

func TestBenchCopies(t *testing.T) {
	a := &ir.Instr{Op: ir.Param}
	b := &ir.Instr{Op: ir.Param}
	k := &ir.Instr{Op: ir.Const}
	for _, c := range []struct {
		op    ir.Op
		args  []*ir.Instr
		after []*ir.Instr
		want  int64
	}{
		{ir.Add, []*ir.Instr{a, b}, nil, 0},
		{ir.Add, []*ir.Instr{a, b}, []*ir.Instr{a}, 0},    // the dead operand is overwritten
		{ir.Add, []*ir.Instr{a, b}, []*ir.Instr{a, b}, 1}, // both live: one copy
		{ir.Sub, []*ir.Instr{a, b}, []*ir.Instr{b}, 0},    // the second operand is never overwritten
		{ir.Sub, []*ir.Instr{a, b}, []*ir.Instr{a}, 1},
		{ir.Sub, []*ir.Instr{k, b}, []*ir.Instr{k}, 0}, // a constant is rematerialized
		{ir.Neg, []*ir.Instr{a}, []*ir.Instr{a}, 1},
		{ir.Div, []*ir.Instr{a, b}, nil, 2}, // into %rax and out of it
		{ir.Rem, []*ir.Instr{a, b}, []*ir.Instr{a, b}, 2},
		{ir.Shl, []*ir.Instr{a, b}, nil, 1}, // the count into %cl
		{ir.Shr, []*ir.Instr{a, b}, []*ir.Instr{a}, 2},
		{ir.Lt, []*ir.Instr{a, b}, []*ir.Instr{a, b}, 0},
	} {
		after := map[*ir.Instr]bool{}
		for _, v := range c.after {
			after[v] = true
		}
		if got := copies(&ir.Instr{Op: c.op, Args: c.args}, after); got != c.want {
			t.Errorf("%v with %d live after: %d copies, want %d", c.op, len(c.after), got, c.want)
		}
	}
}
