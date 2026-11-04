package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// TestDominatorsOnLoweredIR computes the dominator tree of every function
// of every corpus program expected to run and of the generated programs of
// the differential test (D-006 experiment). Each block's immediate
// dominator must dominate all of its predecessors, the entry must dominate
// every block, and Dominates must agree with dominance computed from its
// definition by removing one block at a time, as must Frontier with the
// definition of the dominance frontier.
func TestDominatorsOnLoweredIR(t *testing.T) {
	progs := map[string]*ir.Program{}
	for _, path := range corpus(t) {
		if readExpectation(t, path).class != "run" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		prog, ds := compile([]source{{path, data}})
		if prog == nil || len(ds) != 0 {
			t.Fatalf("%s: no IR: %v", path, ds)
		}
		progs[filepath.Base(path)] = prog
	}
	corpusRun := len(progs)
	for seed := uint64(1); len(progs) < corpusRun+generatedPrograms; seed++ {
		if seed > 3*generatedPrograms {
			t.Fatal("too many generated programs rejected")
		}
		if prog, ds := compile([]source{{"gen.go", []byte(generate(seed))}}); len(ds) == 0 {
			progs[fmt.Sprintf("gen%04d", seed)] = prog
		}
	}
	funcs, blocks, joins := 0, 0, 0
	for name, prog := range progs {
		if errs := ir.Verify(prog); len(errs) != 0 {
			t.Fatalf("%s: %v", name, errs)
		}
		for _, f := range prog.Funcs {
			funcs++
			blocks += len(f.Blocks)
			d := ir.Dominators(f)
			dom := dominanceByDefinition(f)
			for _, b := range f.Blocks {
				if len(d.Preds(b)) > 1 {
					joins++
				}
				if !d.Dominates(f.Entry(), b) {
					t.Errorf("%s: func %s: entry does not dominate b%d", name, f.Name, b.ID)
				}
				idom := d.Idom(b)
				if (idom == nil) != (b == f.Entry()) {
					t.Errorf("%s: func %s: b%d has immediate dominator %v", name, f.Name, b.ID, idom)
					continue
				}
				for _, p := range d.Preds(b) {
					if idom != nil && !d.Dominates(idom, p) {
						t.Errorf("%s: func %s: idom b%d of b%d does not dominate predecessor b%d",
							name, f.Name, idom.ID, b.ID, p.ID)
					}
				}
				for _, a := range f.Blocks {
					if got := d.Dominates(a, b); got != dom[a.ID][b.ID] {
						t.Errorf("%s: func %s: Dominates(b%d, b%d) = %v, want %v", name, f.Name, a.ID, b.ID, got, !got)
					}
				}
				// b's frontier: the blocks c that b does not strictly
				// dominate but that have a predecessor b dominates.
				var df []*ir.Block
				for _, c := range f.Blocks {
					if dom[b.ID][c.ID] && b != c {
						continue
					}
					for _, p := range d.Preds(c) {
						if dom[b.ID][p.ID] {
							df = append(df, c)
							break
						}
					}
				}
				if got := d.Frontier(b); !slices.Equal(got, df) {
					t.Errorf("%s: func %s: Frontier(b%d) = %v, want %v", name, f.Name, b.ID, got, df)
				}
			}
		}
	}
	t.Logf("%d programs (%d corpus, %d generated): %d functions, %d blocks, %d join blocks",
		len(progs), corpusRun, len(progs)-corpusRun, funcs, blocks, joins)
}

// dominanceByDefinition reports for each pair of blocks a, b of f, all
// reachable, whether every path from the entry to b passes through a: that
// is, whether b is unreachable once a is removed.
func dominanceByDefinition(f *ir.Func) [][]bool {
	n := len(f.Blocks)
	dom := make([][]bool, n)
	for a := range n {
		seen := make([]bool, n)
		if a != 0 {
			seen[0] = true
			for work := []int{0}; len(work) > 0; {
				b := work[len(work)-1]
				work = work[:len(work)-1]
				for _, s := range ir.Succs(f.Blocks[b]) {
					if s.ID != a && !seen[s.ID] {
						seen[s.ID] = true
						work = append(work, s.ID)
					}
				}
			}
		}
		dom[a] = make([]bool, n)
		for b := range n {
			dom[a][b] = !seen[b]
		}
	}
	return dom
}
