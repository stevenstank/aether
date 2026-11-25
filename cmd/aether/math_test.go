package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
)

// TestMathExpLog checks math.Exp and math.Log, gc's amd64 assembly in Go
// (D-053), and what is built on them, against gc bit for bit: the program
// testdata/mathprog/explog.go prints the bits of edge cases (NaNs,
// infinities, signed zeros, subnormals, overflow, underflow) and hashes of
// the results for generated inputs, natively with and without -O0 and in
// the interpreter. Each runs as gc decides on this machine, and with
// GODEBUG=cpu.fma=off, which makes gc's Exp take its other path; on a
// machine without FMA both are that path.
func TestMathExpLog(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	src, err := os.ReadFile(filepath.Join("testdata", "mathprog", "explog.go"))
	if err != nil {
		t.Fatal(err)
	}
	gcBin := gcBuild(t, gobin, map[string][]byte{"explog": src})
	dir := t.TempDir()
	for _, o0 := range []bool{false, true} {
		args := []string{"build", "-o", filepath.Join(dir, osExe("explog", o0))}
		if o0 {
			args = append(args, "-O0")
		}
		if stderr, code := runAether(append(args, filepath.Join("testdata", "mathprog", "explog.go"))...); code != 0 {
			t.Fatalf("aether %v: exit %d\n%s", args, code, stderr)
		}
	}
	prog, ds := compile([]source{{"explog.go", src}})
	if len(ds) > 0 {
		t.Fatalf("not compiled: %v", ds)
	}
	for _, env := range [][]string{{}, {"GODEBUG=cpu.fma=off"}} {
		name := strings.Join(env, ",")
		t.Run("env="+name, func(t *testing.T) {
			run := func(path, n string) result {
				return osExec(t, path, dir, []string{n}, env)
			}
			want := run(filepath.Join(gcBin, "explog"), "200000")
			if want.exit != 0 || !strings.Contains(want.stdout, "positive exp") {
				t.Fatalf("gc: %+v", want)
			}
			for _, o0 := range []bool{false, true} {
				if got := run(filepath.Join(dir, osExe("explog", o0)), "200000"); got != want {
					t.Errorf("-O0 %v: aether %+v\ngc %+v", o0, got, want)
				}
			}
			small := run(filepath.Join(gcBin, "explog"), "300")
			var stdout, stderr bytes.Buffer
			code, err := interp.RunOptions(prog, &stderr, interp.Options{Stdout: &stdout,
				Args: []string{"explog", "300"}, Env: env, Steps: interpSteps})
			if err != nil {
				t.Fatalf("interpreter: %v", err)
			}
			if got := (result{stdout.String(), stderr.String(), code}); got != small {
				t.Errorf("interpreter %+v\ngc %+v", got, small)
			}
		})
	}
}

// TestMathRandGodebug checks math/rand's top-level functions under the
// GODEBUG settings that make them deterministic, through Aether's
// internal/godebug (D-053), against gc: natively and in the interpreter.
// testdata/std/mathrand.go is the same program without GODEBUG.
func TestMathRandGodebug(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	path := filepath.Join("testdata", "mathprog", "rand.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gcBin := gcBuild(t, gobin, map[string][]byte{"rand": src})
	dir := t.TempDir()
	exe := filepath.Join(dir, "rand")
	if stderr, code := runAether("build", "-o", exe, path); code != 0 {
		t.Fatalf("aether build: exit %d\n%s", code, stderr)
	}
	prog, ds := compile([]source{{"rand.go", src}})
	if len(ds) > 0 {
		t.Fatalf("not compiled: %v", ds)
	}
	for _, env := range [][]string{{"GODEBUG=randautoseed=0"}, {"GODEBUG=randseednop=0"},
		{"GODEBUG=x=1,randseednop=1,randseednop=0"}, {"GODEBUG=randautoseed=0,randseednop=0"}} {
		// One GODEBUG each: os/exec keeps only the last of an environment
		// variable given twice.
		t.Run(strings.Join(env, ";"), func(t *testing.T) {
			want := osExec(t, filepath.Join(gcBin, "rand"), dir, nil, env)
			if got := osExec(t, exe, dir, nil, env); got != want {
				t.Errorf("aether %+v\ngc %+v", got, want)
			}
			var stdout, stderr bytes.Buffer
			code, err := interp.RunOptions(prog, &stderr, interp.Options{Stdout: &stdout, Args: []string{"rand"}, Env: env, Steps: interpSteps})
			if err != nil {
				t.Fatalf("interpreter: %v", err)
			}
			if got := (result{stdout.String(), stderr.String(), code}); got != want {
				t.Errorf("interpreter %+v\ngc %+v", got, want)
			}
		})
	}
}
