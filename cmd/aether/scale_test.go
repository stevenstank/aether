package main

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/ir"
)

// The analyses' costs on the adversarial programs, without the limits on
// promotion: each program is verified as lowered, promoted function by
// function, and verified again in SSA form, as aether build would if the
// limits were higher.
type analysisTimes struct{ verify, promote, verifySSA time.Duration }

// timeAnalyses compiles src and times the analyses, the best of runs.
func timeAnalyses(t *testing.T, name string, src []byte, runs int) analysisTimes {
	t.Helper()
	best := analysisTimes{verify: time.Hour, promote: time.Hour, verifySSA: time.Hour}
	for range runs {
		prog, ds := compile([]source{{name, src}})
		if len(ds) > 0 {
			t.Fatalf("%s: %v", name, ds)
		}
		runtime.GC()
		start := time.Now()
		if errs := ir.Verify(prog); len(errs) > 0 {
			t.Fatalf("%s: %v", name, errs[0])
		}
		v := time.Since(start)
		runtime.GC()
		start = time.Now()
		for _, f := range prog.Funcs {
			if err := ir.Promote(f); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		p := time.Since(start)
		runtime.GC()
		start = time.Now()
		if errs := ir.Verify(prog); len(errs) > 0 {
			t.Fatalf("%s: promoted: %v", name, errs[0])
		}
		best = analysisTimes{min(best.verify, v), min(best.promote, p), min(best.verifySSA, time.Since(start))}
	}
	return best
}

// TestAnalysesScale checks that verification and promotion grow about
// linearly on the adversarial programs and the nesting axis: from n to 8n,
// each takes less than 32 times as long (linear is 8, quadratic 64). The
// times are the best of five runs.
func TestAnalysesScale(t *testing.T) {
	if testing.Short() {
		t.Skip("times large programs")
	}
	const n = 512
	axes := append(slices.Clone(adversarialAxes), scaleAxes[2]) // nest
	for _, ax := range axes {
		small := timeAnalyses(t, ax.name, []byte(ax.gen(n)), 5)
		large := timeAnalyses(t, ax.name, []byte(ax.gen(8*n)), 5)
		for _, c := range []struct {
			what     string
			from, to time.Duration
		}{
			{"verify", small.verify, large.verify},
			{"promote", small.promote, large.promote},
			{"verify in SSA form", small.verifySSA, large.verifySSA},
		} {
			r := float64(c.to) / float64(max(c.from, time.Microsecond))
			t.Logf("%s: %s: %v at n=%d, %v at n=%d: %.1f×", ax.name, c.what, c.from, n, c.to, 8*n, r)
			if r >= 32 {
				t.Errorf("%s: %s grows %.1f× from n=%d to %d, want under 32×", ax.name, c.what, r, n, 8*n)
			}
		}
	}
}

// TestOptimizedIRVerified checks that emitProgram still verifies what the
// passes produce, now that only the promoted functions are verified again
// and EmitWith verifies nothing: a pass that breaks a function is an error.
func TestOptimizedIRVerified(t *testing.T) {
	saved := buildPasses
	defer func() { buildPasses = saved }()
	buildPasses = append(slices.Clone(saved), func(f *ir.Func) (bool, error) {
		b := f.Blocks[len(f.Blocks)-1]
		b.Instrs = b.Instrs[:len(b.Instrs)-1] // drop the terminator
		return true, nil
	})
	prog, ds := compile([]source{{"p.go", []byte("package main\n\nfunc main() { println(1) }\n")}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, err := emitProgram(prog); err == nil || !strings.Contains(err.Error(), "malformed IR after optimization") {
		t.Errorf("broken pass: %v, want malformed IR after optimization", err)
	}
}
