package main

import (
	"bytes"
	"errors"
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
			if diag.ExitCode(ds) != diag.ExitInvalid || !strings.Contains(ds[0].Message, "overflow") {
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
	for name, r := range want {
		// A panicking program is compared by the panic rule.
		if q, ok, err := panicPrefix(r); err != nil {
			t.Errorf("%s: gc output does not fit the panic rule: %v\n%+v", name, err, r)
		} else if ok {
			want[name] = result{stderr: q, exit: 2}
		}
	}
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
	// Errors are grouped under a "# bad/NAME" header per package. gc says
	// "overflows" for a constant that does not fit int, and "constant
	// multiplication overflow" for one beyond its untyped precision limit
	// (seen 2026-10-29 for a generated product of many large literals).
	for _, part := range strings.Split(string(out), "# bad/")[1:] {
		name, body, _ := strings.Cut(part, "\n")
		if progs[name] != nil && strings.Contains(body, "overflow") {
			delete(progs, name)
		}
	}
	if len(progs) > 0 {
		t.Errorf("gc did not reject %d programs Aether classified as invalid\n%s", len(progs), out)
	}
}

// panicPrefix applies the panic comparison rule (SEMANTICS.md, V-3) to a
// gc-built binary's result. A result is a panic if its stderr contains the
// text "panic: ". Then Q is the exact stderr up to and including the first
// newline after its first occurrence, the line from there must be
// "panic: runtime error: " and a message, stdout must be empty, and the
// exit status 2; Q is what Aether must write, with nothing after it. The
// traceback after Q is not compared. Programs print only integers and
// bool, so earlier output cannot contain the text.
func panicPrefix(gc result) (q string, ok bool, err error) {
	i := strings.Index(gc.stderr, "panic: ")
	if i < 0 {
		return "", false, nil
	}
	line, _, found := strings.Cut(gc.stderr[i:], "\n")
	switch {
	case !found:
		return "", false, errors.New("no newline after the panic message")
	case !strings.HasPrefix(line, "panic: runtime error: ") || len(line) == len("panic: runtime error: "):
		return "", false, fmt.Errorf("panic line %q is not a run-time error", line)
	case gc.exit != 2:
		return "", false, fmt.Errorf("exit status %d, want 2", gc.exit)
	case gc.stdout != "":
		return "", false, fmt.Errorf("stdout %q, want none", gc.stdout)
	}
	return gc.stderr[:i+len(line)+1], true, nil
}

func TestPanicPrefix(t *testing.T) {
	const tb = "\ngoroutine 1 [running]:\nmain.main()\n\t/x/main.go:6 +0x1d\nexit status 2\n"
	good := []struct{ stderr, q string }{
		{"7\npanic: runtime error: integer divide by zero\n" + tb, "7\npanic: runtime error: integer divide by zero\n"},
		{"1panic: runtime error: negative shift amount\n" + tb, "1panic: runtime error: negative shift amount\n"},
		{"panic: runtime error: integer divide by zero\n", "panic: runtime error: integer divide by zero\n"},
	}
	for _, tt := range good {
		if q, ok, err := panicPrefix(result{stderr: tt.stderr, exit: 2}); err != nil || !ok || q != tt.q {
			t.Errorf("%q: %q, %v, %v; want %q", tt.stderr, q, ok, err, tt.q)
		}
	}
	if _, ok, err := panicPrefix(result{stderr: "1 2\n"}); ok || err != nil {
		t.Errorf("no panic: %v, %v", ok, err)
	}
	bad := []result{
		{stderr: "panic: runtime error: integer divide by zero\n" + tb, exit: 1},
		{stderr: "panic: runtime error: integer divide by zero\n", exit: 2, stdout: "x"},
		{stderr: "panic: runtime error: integer divide by zero", exit: 2},
		{stderr: "panic: 1\n" + tb, exit: 2},
		{stderr: "panic: runtime error: \n", exit: 2},
	}
	for _, r := range bad {
		if q, ok, err := panicPrefix(r); err == nil {
			t.Errorf("%+v accepted: %q, %v", r, q, ok)
		}
	}
}

// TestGlobalReadOrder pins Aether's choice where the order of evaluation is
// unspecified (spec §Order of evaluation; SEMANTICS.md §Evaluation order):
// a global read as an operand before a call that assigns it is read first,
// left to right, in the interpreter and natively. The program is not
// compared with gc, which may choose differently: go1.26.1 reads x after
// the call in each case and prints "10 2", "12", "12", "1 2" (observed
// 2026-10-30). The last line reads a local copy, so both orders agree.
func TestGlobalReadOrder(t *testing.T) {
	src := []byte(`package main

var x = 1

func f() int { x = 10; return 2 }

func main() {
	println(x, f())
	x = 1
	println(x + f())
	x = 1
	x += f()
	println(x)
	x = 1
	y := x
	println(y, f())
}
`)
	want := result{stderr: "1 2\n3\n3\n1 2\n"}
	got, ds, err := interpret("order.go", src)
	if len(ds) > 0 || err != nil || got != want {
		t.Errorf("interpreter: %+v %v %v, want %+v", got, ds, err, want)
	}
	requireTools(t)
	exe := filepath.Join(t.TempDir(), "order")
	if ds, err := build([]source{{"order.go", src}}, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
		t.Fatalf("not built: %v %v", ds, err)
	}
	if got := runExe(t, exe); got != want {
		t.Errorf("native: %+v, want %+v", got, want)
	}
}
