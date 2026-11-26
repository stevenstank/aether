package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNativePrograms builds each program of testdata/net, which use
// packages net and net/http over Aether's network poller (D-056, D-057),
// and of testdata/native, which use weak pointers and cleanups (D-057),
// with gc and with Aether, with and without -O0, and compares what they
// do, also under the collector's stress mode. They run natively only: the
// interpreter makes no socket system calls (D-051) and frees nothing.
func TestNativePrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	var files []string
	for _, dir := range []string{"net", "native"} {
		fs, err := filepath.Glob(filepath.Join("testdata", dir, "*.go"))
		if err != nil || len(fs) == 0 {
			t.Fatal(dir, fs, err)
		}
		files = append(files, fs...)
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
			if want[name].exit != 0 {
				t.Fatalf("gc: %+v", want[name])
			}
			for _, o0 := range []bool{false, true} {
				out := filepath.Join(t.TempDir(), name)
				args := []string{"build", "-o", out}
				if o0 {
					args = append(args, "-O0")
				}
				if stderr, code := runAether(append(args, f)...); code != 0 {
					t.Fatalf("aether %v: exit %d\n%s", args, code, stderr)
				}
				if got := runExe(t, out); got != want[name] {
					t.Errorf("-O0 %v: aether %+v, gc %+v", o0, got, want[name])
				}
				if got := runExeEnv(t, out, gcStressEnv); got != want[name] {
					t.Errorf("-O0 %v, collector stress: aether %+v, gc %+v", o0, got, want[name])
				}
			}
		})
	}
}
