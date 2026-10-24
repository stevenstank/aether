package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/interp"
)

// result is the observable behavior of a program run.
type result struct {
	stdout, stderr string
	exit           int
}

// interpret compiles src and runs its IR with the interpreter: the test-only
// equivalent of an "aether run-ir" command. An S1 program never writes to
// stdout and always exits 0.
func interpret(name string, src []byte) (result, []diag.Diagnostic, error) {
	prog, ds := compile([]source{{name, src}})
	if len(ds) > 0 {
		return result{}, ds, nil
	}
	var stderr bytes.Buffer
	err := interp.Run(prog, &stderr)
	return result{stderr: stderr.String()}, nil, err
}

// gcRun builds every program with gc in one module outside this one, with
// the installed toolchain only (go build, never go run: D-014), runs each
// binary, and returns the results by name.
func gcRun(t *testing.T, gobin string, progs map[string][]byte) map[string]result {
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
	results := map[string]result{}
	for name := range progs {
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(filepath.Join(bin, name))
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		r := result{}
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("%s: %v", name, err)
			}
			r.exit = exit.ExitCode()
		}
		r.stdout, r.stderr = stdout.String(), stderr.String()
		results[name] = r
	}
	return results
}

// generatedPrograms is how many generated programs the differential test
// requires gc and the interpreter to agree on.
const generatedPrograms = 200

// TestInterpAgainstGc is the Phase 2 differential test: for every corpus
// program expected to run and for generated S1 programs, the interpreter's
// stdout, stderr, and exit status must equal those of the gc-built binary.
// gc is only the oracle: the interpreter's results come from Aether's own
// lowering and IR, never from gc.
func TestInterpAgainstGc(t *testing.T) {
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
	rejected := 0
	for seed := uint64(1); len(progs) < corpusRun+generatedPrograms; seed++ {
		if seed > 3*generatedPrograms {
			t.Fatalf("too many generated programs rejected: %d", rejected)
		}
		src := []byte(generate(seed))
		if _, ds, _ := interpret("gen.go", src); len(ds) > 0 {
			// Only an overflowing constant expression is expected.
			if diag.ExitCode(ds) != diag.ExitInvalid {
				t.Errorf("generated program %d rejected:\n%s\n%s", seed, ds[0], src)
			}
			rejected++
			continue
		}
		progs[fmt.Sprintf("gen%04d", seed)] = src
	}
	t.Logf("%d corpus programs, %d generated (%d generated programs with an overflowing constant skipped)",
		corpusRun, generatedPrograms, rejected)

	want := gcRun(t, gobin, progs)
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
	}
}
