package amd64

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// TestShareCells checks that values never live at once share cells, and
// slots never live at once share words (D-058), in a function not in SSA
// form and in one in SSA form, and that the result runs.
func TestShareCells(t *testing.T) {
	// main computes and prints 100 sums one after another, each through
	// a slot of its own: 400 values and 100 slots.
	build := func() *ir.Program {
		return program(func(f *ir.Func, b *ir.Block) {
			for i := range 100 {
				s := f.NewSlot("s", ir.I64, p1)
				f.Store(b, s, f.Binary(b, ir.Add, f.Const(b, int64(i), p1), f.Const(b, 1, p1), p1), p1)
				f.Println(b, []*ir.Instr{f.Load(b, s, p1)}, p1)
			}
		})
	}
	p := build()
	f := p.Funcs[0]
	plan := shareCells(f, nil, nil, false, false)
	word, words := shareSlots(f, false)
	if plan.n > 3 || words != 1 {
		t.Errorf("%d cells and %d words, want at most 3 and 1", plan.n, words)
	}
	if err := checkCells(f, plan, word, words); err != nil {
		t.Fatal(err)
	}
	unique := shareCells(f, nil, nil, false, true)
	if _, n := shareSlots(f, true); unique.n != 400 || n != 100 {
		t.Errorf("unique: %d cells and %d words, want 400 and 100", unique.n, n)
	}
	asm, err := Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for i := range 100 {
		want += strconv.Itoa(i+1) + "\n"
	}
	if got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || got != want {
		t.Errorf("exit %d, %q", code, got)
	}
	// In SSA form, after promotion: the loads are gone.
	p = build()
	if err := ir.Promote(p.Funcs[0]); err != nil {
		t.Fatal(err)
	}
	f = p.Funcs[0]
	if plan := shareCells(f, nil, nil, false, false); plan.n > 3 {
		t.Errorf("SSA: %d cells, want at most 3", plan.n)
	} else if err := checkCells(f, plan, nil, 0); err != nil {
		t.Fatal(err)
	}
}

// TestCheckCellsRejects checks that checkCells finds plans in which a
// value or a slot is read from storage another one wrote.
func TestCheckCellsRejects(t *testing.T) {
	p := program(func(f *ir.Func, b *ir.Block) {
		x, y := f.Const(b, 1, p1), f.Const(b, 2, p1)
		s1, s2 := f.NewSlot("a", ir.I64, p1), f.NewSlot("b", ir.I64, p1)
		f.Store(b, s1, x, p1)
		f.Store(b, s2, y, p1)
		f.Println(b, []*ir.Instr{x, y, f.Load(b, s1, p1), f.Load(b, s2, p1)}, p1)
	})
	f := p.Funcs[0]
	in := f.Entry().Instrs
	x, y := in[0], in[1]
	plan := shareCells(f, nil, nil, false, false)
	word, words := shareSlots(f, false)
	if err := checkCells(f, plan, word, words); err != nil {
		t.Fatal(err)
	}
	if plan.cells[x] == plan.cells[y] || word[0] == word[1] {
		t.Fatalf("x and y, or the slots, share storage: %v %v", plan.cells, word)
	}
	bad := cellPlan{cells: map[*ir.Instr]int{}, tuples: plan.tuples, n: plan.n}
	for v, c := range plan.cells {
		bad.cells[v] = c
	}
	bad.cells[y] = bad.cells[x]
	if err := checkCells(f, bad, word, words); err == nil || !strings.Contains(err.Error(), "another value wrote") {
		t.Errorf("shared cell: %v", err)
	}
	if err := checkCells(f, plan, []int{0, 0}, 2); err == nil || !strings.Contains(err.Error(), "another slot wrote") {
		t.Errorf("shared word: %v", err)
	}
	if err := checkCells(f, plan, []int{0, 2}, 2); err == nil || !strings.Contains(err.Error(), "word 2 of 2") {
		t.Errorf("word out of range: %v", err)
	}

	// Two φs of a block in one cell.
	q := &ir.Program{}
	g := q.NewFunc("main", "p.go", p1)
	g.SSA = true
	entry, join := g.Entry(), g.NewBlock()
	a, c := g.Const(entry, 1, p1), g.Const(entry, 2, p1)
	g.Jump(entry, join, p1)
	phi1, phi2 := g.Phi(join, ir.I64, p1), g.Phi(join, ir.I64, p1)
	phi1.AddIncoming(ir.Edge{From: entry, Index: 0}, a)
	phi2.AddIncoming(ir.Edge{From: entry, Index: 0}, c)
	g.Println(join, []*ir.Instr{phi1, phi2}, p1)
	g.Ret(join, p1)
	if errs := ir.Verify(q); len(errs) > 0 {
		t.Fatal(errs)
	}
	plan = shareCells(g, nil, nil, false, false)
	if err := checkCells(g, plan, nil, 0); err != nil {
		t.Fatal(err)
	}
	bad = cellPlan{cells: map[*ir.Instr]int{a: 0, c: 1, phi1: 2, phi2: 2}, n: 3}
	if err := checkCells(g, bad, nil, 0); err == nil || !strings.Contains(err.Error(), "shares cell 2 with another φ") {
		t.Errorf("φs in one cell: %v", err)
	}
}
