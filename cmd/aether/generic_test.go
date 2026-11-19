package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenericPrograms runs the generic corpus programs (s5_g*) and
// generated generic programs (generateGenerics) under gc, the interpreter,
// aether build with and without -O0, and ABI-R1: all must agree (D-045).
func TestGenericPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if !strings.HasPrefix(filepath.Base(path), "s5_g") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	if len(progs) == 0 {
		t.Fatal("no s5_g corpus programs")
	}
	for seed := uint64(1); seed <= 30; seed++ {
		progs[fmt.Sprintf("gnr%04d", seed)] = []byte(generateGenerics(seed))
	}
	checkR1Programs(t, progs)
}
