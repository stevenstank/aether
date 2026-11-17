package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInterfacePrograms runs the interface corpus programs (s5_i*) and
// generated interface programs (generateIfaces) under gc, the interpreter,
// aether build with and without -O0, and ABI-R1: all must agree (D-041).
func TestInterfacePrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if !strings.HasPrefix(filepath.Base(path), "s5_i") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	if len(progs) == 0 {
		t.Fatal("no s5_i corpus programs")
	}
	for seed := uint64(1); seed <= 30; seed++ {
		progs[fmt.Sprintf("ifc%04d", seed)] = []byte(generateIfaces(seed))
	}
	checkR1Programs(t, progs)
}
