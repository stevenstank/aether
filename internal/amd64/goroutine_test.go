package amd64

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

// The shape of goGCProgram.
const (
	goWorkers = 3
	goRounds  = 4
	goNodes   = 200
)

// goGCProgram tests the collector's roots across goroutine stacks (D-048).
// Each of goWorkers goroutines, in each of goRounds rounds, builds a list
// of goNodes heap nodes whose only reference is in its own frame, switches
// to main, and, when resumed, prints the list's sum and allocates garbage.
// Main keeps an object referenced only from its frame while the workers
// run, and allocates garbage between the rounds; so collections run with
// main and workers suspended. A missing root shows, under AETHER_GCSTRESS,
// as a wrong sum or a crash.
func goGCProgram() *ir.Program {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	next := p.NewGlobal("next", ir.I64, "p.go", p1)
	var handles []*ir.Global
	for k := range goWorkers {
		handles = append(handles, p.NewGlobal(fmt.Sprintf("h%d", k), ir.I64, "p.go", p1))
	}

	// build(n, seed) returns a list of n nodes {next, seed+i}.
	build := p.NewFunc("build", "p.go", p1)
	build.Params = []ir.Type{ir.I64, ir.I64}
	build.Results = []ir.Type{ir.Ptr}
	{
		f := build
		list, i := f.NewSlot("list", ir.Ptr, p1), f.NewSlot("i", ir.I64, p1)
		n, seed := f.NewSlot("n", ir.I64, p1), f.NewSlot("seed", ir.I64, p1)
		b0, head, body, done := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Store(b0, n, f.Param(b0, 0, p1), p1)
		f.Store(b0, seed, f.Param(b0, 1, p1), p1)
		f.Store(b0, list, f.TypedConst(b0, ir.Ptr, 0, p1), p1)
		f.Store(b0, i, f.Const(b0, 0, p1), p1)
		f.Jump(b0, head, p1)
		f.Branch(head, f.Compare(head, ir.Lt, f.Load(head, i, p1), f.Load(head, n, p1), p1), body, done, p1)
		o := f.Alloc(body, f.Const(body, 16, p1), p1)
		f.StM(body, o, f.Const(body, 0, p1), f.Load(body, list, p1), p1)
		iv := f.Load(body, i, p1)
		f.StM(body, o, f.Const(body, 8, p1), f.Binary(body, ir.Add, f.Load(body, seed, p1), iv, p1), p1)
		f.Store(body, list, o, p1)
		f.Store(body, i, f.Binary(body, ir.Add, iv, f.Const(body, 1, p1), p1), p1)
		f.Jump(body, head, p1)
		f.Ret(done, p1, f.Load(done, list, p1))
	}

	// sum(list) returns the sum of the list's values.
	sum := p.NewFunc("sum", "p.go", p1)
	sum.Params = []ir.Type{ir.Ptr}
	sum.Results = []ir.Type{ir.I64}
	{
		f := sum
		cur, acc := f.NewSlot("cur", ir.Ptr, p1), f.NewSlot("acc", ir.I64, p1)
		b0, head, body, done := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Store(b0, cur, f.Param(b0, 0, p1), p1)
		f.Store(b0, acc, f.Const(b0, 0, p1), p1)
		f.Jump(b0, head, p1)
		f.Branch(head, f.Compare(head, ir.Ne, f.Load(head, cur, p1), f.TypedConst(head, ir.Ptr, 0, p1), p1), body, done, p1)
		c := f.Load(body, cur, p1)
		f.Store(body, acc, f.Binary(body, ir.Add, f.Load(body, acc, p1), f.LdM(body, ir.I64, c, f.Const(body, 8, p1), p1), p1), p1)
		f.Store(body, cur, f.LdM(body, ir.Ptr, c, f.Const(body, 0, p1), p1), p1)
		f.Jump(body, head, p1)
		f.Ret(done, p1, f.Load(done, acc, p1))
	}

	// churn(k) allocates k objects of garbage, each pointing to itself.
	churn := p.NewFunc("churn", "p.go", p1)
	churn.Params = []ir.Type{ir.I64}
	{
		f := churn
		i, k := f.NewSlot("i", ir.I64, p1), f.NewSlot("k", ir.I64, p1)
		b0, head, body, done := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		f.Store(b0, k, f.Param(b0, 0, p1), p1)
		f.Store(b0, i, f.Const(b0, 0, p1), p1)
		f.Jump(b0, head, p1)
		f.Branch(head, f.Compare(head, ir.Lt, f.Load(head, i, p1), f.Load(head, k, p1), p1), body, done, p1)
		o := f.Alloc(body, f.Const(body, 48, p1), p1)
		f.StM(body, o, f.Const(body, 0, p1), o, p1)
		f.Store(body, i, f.Binary(body, ir.Add, f.Load(body, i, p1), f.Const(body, 1, p1), p1), p1)
		f.Jump(body, head, p1)
		f.Ret(done, p1)
	}

	worker := p.NewFunc("worker", "p.go", p1)
	{
		f := worker
		id, r, list := f.NewSlot("id", ir.I64, p1), f.NewSlot("r", ir.I64, p1), f.NewSlot("list", ir.Ptr, p1)
		b0, head, body, done := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
		n := f.GLoad(b0, next, p1)
		f.GStore(b0, next, f.Binary(b0, ir.Add, n, f.Const(b0, 1, p1), p1), p1)
		f.Store(b0, id, n, p1)
		f.Store(b0, r, f.Const(b0, 0, p1), p1)
		f.Jump(b0, head, p1)
		f.Branch(head, f.Compare(head, ir.Lt, f.Load(head, r, p1), f.Const(head, goRounds, p1), p1), body, done, p1)
		seed := f.Binary(body, ir.Add, f.Binary(body, ir.Mul, f.Load(body, id, p1), f.Const(body, 100, p1), p1), f.Load(body, r, p1), p1)
		f.Store(body, list, f.Call(body, build, []*ir.Instr{f.Const(body, goNodes, p1), seed}, p1), p1)
		f.GoSwitch(body, f.Const(body, 0, p1), p1)
		s := f.Call(body, sum, []*ir.Instr{f.Load(body, list, p1)}, p1)
		f.Println(body, []*ir.Instr{f.Load(body, id, p1), f.Load(body, r, p1), s}, p1)
		f.Call(body, churn, []*ir.Instr{f.Const(body, 50, p1)}, p1)
		f.Store(body, r, f.Binary(body, ir.Add, f.Load(body, r, p1), f.Const(body, 1, p1), p1), p1)
		f.Jump(body, head, p1)
		f.GoExit(done, f.Const(done, 0, p1), p1)
	}

	f := main
	pass, mark := f.NewSlot("pass", ir.I64, p1), f.NewSlot("mark", ir.Ptr, p1)
	b0, head, body, done := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	for _, h := range handles {
		f.GStore(b0, h, f.GoNew(b0, worker, p1), p1)
	}
	m := f.Alloc(b0, f.Const(b0, 16, p1), p1)
	f.StM(b0, m, f.Const(b0, 8, p1), f.Const(b0, 777, p1), p1)
	f.Store(b0, mark, m, p1)
	f.Store(b0, pass, f.Const(b0, 0, p1), p1)
	f.Jump(b0, head, p1)
	f.Branch(head, f.Compare(head, ir.Le, f.Load(head, pass, p1), f.Const(head, goRounds, p1), p1), body, done, p1)
	for _, h := range handles {
		f.GoSwitch(body, f.GLoad(body, h, p1), p1)
		f.Call(body, churn, []*ir.Instr{f.Const(body, 200, p1)}, p1)
	}
	f.Store(body, pass, f.Binary(body, ir.Add, f.Load(body, pass, p1), f.Const(body, 1, p1), p1), p1)
	f.Jump(body, head, p1)
	f.Println(done, []*ir.Instr{f.LdM(done, ir.I64, f.Load(done, mark, p1), f.Const(done, 8, p1), p1)}, p1)
	f.Ret(done, p1)
	return p
}

// goGCWant is goGCProgram's output, worked out by hand.
func goGCWant() string {
	var w strings.Builder
	for r := range goRounds {
		for k := range goWorkers {
			fmt.Fprintln(&w, k, r, goNodes*(100*k+r)+goNodes*(goNodes-1)/2)
		}
	}
	w.WriteString("777\n")
	return w.String()
}

// executeEnv is execute with the environment env.
func executeEnv(t *testing.T, exe string, env []string) (string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := command(t, exe)
	cmd.Env = env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		cmd.checkTimeout(t, err)
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout: %q", stdout.String())
	}
	return stderr.String(), code
}

// buildVariant emits prog with the backend variant v and links it with
// the runtime files rt.
func buildVariant(t *testing.T, prog *ir.Program, v struct {
	name    string
	promote bool
	opts    EmitOptions
}, rt map[string][]byte) string {
	t.Helper()
	if v.promote {
		for _, f := range prog.Funcs {
			if err := ir.Promote(f); err != nil {
				t.Fatal(err)
			}
		}
	}
	asm, err := EmitWith(prog, v.opts)
	if err != nil {
		t.Fatalf("%s: %v", v.name, err)
	}
	files := map[string][]byte{"prog.s": asm}
	for name, data := range rt {
		files[name] = data
	}
	return link(t, t.TempDir(), files)
}

// runtimeFiles returns the runtime sources by name.
func runtimeFiles() map[string][]byte {
	rt := map[string][]byte{}
	for _, f := range Runtime() {
		rt[f.Name] = f.Data
	}
	return rt
}

var stressEnv = []string{"AETHER_GCSTRESS=1"}

// TestGoroutineGC runs goGCProgram in the interpreter and natively with
// every backend variant, with and without AETHER_GCSTRESS, and requires
// the output worked out by hand.
func TestGoroutineGC(t *testing.T) {
	want := goGCWant()
	var got bytes.Buffer
	if code, err := interp.Run(goGCProgram(), &got); err != nil || code != 0 || got.String() != want {
		t.Fatalf("interpreter: exit %d, %v, output %q, want %q", code, err, got.String(), want)
	}
	rt := runtimeFiles()
	for _, v := range memVariants {
		exe := buildVariant(t, goGCProgram(), v, rt)
		for _, env := range [][]string{nil, stressEnv} {
			if out, code := executeEnv(t, exe, env); code != 0 || out != want {
				t.Errorf("%s %v: exit %d, output %q, want %q", v.name, env, code, out, want)
			}
		}
	}
}

// TestGoroutineGCControl is the negative control of TestGoroutineGC: with
// the collector not scanning suspended goroutines' stacks (the call of
// .Lscangs removed from heap.s), goGCProgram must fail under
// AETHER_GCSTRESS, natively with every backend variant.
func TestGoroutineGCControl(t *testing.T) {
	rt := runtimeFiles()
	const call = "\tcall\t.Lscangs\n"
	heap := string(rt["heap.s"])
	if strings.Count(heap, call) != 1 {
		t.Fatalf("heap.s has %d lines %q, want 1", strings.Count(heap, call), call)
	}
	rt["heap.s"] = []byte(strings.Replace(heap, call, "", 1))
	want := goGCWant()
	for _, v := range memVariants {
		exe := buildVariant(t, goGCProgram(), v, rt)
		out, code := executeEnv(t, exe, stressEnv)
		if code == 0 && out == want {
			t.Errorf("%s: the program ran correctly without scanning suspended stacks", v.name)
		}
		t.Logf("%s: exit %d, output %.60q", v.name, code, out)
	}
}

// goReuseProgram starts n goroutines, one at a time; each counts itself in
// a global and ends. Main keeps a value computed before each switch live
// across it. n above interp.MaxGoroutines shows that ended goroutines'
// stacks are reused.
func goReuseProgram(n int64) *ir.Program {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	count := p.NewGlobal("count", ir.I64, "p.go", p1)
	g := p.NewFunc("g", "p.go", p1)
	gb := g.Entry()
	g.GStore(gb, count, g.Binary(gb, ir.Add, g.GLoad(gb, count, p1), g.Const(gb, 1, p1), p1), p1)
	g.GoExit(gb, g.Const(gb, 0, p1), p1)

	f := main
	i, acc := f.NewSlot("i", ir.I64, p1), f.NewSlot("acc", ir.I64, p1)
	b0, head, body, done := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	f.Store(b0, i, f.Const(b0, 0, p1), p1)
	f.Store(b0, acc, f.Const(b0, 0, p1), p1)
	f.Jump(b0, head, p1)
	f.Branch(head, f.Compare(head, ir.Lt, f.Load(head, i, p1), f.Const(head, n, p1), p1), body, done, p1)
	iv := f.Load(body, i, p1)
	x := f.Binary(body, ir.Mul, iv, f.Const(body, 3, p1), p1) // live across the switch
	f.GoSwitch(body, f.GoNew(body, g, p1), p1)
	f.Store(body, acc, f.Binary(body, ir.Add, f.Load(body, acc, p1), x, p1), p1)
	f.Store(body, i, f.Binary(body, ir.Add, iv, f.Const(body, 1, p1), p1), p1)
	f.Jump(body, head, p1)
	f.Println(done, []*ir.Instr{f.GLoad(done, count, p1), f.Load(done, acc, p1)}, p1)
	f.Ret(done, p1)
	return p
}

// goLimitProgram starts goroutines without running them, one more than
// interp.MaxGoroutines, so that no stack is left for the last.
func goLimitProgram() *ir.Program {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	g := p.NewFunc("g", "p.go", p1)
	gb := g.Entry()
	g.GoExit(gb, g.Const(gb, 0, p1), p1)
	f := main
	i := f.NewSlot("i", ir.I64, p1)
	b0, head, body, done := f.Entry(), f.NewBlock(), f.NewBlock(), f.NewBlock()
	f.Store(b0, i, f.Const(b0, 0, p1), p1)
	f.Jump(b0, head, p1)
	f.Branch(head, f.Compare(head, ir.Le, f.Load(head, i, p1), f.Const(head, interp.MaxGoroutines, p1), p1), body, done, p1)
	f.GoNew(body, g, p1)
	f.Store(body, i, f.Binary(body, ir.Add, f.Load(body, i, p1), f.Const(body, 1, p1), p1), p1)
	f.Jump(body, head, p1)
	f.Println(done, []*ir.Instr{f.Const(done, 1, p1)}, p1)
	f.Ret(done, p1)
	return p
}

// goInitProgram starts a goroutine in Init, which prints, switches back,
// and is resumed by main, where it prints, switches to itself, and ends;
// then main prints.
func goInitProgram() *ir.Program {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	init := p.NewFunc("init", "p.go", p1)
	p.Init = init
	h := p.NewGlobal("h", ir.I64, "p.go", p1)
	g := p.NewFunc("g", "p.go", p1)
	gb := g.Entry()
	g.Println(gb, []*ir.Instr{g.Const(gb, 1, p1)}, p1)
	g.GoSwitch(gb, g.Const(gb, 0, p1), p1)
	g.Println(gb, []*ir.Instr{g.Const(gb, 3, p1)}, p1)
	g.GoSwitch(gb, g.GLoad(gb, h, p1), p1) // itself
	g.Println(gb, []*ir.Instr{g.Const(gb, 4, p1)}, p1)
	g.GoExit(gb, g.Const(gb, 0, p1), p1)

	ib := init.Entry()
	hv := init.GoNew(ib, g, p1)
	init.GStore(ib, h, hv, p1)
	init.GoSwitch(ib, hv, p1)
	init.Println(ib, []*ir.Instr{init.Const(ib, 2, p1)}, p1)
	init.Ret(ib, p1)

	b := main.Entry()
	main.GoSwitch(b, main.GLoad(b, h, p1), p1)
	main.Println(b, []*ir.Instr{main.Const(b, 5, p1)}, p1)
	main.Ret(b, p1)
	return p
}

// goOverflowProgram runs unbounded recursion in a goroutine.
func goOverflowProgram() *ir.Program {
	p := &ir.Program{}
	main := p.NewFunc("main", "p.go", p1)
	rec := p.NewFunc("rec", "p.go", p1)
	rec.Params = []ir.Type{ir.I64}
	rec.Results = []ir.Type{ir.I64}
	rb := rec.Entry()
	r := rec.Call(rb, rec, []*ir.Instr{rec.Binary(rb, ir.Add, rec.Param(rb, 0, p1), rec.Const(rb, 1, p1), p1)}, p1)
	rec.Ret(rb, p1, rec.Binary(rb, ir.Add, r, rec.Const(rb, 1, p1), p1))
	g := p.NewFunc("g", "p.go", p1)
	gb := g.Entry()
	g.Println(gb, []*ir.Instr{g.Call(gb, rec, []*ir.Instr{g.Const(gb, 0, p1)}, p1)}, p1)
	g.GoExit(gb, g.Const(gb, 0, p1), p1)
	b := main.Entry()
	main.Println(b, []*ir.Instr{main.Const(b, 7, p1)}, p1)
	main.GoSwitch(b, main.GoNew(b, g, p1), p1)
	main.Ret(b, p1)
	return p
}

// TestGoroutinePrograms runs small goroutine programs in the interpreter
// and natively with every backend variant, and requires the output and
// exit status given.
func TestGoroutinePrograms(t *testing.T) {
	const n = interp.MaxGoroutines + 1000
	tests := []struct {
		name string
		prog func() *ir.Program
		want string
		code int
		// one runs only the default backend: the limit is the runtime's,
		// and each run touches a page of every slot (512 MiB).
		one bool
	}{
		{"reuse", func() *ir.Program { return goReuseProgram(n) }, fmt.Sprintln(n, 3*n*(n-1)/2), 0, false},
		{"init", goInitProgram, "1\n2\n3\n4\n5\n", 0, false},
		{"overflow", goOverflowProgram, "7\nfatal error: stack overflow\n", 2, false},
		{"limit", goLimitProgram, "fatal error: runtime: cannot allocate goroutine stack\n", 2, true},
	}
	rt := runtimeFiles()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bytes.Buffer
			if code, err := interp.Run(tt.prog(), &got); err != nil || code != tt.code || got.String() != tt.want {
				t.Fatalf("interpreter: exit %d, %v, output %q; want exit %d, %q", code, err, got.String(), tt.code, tt.want)
			}
			variants := memVariants
			if tt.one {
				variants = variants[len(variants)-1:]
			}
			for _, v := range variants {
				exe := buildVariant(t, tt.prog(), v, rt)
				if out, code := executeEnv(t, exe, nil); code != tt.code || out != tt.want {
					t.Errorf("%s: exit %d, output %q; want exit %d, %q", v.name, code, out, tt.code, tt.want)
				}
			}
		})
	}
}

// TestGoroutineDefects checks what breaks GoSwitch's and GoExit's
// preconditions, or a goroutine's function that returns: lowering never
// generates these, so they are defects, which the interpreter reports as
// errors and the native runtime ends with a fatal error.
func TestGoroutineDefects(t *testing.T) {
	tests := []struct {
		name  string
		build func(p *ir.Program, main *ir.Func, b *ir.Block, g *ir.Func)
		want  string
	}{
		{"main exits", func(p *ir.Program, main *ir.Func, b *ir.Block, g *ir.Func) {
			main.GoExit(b, main.GoNew(b, g, p1), p1)
		}, "fatal error: aether: bad goroutine switch\n"},
		{"exit to itself", func(p *ir.Program, main *ir.Func, b *ir.Block, g *ir.Func) {
			h := p.NewGlobal("h", ir.I64, "p.go", p1)
			gb := g.Entry()
			g.GoExit(gb, g.GLoad(gb, h, p1), p1)
			v := main.GoNew(b, g, p1)
			main.GStore(b, h, v, p1)
			main.GoSwitch(b, v, p1)
			main.Ret(b, p1)
		}, "fatal error: aether: bad goroutine switch\n"},
		{"switch to an ended goroutine", func(p *ir.Program, main *ir.Func, b *ir.Block, g *ir.Func) {
			v := main.GoNew(b, g, p1)
			main.GoSwitch(b, v, p1)
			main.GoSwitch(b, v, p1)
			main.Ret(b, p1)
		}, "fatal error: aether: bad goroutine switch\n"},
		{"function returns", func(p *ir.Program, main *ir.Func, b *ir.Block, g *ir.Func) {
			gb := g.Entry()
			g.Ret(gb, p1)
			main.GoSwitch(b, main.GoNew(b, g, p1), p1)
			main.Ret(b, p1)
		}, "fatal error: aether: goroutine function returned\n"},
	}
	rt := runtimeFiles()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := func() *ir.Program {
				p := &ir.Program{}
				main := p.NewFunc("main", "p.go", p1)
				g := p.NewFunc("g", "p.go", p1)
				tt.build(p, main, main.Entry(), g)
				if len(g.Entry().Instrs) == 0 {
					gb := g.Entry()
					g.GoExit(gb, g.Const(gb, 0, p1), p1)
				}
				return p
			}
			if code, err := interp.Run(prog(), &bytes.Buffer{}); err == nil {
				t.Errorf("interpreter: exit %d, no error", code)
			}
			for _, v := range memVariants {
				exe := buildVariant(t, prog(), v, rt)
				if out, code := executeEnv(t, exe, nil); code != 2 || out != tt.want {
					t.Errorf("%s: exit %d, output %q; want exit 2, %q", v.name, code, out, tt.want)
				}
			}
		})
	}
}
