package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

var update = flag.Bool("update", false, "rewrite the .golden files in testdata/check")

// expectation is the outcome a corpus file declares in its header
// (docs/TESTING.md §Distinguishing compiler bugs from subset limitations):
//
//	// aether:expect run
//	// aether:expect invalid
//	// aether:expect unsupported <feature>
//
// plus, optionally, what the official toolchain does with the file:
//
//	// aether:gc valid|invalid|skip <reason>
//
// Without an aether:gc line, a file is valid Go unless it expects invalid.
type expectation struct {
	class   string // "run", "invalid", or "unsupported"
	feature string
	gc      string // "valid", "invalid", or "skip"
}

func readExpectation(t *testing.T, path string) expectation {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e expectation
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "// aether:expect "); ok {
			f := strings.Fields(rest)
			e.class = f[0]
			if len(f) > 1 {
				e.feature = f[1]
			}
		}
		if rest, ok := strings.CutPrefix(line, "// aether:gc "); ok {
			e.gc = strings.Fields(rest)[0]
		}
	}
	switch e.class {
	case "run", "invalid":
	case "unsupported":
		if e.feature == "" {
			t.Fatalf("%s: unsupported expectation without a feature", path)
		}
	default:
		t.Fatalf("%s: missing or unknown aether:expect header", path)
	}
	if e.gc == "" {
		e.gc = "valid"
		if e.class == "invalid" {
			e.gc = "invalid"
		}
	}
	return e
}

func corpus(tb testing.TB) []string {
	tb.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "check", "*.go"))
	if err != nil || len(files) == 0 {
		tb.Fatalf("no corpus files: %v", err)
	}
	return files
}

// TestCheckCorpus runs "aether check" on every corpus file and compares the
// result with the file's declared expectation and its reviewed golden output.
func TestCheckCorpus(t *testing.T) {
	wantExit := map[string]int{"run": diag.ExitOK, "invalid": diag.ExitInvalid, "unsupported": diag.ExitUnsupported}
	for _, path := range corpus(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			e := readExpectation(t, path)
			var stdout, stderr bytes.Buffer
			code := run([]string{"check", path}, &stdout, &stderr)
			if stdout.Len() != 0 {
				t.Errorf("stdout not empty: %q", stdout.String())
			}
			if code != wantExit[e.class] {
				t.Errorf("exit code %d, want %d (%s)\n%s", code, wantExit[e.class], e.class, stderr.String())
			}
			if e.class == "run" && stderr.Len() != 0 {
				t.Errorf("accepted program produced diagnostics:\n%s", stderr.String())
			}
			if e.class == "unsupported" && !strings.Contains(stderr.String(), "["+e.feature+"]") {
				t.Errorf("no diagnostic for feature %s:\n%s", e.feature, stderr.String())
			}
			for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
				if line != "" && !strings.HasPrefix(line, path+":") {
					t.Errorf("diagnostic without a position in %s: %q", path, line)
				}
			}
			golden := strings.TrimSuffix(path, ".go") + ".golden"
			if *update {
				if err := os.WriteFile(golden, stderr.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden file (run go test -update): %v", err)
			}
			if !bytes.Equal(stderr.Bytes(), want) {
				t.Errorf("diagnostics differ from %s\ngot:\n%s\nwant:\n%s", golden, stderr.String(), want)
			}
		})
	}
}

// TestCheckDeterministic checks that repeated runs give byte-identical output.
func TestCheckDeterministic(t *testing.T) {
	for _, path := range corpus(t) {
		var first []byte
		for i := 0; i < 5; i++ {
			var stdout, stderr bytes.Buffer
			run([]string{"check", path}, &stdout, &stderr)
			if i == 0 {
				first = stderr.Bytes()
			} else if !bytes.Equal(first, stderr.Bytes()) {
				t.Fatalf("%s: run %d differs:\n%s\nvs\n%s", path, i, first, stderr.Bytes())
			}
		}
	}
}
