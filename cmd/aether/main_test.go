package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

func TestRunUsage(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		code       int
		wantStdout string // substring; empty means stdout must be empty
		wantStderr string // substring; empty means stderr must be empty
	}{
		{"no arguments", nil, diag.ExitUsage, "", "usage: aether"},
		{"help", []string{"help"}, diag.ExitOK, "usage: aether", ""},
		{"unknown command", []string{"build"}, diag.ExitUsage, "", `unknown command "build"`},
		{"check without file", []string{"check"}, diag.ExitUsage, "", "no input file"},
		{"check unknown flag", []string{"check", "-x", "a.go"}, diag.ExitUsage, "", "unknown flag -x"},
		{"check non-go file", []string{"check", "main.c"}, diag.ExitUsage, "", "is not a .go file"},
		{"check missing file", []string{"check", "testdata/does-not-exist.go"}, diag.ExitUsage, "", "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.code {
				t.Errorf("exit code %d, want %d", code, tt.code)
			}
			checkStream(t, "stdout", stdout.String(), tt.wantStdout)
			checkStream(t, "stderr", stderr.String(), tt.wantStderr)
		})
	}
}

func checkStream(t *testing.T, name, got, want string) {
	t.Helper()
	if want == "" && got != "" {
		t.Errorf("%s not empty: %q", name, got)
	}
	if want != "" && !strings.Contains(got, want) {
		t.Errorf("%s = %q, want it to contain %q", name, got, want)
	}
}

func TestRunMultipleFiles(t *testing.T) {
	a := filepath.Join("testdata", "check", "s1_01_empty_main.go")
	b := filepath.Join("testdata", "check", "s1_02_println_no_args.go")
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", a, b}, &stdout, &stderr)
	if code != diag.ExitUnsupported {
		t.Errorf("exit code %d, want %d", code, diag.ExitUnsupported)
	}
	want := b + ": unsupported: a program of 2 source files is not supported in subset S1 [multiple-files]\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout not empty: %q", stdout.String())
	}
}

// TestInternalErrorReporting checks that a crash inside Aether is reported as
// an internal error with its own exit code, never as a problem with the
// program, and that it takes precedence over other diagnostics.
func TestInternalErrorReporting(t *testing.T) {
	ds := guard(func() []diag.Diagnostic { panic("boom") })
	var stderr bytes.Buffer
	if code := report(&stderr, ds); code != diag.ExitInternal {
		t.Errorf("exit code %d, want %d", code, diag.ExitInternal)
	}
	if want := "internal: internal compiler error: boom\n"; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}

	ds = guard(func() []diag.Diagnostic {
		return []diag.Diagnostic{
			{Class: diag.Unsupported, Pos: diag.Pos{File: "a.go", Line: 1, Col: 1}, Message: "x", Feature: "f"},
			{Class: diag.Internal, Pos: diag.Pos{File: "a.go", Line: 2, Col: 1}, Message: "gate failure"},
		}
	})
	if code := report(&bytes.Buffer{}, ds); code != diag.ExitInternal {
		t.Errorf("mixed diagnostics: exit code %d, want %d", code, diag.ExitInternal)
	}
}

// TestBinary builds the aether command and runs it as a process, checking
// exit codes, output streams, and that checking a program creates no files.
func TestBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the aether binary")
	}
	bin := filepath.Join(t.TempDir(), "aether")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	work := t.TempDir()
	cases := []struct {
		file string
		code int
	}{
		{"s1_03_milestone1.go", diag.ExitOK},
		{"s1_n05_missing_main.go", diag.ExitInvalid},
		{"unsupported_operator_div.go", diag.ExitUnsupported},
	}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join("testdata", "check", c.file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, c.file), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := listDir(t, work)
	for _, c := range cases {
		cmd := exec.Command(bin, "check", c.file)
		cmd.Dir = work
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != c.code {
			t.Errorf("%s: exit code %d, want %d\n%s", c.file, code, c.code, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("%s: stdout not empty: %q", c.file, stdout.String())
		}
		if (c.code == diag.ExitOK) != (stderr.Len() == 0) {
			t.Errorf("%s: stderr = %q", c.file, stderr.String())
		}
	}
	if after := listDir(t, work); !slices.Equal(before, after) {
		t.Errorf("aether check changed the working directory: %v -> %v", before, after)
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
