package amd64

// Experiment ABI-R1 (D-030): EmitOptions.ABIR1. No built executable uses it:
// aether build stays on ABI-S0 (D-011), with and without -O0.
//
// Arguments 0-5 are in argRegs, and later ones at their ABI-S0 positions. One
// result is in %rax, and two or more are all at their ABI-S0 positions.
// Everything else is as in ABI-S0: every register but %rbp and %rsp is
// caller-saved, and no value lives in a register across a call.
//
// A value's allocated register may be another argument's register, so the moves
// at entry and at a call are parallel copies, with %rax as the temporary. The
// prologue's stack check uses only %rax, so the argument registers reach the
// entry copy intact. The entry copy runs once, right after the prologue, which
// is why checkR1Entry requires that nothing clobbers a register before the last
// param, and that entry cannot be reached again. A param with no use has no
// copy: the allocator may give its register to the next param.
//
// checkR1 verifies the emitted text independently of how it was sequenced.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stevenstank/aether/internal/ir"
)

// argRegs are the registers of arguments 0-5. %rax is not one, since the stack
// check needs it at entry; %rcx and %rdx stay reserved scratch.
var argRegs = [...]string{"%rdi", "%rsi", "%r8", "%r9", "%r10", "%r11"}

// abiR1 is what emitting a function under ABI-R1 keeps for checkR1.
type abiR1 struct {
	uses  map[*ir.Instr]int
	entry [2]int               // the entry copy's text, as offsets into the buffer
	sites map[*ir.Instr][2]int // each instruction's text
}

// outgoingWords is the outgoing area a call of c needs. Under ABI-R1 a call with
// no word to pass needs none; any other keeps the whole ABI-S0 area, unused words
// included, so that its stack words stay at their ABI-S0 positions.
func outgoingWords(c *ir.Func, r1 bool) int {
	p, r := len(c.Params), len(c.Results)
	if r1 && p <= len(argRegs) && r <= 1 {
		return 0
	}
	return p + r
}

// regCopy is one move of a parallel copy.
type regCopy struct{ dst, src string }

// parallelCopy emits moves as one parallel copy. A move is emitted once no
// remaining move reads its destination. If none is ready, the remaining moves
// are a permutation: saving one destination in %rax breaks its cycle, and %rax
// is free again by the time another cycle needs it.
func (e *emitter) parallelCopy(moves []regCopy) {
	moves = slices.DeleteFunc(slices.Clone(moves), func(m regCopy) bool { return m.dst == m.src })
	for len(moves) > 0 {
		i := slices.IndexFunc(moves, func(m regCopy) bool {
			return !slices.ContainsFunc(moves, func(n regCopy) bool { return n.src == m.dst })
		})
		if i < 0 {
			d := moves[0].dst
			e.op("movq\t%s, %%rax", d)
			for j := range moves {
				if moves[j].src == d {
					moves[j].src = "%rax"
				}
			}
			continue
		}
		e.move(moves[i].src, moves[i].dst)
		moves = slices.Delete(moves, i, i+1)
	}
}

// r1Entry emits the copy of the arguments in registers to their locations.
func (e *emitter) r1Entry() {
	var moves []regCopy
	for _, in := range e.f.Entry().Instrs {
		if in.Op == ir.Param && in.Index < len(argRegs) && e.r1.uses[in] > 0 {
			moves = append(moves, regCopy{e.val(in), argRegs[in.Index]})
		}
	}
	e.parallelCopy(moves)
}

// r1Param emits a param, which reads memory only if its argument is on the stack.
func (e *emitter) r1Param(in *ir.Instr) {
	if in.Index < len(argRegs) || e.r1.uses[in] == 0 {
		return
	}
	e.toMem(fmt.Sprintf("%d(%%rbp)", 16+8*in.Index), e.val(in))
}

// toMem copies src to dst, through %rax if both are in memory.
func (e *emitter) toMem(src, dst string) {
	if isReg(src) || isReg(dst) || isImm(src) {
		e.op("movq\t%s, %s", src, dst)
		return
	}
	e.op("movq\t%s, %%rax", src)
	e.op("movq\t%%rax, %s", dst)
}

// r1Call emits a call. The stack words go first: writing them reads the
// argument registers' old contents, and they may use %rax.
func (e *emitter) r1Call(in *ir.Instr) {
	var moves []regCopy
	for i, a := range in.Args {
		if i < len(argRegs) {
			moves = append(moves, regCopy{argRegs[i], e.val(a)})
		} else {
			e.toMem(e.val(a), fmt.Sprintf("%d(%%rsp)", 8*i))
		}
	}
	e.parallelCopy(moves)
	e.op("call\t%s", funcSym(in.Callee))
	switch in.Type {
	case ir.NoType:
	case ir.Tuple:
		for j := range in.Callee.Results {
			e.op("movq\t%d(%%rsp), %%rax", 8*(len(in.Args)+j))
			e.op("movq\t%%rax, %d(%%rbp)", e.fr.ValueOffset(e.tuples[in]+j))
		}
	default:
		e.op("movq\t%%rax, %s", e.val(in))
	}
}

// r1Ret emits a return.
func (e *emitter) r1Ret(in *ir.Instr) {
	if len(in.Args) == 1 {
		e.load(in.Args[0], "%rax")
	} else {
		for j, a := range in.Args {
			e.put(a, fmt.Sprintf("%d(%%rbp)", 16+8*(len(e.f.Params)+j)))
		}
	}
	e.op("movq\t%%rbp, %%rsp")
	e.op("popq\t%%rbp")
	e.op("ret")
}

// checkR1Entry checks D-030's entry precondition on the IR, which does not
// require it: the argument registers hold their values only at entry, so entry
// must have no predecessor, and before the last param only params and stores of
// them may run. A function with no param reads no argument register.
func checkR1Entry(f *ir.Func) error {
	entry := f.Entry()
	last := -1
	for i, in := range entry.Instrs {
		if in.Op == ir.Param {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	for _, b := range f.Blocks {
		if slices.Contains(ir.Succs(b), entry) {
			return fmt.Errorf("amd64: func %s: abi-r1: entry block has predecessor b%d", f.Name, b.ID)
		}
	}
	for _, in := range entry.Instrs[:last] {
		if in.Op != ir.Param && (in.Op != ir.Store || in.Args[0].Op != ir.Param) {
			return fmt.Errorf("amd64: func %s: abi-r1: %s runs before the last param", f.Name, in)
		}
	}
	return nil
}

// asmInstr is one parsed instruction: src alone for a single operand.
type asmInstr struct{ op, src, dst string }

// parseInstrs returns the instructions of text, without comments.
func parseInstrs(text []byte) []asmInstr {
	var out []asmInstr
	for l := range strings.SplitSeq(string(text), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l[0] == '#' {
			continue
		}
		op, rest, _ := strings.Cut(l, "\t")
		src, dst, _ := strings.Cut(rest, ", ")
		out = append(out, asmInstr{op, src, dst})
	}
	return out
}

// r1Sim tracks what moves leave in registers and memory. An operand nothing has
// written holds its own name, so a location reads as what it held before the
// sequence, and an immediate reads as itself.
type r1Sim struct {
	held map[string]string
	may  func(dst string) bool // what the sequence may write
}

func (s *r1Sim) read(op string) string {
	if v, ok := s.held[op]; ok {
		return v
	}
	return op
}

// run simulates instructions that may only be moves.
func (s *r1Sim) run(instrs []asmInstr) error {
	for _, in := range instrs {
		dst, val := in.dst, s.read(in.src)
		switch in.op {
		case "movq", "movabsq":
		case "movl", "xorl":
			// A 32-bit write of a constant, which zeroes the upper half.
			for r, sub := range subRegs {
				if sub[0] == in.dst {
					dst = r
				}
			}
			switch {
			case in.op == "xorl" && in.src == in.dst:
				val = "$0"
			case in.op == "movl" && isImm(in.src):
			default:
				return fmt.Errorf("unexpected %s %s, %s", in.op, in.src, in.dst)
			}
		default:
			return fmt.Errorf("unexpected %s %s", in.op, in.src)
		}
		if !s.may(dst) {
			return fmt.Errorf("writes %s", dst)
		}
		s.held[dst] = val
	}
	return nil
}

// checkR1 verifies the function e just emitted against ABI-R1, from the text of
// its entry copy, params, calls, and returns. Locations come from e.val, which
// checkAllocation and checkSelection cover.
func (e *emitter) checkR1() error {
	f, r1, text := e.f, e.r1, e.b.Bytes()
	site := func(in *ir.Instr) []asmInstr {
		s := r1.sites[in]
		return parseInstrs(text[s[0]:s[1]])
	}
	// Entry: each used param's location is its own, and ends up holding its
	// argument, and the copy writes nothing else.
	locs := map[string]bool{}
	for _, in := range f.Entry().Instrs {
		if in.Op == ir.Param && r1.uses[in] > 0 {
			if locs[e.val(in)] {
				return fmt.Errorf("two params share %s", e.val(in))
			}
			locs[e.val(in)] = true
		}
	}
	entry := &r1Sim{map[string]string{}, func(d string) bool { return d == "%rax" || locs[d] }}
	if err := entry.run(parseInstrs(text[r1.entry[0]:r1.entry[1]])); err != nil {
		return fmt.Errorf("entry copy: %v", err)
	}
	last := -1
	for i, in := range f.Entry().Instrs {
		if in.Op == ir.Param {
			last = i
		}
	}
	for i, in := range f.Entry().Instrs {
		switch {
		case in.Op == ir.Param && in.Index < len(argRegs) && r1.uses[in] > 0:
			if got := entry.read(e.val(in)); got != argRegs[in.Index] {
				return fmt.Errorf("%s: %s holds %s, not %s", in, e.val(in), got, argRegs[in.Index])
			}
		case in.Op == ir.Store && i < last:
			s := fmt.Sprintf("%d(%%rbp)", e.fr.SlotOffset(in.Slot.ID))
			sim := &r1Sim{map[string]string{}, func(d string) bool { return d == "%rax" || d == s }}
			if err := sim.run(site(in)); err != nil {
				return fmt.Errorf("%s: %v", in, err)
			}
		}
	}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			var err error
			switch in.Op {
			case ir.Param:
				err = e.checkR1Param(in, site(in))
			case ir.Call:
				err = e.checkR1Call(in, site(in))
			case ir.Ret:
				err = e.checkR1Ret(in, site(in))
			}
			if err != nil {
				return fmt.Errorf("%s: %v", in, err)
			}
		}
	}
	return nil
}

func (e *emitter) checkR1Param(in *ir.Instr, code []asmInstr) error {
	if in.Index < len(argRegs) || e.r1.uses[in] == 0 {
		if len(code) > 0 {
			return fmt.Errorf("%d instructions, want none", len(code))
		}
		return nil
	}
	dst, want := e.val(in), fmt.Sprintf("%d(%%rbp)", 16+8*in.Index)
	sim := &r1Sim{map[string]string{}, func(d string) bool { return d == "%rax" || d == dst }}
	if err := sim.run(code); err != nil {
		return err
	}
	if got := sim.read(dst); got != want {
		return fmt.Errorf("%s holds %s, not %s", dst, got, want)
	}
	return nil
}

func (e *emitter) checkR1Call(in *ir.Instr, code []asmInstr) error {
	c := in.Callee
	p, r := len(c.Params), len(c.Results)
	if (p > len(argRegs) || r > 1) && e.fr.Out < p+r {
		return fmt.Errorf("outgoing area of %d words, want at least %d", e.fr.Out, p+r)
	}
	call := slices.IndexFunc(code, func(x asmInstr) bool { return x.op == "call" })
	if call < 0 || code[call].src != funcSym(c) {
		return fmt.Errorf("no call of %s", funcSym(c))
	}
	may := map[string]bool{"%rax": true}
	want := map[string]string{}
	for i, a := range in.Args {
		loc := fmt.Sprintf("%d(%%rsp)", 8*i)
		if i < len(argRegs) {
			loc = argRegs[i]
		}
		may[loc], want[loc] = true, e.val(a)
	}
	before := &r1Sim{map[string]string{}, func(d string) bool { return may[d] }}
	if err := before.run(code[:call]); err != nil {
		return err
	}
	for i := range in.Args {
		loc := fmt.Sprintf("%d(%%rsp)", 8*i)
		if i < len(argRegs) {
			loc = argRegs[i]
		}
		if got := before.read(loc); got != want[loc] {
			return fmt.Errorf("argument %d: %s holds %s, not %s", i, loc, got, want[loc])
		}
	}
	// After the call, what the callee left is the only thing worth reading: %rax,
	// or the result words.
	res := map[string]string{}
	may = map[string]bool{"%rax": true}
	after := &r1Sim{map[string]string{}, func(d string) bool { return may[d] }}
	switch r {
	case 0:
	case 1:
		after.held["%rax"] = "the result"
		may[e.val(in)], res[e.val(in)] = true, "the result"
	default:
		for j := range r {
			word := fmt.Sprintf("%d(%%rsp)", 8*(p+j))
			cell := fmt.Sprintf("%d(%%rbp)", e.fr.ValueOffset(e.tuples[in]+j))
			after.held[word] = fmt.Sprintf("result %d", j)
			may[cell], res[cell] = true, after.held[word]
		}
	}
	if err := after.run(code[call+1:]); err != nil {
		return err
	}
	for j := range r {
		var loc string
		if r == 1 {
			loc = e.val(in)
		} else {
			loc = fmt.Sprintf("%d(%%rbp)", e.fr.ValueOffset(e.tuples[in]+j))
		}
		if got := after.read(loc); got != res[loc] {
			return fmt.Errorf("%s holds %s, not %s", loc, got, res[loc])
		}
	}
	return nil
}

func (e *emitter) checkR1Ret(in *ir.Instr, code []asmInstr) error {
	n := len(code) - 3
	if n < 0 || code[n] != (asmInstr{"movq", "%rbp", "%rsp"}) || code[n+1] != (asmInstr{"popq", "%rbp", ""}) || code[n+2] != (asmInstr{"ret", "", ""}) {
		return fmt.Errorf("no epilogue")
	}
	result := func(j int) string {
		if len(in.Args) == 1 {
			return "%rax"
		}
		return fmt.Sprintf("%d(%%rbp)", 16+8*(len(e.f.Params)+j))
	}
	may := map[string]bool{"%rax": true}
	for j := range in.Args {
		may[result(j)] = true
	}
	sim := &r1Sim{map[string]string{}, func(d string) bool { return may[d] }}
	if err := sim.run(code[:n]); err != nil {
		return err
	}
	for j, a := range in.Args {
		if got := sim.read(result(j)); got != e.val(a) {
			return fmt.Errorf("result %d: %s holds %s, not %s", j, result(j), got, e.val(a))
		}
	}
	return nil
}
