package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
)

// reliabilityRuns is how many times TestReliability runs each native
// executable in each environment. More than one run looks for behavior
// that differs between runs of the same binary, such as a scheduling or
// collection fault that does not show every time.
var reliabilityRuns = flag.Int("reliability.runs", 1, "runs of each native executable in TestReliability")

// reliabilitySteps is the interpreter's step budget in TestReliability:
// ten times interpSteps, for programs that do real work.
const reliabilitySteps = 10 * interpSteps

// reliabilityModes are the ways TestReliability runs each native build:
// normally and under the collector's stress mode (D-046).
var reliabilityModes = []struct {
	name string
	env  []string
}{{"", nil}, {"gcstress", gcStressEnv}}

// TestReliability runs each program of testdata/reliability, larger
// programs that combine the supported features as real code does, built
// by gc, and requires the same behavior from Aether in every execution
// mode: the interpreter, and aether build with and without -O0, each run
// normally and under AETHER_GCSTRESS=1. A panic or fatal error is
// compared by the panic rule (panicPrefix).
func TestReliability(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	files, err := filepath.Glob(filepath.Join("testdata", "reliability", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatal(files, err)
	}
	progs := map[string][]byte{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(f), ".go")] = src
	}
	want := gcRun(t, gobin, progs)
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".go")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// These programs may print before they panic, so stdout is
			// compared whole and stderr by the panic rule.
			w := want[name]
			if q, ok, err := panicPrefix(result{stderr: w.stderr, exit: w.exit}); err != nil {
				t.Fatalf("gc output does not fit the panic rule: %v\n%+v", err, w)
			} else if ok {
				w.stderr = q
			}
			check := func(mode string, got result) {
				t.Helper()
				if got != w {
					t.Errorf("%s:\n%s", mode, resultDiff(w, got))
				}
			}
			prog, ds := compile([]source{{f, progs[name]}})
			if len(ds) > 0 {
				t.Fatalf("not compiled: %v", ds)
			}
			var stdout, stderr bytes.Buffer
			code, err := interp.RunOptions(prog, &stderr, interp.Options{Stdout: &stdout, Steps: reliabilitySteps})
			got := result{stdout.String(), stderr.String(), code}
			if err != nil {
				t.Fatalf("not interpreted: %v", err)
			}
			check("interpreter", got)
			for _, o0 := range []bool{false, true} {
				out := filepath.Join(t.TempDir(), name)
				args := []string{"build", "-o", out}
				build := "aether build"
				if o0 {
					args, build = append(args, "-O0"), build+" -O0"
				}
				if stderr, code := runAether(append(args, f)...); code != 0 {
					t.Fatalf("%s: exit %d\n%s", build, code, stderr)
				}
				for _, m := range reliabilityModes {
					mode := build
					if m.name != "" {
						mode += " " + m.name
					}
					for run := 1; run <= *reliabilityRuns; run++ {
						label := mode
						if *reliabilityRuns > 1 {
							label = fmt.Sprintf("%s, run %d", mode, run)
						}
						check(label, runExeEnv(t, out, m.env))
					}
				}
			}
		})
	}
}

// resultDiff describes how got differs from want, stream by stream, with
// lineDiff for the output.
func resultDiff(want, got result) string {
	var b strings.Builder
	if want.exit != got.exit {
		fmt.Fprintf(&b, "exit status %d, gc %d\n", got.exit, want.exit)
	}
	if want.stdout != got.stdout {
		fmt.Fprintf(&b, "stdout (- gc, + aether):\n%s", lineDiff(want.stdout, got.stdout))
	}
	if want.stderr != got.stderr {
		fmt.Fprintf(&b, "stderr (- gc, + aether):\n%s", lineDiff(want.stderr, got.stderr))
	}
	return b.String()
}
