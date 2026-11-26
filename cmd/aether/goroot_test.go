package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/build/constraint"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/toolchain"
)

// gorootDirs are the directories of GOROOT/test that gc's test driver,
// cmd/internal/testdir, reads (its dirs variable in go1.26.1).
var gorootDirs = []string{".", "ken", "chan", "interface", "internal/runtime/sys", "syntax", "dwarf", "fixedbugs", "codegen", "abi", "typeparam", "typeparam/mdempsky", "arenas", "simd"}

// gorootRunTimeout bounds one run of a conformance test. testdir sets no
// limit on "run" tests; a run this long is classified as a bug.
const gorootRunTimeout = 60 * time.Second

// TestGorootRun is the conformance harness of D-049. It builds every test of
// GOROOT/test whose execution recipe is exactly "run", runs it as testdir
// does, and classifies it:
//
//   - skip: its build constraints exclude linux/amd64 (testdir skips it);
//   - pass: exit status 0, and stdout and stderr together equal the .out
//     file, or are empty without one;
//   - unsupported FEATURE: aether build rejected it, FEATURE being the
//     feature of its first diagnostic;
//   - bug KIND: anything else. KIND is invalid or internal for a build
//     that failed with that class, and output, exit N, signal, or timeout
//     for a run that failed.
//
// The classes are compared with testdata/goroot/run.golden, which ends with
// the counts: the published pass rate. -update rewrites it.
func TestGorootRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs about a thousand programs")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	for _, tool := range []string{"as", "ld"} {
		if _, err := toolchain.Find(tool, nil); err != nil {
			t.Skipf("GNU %s not found: %v", tool, err)
		}
	}
	// The toolchain only locates the test tree (D-049).
	out, err := exec.Command(gobin, "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	testDir := filepath.Join(strings.TrimSpace(string(out)), "test")
	names := gorootRunTests(t, testDir)
	if len(names) == 0 {
		t.Fatalf("no run tests in %s", testDir)
	}

	exeDir := t.TempDir()
	lines := make([]string, len(names))
	work := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range work {
				lines[i] = names[i] + " " + classifyGorootTest(t, testDir, names[i], filepath.Join(exeDir, fmt.Sprint(i)))
			}
		})
	}
	for i := range names {
		work <- i
	}
	close(work)
	wg.Wait()

	counts := map[string]int{}
	for _, l := range lines {
		counts[strings.Fields(l)[1]]++
	}
	run := len(names) - counts["skip"]
	got := strings.Join(lines, "\n") + "\n" + fmt.Sprintf(
		"# %d tests, %d skipped, %d run: %d pass (%.1f%%), %d unsupported, %d bug\n",
		len(names), counts["skip"], run, counts["pass"], 100*float64(counts["pass"])/float64(run),
		counts["unsupported"], counts["bug"])
	t.Log(strings.TrimSpace(got[strings.LastIndex(got[:len(got)-1], "\n")+1:]))
	golden := filepath.Join("testdata", "goroot", "run.golden")
	if *update {
		writeFile(t, golden, []byte(got))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("results differ from %s (rerun with -update and review the diff):\n%s", golden, lineDiff(string(want), got))
	}
}

// gorootRunTests returns the tests of gorootDirs whose recipe is exactly
// "run": the first non-empty line that is not a build constraint, without
// its "//" (testdir's run method).
func gorootRunTests(t *testing.T, testDir string) []string {
	var names []string
	for _, dir := range gorootDirs {
		files, err := filepath.Glob(filepath.Join(testDir, dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var action string
			for rest := string(src); action == "" && rest != ""; {
				var line string
				line, rest, _ = strings.Cut(rest, "\n")
				if constraint.IsGoBuild(line) || constraint.IsPlusBuild(line) {
					continue
				}
				action = strings.TrimSpace(strings.TrimPrefix(line, "//"))
			}
			if action == "run" {
				names = append(names, filepath.ToSlash(filepath.Join(dir, filepath.Base(path))))
			}
		}
	}
	slices.Sort(names)
	return names
}

// classifyGorootTest builds and runs the test name and returns its class.
func classifyGorootTest(t *testing.T, testDir, name, exe string) string {
	path := filepath.Join(testDir, name)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Error(err)
		return "bug unreadable"
	}
	header, _, ok := strings.Cut(string(src), "\npackage")
	if ok && !gorootShouldTest(header) {
		return "skip"
	}
	ds, err := build([]source{{name: path, data: src}}, buildOptions{out: exe}, io.Discard)
	if err != nil && len(ds) == 0 {
		t.Errorf("%s: %v", name, err)
		return "bug build-error"
	}
	if len(ds) > 0 {
		diag.Sort(ds)
		switch ds[0].Class {
		case diag.Unsupported:
			return "unsupported " + ds[0].Feature
		case diag.Tool:
			t.Errorf("%s: %s", name, ds[0])
		}
		return "bug " + ds[0].Class.String()
	}
	defer os.Remove(exe)
	want, err := os.ReadFile(strings.TrimSuffix(path, ".go") + ".out")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Error(err)
	}
	got, kind := runGorootTest(t, testDir, exe)
	switch {
	case kind != "":
		return "bug " + kind
	case !bytes.Equal(got, want):
		return "bug output"
	}
	return "pass"
}

// runGorootTest runs exe in testDir, as testdir runs a test, and returns
// its stdout and stderr together, and "" or why the run failed: exit N,
// signal, or timeout.
func runGorootTest(t *testing.T, testDir, exe string) ([]byte, string) {
	ctx, cancel := context.WithTimeout(context.Background(), gorootRunTimeout)
	defer cancel()
	var out bytes.Buffer
	var cmd *exec.Cmd
	for try := 0; ; try++ {
		cmd = exec.CommandContext(ctx, exe)
		cmd.Dir = testDir
		// The go command testdir runs a test with exports GOROOT, which
		// Aether's runtime.GOROOT returns (v2 evaluation).
		cmd.Env = append(os.Environ(), "PWD="+testDir, "GOROOT="+filepath.Dir(testDir))
		cmd.Stdout, cmd.Stderr = &out, &out
		cmd.WaitDelay = waitDelay
		err := cmd.Start()
		if err == nil {
			break
		}
		// See runExe.
		if !errors.Is(err, syscall.ETXTBSY) || try == 50 {
			t.Errorf("%s: %v", exe, err)
			return nil, "start"
		}
		time.Sleep(10 * time.Millisecond)
	}
	err := cmd.Wait()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, "timeout"
	case err == nil:
		return out.Bytes(), ""
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() < 0 {
		return nil, "signal"
	}
	return nil, fmt.Sprintf("exit %d", exit.ExitCode())
}

// gorootShouldTest evaluates the build constraints of a test's header as
// testdir's shouldTest does on linux/amd64 with cgo enabled.
func gorootShouldTest(header string) bool {
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, "package ") {
			break
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			continue
		}
		ok := expr.Eval(func(tag string) bool {
			switch tag {
			case "linux", "amd64", "gc", "cgo", "test_run":
				return true
			}
			if v, ok := strings.CutPrefix(tag, "go1."); ok {
				var n int
				_, err := fmt.Sscanf(v, "%d", &n)
				return err == nil && fmt.Sprint(n) == v && n >= 1 && n <= 26
			}
			return false
		})
		if !ok {
			return false
		}
	}
	return true
}

// lineDiff lists the lines of want and got that differ, by position: enough
// to review a changed classification.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := range max(len(w), len(g)) {
		var x, y string
		if i < len(w) {
			x = w[i]
		}
		if i < len(g) {
			y = g[i]
		}
		if x != y {
			fmt.Fprintf(&b, "-%s\n+%s\n", x, y)
		}
	}
	return b.String()
}
