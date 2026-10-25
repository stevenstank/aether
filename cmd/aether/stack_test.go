package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runLimited runs exe through /bin/sh with the given ulimit options. A
// signal, such as SIGSEGV from a stack overflow, fails the test.
func runLimited(t *testing.T, exe string, limits ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	script := ""
	for _, l := range limits {
		script += "ulimit " + l + " && "
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script+`exec "$0"`, exe)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := result{}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() < 0 {
			t.Fatalf("%s with ulimit %v: %v", exe, limits, err)
		}
		r.exit = exit.ExitCode()
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// TestBuildLargeFrame is the regression test for F1: a valid S1 program
// whose frame is larger than the default 8 MiB process stack built
// successfully, but its executable died with SIGSEGV. The stack is now mapped
// by the runtime to fit the frame, so the program runs under the default
// and under a reduced stack limit, like gc's. A small program is checked
// under the same limits.
func TestBuildLargeFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2 MB program")
	}
	requireTools(t)
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh to set limits with")
	}
	// One value per argument: a frame of at least 8*n = 8.8 MB.
	const n = 1_100_000
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	args := strings.Repeat("0, ", n-1) + "0"
	writeFile(t, src, []byte("package main\n\nfunc main() {\n\tprintln("+args+")\n}\n"))
	big := filepath.Join(dir, "big")
	if stderr, code := runAether("build", "-o", big, src); code != 0 {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	small := filepath.Join(dir, "small")
	if stderr, code := runAether("build", "-o", small, corpusFile("s1_03_milestone1")); code != 0 {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	for _, limit := range []string{"-s 8192", "-s 1024"} {
		if got, want := runLimited(t, big, limit), (result{stderr: strings.Repeat("0 ", n-1) + "0\n"}); got != want {
			t.Errorf("large frame, ulimit %s: exit %d, %d bytes of stderr, stdout %q; want exit 0, %d bytes",
				limit, got.exit, len(got.stderr), got.stdout, len(want.stderr))
		}
		if got, want := runLimited(t, small, limit), (result{stderr: "-6 42\n6 -9223372036854775808\n"}); got != want {
			t.Errorf("small frame, ulimit %s: %+v, want %+v", limit, got, want)
		}
	}
}
