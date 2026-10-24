package toolchain

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fake installs an executable shell script called name in dir.
func fake(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakePath makes dir the only PATH entry.
func fakePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	return dir
}

func TestFindGNU(t *testing.T) {
	for _, name := range []string{"as", "ld"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not installed", name)
		}
		var log bytes.Buffer
		tool, err := Find(name, &log)
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(tool.Path) || !strings.HasPrefix(tool.Version, "GNU ") {
			t.Errorf("%s: %+v", name, tool)
		}
		if !strings.Contains(log.String(), "# "+name+" version: "+tool.Version+"\n") {
			t.Errorf("version not logged:\n%s", log.String())
		}
		t.Logf("%s: %s", tool.Path, tool.Version)
	}
}

func TestFindRejects(t *testing.T) {
	tests := []struct{ name, tool, script, want string }{
		{"missing", "as", "", "as: not found on PATH"},
		{"not GNU", "as", "echo 'LLVM version 18'\n", `not GNU as: version line does not start with "GNU assembler"`},
		{"wrong target", "as", "echo 'GNU assembler 2.42'\necho \"This assembler was configured for a target of \\`aarch64-linux-gnu'.\"\n",
			"does not support x86-64 ELF"},
		{"ld without x86-64", "ld", "echo 'GNU ld 2.42'\necho '  Supported emulations:'\necho '   aarch64linux'\n", `no "elf_x86_64"`},
		{"version fails", "ld", "echo 'broken' >&2\nexit 3\n", "exit status 3: broken"},
		{"unknown tool", "cc", "", "cc: unknown tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := fakePath(t)
			if tt.script != "" {
				fake(t, dir, tt.tool, tt.script)
			}
			_, err := Find(tt.tool, nil)
			var terr *Error
			if !errors.As(err, &terr) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an *Error containing %q", err, tt.want)
			}
			if tt.script != "" && tt.tool != "cc" && terr.Path != filepath.Join(dir, tt.tool) {
				t.Errorf("error does not name the path: %v", err)
			}
		})
	}
}

// gnuAs is a stand-in that identifies as GNU as for x86-64 and then runs
// body.
const gnuAs = "if [ \"$1\" = --version ]; then echo 'GNU assembler 2.42'; echo \"configured for a target of \\`x86_64-linux-gnu'.\"; exit 0; fi\n"

func TestRun(t *testing.T) {
	dir := fakePath(t)
	work := t.TempDir()
	t.Setenv("AETHER_PROBE", "leaked")
	fake(t, dir, "as", gnuAs+"echo \"$LC_ALL|$AETHER_PROBE|$HOME|$(pwd)|$*\" > probe.txt\n")
	tool, err := Find("as", nil)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := tool.Run(work, []string{"--64", "-o", "p.o", "p.s"}, &log); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(work, "probe.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// Only LC_ALL=C reaches the tool, it runs in the given directory, and
	// it receives the arguments unchanged.
	if want := "C|||" + work + "|--64 -o p.o p.s\n"; string(got) != want {
		t.Errorf("tool saw %q, want %q", got, want)
	}
	if want := "LC_ALL=C " + tool.Path + " --64 -o p.o p.s\n"; log.String() != want {
		t.Errorf("log %q, want %q", log.String(), want)
	}
}

func TestRunFailure(t *testing.T) {
	dir := fakePath(t)
	fake(t, dir, "as", gnuAs+"echo 'p.s:1: Error: no such instruction' >&2\nexit 1\n")
	tool, err := Find("as", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = tool.Run(t.TempDir(), []string{"p.s"}, nil)
	want := "as (" + tool.Path + ", GNU assembler 2.42): exit status 1: p.s:1: Error: no such instruction"
	if err == nil || err.Error() != want {
		t.Errorf("got %v\nwant %s", err, want)
	}
}
