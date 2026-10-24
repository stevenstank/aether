package main

import (
	"os"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

// FuzzCheck checks that no input, valid or not, makes check panic or report
// an internal error (docs/ROADMAP.md, Phase 1). The corpus files seed it.
func FuzzCheck(f *testing.F) {
	for _, path := range corpus(f) {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		ds := guard(func() []diag.Diagnostic { return check([]source{{name: "fuzz.go", data: src}}) })
		for _, d := range ds {
			if d.Class == diag.Internal {
				t.Fatalf("internal error: %s\nsource:\n%s", d, src)
			}
		}
	})
}
