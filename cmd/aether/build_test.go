package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/diag"
)

// requireTools skips the test unless executables can be built and run here.
func requireTools(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("aether build needs a linux/amd64 host")
	}
	for _, name := range []string{"as", "ld"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("GNU %s not found: %v", name, err)
		}
	}
}

func runAether(args ...string) (stderr string, code int) {
	var stdout, errBuf bytes.Buffer
	code = run(args, &stdout, &errBuf)
	if stdout.Len() != 0 {
		return "unexpected stdout: " + stdout.String(), -1
	}
	return errBuf.String(), code
}

// noFile fails if path exists.
func noFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("%s was created", path)
	}
}

// corpusFile returns the path of a corpus file by name.
func corpusFile(name string) string { return filepath.Join("testdata", "check", name+".go") }

func TestBuildUsage(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	src := corpusFile("s1_03_milestone1")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no output", []string{src}, "no output file: use -o FILE"},
		{"no input", []string{"-o", out}, "no input file"},
		{"unknown flag", []string{"-z", "-o", out, src}, "flag provided but not defined: -z"},
		{"flag after input", []string{src, "-o", out}, "unknown flag -o"},
		{"not a .go file", []string{"-o", out, "prog.c"}, "prog.c is not a .go file"},
		{"output is the input", []string{"-o", src, src}, "is the input file"},
		{"output is a directory", []string{"-o", dir, src}, "is not a regular file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr, code := runAether(append([]string{"build"}, tt.args...)...)
			if code != diag.ExitUsage || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, stderr %q; want exit 2 and %q", code, stderr, tt.want)
			}
			noFile(t, out)
		})
	}
}

// TestBuildCorpus builds every corpus program expected to run, runs the
// executable, and requires the interpreter's behavior: the output on stderr,
// nothing on stdout, and exit status 0.
func TestBuildCorpus(t *testing.T) {
	requireTools(t)
	for _, path := range corpus(t) {
		if readExpectation(t, path).class != "run" {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, ds, err := interpret(path, src)
			if len(ds) > 0 || err != nil {
				t.Fatal(ds, err)
			}
			out := filepath.Join(t.TempDir(), "out")
			if stderr, code := runAether("build", "-o", out, path); code != 0 || stderr != "" {
				t.Fatalf("build: exit %d\n%s", code, stderr)
			}
			if fi, err := os.Stat(out); err != nil || fi.Mode().Perm() != 0o755 {
				t.Fatalf("output: %v %v", fi, err)
			}
			if got := runExe(t, out); got != want {
				t.Errorf("native %+v, interpreter %+v", got, want)
			}
		})
	}
}

// runExe runs an executable and returns what it did.
//
// Starting it can fail with ETXTBSY if the file was just written by this
// process: a child that a parallel test forks holds a copy of the writing
// descriptor until it execs (golang/go#22315). The start is retried then.
func runExe(t *testing.T, path string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	var cmd *exec.Cmd
	for try := 0; ; try++ {
		cmd = exec.CommandContext(ctx, path)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Start()
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.ETXTBSY) || try == 50 {
			t.Fatalf("%s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	r := result{}
	if err := cmd.Wait(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() < 0 {
			t.Fatalf("%s: %v", path, err) // a timeout or a signal
		}
		r.exit = exit.ExitCode()
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// TestBuildRejects checks that invalid and unsupported programs fail with
// their class, before any tool runs, without creating the output or
// changing an existing one.
func TestBuildRejects(t *testing.T) {
	tests := []struct {
		file string
		code int
	}{
		{"s1_n01_constant_overflow", diag.ExitInvalid},
		{"s1_n05_missing_main", diag.ExitInvalid},
		{"unsupported_operator_div", diag.ExitUnsupported},
		{"unsupported_import", diag.ExitUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			// Tools that would fail if run: rejection must come first.
			t.Setenv("PATH", t.TempDir())
			dir := t.TempDir()
			out := filepath.Join(dir, "out")
			stderr, code := runAether("build", "-x", "-o", out, corpusFile(tt.file))
			if code != tt.code || !strings.HasPrefix(stderr, corpusFile(tt.file)+":") {
				t.Errorf("exit %d, want %d\n%s", code, tt.code, stderr)
			}
			noFile(t, out)
			existing := []byte("keep me\n")
			if err := os.WriteFile(out, existing, 0o600); err != nil {
				t.Fatal(err)
			}
			runAether("build", "-o", out, corpusFile(tt.file))
			checkUnchanged(t, out, existing)
		})
	}
}

func checkUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("%s changed: %q %v", path, got, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("%s mode changed: %v %v", path, fi, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("stray files next to the output: %v", entries)
	}
}

// fakeTool installs a shell script called name in dir.
func fakeTool(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// linkTool makes dir/name a symbolic link to the real tool.
func linkTool(t *testing.T, dir, name string) {
	t.Helper()
	real, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

const (
	asVersion = "if [ \"$1\" = --version ]; then echo 'GNU assembler 2.42'; echo \"configured for a target of \\`x86_64-linux-gnu'.\"; exit 0; fi\n"
	ldVersion = "if [ \"$1\" = -V ]; then echo 'GNU ld 2.42'; echo '   elf_x86_64'; exit 0; fi\n"
)

// TestBuildToolFailures checks that a missing, unidentified, or failing
// tool gives a tool diagnostic naming it, exit status 5, and leaves the
// output path as it was.
func TestBuildToolFailures(t *testing.T) {
	requireTools(t)
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string
	}{
		{"no tools", func(t *testing.T, dir string) {}, "tool: as: not found on PATH"},
		{"no ld", func(t *testing.T, dir string) { linkTool(t, dir, "as") }, "tool: ld: not found on PATH"},
		{"as is not GNU", func(t *testing.T, dir string) {
			fakeTool(t, dir, "as", "echo 'clang version 18'\n")
			linkTool(t, dir, "ld")
		}, "not GNU as"},
		{"as fails", func(t *testing.T, dir string) {
			fakeTool(t, dir, "as", asVersion+"echo 'Error: simulated' >&2\nexit 1\n")
			linkTool(t, dir, "ld")
		}, "exit status 1: Error: simulated"},
		{"ld fails", func(t *testing.T, dir string) {
			linkTool(t, dir, "as")
			fakeTool(t, dir, "ld", ldVersion+"echo 'simulated link failure' >&2\nexit 1\n")
		}, "exit status 1: simulated link failure"},
		{"ld succeeds without output", func(t *testing.T, dir string) {
			linkTool(t, dir, "as")
			fakeTool(t, dir, "ld", ldVersion+"exit 0\n")
		}, "succeeded without writing a.out"},
	}
	src := corpusFile("s1_03_milestone1")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := t.TempDir()
			tt.setup(t, tools)
			t.Setenv("PATH", tools)
			out := filepath.Join(t.TempDir(), "out")
			stderr, code := runAether("build", "-o", out, src)
			noFile(t, out)
			if code != diag.ExitTool || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, stderr %q; want exit 5 and %q", code, stderr, tt.want)
			}
			existing := []byte("keep me\n")
			if err := os.WriteFile(out, existing, 0o600); err != nil {
				t.Fatal(err)
			}
			runAether("build", "-o", out, src)
			checkUnchanged(t, out, existing)
		})
	}
}

// TestBuildWorkAndLog checks -x and -work, the work directory's contents,
// that the work directory is removed without -work, and that a successful
// build replaces an existing regular file.
func TestBuildWorkAndLog(t *testing.T) {
	requireTools(t)
	src := corpusFile("s1_03_milestone1")
	out := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(out, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr, code := runAether("build", "-x", "-work", "-o", out, src)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	work := strings.TrimPrefix(firstLine(stderr, "WORK="), "WORK=")
	if work == "" {
		t.Fatalf("no WORK= line:\n%s", stderr)
	}
	defer os.RemoveAll(work)
	for _, f := range []string{"prog.s", "prog.o", "print.s", "print.o", "start.s", "start.o", "a.out"} {
		if _, err := os.Stat(filepath.Join(work, f)); err != nil {
			t.Errorf("work directory: %v", err)
		}
	}
	for _, cmd := range []string{
		"cd " + work,
		" --64 -o prog.o prog.s", " --64 -o print.o print.s", " --64 -o start.o start.s",
		" -static -nostdlib -e _start -o a.out prog.o print.o start.o",
		"# as version: GNU assembler", "# ld version: GNU ld",
	} {
		if !strings.Contains(stderr, cmd) {
			t.Errorf("log lacks %q:\n%s", cmd, stderr)
		}
	}
	if r := runExe(t, out); r.stderr != "-6 42\n6 -9223372036854775808\n" || r.exit != 0 {
		t.Errorf("replaced output runs as %+v", r)
	}

	stderr, code = runAether("build", "-x", "-o", out, src)
	work = strings.TrimPrefix(firstLine(stderr, "cd "), "cd ")
	if code != 0 || work == "" {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Errorf("work directory %s not removed: %v", work, err)
	}
}

func firstLine(s, prefix string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// TestBuildOutputRefusals checks output paths that must be refused before
// anything is built: exit 2, and the path, its target, and the input are left
// exactly as they were. checkOutput runs before the build, so a change to the
// path during the build is not covered (ARCHITECTURE.md §8).
func TestBuildOutputRefusals(t *testing.T) {
	src := readFile(t, corpusFile("s1_03_milestone1"))
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) (in, out string)
		want  string
	}{
		{"symlink to an existing file", func(t *testing.T, dir string) (string, string) {
			writeFile(t, filepath.Join(dir, "target"), []byte("keep me\n"))
			mustSymlink(t, "target", filepath.Join(dir, "out"))
			return "", filepath.Join(dir, "out")
		}, "is not a regular file"},
		{"symlink to a missing file", func(t *testing.T, dir string) (string, string) {
			mustSymlink(t, "missing", filepath.Join(dir, "out"))
			return "", filepath.Join(dir, "out")
		}, "is not a regular file"},
		{"fifo", func(t *testing.T, dir string) (string, string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "out"), 0o600); err != nil {
				t.Skipf("mkfifo: %v", err)
			}
			return "", filepath.Join(dir, "out")
		}, "is not a regular file"},
		{"hard link to the input", func(t *testing.T, dir string) (string, string) {
			if err := os.Link(filepath.Join(dir, "in.go"), filepath.Join(dir, "out")); err != nil {
				t.Skipf("link: %v", err)
			}
			return "", filepath.Join(dir, "out")
		}, "is the input file"},
		{"input is a symlink to the output", func(t *testing.T, dir string) (string, string) {
			mustSymlink(t, "in.go", filepath.Join(dir, "link.go"))
			return filepath.Join(dir, "link.go"), filepath.Join(dir, "in.go")
		}, "is the input file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "in.go"), src)
			in, out := tt.setup(t, dir)
			if in == "" {
				in = filepath.Join(dir, "in.go")
			}
			before := snapshot(t, dir)
			stderr, code := runAether("build", "-o", out, in)
			if code != diag.ExitUsage || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, stderr %q; want exit 2 and %q", code, stderr, tt.want)
			}
			if after := snapshot(t, dir); after != before {
				t.Errorf("directory changed:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func mustSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// snapshot describes every entry of dir: name, type and permissions, link
// target, and contents of regular files.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s %v", e.Name(), fi.Mode())
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			fmt.Fprintf(&b, " -> %s", target)
		case fi.Mode().IsRegular():
			fmt.Fprintf(&b, " %q", readFile(t, path))
		}
		b.WriteString("; ")
	}
	return b.String()
}

// TestBuildCleanup checks that a failing assembler or linker leaves nothing
// in the temporary directory, and that -work keeps the work directory of a
// failed build.
func TestBuildCleanup(t *testing.T) {
	requireTools(t)
	src := corpusFile("s1_03_milestone1")
	for _, tool := range []string{"as", "ld"} {
		t.Run(tool+" fails", func(t *testing.T) {
			tools := t.TempDir()
			fakeTool(t, tools, tool, map[string]string{"as": asVersion, "ld": ldVersion}[tool]+"exit 1\n")
			linkTool(t, tools, map[string]string{"as": "ld", "ld": "as"}[tool])
			t.Setenv("PATH", tools)
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			out := filepath.Join(t.TempDir(), "out")
			if _, code := runAether("build", "-o", out, src); code != diag.ExitTool {
				t.Fatalf("exit %d, want 5", code)
			}
			if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
				t.Errorf("left in TMPDIR: %v", entries)
			}
			noFile(t, out)

			stderr, code := runAether("build", "-work", "-o", out, src)
			work := strings.TrimPrefix(firstLine(stderr, "WORK="), "WORK=")
			if code != diag.ExitTool || !strings.HasPrefix(work, tmp) {
				t.Fatalf("-work: exit %d, stderr %q", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(work, "prog.s")); err != nil {
				t.Errorf("-work did not keep the work directory: %v", err)
			}
			noFile(t, out)
		})
	}
}

// TestBuildFileFailures checks failures of Aether's own file operations
// after the program compiled: they exit 2, keep an existing output, and leave
// no temporary files.
func TestBuildFileFailures(t *testing.T) {
	requireTools(t)
	src := corpusFile("s1_03_milestone1")
	existing := []byte("keep me\n")

	t.Run("no work directory", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		out := filepath.Join(t.TempDir(), "out")
		writeFile(t, out, existing)
		if err := os.Chmod(out, 0o600); err != nil {
			t.Fatal(err)
		}
		stderr, code := runAether("build", "-o", out, src)
		if code != diag.ExitUsage || !strings.Contains(stderr, "aether build: ") {
			t.Errorf("exit %d, stderr %q; want exit 2", code, stderr)
		}
		checkUnchanged(t, out, existing)
	})

	t.Run("output directory not writable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write to a read-only directory")
		}
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		dir := t.TempDir()
		out := filepath.Join(dir, "out")
		writeFile(t, out, existing)
		if err := os.Chmod(out, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
		stderr, code := runAether("build", "-o", out, src)
		if code != diag.ExitUsage || !strings.Contains(stderr, "permission denied") {
			t.Errorf("exit %d, stderr %q; want exit 2 and permission denied", code, stderr)
		}
		checkUnchanged(t, out, existing)
		if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
			t.Errorf("left in TMPDIR: %v", entries)
		}
	})
}
