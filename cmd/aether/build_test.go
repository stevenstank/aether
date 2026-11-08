package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/amd64"
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
		{"no other optimization level", []string{"-O1", "-o", out, src}, "flag provided but not defined: -O1"},
		{"-O0 after input", []string{"-o", out, src, "-O0"}, "unknown flag -O0"},
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
		cmd.WaitDelay = waitDelay
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
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("%s: killed after running for the %v timeout", path, runTimeout)
		}
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() < 0 {
			t.Fatalf("%s: %v", path, err) // a signal
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
		{"unsupported_defer", diag.ExitUnsupported},
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
	for _, f := range []string{"prog.s", "prog.o", "float.s", "float.o", "pow10.s", "pow10.o", "print.s", "print.o", "start.s", "start.o", "a.out"} {
		if _, err := os.Stat(filepath.Join(work, f)); err != nil {
			t.Errorf("work directory: %v", err)
		}
	}
	for _, cmd := range []string{
		"cd " + work,
		" --64 -o prog.o prog.s", " --64 -o float.o float.s", " --64 -o pow10.o pow10.s", " --64 -o print.o print.s", " --64 -o start.o start.s",
		" -static -nostdlib -e _start -o a.out prog.o float.o pow10.o print.o start.o",
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

// TestBuildBackend checks that aether build uses emitProgram: its executable
// is the one that assembly gives, with values in registers, and it behaves
// like the interpreter's run of the program.
func TestBuildBackend(t *testing.T) {
	requireTools(t)
	src := []byte("package main\n\nfunc main() {\n\ts := 0\n\tfor i := 0; i < 10; i++ {\n\t\ts += i\n\t}\n\tprintln(s)\n}\n")
	dir := t.TempDir()
	file := filepath.Join(dir, "p.go")
	writeFile(t, file, src)
	out := filepath.Join(dir, "out")
	if stderr, code := runAether("build", "-o", out, file); code != 0 {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	prog, ds := compile([]source{{file, src}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	asm, err := emitProgram(prog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(asm), ", %rbx\n") {
		t.Errorf("no value in a register:\n%s", asm)
	}
	for _, f := range prog.Funcs {
		if !f.SSA {
			t.Errorf("func %s is not in SSA form", f.Name)
		}
	}
	re := filepath.Join(dir, "re")
	if ds, err := assemble(asm, buildOptions{out: re}, io.Discard); len(ds) > 0 || err != nil {
		t.Fatalf("assemble: %v %v", ds, err)
	}
	if !bytes.Equal(readFile(t, out), readFile(t, re)) {
		t.Error("aether build's executable differs from emitProgram's")
	}
	if got, want := runExe(t, out), (result{stderr: "45\n"}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestBuildBackendLimits checks the limits on emitProgram. No function of
// the S2 suite or the workloads reaches them, so all of them are promoted.
// A function over the limits builds and runs slot-based, and one over only
// the allocator's limit builds and runs in SSA form without registers.
func TestBuildBackendLimits(t *testing.T) {
	requireTools(t)
	progs, _ := s2Suite(t)
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
	}
	maxValues := 0
	for _, p := range progs {
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if _, err := emitProgram(prog); err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		for _, f := range prog.Funcs {
			if !f.SSA {
				t.Errorf("%s: func %s was not promoted", p.name, f.Name)
			}
			n := 0
			for _, b := range f.Blocks {
				for _, in := range b.Instrs {
					if in.ID >= 0 {
						n++
					}
				}
			}
			maxValues = max(maxValues, n)
		}
	}
	t.Logf("largest function: %d values", maxValues)
	dir := t.TempDir()
	println := func(n int) []byte {
		return []byte("package main\n\nfunc main() {\n\tprintln(" + strings.Repeat("0, ", n-1) + "0)\n}\n")
	}
	for _, c := range []struct {
		name string
		n    int
		ssa  bool
	}{
		{"over the allocator's limit", 3000, true},
		{"over the limit on promotion", maxPromoteInstrs + 1, false},
	} {
		prog, ds := compile([]source{{"p.go", println(c.n)}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		asm, err := emitProgram(prog)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, f := range prog.Funcs {
			if f.Name == "main" && f.SSA != c.ssa {
				t.Errorf("%s: main.SSA = %v, want %v", c.name, f.SSA, c.ssa)
			}
		}
		exe := filepath.Join(dir, "out")
		if ds, err := assemble(asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("%s: %v %v", c.name, ds, err)
		}
		if got, want := runExe(t, exe), (result{stderr: strings.Repeat("0 ", c.n-1) + "0\n"}); got != want {
			t.Errorf("%s: exit %d, %d bytes of stderr; want exit 0, %d bytes", c.name, got.exit, len(got.stderr), len(want.stderr))
		}
	}
}

// TestBuildBackendWorstCase builds the most expensive kind of function the
// limits allow, to keep their cost visible: maxAllocValues values, half of
// them parameters live at once, so nearly all of them must be spilled.
func TestBuildBackendWorstCase(t *testing.T) {
	const n = 1000
	var params, sum, args strings.Builder
	for i := range n {
		if i > 0 {
			params.WriteString(", ")
			sum.WriteString(" + ")
			args.WriteString(", ")
		}
		fmt.Fprintf(&params, "p%d", i)
		fmt.Fprintf(&sum, "p%d", i)
		args.WriteString("1")
	}
	src := fmt.Sprintf("package main\n\nfunc f(%s int) int {\n\treturn %s\n}\n\nfunc main() { println(f(%s)) }\n", params.String(), sum.String(), args.String())
	prog, ds := compile([]source{{"p.go", []byte(src)}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	start := time.Now()
	if _, err := emitProgram(prog); err != nil {
		t.Fatal(err)
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("emitProgram: %v; cumulative allocation %d MiB, system %d MiB", time.Since(start).Round(time.Millisecond), m.TotalAlloc>>20, m.Sys>>20)
}

// TestBuildO0 checks that -O0 selects the slot-based backend: its executable
// is what amd64.Emit gives for the unpromoted program, it uses no allocatable
// register for values, it differs from the default build, and both behave
// the same. It also combines with the other flags.
func TestBuildO0(t *testing.T) {
	requireTools(t)
	src := []byte("package main\n\nfunc main() {\n\ts := 0\n\tfor i := 0; i < 10; i++ {\n\t\ts += i\n\t}\n\tprintln(s)\n}\n")
	dir := t.TempDir()
	file := filepath.Join(dir, "p.go")
	writeFile(t, file, src)
	o0, def := filepath.Join(dir, "o0"), filepath.Join(dir, "def")
	if stderr, code := runAether("build", "-O0", "-o", o0, file); code != 0 {
		t.Fatalf("build -O0: exit %d\n%s", code, stderr)
	}
	if stderr, code := runAether("build", "-o", def, file); code != 0 {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	prog, ds := compile([]source{{file, src}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	asm, err := amd64.Emit(prog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(asm), "%rbx") {
		t.Errorf("slot-based assembly uses %%rbx:\n%s", asm)
	}
	re := filepath.Join(dir, "re")
	if ds, err := assemble(asm, buildOptions{out: re}, io.Discard); len(ds) > 0 || err != nil {
		t.Fatalf("assemble: %v %v", ds, err)
	}
	if !bytes.Equal(readFile(t, o0), readFile(t, re)) {
		t.Error("-O0's executable differs from amd64.Emit's")
	}
	if bytes.Equal(readFile(t, o0), readFile(t, def)) {
		t.Error("-O0's executable is the same as the default build's")
	}
	want := result{stderr: "45\n"}
	for _, exe := range []string{o0, def} {
		if got := runExe(t, exe); got != want {
			t.Errorf("%s: got %+v, want %+v", exe, got, want)
		}
	}
	both := filepath.Join(dir, "both")
	stderr, code := runAether("build", "-x", "-O0", "-o", both, file)
	if code != 0 || !strings.Contains(stderr, "as") {
		t.Fatalf("build -x -O0: exit %d\n%s", code, stderr)
	}
	if !bytes.Equal(readFile(t, both), readFile(t, o0)) {
		t.Error("-x changes -O0's executable")
	}
}

// TestO0CrossCheck is the optimization cross-check of ROADMAP Phase 5: every
// program of the S2 suite, built through the build path with -O0 and by
// default, must give the interpreter's stderr and exit status.
func TestO0CrossCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about 480 executables")
	}
	requireTools(t)
	progs, _ := s2Suite(t)
	dir := t.TempDir()
	for _, p := range progs {
		want, ds, err := interpret(p.name, p.src)
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: not interpreted: %v %v", p.name, ds, err)
		}
		for _, o0 := range []bool{true, false} {
			exe := filepath.Join(dir, fmt.Sprintf("%s.%v", strings.TrimSuffix(p.name, ".go"), o0))
			ds, err := build([]source{{p.name, p.src}}, buildOptions{out: exe, o0: o0}, io.Discard)
			if len(ds) > 0 || err != nil {
				t.Fatalf("%s: -O0 %v: not built: %v %v", p.name, o0, ds, err)
			}
			if got := runExe(t, exe); got != want {
				t.Errorf("%s: -O0 %v: native %+v, interpreter %+v", p.name, o0, got, want)
			}
		}
	}
	t.Logf("%d programs built with and without -O0; all match the interpreter", len(progs))
}
