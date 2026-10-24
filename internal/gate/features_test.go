package gate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

// documentedFeatures returns the feature names in the table that follows the
// "**Feature names.**" paragraph of docs/SEMANTICS.md: every backquoted name
// in the second column of its rows.
func documentedFeatures(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "SEMANTICS.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "**Feature names.**") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal(`docs/SEMANTICS.md has no "**Feature names.**" paragraph`)
	}
	name := regexp.MustCompile("`([a-z-]+)`")
	docs := map[string]bool{}
	inTable := false
	for _, line := range lines[start+1:] {
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break // end of the table
			}
			continue
		}
		inTable = true
		cols := strings.Split(line, "|")
		if len(cols) < 3 {
			continue
		}
		for _, m := range name.FindAllStringSubmatch(cols[2], -1) {
			docs[m[1]] = true
		}
	}
	if len(docs) == 0 {
		t.Fatal("no feature names found in the docs/SEMANTICS.md feature table")
	}
	return docs
}

// TestFeatureTableMatchesGate checks that the feature names the gate can
// report and the names documented in docs/SEMANTICS.md are the same set.
func TestFeatureTableMatchesGate(t *testing.T) {
	docs := documentedFeatures(t)
	code := map[string]bool{}
	for _, f := range Features() {
		code[f] = true
		if !docs[f] {
			t.Errorf("feature %q is reported by internal/gate but missing from the docs/SEMANTICS.md feature table", f)
		}
	}
	for f := range docs {
		if !code[f] {
			t.Errorf("feature %q is documented in docs/SEMANTICS.md but internal/gate never reports it", f)
		}
	}
}

// TestUnregisteredFeatureIsInternal checks the guard that keeps the registry
// complete: a rejection with an unregistered name is an internal error.
func TestUnregisteredFeatureIsInternal(t *testing.T) {
	d := unsupported(diag.Pos{File: "p.go", Line: 1, Col: 1}, "no-such-feature", "something")
	if d.Class != diag.Internal || !strings.Contains(d.Message, `"no-such-feature"`) {
		t.Errorf("got %s, want an internal diagnostic naming the feature", d)
	}
	if d := unsupported(diag.Pos{}, FeatImport, "import"); d.Class != diag.Unsupported {
		t.Errorf("registered feature: got %s", d)
	}
}
