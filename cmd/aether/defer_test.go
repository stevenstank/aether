package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeferPrograms runs the defer and panic corpus programs (s5_d*) under
// gc, the interpreter, aether build with and without -O0, and ABI-R1: all
// must agree (D-043).
func TestDeferPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if !strings.HasPrefix(filepath.Base(path), "s5_d") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	if len(progs) == 0 {
		t.Fatal("no s5_d corpus programs")
	}
	checkR1Programs(t, progs)
}
