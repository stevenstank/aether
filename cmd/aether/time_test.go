package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
)

// TestTimeZoneEnv checks the local zone of gc's time compiled by Aether
// (D-054) against gc's, under exact environments: TZ unset (the system's
// /etc/localtime), empty (UTC), a zone, one with a leading colon, an
// unknown one, and one os.Setenv sets before time first uses it.
func TestTimeZoneEnv(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	path := filepath.Join("testdata", "timeprog", "zone.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gcBin := gcBuild(t, gobin, map[string][]byte{"zone": src})
	dir := t.TempDir()
	exe := filepath.Join(dir, "zone")
	if stderr, code := runAether("build", "-o", exe, path); code != 0 {
		t.Fatalf("aether build: exit %d\n%s", code, stderr)
	}
	prog, ds := compile([]source{{"zone.go", src}})
	if len(ds) > 0 {
		t.Fatalf("not compiled: %v", ds)
	}
	for _, c := range []struct {
		env  []string
		args []string
	}{
		{[]string{}, nil},
		{[]string{"TZ="}, nil},
		{[]string{"TZ=America/New_York"}, nil},
		{[]string{"TZ=:Asia/Tokyo"}, nil},
		{[]string{"TZ=Nowhere/City"}, nil},
		{[]string{"TZ=UTC"}, []string{"Asia/Tokyo"}},
	} {
		t.Run(strings.Join(append(c.env, c.args...), ","), func(t *testing.T) {
			want := osExec(t, filepath.Join(gcBin, "zone"), dir, c.args, c.env)
			if got := osExec(t, exe, dir, c.args, c.env); got != want {
				t.Errorf("aether %+v\ngc %+v", got, want)
			}
			var stdout, stderr bytes.Buffer
			code, err := interp.RunOptions(prog, &stderr, interp.Options{Stdout: &stdout,
				Args: append([]string{"zone"}, c.args...), Env: c.env, Steps: interpSteps})
			if err != nil {
				t.Fatalf("interpreter: %v", err)
			}
			if got := (result{stdout.String(), stderr.String(), code}); got != want {
				t.Errorf("interpreter %+v\ngc %+v", got, want)
			}
		})
	}
}

// TestTimersAsync runs testdata/std/timers.go, which TestStdPrograms runs
// with gc's synchronous timer channels, with GODEBUG=asynctimerchan=1:
// buffered timer channels, which the scheduler fills when their timers
// run, also while a select polls them (D-054).
func TestTimersAsync(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	path := filepath.Join("testdata", "std", "timers.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gcBin := gcBuild(t, gobin, map[string][]byte{"timers": src})
	dir := t.TempDir()
	exe := filepath.Join(dir, "timers")
	if stderr, code := runAether("build", "-o", exe, path); code != 0 {
		t.Fatalf("aether build: exit %d\n%s", code, stderr)
	}
	env := []string{"GODEBUG=asynctimerchan=1"}
	want := osExec(t, filepath.Join(gcBin, "timers"), dir, nil, env)
	if !strings.Contains(want.stdout, "0 1 true false") {
		t.Fatalf("gc's timer channels are not buffered: %+v", want)
	}
	if got := osExec(t, exe, dir, nil, env); got != want {
		t.Errorf("aether %+v\ngc %+v", got, want)
	}
	prog, ds := compile([]source{{"timers.go", src}})
	if len(ds) > 0 {
		t.Fatalf("not compiled: %v", ds)
	}
	var stdout, stderr bytes.Buffer
	code, err := interp.RunOptions(prog, &stderr, interp.Options{Stdout: &stdout, Args: []string{"timers"}, Env: env, Steps: interpSteps})
	if err != nil {
		t.Fatalf("interpreter: %v", err)
	}
	if got := (result{stdout.String(), stderr.String(), code}); got != want {
		t.Errorf("interpreter %+v\ngc %+v", got, want)
	}
}
