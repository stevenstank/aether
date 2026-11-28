package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/tui"
)

// recorder is a tui.Progress that keeps what it is told.
type recorder struct {
	mu     sync.Mutex
	stages []string
	lines  []string
}

func (r *recorder) Stage(s tui.Stage, done, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := [...]string{"read", "check", "emit", "assemble", "link", "write"}[s]
	if total > 0 {
		name += "#" + strings.Repeat("|", done)
	}
	if len(r.stages) == 0 || r.stages[len(r.stages)-1] != name {
		r.stages = append(r.stages, name)
	}
}

func (r *recorder) Log(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
}

func absFile(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTUICheckMatchesCLI checks that the TUI's check reports exactly what
// aether check reports, for a program of each class.
func TestTUICheckMatchesCLI(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.go")
	if err := os.WriteFile(invalid, []byte("package main\n\nfunc main() {\n\tx := 1\n\tprintln(y)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{
		absFile(t, corpusFile("s1_03_milestone1")),
		invalid,
		absFile(t, corpusFile("unsupported_generic_local_type_arg")),
	} {
		var p recorder
		res := runRequest(tui.Request{Mode: tui.Check, Files: []string{file}}, &p)
		stderr, code := runAether("check", file)
		var got []string
		for _, d := range res.Diags {
			got = append(got, d.String())
		}
		want := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
		if stderr == "" {
			want = nil
		}
		if res.Exit != code || res.Err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: TUI exit %d err %v diagnostics\n%s\nCLI exit %d:\n%s", file, res.Exit, res.Err, strings.Join(got, "\n"), code, stderr)
		}
		if !slices.Equal(p.stages, []string{"read", "check"}) {
			t.Errorf("%s: stages %q", file, p.stages)
		}
	}
}

func TestTUIBuild(t *testing.T) {
	requireTools(t)
	out := filepath.Join(t.TempDir(), "m1")
	src := absFile(t, corpusFile("s1_03_milestone1"))
	var p recorder
	req := tui.Request{Mode: tui.Build, Files: []string{src}, Output: out, Options: tui.Options{Commands: true}}
	res := runRequest(req, &p)
	if res.Exit != diag.ExitOK || res.Err != nil || len(res.Diags) != 0 {
		t.Fatalf("build: exit %d, err %v, %v", res.Exit, res.Err, res.Diags)
	}
	n := len(amd64.Runtime()) + 1
	want := []string{"read", "check", "emit", "assemble"}
	for i := 0; i <= n; i++ {
		want = append(want, "assemble#"+strings.Repeat("|", i))
	}
	want = append(want, "link", "write")
	if !slices.Equal(p.stages, want) {
		t.Errorf("stages\n%q\nwant\n%q", p.stages, want)
	}
	log := strings.Join(p.lines, "\n")
	for _, w := range []string{"# as version: ", "# ld version: ", " --64 -o prog.o prog.s"} {
		if !strings.Contains(log, w) {
			t.Errorf("-x log lacks %q:\n%s", w, log)
		}
	}
	// The executable is the one aether build writes.
	cli := filepath.Join(t.TempDir(), "m1")
	if stderr, code := runAether("build", "-o", cli, src); code != 0 {
		t.Fatalf("aether build: %d %s", code, stderr)
	}
	a, err1 := os.ReadFile(out)
	b, err2 := os.ReadFile(cli)
	if err1 != nil || err2 != nil || !slices.Equal(a, b) {
		t.Errorf("the TUI's executable differs from aether build's (%v, %v)", err1, err2)
	}
	if stdout, err := exec.Command(out).CombinedOutput(); err != nil {
		t.Errorf("running the executable: %v\n%s", err, stdout)
	}
}

func TestTUIUsageErrors(t *testing.T) {
	dir := t.TempDir()
	src := absFile(t, corpusFile("s1_03_milestone1"))
	tests := []struct {
		req  tui.Request
		want string
	}{
		{tui.Request{Mode: tui.Check, Files: []string{filepath.Join(dir, "missing.go")}}, "no such file or directory"},
		{tui.Request{Mode: tui.Check}, "no input file"},
		{tui.Request{Mode: tui.Build, Files: []string{src}}, "no output file: use -o FILE"},
		{tui.Request{Mode: tui.Build, Files: []string{src}, Output: dir}, "exists and is not a regular file"},
		{tui.Request{Mode: tui.Build, Files: []string{src}, Output: src}, "is the input file"},
	}
	for _, tt := range tests {
		var p recorder
		res := runRequest(tt.req, &p)
		if res.Exit != diag.ExitUsage || res.Err == nil || !strings.Contains(res.Err.Error(), tt.want) ||
			strings.HasPrefix(res.Err.Error(), "aether ") {
			t.Errorf("%+v: exit %d, err %v, want %q", tt.req, res.Exit, res.Err, tt.want)
		}
	}
}

func TestTUICommandLine(t *testing.T) {
	req := tui.Request{Mode: tui.Build, Files: []string{"a.go", "b.go"}, Output: "out",
		Options: tui.Options{O0: true, Commands: true, KeepWork: true}}
	if got, want := commandLine(req), []string{"build", "-x", "-work", "-O0", "-o", "out", "a.go", "b.go"}; !slices.Equal(got, want) {
		t.Errorf("commandLine = %q, want %q", got, want)
	}
	// The command line is one that runBuild accepts as it is.
	fs := []string{"-x", "-work", "-O0", "-o", "out"}
	if got := commandLine(req)[1:6]; !slices.Equal(got, fs) {
		t.Errorf("flags %q", got)
	}
	if got, want := commandLine(tui.Request{Mode: tui.Check, Files: []string{"a.go"}, Output: "ignored"}), []string{"check", "a.go"}; !slices.Equal(got, want) {
		t.Errorf("commandLine = %q, want %q", got, want)
	}
}

func TestTUICommand(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o644)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"tui", "a", "b"}, "usage: aether tui [DIR]"},
		{[]string{"tui", "-x"}, "usage: aether tui [DIR]"},
		{[]string{"tui", filepath.Join(t.TempDir(), "missing")}, "no such file or directory"},
		{[]string{"tui", file}, "is not a directory"},
		// Under go test, standard output is not a terminal.
		{[]string{"tui", t.TempDir()}, "not an interactive terminal"},
	}
	for _, tt := range tests {
		stderr, code := runAether(tt.args...)
		if code != diag.ExitUsage || !strings.Contains(stderr, tt.want) {
			t.Errorf("%q: exit %d, stderr %q, want %q", tt.args, code, stderr, tt.want)
		}
	}
}

// TestTUIIsSeparate checks that the TUI depends on no part of the compiler
// but its diagnostics: it compiles only through the driver's runner.
func TestTUIIsSeparate(t *testing.T) {
	graph := importGraph(t, modulePath+"/internal/tui")
	for _, imp := range graph[modulePath+"/internal/tui"] {
		if strings.HasPrefix(imp, modulePath+"/") && imp != modulePath+"/internal/diag" {
			t.Errorf("internal/tui imports %s", imp)
		}
	}
}

func TestLineWriter(t *testing.T) {
	var got []string
	w := &lineWriter{emit: func(s string) { got = append(got, s) }}
	w.Write([]byte("a\nb"))
	w.Write([]byte("c\n\nd"))
	w.flush()
	w.flush()
	if want := []string{"a", "bc", "", "d"}; !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
}
