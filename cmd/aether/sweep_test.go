package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

// TestGorootRunSweep classifies every top-level "// run" program in the Go
// distribution's test directory (docs/ROADMAP.md, Phase 1). It checks only
// that no program causes an internal error; most are expected to be
// unsupported.
func TestGorootRunSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("classifies the GOROOT test corpus")
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Skipf("go env GOROOT: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "test")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Skipf("no GOROOT test corpus in %s", dir)
	}
	counts := map[string]int{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !hasRunDirective(data) {
			continue
		}
		ds := guard(func() []diag.Diagnostic { return check([]source{{name: path, data: data}}) })
		outcome := "accepted"
		switch diag.ExitCode(ds) {
		case diag.ExitInvalid:
			outcome = "invalid"
		case diag.ExitUnsupported:
			outcome = "unsupported"
		case diag.ExitInternal:
			outcome = "internal"
			for _, d := range ds {
				if d.Class == diag.Internal {
					t.Errorf("%s: %s", filepath.Base(path), d)
				}
			}
		}
		counts[outcome]++
	}
	t.Logf("GOROOT/test // run programs: %d accepted, %d invalid, %d unsupported, %d internal",
		counts["accepted"], counts["invalid"], counts["unsupported"], counts["internal"])
	if counts["accepted"]+counts["invalid"]+counts["unsupported"]+counts["internal"] == 0 {
		t.Error("no // run programs found")
	}
}

func hasRunDirective(data []byte) bool {
	for _, line := range bytes.Split(data, []byte("\n")) {
		if string(bytes.TrimRight(line, "\r")) == "// run" {
			return true
		}
	}
	return false
}
