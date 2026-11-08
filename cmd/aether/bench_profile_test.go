package main

// Dynamic profiling of Aether-built executables for the benchmark harness
// (BENCHMARKS.md). perf and other counter tools are not available in the
// reference environment, so dynamic counts come from instrumentation:
// instrument copies Emit's assembly with a 64-bit counter at the start of
// every straight-line region (after every label and after every
// conditional jump) and a probe of %rsp in every function's entry block.
// A region's count times its static contents gives exact dynamic counts of
// machine instructions and memory operands in the main.* functions; the
// runtime's helpers are not counted. The instrumented executable writes
// the peak stack and the counters to stdout, which Aether programs never
// write, so its stderr and exit status are still the program's.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/ir"
	"github.com/stevenstank/aether/internal/opt"
)

// memClass classifies a memory operand or implicit stack access.
type memClass int

const (
	mNone   memClass = iota - 1 // not a memory operand
	mSlot                       // -N(%rbp) in the slot area
	mValue                      // -N(%rbp) in the value-cell area
	mArgIn                      // +N(%rbp): this function's arguments and results
	mArgOut                     // N(%rsp): the outgoing area
	mGlobal                     // sym(%rip), program globals
	mLimit                      // aether.stacklimit(%rip), the prologue check
	mStack                      // push, pop, call, ret
	nClass
)

var classNames = [nClass]string{"slot", "value", "argin", "argout", "global", "limit", "stack"}

// regionStat is the static content of one straight-line region.
type regionStat struct {
	fn            string
	block         int // IR block whose label starts the region, or -1
	instrs        int
	loads, stores [nClass]int
	movRR         int            // register-to-register moves
	calls         int            // calls to Aether functions
	helperCalls   int            // calls to runtime helpers
	cellMoves     int            // movq between a frame cell and a register or immediate
	phiCopies     int            // stores of phi copies (jump and branch code)
	byOp          map[string]int // instructions by the IR op that emitted them
	cellMovesByOp map[string]int
}

var (
	reBlock    = regexp.MustCompile(`^\.L(\d+)\.b(\d+):$`)
	reLabel    = regexp.MustCompile(`^(\.L[\w.]+|\d+|main\.\w+):$`)
	reMainMain = regexp.MustCompile(`main\.main\b`)
	reStackQ   = regexp.MustCompile(`(aether\.mainstack:\n\t\.quad\t)(\d+)`)
)

// splitOperands splits an AT&T operand list at top-level commas.
func splitOperands(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

// classify returns the class of operand op, mNone if it is not in memory.
// slots is the function's slot count. A memory form Emit does not produce
// is an error.
func classify(op string, slots int) (memClass, error) {
	if !strings.Contains(op, "(") {
		return mNone, nil
	}
	switch {
	case strings.HasSuffix(op, "(%rip)"):
		if strings.HasPrefix(op, "aether.stacklimit") {
			return mLimit, nil
		}
		return mGlobal, nil
	case strings.HasSuffix(op, "(%rsp)"):
		return mArgOut, nil
	case strings.HasSuffix(op, "(%rbp)"):
		n, err := strconv.Atoi(strings.TrimSuffix(op, "(%rbp)"))
		if err != nil {
			return mNone, fmt.Errorf("unclassified operand %s", op)
		}
		if n > 0 {
			return mArgIn, nil
		}
		if -n <= 8*slots {
			return mSlot, nil
		}
		return mValue, nil
	}
	return mNone, fmt.Errorf("unclassified operand %s", op)
}

// account adds one instruction line to r. irOp is the IR op that emitted it.
func account(r *regionStat, line string, slots int, irOp string) error {
	f := strings.Fields(strings.TrimSpace(line))
	mn := f[0]
	var ops []string
	if len(f) > 1 {
		ops = splitOperands(strings.Join(f[1:], " "))
	}
	r.instrs++
	r.byOp[irOp]++
	switch mn {
	case "pushq":
		r.stores[mStack]++
		return nil
	case "popq", "ret":
		r.loads[mStack]++
		return nil
	case "call":
		r.stores[mStack]++ // the return address
		if strings.HasPrefix(ops[0], "main.") {
			r.calls++
		} else {
			r.helperCalls++
		}
		return nil
	}
	if mn == "leaq" {
		return nil // computes an address, reads no memory
	}
	isMov := strings.HasPrefix(mn, "mov") || mn == "cmovaeq"
	cls := make([]memClass, len(ops))
	for i, op := range ops {
		c, err := classify(op, slots)
		if err != nil {
			return err
		}
		cls[i] = c
		if c == mNone {
			continue
		}
		switch last := i == len(ops)-1; {
		case last && isMov:
			r.stores[c]++
		case last && (mn == "cmpq" || mn == "testq"):
			r.loads[c]++
		case last:
			return fmt.Errorf("read-modify-write memory destination in %q", line)
		default:
			r.loads[c]++
		}
	}
	if isMov && len(ops) == 2 && strings.HasPrefix(ops[0], "%") && strings.HasPrefix(ops[1], "%") {
		r.movRR++
	}
	if mn == "movq" && len(ops) == 2 {
		cell := func(c memClass) bool { return c == mSlot || c == mValue }
		other := func(op string) bool { return strings.HasPrefix(op, "%") || strings.HasPrefix(op, "$") }
		if cell(cls[0]) && other(ops[1]) || cell(cls[1]) && other(ops[0]) {
			r.cellMoves++
			r.cellMovesByOp[irOp]++
			if cell(cls[1]) && (irOp == "jump" || irOp == "branch") {
				r.phiCopies++
			}
		}
	}
	return nil
}

// instrumented is the result of instrumenting Emit's output.
type instrumented struct {
	asm     []byte
	regions []regionStat
}

// isInstr reports whether line is an instruction line of Emit's output.
func isInstr(line string) bool {
	return strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "\t.") && !strings.HasPrefix(line, "\t#")
}

// instrument inserts a counter at the start of every straight-line region
// of asm, Emit's output for prog, a probe of %rsp at the start of every
// function's entry block, and a wrapper around main.main that writes the
// peak stack and the counters to stdout. The inserted instructions only
// change the flags where they are dead: at a label or right after a
// conditional jump, Emit never reads the flags.
func instrument(asm []byte, prog *ir.Program) (instrumented, error) {
	var out bytes.Buffer
	var regions []regionStat
	slots := map[string]int{}
	for _, f := range prog.Funcs {
		slots["main."+f.Name] = len(f.Slots)
	}
	cur := ""
	irOp := ""
	open := -1 // the region instructions are added to
	start := func(block int) {
		regions = append(regions, regionStat{fn: cur, block: block, byOp: map[string]int{}, cellMovesByOp: map[string]int{}})
		open = len(regions) - 1
		fmt.Fprintf(&out, "\tincq\taether_prof+%d(%%rip)\n", 8*(open+1))
	}
	for _, line := range strings.Split(string(asm), "\n") {
		if m := funcType.FindStringSubmatch(line); m != nil {
			cur = m[1]
			irOp = "prologue"
		}
		if strings.HasPrefix(line, "\t# ") {
			// "# v4 = ne v16, v3 @6:8" or "# jump b1 @6:2"
			w := strings.Fields(line)
			irOp = w[1]
			if len(w) > 3 && w[2] == "=" {
				irOp = w[3]
			}
		}
		out.WriteString(line)
		out.WriteByte('\n')
		if cur == "" {
			continue
		}
		if strings.HasPrefix(line, "\t.size\t") {
			cur, open = "", -1
			continue
		}
		if reLabel.MatchString(line) {
			blk := -1
			if m := reBlock.FindStringSubmatch(line); m != nil {
				blk, _ = strconv.Atoi(m[2])
			}
			start(blk)
			if blk == 0 {
				out.WriteString("\tcmpq\taether_minrsp(%rip), %rsp\n\tjae\t9f\n\tmovq\t%rsp, aether_minrsp(%rip)\n9:\n")
			}
			continue
		}
		if !isInstr(line) {
			continue
		}
		if open < 0 {
			return instrumented{}, fmt.Errorf("instruction outside a region: %q", line)
		}
		if err := account(&regions[open], line, slots[cur], irOp); err != nil {
			return instrumented{}, err
		}
		if mn := strings.Fields(line)[0]; mn[0] == 'j' && mn != "jmp" {
			start(-1)
		}
	}
	s := reMainMain.ReplaceAllString(out.String(), "main.mainprof")
	// The wrapper adds 16 bytes below _start's call: its return address
	// and the alignment word.
	if !reStackQ.MatchString(s) {
		return instrumented{}, errors.New("no aether.mainstack in the assembly")
	}
	s = reStackQ.ReplaceAllStringFunc(s, func(m string) string {
		sm := reStackQ.FindStringSubmatch(m)
		n, _ := strconv.Atoi(sm[2])
		return sm[1] + strconv.Itoa(n+16)
	})
	n := len(regions) + 1
	s += fmt.Sprintf(`	.text
	.globl	main.main
	.type	main.main, @function
main.main:
	movq	%%rsp, aether_rsp0(%%rip)
	subq	$8, %%rsp
	call	main.mainprof
	addq	$8, %%rsp
	movq	aether_rsp0(%%rip), %%rax
	subq	aether_minrsp(%%rip), %%rax
	movq	%%rax, aether_prof(%%rip)
	movl	$1, %%eax
	movl	$1, %%edi
	leaq	aether_prof(%%rip), %%rsi
	movq	$%d, %%rdx
	syscall
	ret
	.size	main.main, .-main.main
	.data
	.p2align	3
aether_minrsp:
	.quad	-1
	.bss
	.p2align	3
aether_rsp0:
	.zero	8
aether_prof:
	.zero	%d
	.section	.note.GNU-stack,"",@progbits
`, 8*n, 8*n)
	return instrumented{asm: []byte(s), regions: regions}, nil
}

// dynStats are dynamic totals over a run.
type dynStats struct {
	instrs, movRR, calls, helperCalls int64
	cellMoves, phiCopies              int64
	byOp, cellMovesByOp               map[string]int64
	loads, stores                     [nClass]int64
	peakStack                         int64
	byFunc                            map[string]*dynStats
}

func (d *dynStats) addRegion(r regionStat, n int64) {
	d.instrs += n * int64(r.instrs)
	d.movRR += n * int64(r.movRR)
	d.calls += n * int64(r.calls)
	d.helperCalls += n * int64(r.helperCalls)
	d.cellMoves += n * int64(r.cellMoves)
	d.phiCopies += n * int64(r.phiCopies)
	if d.byOp == nil {
		d.byOp, d.cellMovesByOp = map[string]int64{}, map[string]int64{}
	}
	for op, k := range r.byOp {
		d.byOp[op] += n * int64(k)
	}
	for op, k := range r.cellMovesByOp {
		d.cellMovesByOp[op] += n * int64(k)
	}
	for c := range nClass {
		d.loads[c] += n * int64(r.loads[c])
		d.stores[c] += n * int64(r.stores[c])
	}
}

func (d *dynStats) mem() (n int64) {
	for c := range nClass {
		n += d.loads[c] + d.stores[c]
	}
	return n
}

func (d *dynStats) cell() int64 {
	return d.loads[mSlot] + d.stores[mSlot] + d.loads[mValue] + d.stores[mValue]
}

func (d *dynStats) abi() int64 {
	return d.loads[mArgIn] + d.stores[mArgIn] + d.loads[mArgOut] + d.stores[mArgOut] +
		d.loads[mStack] + d.stores[mStack] + d.loads[mLimit]
}

// benchRun executes exe and returns stdout, stderr, exit status, wall time,
// and user+system CPU time. Like runExe, it kills exe after runTimeout.
func benchRun(t *testing.T, exe string) (stdout, stderr []byte, code int, wall, cpu time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	var o, e bytes.Buffer
	var cmd *exec.Cmd
	var t0 time.Time
	for try := 0; ; try++ {
		cmd = exec.CommandContext(ctx, exe)
		cmd.WaitDelay = waitDelay
		cmd.Stdout, cmd.Stderr = &o, &e
		t0 = time.Now()
		err := cmd.Start()
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.ETXTBSY) || try == 50 {
			t.Fatalf("%s: %v", exe, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	err := cmd.Wait()
	wall = time.Since(t0)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("%s: killed after running for the %v timeout", exe, runTimeout)
		}
		if x, ok := err.(*exec.ExitError); !ok || x.ExitCode() < 0 {
			t.Fatalf("%s: %v", exe, err)
		}
	}
	ru := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	cpu = time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
	return o.Bytes(), e.Bytes(), cmd.ProcessState.ExitCode(), wall, cpu
}

// checkRun reports how a run with the given stderr and exit status differs
// from the expected result: stderr want and exit status 0.
func checkRun(stderr []byte, code int, want []byte) error {
	switch {
	case code != 0:
		return fmt.Errorf("exit status %d, want 0 (stderr %q)", code, stderr)
	case !bytes.Equal(stderr, want):
		return fmt.Errorf("stderr %q, want %q", stderr, want)
	}
	return nil
}

// compiledVariants are the variants Aether builds, each a step of Phase 5:
// slot-based (aether build -O0), promoted to SSA, with registers (D-026), with
// instruction selection (D-029), with ABI-R1 (D-031), with the register forms
// of arith.go, with constant folding, with dead code elimination, and with
// coalescing (EmitOptions.Coalesce), which is what aether build produces.
var compiledVariants = []string{"slot", "ssa", "ra", "isel", "abir1", "two", "fold", "dce", "copy"}

// compiledPipeline reports whether variant is one of the default build's
// pipeline with some of its passes and options: two, fold, dce, or copy.
func compiledPipeline(variant string) bool {
	return variant == "two" || variant == "fold" || variant == "dce" || variant == "copy"
}

// benchPasses are the passes of a compiled variant: none before fold.
func benchPasses(variant string) []pass {
	switch variant {
	case "fold":
		return []pass{opt.Fold}
	case "dce", "copy":
		return buildPasses
	}
	return nil
}

// benchEmit compiles src and emits it as the given compiled variant.
func benchEmit(t *testing.T, name string, src []byte, variant string) (*ir.Program, []byte) {
	t.Helper()
	prog, ds := compile([]source{{name, src}})
	if len(ds) > 0 {
		t.Fatalf("%s: %v", name, ds)
	}
	// isel is ABI-S0 with selection, the baseline of D-030; abir1 is what
	// aether build produced from D-031, two adds the register forms of
	// arith.go, fold folds constants (opt.Fold), dce also removes dead code
	// (opt.DCE), and copy, which also coalesces, is what aether build produces
	// now.
	if variant == "isel" || variant == "abir1" || compiledPipeline(variant) {
		opts := amd64.EmitOptions{Registers: true, Select: true, ABIR1: variant != "isel", TwoAddress: compiledPipeline(variant), Coalesce: variant == "copy"}
		if err := optimize(prog, benchPasses(variant)...); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		asm, err := amd64.EmitWith(prog, opts)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return prog, asm
	}
	if variant == "ssa" || variant == "ra" {
		if err := promoteProgram(prog, &promoteStats{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	asm, err := amd64.EmitWith(prog, amd64.EmitOptions{Registers: variant == "ra"})
	if err != nil {
		t.Fatal(err)
	}
	return prog, asm
}

// benchBuild is benchEmit followed by a link into exe; it also writes the
// assembly to exe.s.
func benchBuild(t *testing.T, name string, src []byte, variant string, exe string) (*ir.Program, []byte) {
	t.Helper()
	prog, asm := benchEmit(t, name, src, variant)
	if err := os.WriteFile(exe+".s", asm, 0o644); err != nil {
		t.Fatal(err)
	}
	if ds, err := assemble(asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
		t.Fatalf("%s: %v %v", name, ds, err)
	}
	return prog, asm
}

// profile builds and runs the instrumented copy of asm as exe and returns
// the dynamic totals and the per-region counts. The run must exit with
// status 0 and stderr want.
func profile(t *testing.T, prog *ir.Program, asm []byte, exe string, want []byte) (dynStats, instrumented, []int64) {
	t.Helper()
	ins, err := instrument(asm, prog)
	if err != nil {
		t.Fatalf("%s: %v", exe, err)
	}
	if err := os.WriteFile(exe+".s", ins.asm, 0o644); err != nil {
		t.Fatal(err)
	}
	if ds, err := assemble(ins.asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
		t.Fatalf("%s: %v %v", exe, ds, err)
	}
	stdout, stderr, code, _, _ := benchRun(t, exe)
	if err := checkRun(stderr, code, want); err != nil {
		t.Fatalf("%s: instrumented run: %v", exe, err)
	}
	if len(stdout) != 8*(len(ins.regions)+1) {
		t.Fatalf("%s: %d bytes of counters, want %d", exe, len(stdout), 8*(len(ins.regions)+1))
	}
	counts := make([]int64, len(ins.regions)+1)
	if err := binary.Read(bytes.NewReader(stdout), binary.LittleEndian, counts); err != nil {
		t.Fatal(err)
	}
	d := dynStats{peakStack: counts[0], byFunc: map[string]*dynStats{}}
	for i, r := range ins.regions {
		n := counts[i+1]
		d.addRegion(r, n)
		fd := d.byFunc[r.fn]
		if fd == nil {
			fd = &dynStats{}
			d.byFunc[r.fn] = fd
		}
		fd.addRegion(r, n)
	}
	return d, ins, counts[1:]
}

// blockFreq maps region counts back to the entry counts of the IR blocks
// of every function of prog, by function symbol.
func blockFreq(prog *ir.Program, ins instrumented, counts []int64) map[string][]int64 {
	out := map[string][]int64{}
	for _, f := range prog.Funcs {
		out["main."+f.Name] = make([]int64, len(f.Blocks))
	}
	for i, r := range ins.regions {
		if r.block >= 0 {
			if fr, ok := out[r.fn]; ok {
				fr[r.block] = counts[i]
			}
		}
	}
	return out
}
