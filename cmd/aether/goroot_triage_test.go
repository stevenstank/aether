package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

// gorootTriage names the file TestGorootTriage writes its report to.
var gorootTriage = flag.String("goroot.triage", "", "write a report of TestGorootRun's unsupported tests to this file")

// triagePos matches what varies between diagnostics of the same cause: a
// position and a quoted name.
var triagePos = regexp.MustCompile(`^\S+:\d+:\d+: `)

// TestGorootTriage, run only with -goroot.triage, reads the unsupported
// tests of testdata/goroot/run.golden, checks each again, and reports
// every one with its first diagnostic, and the diagnostics' messages
// without their positions by how many tests they stop: the work list
// for finding cheap wins and misclassified tests.
func TestGorootTriage(t *testing.T) {
	if *gorootTriage == "" {
		t.Skip("run with -goroot.triage=FILE")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	out, err := exec.Command(gobin, "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	testDir := filepath.Join(strings.TrimSpace(string(out)), "test")
	golden, err := os.ReadFile(filepath.Join("testdata", "goroot", "run.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var tests, causes []string
	byCause := map[string][]string{}
	for _, l := range strings.Split(string(golden), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || f[1] != "unsupported" {
			continue
		}
		path := filepath.Join(testDir, f[0])
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, ds := compile([]source{{name: path, data: src}})
		if len(ds) == 0 {
			tests = append(tests, f[0]+"\tno diagnostic now")
			continue
		}
		diag.Sort(ds)
		msg := strings.ReplaceAll(ds[0].String(), testDir+string(filepath.Separator), "")
		tests = append(tests, fmt.Sprintf("%s\t%d diagnostics\t%s", f[0], len(ds), msg))
		cause := triagePos.ReplaceAllString(msg, "")
		if byCause[cause] == nil {
			causes = append(causes, cause)
		}
		byCause[cause] = append(byCause[cause], f[0])
	}
	slices.SortStableFunc(causes, func(a, b string) int { return len(byCause[b]) - len(byCause[a]) })
	var b strings.Builder
	fmt.Fprintf(&b, "# %d unsupported tests, %d causes\n\n## causes\n", len(tests), len(causes))
	for _, c := range causes {
		fmt.Fprintf(&b, "%4d  %s\n      %s\n", len(byCause[c]), c, strings.Join(byCause[c], " "))
	}
	fmt.Fprintf(&b, "\n## tests\n%s\n", strings.Join(tests, "\n"))
	if err := os.WriteFile(*gorootTriage, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d unsupported tests, %d causes: %s", len(tests), len(causes), *gorootTriage)
}
