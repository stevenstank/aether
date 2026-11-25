package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
)

// osCase is one run of a program of testdata/osprog: its arguments, its
// exact environment, ROOT and PWD added as the run's directory if root.
type osCase struct {
	prog string
	args []string
	env  []string
	root bool
}

var osCases = []osCase{
	{prog: "files", args: []string{"in.txt"}},
	{prog: "env", env: []string{"A=1", "B=", "PATHS=x:y"}},
	{prog: "env", env: []string{"A=1", "HOME=/home/x", "TMPDIR=/var/t"}},
	{prog: "wd", root: true},
	{prog: "stdclose"},
}

// TestOSPrograms checks Aether's os files and environment (D-051) against
// gc: each case runs with gc, in the interpreter, and natively with and
// without -O0, each in a fresh directory holding in.txt, and must print
// the same, exit the same, and leave the directory the same.
func TestOSPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	for _, tool := range []string{"as", "ld"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	progs := map[string][]byte{}
	for _, c := range osCases {
		src, err := os.ReadFile(filepath.Join("testdata", "osprog", c.prog+".go"))
		if err != nil {
			t.Fatal(err)
		}
		progs[c.prog] = src
	}
	gcBin := gcBuild(t, gobin, progs)
	aeBin := t.TempDir()
	for name := range progs {
		for _, o0 := range []bool{false, true} {
			args := []string{"build", "-o", filepath.Join(aeBin, osExe(name, o0))}
			if o0 {
				args = append(args, "-O0")
			}
			args = append(args, filepath.Join("testdata", "osprog", name+".go"))
			if stderr, code := runAether(args...); code != 0 {
				t.Fatalf("aether %v: exit %d\n%s", args, code, stderr)
			}
		}
	}
	for i, c := range osCases {
		t.Run(fmt.Sprintf("%s-%d", c.prog, i), func(t *testing.T) {
			want, wantTree := osRun(t, c, func(dir string, env []string) result {
				return osExec(t, filepath.Join(gcBin, c.prog), dir, c.args, env)
			})
			if strings.Contains(want.stderr, "panic") {
				t.Fatalf("gc panicked: %+v", want)
			}
			got, tree := osRun(t, c, func(dir string, env []string) result {
				prog, ds := compile([]source{{c.prog + ".go", progs[c.prog]}})
				if len(ds) > 0 {
					t.Fatalf("not compiled: %v", ds)
				}
				var stdout, stderr bytes.Buffer
				code, err := interp.RunOptions(prog, &stderr, interp.Options{
					Stdout: &stdout, Args: append([]string{c.prog}, c.args...), Env: env, Dir: dir, Steps: interpSteps})
				if err != nil {
					t.Fatalf("interpreter: %v", err)
				}
				return result{stdout.String(), stderr.String(), code}
			})
			if got != want || tree != wantTree {
				t.Errorf("interpreter %+v\n%s\ngc %+v\n%s", got, tree, want, wantTree)
			}
			for _, o0 := range []bool{false, true} {
				got, tree := osRun(t, c, func(dir string, env []string) result {
					return osExec(t, filepath.Join(aeBin, osExe(c.prog, o0)), dir, c.args, env)
				})
				if got != want || tree != wantTree {
					t.Errorf("-O0 %v: aether %+v\n%s\ngc %+v\n%s", o0, got, tree, want, wantTree)
				}
			}
		})
	}
}

func osExe(name string, o0 bool) string {
	if o0 {
		return name + "-O0"
	}
	return name
}

// osRun makes a fresh directory holding in.txt, runs c in it with run, and
// returns the result and a listing of the directory after the run: each
// path, mode, and content.
func osRun(t *testing.T, c osCase, run func(dir string, env []string) result) (result, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 40)
	if err := os.WriteFile(filepath.Join(dir, "in.txt"), []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	env := c.env
	if env == nil {
		env = []string{}
	}
	if c.root {
		env = append(env, "ROOT="+dir, "PWD="+dir)
	}
	r := run(dir, env)
	var tree strings.Builder
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fmt.Fprintf(&tree, "%s %v", rel, info.Mode())
		if info.Mode().IsRegular() && rel != "in.txt" {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&tree, " %q", b)
		}
		tree.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, tree.String()
}

// osExec runs the executable path in dir with args and exactly env.
func osExec(t *testing.T, path, dir string, args, env []string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = dir, env, waitDelay
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := result{}
	if err := cmd.Run(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() < 0 {
			t.Fatalf("%s: %v", path, err)
		}
		r.exit = exit.ExitCode()
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}
