package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/toolchain"
)

// result is the observable behavior of a program run.
type result struct {
	stdout, stderr string
	exit           int
}

// interpret compiles src and runs its IR with the interpreter: the test-only
// equivalent of an "aether run-ir" command. The interpreter has no standard
// output (interp.Run takes only a stderr writer), so stdout is always empty.
func interpret(name string, src []byte) (result, []diag.Diagnostic, error) {
	prog, ds := compile([]source{{name, src}})
	if len(ds) > 0 {
		return result{}, ds, nil
	}
	var stderr bytes.Buffer
	code, err := interp.RunSteps(prog, &stderr, interpSteps)
	return result{stderr: stderr.String(), exit: code}, nil, err
}

// interpSteps is the interpreter's step budget in tests (D-023): far more
// than any test program needs, so a lowering bug that loops fails the test
// instead of hanging it.
const interpSteps = 100_000_000

// runTimeout bounds every run of a gc-built or Aether-built test program,
// so that a bug that loops fails the test instead of hanging the suite.
const runTimeout = 2 * time.Minute

// gcRun builds every program with gc (gcBuild), runs each binary, and
// returns the results by name.
func gcRun(t *testing.T, gobin string, progs map[string][]byte) map[string]result {
	t.Helper()
	bin := gcBuild(t, gobin, progs)
	results := map[string]result{}
	for name := range progs {
		results[name] = runExe(t, filepath.Join(bin, name))
	}
	return results
}

// gcBuild builds every program with gc in one module outside this one, with
// the installed toolchain only (go build, never go run: D-014), and returns
// the directory holding the binaries, named after the programs.
func gcBuild(t *testing.T, gobin string, progs map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module gen\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, src := range progs {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "main.go"), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "bin") + string(filepath.Separator)
	cmd := exec.Command(gobin, "build", "-o", bin, "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gc build failed: %v\n%s", err, out)
	}
	return bin
}

// generatedPrograms is how many generated programs the differential test
// requires gc and the interpreter to agree on.
const generatedPrograms = 200

// TestDifferentialAgainstGc is the three-way differential test (Phases 2
// and 3): for every corpus program expected to run and for generated S1
// programs, the stdout, stderr, and exit status of the interpreter and of the
// executable built by aether build must equal those of the gc-built binary.
// gc is only the oracle: Aether's results come from its own lowering, IR,
// backend, and runtime, never from gc. Without GNU as and ld, the native
// comparison is skipped with a logged message.
func TestDifferentialAgainstGc(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if readExpectation(t, path).class != "run" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	corpusRun := len(progs)
	rejected := map[string][]byte{}
	for seed := uint64(1); len(progs) < corpusRun+generatedPrograms; seed++ {
		if seed > 3*generatedPrograms {
			t.Fatalf("too many generated programs rejected: %d", len(rejected))
		}
		src := []byte(generate(seed))
		if _, ds, _ := interpret("gen.go", src); len(ds) > 0 {
			// Only an overflowing constant expression is expected, which
			// makes the program invalid Go; gc must agree (below).
			if diag.ExitCode(ds) != diag.ExitInvalid || !strings.Contains(ds[0].Message, "overflows") {
				t.Errorf("generated program %d rejected:\n%s\n%s", seed, ds[0], src)
			}
			rejected[fmt.Sprintf("bad%04d", seed)] = src
			continue
		}
		progs[fmt.Sprintf("gen%04d", seed)] = src
	}
	t.Logf("%d corpus programs, %d generated, %d generated programs invalid (overflowing constant)",
		corpusRun, generatedPrograms, len(rejected))
	gcRejects(t, gobin, rejected)

	want := gcRun(t, gobin, progs)
	native := true
	if _, err := toolchain.Find("as", nil); err != nil {
		native = false
		t.Logf("native comparison skipped: %v", err)
	} else if _, err := toolchain.Find("ld", nil); err != nil {
		native = false
		t.Logf("native comparison skipped: %v", err)
	}
	exeDir := t.TempDir()
	names := make([]string, 0, len(progs))
	for name := range progs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		got, ds, err := interpret(name+".go", progs[name])
		if len(ds) > 0 || err != nil {
			t.Errorf("%s: not run: %v %v", name, ds, err)
			continue
		}
		if got != want[name] {
			t.Errorf("%s: interpreter %+v, gc %+v\n%s", name, got, want[name], progs[name])
		}
		if !native {
			continue
		}
		exe := filepath.Join(exeDir, name)
		ds, err = build([]source{{name + ".go", progs[name]}}, buildOptions{out: exe}, io.Discard)
		if len(ds) > 0 || err != nil {
			t.Errorf("%s: not built: %v %v", name, ds, err)
			continue
		}
		if got := runExe(t, exe); got != want[name] {
			t.Errorf("%s: native %+v, gc %+v\n%s", name, got, want[name], progs[name])
		}
	}
	if native {
		t.Logf("all %d programs also built and run natively", len(names))
	}
}

// gcRejects checks that gc also rejects every program Aether classified as
// invalid: one go build of all of them must report each as a failing
// package with a constant overflow.
func gcRejects(t *testing.T, gobin string, progs map[string][]byte) {
	t.Helper()
	if len(progs) == 0 {
		return
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module bad\n\ngo 1.26\n"))
	for name, src := range progs {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, name, "main.go"), src)
	}
	cmd := exec.Command(gobin, "build", "-o", os.DevNull, "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("gc accepted programs Aether classified as invalid")
	}
	// Errors are grouped under a "# bad/NAME" header per package.
	for _, part := range strings.Split(string(out), "# bad/")[1:] {
		name, body, _ := strings.Cut(part, "\n")
		if progs[name] != nil && strings.Contains(body, "overflows") {
			delete(progs, name)
		}
	}
	if len(progs) > 0 {
		t.Errorf("gc did not reject %d programs Aether classified as invalid\n%s", len(progs), out)
	}
}
