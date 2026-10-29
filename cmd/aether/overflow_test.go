package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gcOverflow (D) begins gc's stack-overflow diagnostic, and overflowLine is
// the fatal error Aether and gc both print (SEMANTICS.md V-2).
const (
	gcOverflow   = "runtime: goroutine stack exceeds"
	overflowLine = "fatal error: stack overflow"
)

// overflowPrefix applies the gc side of the overflow comparison rule
// (SEMANTICS.md V-2) to a gc-built binary's result and returns P, the stderr
// bytes before the first occurrence of D. It returns an error if gc's
// result does not have the form the rule requires.
func overflowPrefix(gc result) (string, error) {
	if gc.exit != 2 {
		return "", fmt.Errorf("exit status %d, want 2", gc.exit)
	}
	if gc.stdout != "" {
		return "", fmt.Errorf("stdout %q, want none", gc.stdout)
	}
	i := strings.Index(gc.stderr, gcOverflow)
	if i < 0 {
		return "", errors.New("no stack-overflow diagnostic")
	}
	p, rest := gc.stderr[:i], gc.stderr[i+len(gcOverflow):]
	// The stack-limit line: D, one space, one or more ASCII digits,
	// "-byte limit", and a newline.
	limit, rest, ok := strings.Cut(rest, "\n")
	digits, ok1 := strings.CutPrefix(limit, " ")
	digits, ok2 := strings.CutSuffix(digits, "-byte limit")
	if !ok || !ok1 || !ok2 || digits == "" || strings.Trim(digits, "0123456789") != "" {
		return "", fmt.Errorf("malformed stack-limit line %q", gcOverflow+limit)
	}
	// The first newline-terminated line after it that begins with
	// "fatal error: " must be exactly the overflow line.
	for {
		line, next, ok := strings.Cut(rest, "\n")
		if !ok {
			return "", errors.New("no newline-terminated fatal error line")
		}
		if strings.HasPrefix(line, "fatal error: ") {
			if line != overflowLine {
				return "", fmt.Errorf("first fatal error line %q, want %q", line, overflowLine)
			}
			return p, nil
		}
		rest = next
	}
}

// TestOverflowPrefix checks the gc side of the rule on fixed inputs, so
// that malformed, unrelated, and incomplete gc output is rejected without
// running gc.
func TestOverflowPrefix(t *testing.T) {
	const diag = gcOverflow + " 1000000000-byte limit\nruntime: sp=0xc020160390 stack=[0xc020160000, 0xc040160000]\n" +
		overflowLine + "\n\nruntime stack:\nruntime.throw(...)\n"
	good := []struct {
		name   string
		stderr string
		p      string
	}{
		{"E14 shape", "1 2\ntrue\n" + diag, "1 2\ntrue\n"},
		{"nothing printed", diag, ""},
		{"no newline before D", "123" + diag, "123"},
		{"other lines before the fatal error", gcOverflow + " 7-byte limit\nx\nfatal: y\n" + overflowLine + "\n", ""},
	}
	for _, tt := range good {
		if p, err := overflowPrefix(result{stderr: tt.stderr, exit: 2}); err != nil || p != tt.p {
			t.Errorf("%s: P = %q, %v; want %q", tt.name, p, err, tt.p)
		}
	}
	bad := []struct {
		name string
		r    result
	}{
		{"exit 0", result{stderr: diag}},
		{"exit 1", result{stderr: diag, exit: 1}},
		{"stdout", result{stdout: "x", stderr: diag, exit: 2}},
		{"panic", result{stderr: "panic: runtime error: integer divide by zero\n", exit: 2}},
		{"no digits", result{stderr: gcOverflow + " -byte limit\n" + overflowLine + "\n", exit: 2}},
		{"not digits", result{stderr: gcOverflow + " 1e9-byte limit\n" + overflowLine + "\n", exit: 2}},
		{"negative", result{stderr: gcOverflow + " -5-byte limit\n" + overflowLine + "\n", exit: 2}},
		{"two spaces", result{stderr: gcOverflow + "  5-byte limit\n" + overflowLine + "\n", exit: 2}},
		{"no newline after the limit", result{stderr: gcOverflow + " 5-byte limit", exit: 2}},
		{"other fatal error first", result{stderr: gcOverflow + " 5-byte limit\nfatal error: out of memory\n" + overflowLine + "\n", exit: 2}},
		{"fatal error with a suffix", result{stderr: gcOverflow + " 5-byte limit\n" + overflowLine + "!\n", exit: 2}},
		{"unterminated fatal error", result{stderr: gcOverflow + " 5-byte limit\n" + overflowLine, exit: 2}},
		{"no fatal error", result{stderr: gcOverflow + " 5-byte limit\nruntime: sp=0\n", exit: 2}},
		{"fatal error before D only", result{stderr: overflowLine + "\n" + gcOverflow + " 5-byte limit\n", exit: 2}},
	}
	for _, tt := range bad {
		if p, err := overflowPrefix(tt.r); err == nil {
			t.Errorf("%s: accepted, P = %q", tt.name, p)
		}
	}
}

// overflowPrograms recurse without end, printing nothing during the
// recursion, so they overflow under gc, natively, and in the interpreter.
var overflowPrograms = map[string]string{
	// E14's program, without bool: a result used after the call.
	"result": `func r(n int) int { return r(n+1) + 1 }
func main() { println(1, 2); println(); println(r(0)) }`,
	"statement": `func f(a, b, c int) { f(b, c, a+1) }
func main() { println(-3); f(1, 2, 3) }`,
	"mutual": `func f(x int) int { return g(x-1) * 2 }
func g(x int) int { return f(x) }
func main() { println(9223372036854775807); println(f(5)) }`,
	"main": `func main() { main() }`,
}

// TestStackOverflowAgainstGc compares unbounded recursion with gc by the
// overflow comparison rule (SEMANTICS.md V-2), both natively and in the
// interpreter. The binaries run one at a time, each with a 2-minute timeout
// (runLimited); a timeout or a signal fails the test. Each gc binary uses
// about 1 GB and 4 s (E14), so the test is skipped only under -short.
func TestStackOverflowAgainstGc(t *testing.T) {
	if testing.Short() {
		t.Skip("each gc-built program uses about 1 GB of stack before it overflows")
	}
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh to run the programs with")
	}
	progs := map[string][]byte{}
	var names []string
	for name, body := range overflowPrograms {
		progs[name] = []byte("package main\n\n" + body + "\n")
		names = append(names, name)
	}
	sort.Strings(names)
	bin := gcBuild(t, gobin, progs)
	dir := t.TempDir()
	for _, name := range names {
		gc := runLimited(t, filepath.Join(bin, name))
		p, err := overflowPrefix(gc)
		if err != nil {
			t.Errorf("%s: gc output does not fit the rule: %v\nstdout %q\nstderr:\n%s", name, err, gc.stdout, gc.stderr)
			continue
		}
		want := result{stderr: p + overflowLine + "\n", exit: 2}
		got, ds, err := interpret(name+".go", progs[name])
		if len(ds) > 0 || err != nil {
			t.Errorf("%s: not interpreted: %v %v", name, ds, err)
		} else if got != want {
			t.Errorf("%s: interpreter %+v, want %+v", name, got, want)
		}
		exe := filepath.Join(dir, name)
		if ds, err := build([]source{{name + ".go", progs[name]}}, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
			t.Errorf("%s: not built: %v %v", name, ds, err)
			continue
		}
		if got := runLimited(t, exe); got != want {
			t.Errorf("%s: native %+v, want %+v", name, got, want)
		}
	}
}
