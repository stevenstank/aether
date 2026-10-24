package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/ir"
)

// TestLowerCorpus compiles every corpus file. A program expected to run must
// lower to verified IR equal to its reviewed *.ir golden file (updated with
// -update); any other program must yield no IR, because it is rejected
// before lowering.
func TestLowerCorpus(t *testing.T) {
	for _, path := range corpus(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			e := readExpectation(t, path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			prog, ds := compile([]source{{path, data}})
			if e.class != "run" {
				if prog != nil || len(ds) == 0 {
					t.Fatalf("%s program was lowered", e.class)
				}
				for _, d := range ds {
					if d.Class == diag.Internal {
						t.Errorf("internal diagnostic: %s", d)
					}
				}
				return
			}
			if prog == nil || len(ds) != 0 {
				t.Fatalf("no IR: %v", ds)
			}
			got := prog.String()
			if again, _ := compile([]source{{path, data}}); again.String() != got {
				t.Fatalf("lowering is not deterministic:\n%s\nvs\n%s", got, again)
			}
			golden := strings.TrimSuffix(path, ".go") + ".ir"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden file (run go test -update): %v", err)
			}
			if got != string(want) {
				t.Errorf("IR differs from %s\ngot:\n%s\nwant:\n%s", golden, got, want)
			}
		})
	}
}

// TestVerifierErrorsAreInternal checks that malformed IR is reported as an
// internal error, with exit status 4, naming the verifier and the component.
func TestVerifierErrorsAreInternal(t *testing.T) {
	prog := &ir.Program{}
	f := prog.NewFunc("main", "p.go", ir.Pos{Line: 3, Col: 6})
	f.Println(f.Entry(), nil, ir.Pos{Line: 4, Col: 2}) // no terminator
	ds := verify(prog, "p.go")
	if len(ds) == 0 {
		t.Fatal("malformed IR accepted")
	}
	if got := diag.ExitCode(ds); got != diag.ExitInternal {
		t.Errorf("exit code %d, want %d", got, diag.ExitInternal)
	}
	want := "p.go: internal: IR verifier: func main: b0: instr 0 (println): missing terminator"
	if got := ds[0].String(); !strings.HasPrefix(got, want) {
		t.Errorf("got %q, want prefix %q", got, want)
	}
}
