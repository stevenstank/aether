package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNamedResultPrograms runs the named-result corpus programs (s5_n*)
// and generated named-result programs (generateNamed) under gc, the
// interpreter, aether build with and without -O0, and ABI-R1: all must
// agree (D-044). The corpus programs with defer statements, s5_d12 and
// s5_d13, run in TestDeferPrograms.
func TestNamedResultPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if !strings.HasPrefix(filepath.Base(path), "s5_n") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	if len(progs) == 0 {
		t.Fatal("no s5_n corpus programs")
	}
	for seed := uint64(1); seed <= 30; seed++ {
		progs[fmt.Sprintf("nmd%04d", seed)] = []byte(generateNamed(seed))
	}
	checkR1Programs(t, progs)
}
