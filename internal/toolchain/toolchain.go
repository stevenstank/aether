// Package toolchain locates, identifies, and runs the GNU assembler and
// linker (D-007, D-008). It is the only Aether package that starts
// processes; a test enforces this.
//
// Tools are found on PATH, resolved to absolute paths, and identified by
// their version output before use. They run by absolute path, with argument
// lists (never a shell), with the environment containing only LC_ALL=C, and
// in a directory chosen by the caller.
package toolchain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// Env is the complete environment every tool runs with (E9).
var Env = []string{"LC_ALL=C"}

// Tool is an identified external tool.
type Tool struct {
	Name    string // "as" or "ld"
	Path    string // absolute
	Version string // first line of its version output
}

// Error is a failure to find, identify, or run a tool. Every field that is
// known is filled in, so the diagnostic can name them all (D-008).
type Error struct {
	Tool    string
	Path    string // path used, or "" if not found
	Version string // version line, if obtained
	Msg     string
	Stderr  string // the tool's output, if it ran
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Tool)
	if e.Path != "" {
		b.WriteString(" (" + e.Path)
		if e.Version != "" {
			b.WriteString(", " + e.Version)
		}
		b.WriteString(")")
	}
	b.WriteString(": " + e.Msg)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		b.WriteString(": " + s)
	}
	return b.String()
}

// identity describes how a tool proves it is the GNU x86-64 ELF tool: the
// arguments that print its version, the prefix of the first line, and a
// field that must appear in the output.
var identity = map[string]struct {
	args          []string
	prefix, field string
}{
	// "This assembler was configured for a target of `x86_64-linux-gnu'."
	"as": {[]string{"--version"}, "GNU assembler", "`x86_64-"},
	// -V prints the -v version line, then the supported emulations.
	"ld": {[]string{"-V"}, "GNU ld", "elf_x86_64"},
}

// Find locates the tool named name ("as" or "ld") on PATH, resolves it to
// an absolute path, and checks that it is the GNU tool and supports x86-64
// ELF. If log is not nil, the identification command and the version line
// are written to it.
func Find(name string, log io.Writer) (Tool, error) {
	id, ok := identity[name]
	if !ok {
		return Tool{}, &Error{Tool: name, Msg: "unknown tool"}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return Tool{}, &Error{Tool: name, Msg: fmt.Sprintf("not found on PATH: %v", err)}
	}
	if path, err = filepath.Abs(path); err != nil {
		return Tool{}, &Error{Tool: name, Msg: err.Error()}
	}
	t := Tool{Name: name, Path: path}
	out, err := t.output("", id.args, log)
	if err != nil {
		return Tool{}, err
	}
	t.Version, _, _ = strings.Cut(string(out), "\n")
	t.Version = strings.TrimSpace(t.Version)
	if log != nil {
		fmt.Fprintf(log, "# %s version: %s\n", name, t.Version)
	}
	switch {
	case !strings.HasPrefix(t.Version, id.prefix):
		return Tool{}, &Error{Tool: name, Path: path, Version: t.Version,
			Msg: fmt.Sprintf("not GNU %s: version line does not start with %q", name, id.prefix)}
	case !hasField(string(out), id.field):
		return Tool{}, &Error{Tool: name, Path: path, Version: t.Version,
			Msg: fmt.Sprintf("does not support x86-64 ELF: no %q in %s %s output", id.field, name, strings.Join(id.args, " "))}
	}
	return t, nil
}

func hasField(out, field string) bool {
	if strings.HasPrefix(field, "`") {
		return strings.Contains(out, field)
	}
	return strings.Contains(" "+strings.Join(strings.Fields(out), " ")+" ", " "+field+" ")
}

// Run runs t with args in dir. If log is not nil, the command is written to
// it first, with its environment and absolute path. A failure is an *Error
// carrying the tool's output.
func (t Tool) Run(dir string, args []string, log io.Writer) error {
	_, err := t.output(dir, args, log)
	return err
}

func (t Tool) output(dir string, args []string, log io.Writer) ([]byte, error) {
	if log != nil {
		fmt.Fprintf(log, "%s %s %s\n", strings.Join(Env, " "), t.Path, strings.Join(args, " "))
	}
	cmd := exec.Command(t.Path, args...)
	cmd.Dir = dir
	cmd.Env = Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := err.Error()
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			msg = "cannot run: " + msg
		}
		return nil, &Error{Tool: t.Name, Path: t.Path, Version: t.Version, Msg: msg,
			Stderr: stderr.String() + stdout.String()}
	}
	return stdout.Bytes(), nil
}
