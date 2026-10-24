package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCorpusAgainstGc builds every corpus file with the official Go toolchain
// and checks that gc agrees with the file's declared validity: files expected
// to be accepted or unsupported must be valid Go, and files expected to be
// invalid must be rejected (D-014). The toolchain is used only as an oracle;
// it never takes part in "aether check".
func TestCorpusAgainstGc(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every corpus file with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	for _, path := range corpus(t) {
		e := readExpectation(t, path)
		if e.gc == "skip" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd := exec.Command(gobin, "build", "-o", filepath.Join(dir, "out"), abs)
			// Build outside this module, with the installed toolchain only.
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
			out, err := cmd.CombinedOutput()
			switch {
			case e.gc == "valid" && err != nil:
				t.Errorf("gc rejected a file declared valid Go:\n%s", out)
			case e.gc == "invalid" && err == nil:
				t.Errorf("gc accepted a file declared invalid Go")
			}
		})
	}
}
