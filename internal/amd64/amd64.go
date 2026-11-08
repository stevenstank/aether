// Package amd64 is Aether's Linux x86-64 backend (D-007). It turns
// verified IR into GNU assembly in AT&T syntax and supplies the runtime
// sources the program is linked with.
//
// The Phase 3 backend is deliberately naive. Every slot and every IR value
// lives in its own 8-byte stack slot in an rbp-based frame; each instruction
// loads its operands into %rax, computes, and stores the result. There is no
// optimization, so every IR instruction maps to a fixed instruction sequence
// (docs/ARCHITECTURE.md §7).
//
// A function in SSA form (D-025) is translated the same way. A phi has a
// cell like any value and emits no code: each edge into its block copies
// the phi's operand for that edge into the phi's cell, as one parallel
// copy for all of the block's phis (edgeCopies).
//
// EmitWith can also keep values of SSA functions in registers instead of
// cells (experiment RA-0, D-026; regalloc.go), as aether build asks it to
// (D-027), use immediates and fused branches (select.go, D-029), and pass
// arguments and a result in registers (ABI-R1, abir1.go, D-031). Emit does none
// of these.
//
// The package sees only IR: it imports no go/* package.
package amd64

import (
	"bytes"
	"fmt"
	"math"
	"slices"

	"github.com/stevenstank/aether/internal/ir"
)

// Runtime helper symbols (docs/ARCHITECTURE.md §Runtime integration
// boundary). They take their argument, if any, in %rdi, return nothing,
// require %rsp to be 16-byte aligned at the call, and preserve %rbx, %rbp,
// and %r12-%r15.
const (
	symPrintInt   = "aether.printint"
	symPrintUint  = "aether.printuint"
	symPrintBool  = "aether.printbool"
	symPrintSpace = "aether.printsp"
	symPrintNL    = "aether.printnl"
)

// panicSyms are the runtime's panic helpers, by run-time error (D-023):
// each writes its message and exits with status 2, and never returns.
var panicSyms = [...]string{ir.PanicDivide: "aether.panicdivide", ir.PanicShift: "aether.panicshift"}

// symMainStack is the read-only 8-byte datum through which the program tells
// the runtime how many bytes of stack main.main or main.init, whichever
// needs more, uses below _start's call: the return address, the saved %rbp,
// and the frame. _start maps a stack that large, plus what the runtime
// helpers need (D-022).
const symMainStack = "aether.mainstack"

// maxFrame bounds a frame so that its size and every rbp-relative offset fit
// a signed 32-bit displacement with room to spare.
const maxFrame = 1 << 30

// symStackLimit is the runtime's 8-byte stack limit L, and
// symStackOverflow the handler a failed prologue check jumps to (D-009,
// D-011).
const (
	symStackLimit    = "aether.stacklimit"
	symStackOverflow = "aether.stackoverflow"
)

// Frame is the stack layout of one function under ABI-S0 (D-011). Above the
// saved %rbp are the return address and the caller's outgoing area, which
// holds this function's arguments and results. Below it, the slots come
// first, then the values, then this function's own outgoing area:
//
//	16+8*i(%rbp)             argument i, then result j at i = P+j
//	 0(%rbp)                 saved %rbp
//	-8(%rbp) .. -8*S(%rbp)   slot s0 .. s(S-1)
//	-8*(S+1)(%rbp) ..        value cell 0 .. V-1
//	 8*k(%rsp)               outgoing word k, k < Out
//
// Size is 8*(S+V+Out) rounded up to a multiple of 16. On entry %rsp is 8
// mod 16 (the caller's call pushed the return address onto an aligned
// stack); the push of %rbp makes it 0 mod 16, and subtracting Size keeps it
// so, which is the alignment every call requires. The prologue checks the
// whole frame against the runtime's stack limit before writing to it.
type Frame struct {
	Slots, Values, Out int
	Size               int
}

// NewFrame lays out a frame for the given number of slots, values, and
// outgoing words. It fails, rather than emit invalid assembly, if the frame
// would be larger than maxFrame.
func NewFrame(slots, values, out int) (Frame, error) {
	if slots < 0 || values < 0 || out < 0 || slots > maxFrame/8 || values > maxFrame/8-slots || out > maxFrame/8-slots-values {
		return Frame{}, fmt.Errorf("frame of %d slots, %d values, and %d outgoing words exceeds %d bytes", slots, values, out, maxFrame)
	}
	size := 8 * (slots + values + out)
	size = (size + 15) &^ 15
	if size > maxFrame {
		return Frame{}, fmt.Errorf("frame of %d bytes exceeds %d bytes", size, maxFrame)
	}
	return Frame{Slots: slots, Values: values, Out: out, Size: size}, nil
}

// SlotOffset returns the %rbp-relative offset of slot i.
func (f Frame) SlotOffset(i int) int { return -8 * (i + 1) }

// ValueOffset returns the %rbp-relative offset of value cell i.
func (f Frame) ValueOffset(i int) int { return -8 * (f.Slots + i + 1) }

// Emit returns the assembly for prog. It verifies prog first; malformed IR,
// or IR this backend cannot translate, is an error, and no assembly is
// returned. The output depends only on prog: it contains no paths, times,
// or host data.
func Emit(prog *ir.Program) ([]byte, error) { return EmitWith(prog, EmitOptions{}) }

// EmitOptions select variants of the backend.
type EmitOptions struct {
	// Registers keeps values of SSA functions in registers where
	// allocate assigns them (RA-0, D-026). Every allocation is checked
	// with checkAllocation; one that fails is an error.
	Registers bool
	// Select uses immediates and fuses comparisons into branches
	// (select.go). Every selection is checked with checkSelection; one that
	// fails is an error.
	Select bool
	// ABIR1 uses the calling convention ABI-R1 of D-030 (abir1.go) for the
	// whole program, as aether build does since D-031; without it, ABI-S0
	// (D-011). Every entry, call, and return is checked with checkR1; one that
	// fails is an error.
	ABIR1 bool
	// TwoAddress computes arithmetic and conversions in their result's
	// register (arith.go), as aether build does; it needs Select. Every such
	// sequence is checked with checkArith; one that fails is an error.
	TwoAddress bool
	// Coalesce gives values the registers that make moves unnecessary
	// where one is free (preferences in regalloc.go), as aether build
	// does; it needs Registers. The allocation is checked as without it.
	Coalesce bool
}

// EmitWith is Emit with options.
func EmitWith(prog *ir.Program, opts EmitOptions) ([]byte, error) {
	if errs := ir.Verify(prog); len(errs) > 0 {
		return nil, fmt.Errorf("amd64: malformed IR: %v", errs[0])
	}
	var b bytes.Buffer
	b.WriteString("# Generated by Aether's amd64 backend.\n")
	b.WriteString("\t.text\n")
	// Verify guarantees exactly one main function.
	var stack int
	for fi, f := range prog.Funcs {
		fr, err := emitFunc(&b, fi, f, opts)
		if err != nil {
			return nil, err
		}
		if f.Name == "main" || f == prog.Init {
			stack = max(stack, 16+fr.Size)
		}
	}
	if prog.Init == nil {
		// _start always calls main.init (D-009); without package
		// initialization it only returns.
		fmt.Fprintf(&b, "\t.globl\t%s\n\t.type\t%s, @function\n%s:\n\tret\n\t.size\t%s, .-%s\n", symInit, symInit, symInit, symInit, symInit)
	}
	if len(prog.Globals) > 0 {
		// Each global is 8 bytes in canonical form, zero at startup.
		b.WriteString("\t.bss\n\t.p2align\t3\n")
		for _, g := range prog.Globals {
			sym := globalSym(g)
			fmt.Fprintf(&b, "\t.type\t%s, @object\n\t.size\t%s, 8\n%s:\n\t.zero\t8\n", sym, sym, sym)
		}
	}
	fmt.Fprintf(&b, "\t.section\t.rodata\n\t.p2align\t3\n\t.globl\t%s\n\t.type\t%s, @object\n\t.size\t%s, 8\n%s:\n\t.quad\t%d\n",
		symMainStack, symMainStack, symMainStack, symMainStack, stack)
	// No executable stack.
	b.WriteString("\t.section\t.note.GNU-stack,\"\",@progbits\n")
	return b.Bytes(), nil
}

// funcSym is the symbol of an Aether-compiled function (D-011). The
// program's Init is main.init, which _start calls before main.main.
func funcSym(f *ir.Func) string { return "main." + f.Name }

const symInit = "main.init"

// globalSym is the symbol of a global variable, local to the program's
// object file. Go gives package-level variables and functions distinct
// names, and no variable is named init or main.
func globalSym(g *ir.Global) string { return "main." + g.Name }

// emitFunc emits f, the program's function number fi, and returns its
// frame.
func emitFunc(b *bytes.Buffer, fi int, f *ir.Func, opts EmitOptions) (Frame, error) {
	if opts.ABIR1 {
		if err := checkR1Entry(f); err != nil {
			return Frame{}, err
		}
	}
	regs := map[*ir.Instr]int{}
	var sel *selection
	none := valueSet{}
	if opts.Select {
		sel = selectFunc(f)
		sel.twoAddr = opts.TwoAddress
		none = sel.noLocation()
	}
	if opts.Registers {
		var err error
		if regs, err = allocateExcept(f, none, opts.Coalesce); err != nil {
			return Frame{}, err
		}
		if err := checkAllocation(f, regs); err != nil {
			return Frame{}, err
		}
	}
	if sel != nil {
		if err := checkSelection(f, sel, regs); err != nil {
			return Frame{}, err
		}
	}
	// Each value without a register has a cell, numbered by the rank of
	// its value number among those values. Lowering numbers values
	// densely, so without registers the cell is the value number; slot
	// promotion removes loads and leaves gaps, which take no cells.
	var vals []*ir.Instr
	out := 0
	for _, blk := range f.Blocks {
		for _, in := range blk.Instrs {
			if _, ok := regs[in]; in.ID >= 0 && !ok && !none[in] {
				vals = append(vals, in)
			}
			if in.Op == ir.Call {
				out = max(out, outgoingWords(in.Callee, opts.ABIR1))
			}
		}
	}
	slices.SortFunc(vals, func(x, y *ir.Instr) int { return x.ID - y.ID })
	cells := map[*ir.Instr]int{}
	for i, in := range vals {
		cells[in] = i
	}
	values := len(vals)
	// A Tuple call has its own cells, one per result, after the values'
	// cells (D-023 tuple storage); tuples maps it to the first one.
	tuples := map[*ir.Instr]int{}
	for _, blk := range f.Blocks {
		for _, in := range blk.Instrs {
			if in.Type == ir.Tuple {
				tuples[in] = values
				values += len(in.Callee.Results)
			}
		}
	}
	fr, err := NewFrame(len(f.Slots), values, out)
	if err != nil {
		return Frame{}, fmt.Errorf("amd64: func %s: %v", f.Name, err)
	}
	sym := funcSym(f)
	fmt.Fprintf(b, "\t.globl\t%s\n\t.type\t%s, @function\n%s:\n", sym, sym, sym)
	// The bound check comes before anything is written below the return
	// address: entry %rsp must be at least L + Size + 8 (D-011). %rax is
	// free, since neither ABI-S0 nor ABI-R1 passes anything in it at entry.
	fmt.Fprintf(b, "\tmovq\t%s(%%rip), %%rax\n\taddq\t$%d, %%rax\n\tcmpq\t%%rax, %%rsp\n\tjb\t%s\n",
		symStackLimit, fr.Size+8, symStackOverflow)
	b.WriteString("\tpushq\t%rbp\n\tmovq\t%rsp, %rbp\n")
	if fr.Size > 0 {
		fmt.Fprintf(b, "\tsubq\t$%d, %%rsp\n", fr.Size)
	}
	// Blocks are emitted in order, each under a local label; the entry
	// block follows the prologue.
	label := func(blk *ir.Block) string { return fmt.Sprintf(".L%d.b%d", fi, blk.ID) }
	e := &emitter{b: b, fr: fr, f: f, fi: fi, label: label, cells: cells, regs: regs, tuples: tuples, sel: sel}
	var blocks []*ir.Block
	if sel != nil {
		sel.fwd = e.forwarders()
	}
	if opts.ABIR1 {
		e.r1 = &abiR1{uses: useCounts(f), sites: map[*ir.Instr][2]int{}}
		e.r1.entry[0] = b.Len()
		e.r1Entry()
		e.r1.entry[1] = b.Len()
	}
	for _, blk := range f.Blocks {
		if sel == nil || sel.fwd[blk] == nil {
			blocks = append(blocks, blk)
		}
	}
	for i, blk := range blocks {
		e.next = nil
		if i+1 < len(blocks) {
			e.next = blocks[i+1]
		}
		fmt.Fprintf(b, "%s:\n", label(blk))
		for _, in := range blk.Instrs {
			fmt.Fprintf(b, "\t# %s @%s\n", in, in.Pos)
			start := b.Len()
			if err := e.instr(blk, in); err != nil {
				return Frame{}, fmt.Errorf("amd64: func %s: %s: %v", f.Name, in, err)
			}
			if e.r1 != nil {
				e.r1.sites[in] = [2]int{start, b.Len()}
			}
		}
	}
	if err := e.err; err != nil {
		return Frame{}, fmt.Errorf("amd64: func %s: %v", f.Name, err)
	}
	if err := e.checkArith(); err != nil {
		return Frame{}, fmt.Errorf("amd64: func %s: two-address: %v", f.Name, err)
	}
	if e.r1 != nil {
		if err := e.checkR1(); err != nil {
			return Frame{}, fmt.Errorf("amd64: func %s: abi-r1: %v", f.Name, err)
		}
	}
	fmt.Fprintf(b, "\t.size\t%s, .-%s\n", sym, sym)
	return fr, nil
}

// setcc maps a comparison to the set instruction that tests its signed
// condition after "cmpq y, x" (which computes x - y), and setccUnsigned
// to the one that tests its unsigned condition. Canonical values of an
// unsigned type are zero-extended, so a 64-bit unsigned comparison is
// exact for every width; sign-extended values make the signed one exact.
var (
	setcc         = map[ir.Op]string{ir.Eq: "sete", ir.Ne: "setne", ir.Lt: "setl", ir.Le: "setle", ir.Gt: "setg", ir.Ge: "setge"}
	setccUnsigned = map[ir.Op]string{ir.Eq: "sete", ir.Ne: "setne", ir.Lt: "setb", ir.Le: "setbe", ir.Gt: "seta", ir.Ge: "setae"}
)

// canonical maps an integer type narrower than 64 bits to the instruction
// that puts %rax in its canonical form (D-023): the low bits sign- or
// zero-extended. A 32-bit move to %eax zeroes the upper half.
var canonical = map[ir.Type]string{
	ir.I8: "movsbq\t%al, %rax", ir.I16: "movswq\t%ax, %rax", ir.I32: "movslq\t%eax, %rax",
	ir.U8: "movzbl\t%al, %eax", ir.U16: "movzwl\t%ax, %eax", ir.U32: "movl\t%eax, %eax",
}

// emitter holds what translating one function's instructions needs.
type emitter struct {
	b      *bytes.Buffer
	fr     Frame
	f      *ir.Func
	fi     int // the function's index in the program
	label  func(*ir.Block) string
	cells  map[*ir.Instr]int // the cell of each value without a register
	regs   map[*ir.Instr]int // the register of each value with one, in allocRegs
	tuples map[*ir.Instr]int // each Tuple call's first result cell
	sel    *selection        // instruction selection, or nil (select.go)
	r1     *abiR1            // ABI-R1's state, or nil for ABI-S0 (abir1.go)
	next   *ir.Block         // the block emitted after the current one, or nil
	arith  []arithSite       // the register-form sequences (arith.go)
	err    error             // the first error of the current function
}

// val is the location of v: its register or its cell, or the operand of an
// immediate. A value with no location that is read is an internal error.
func (e *emitter) val(v *ir.Instr) string {
	if e.sel != nil {
		if c, ok := e.sel.imm[v]; ok {
			return fmt.Sprintf("$%d", c)
		}
	}
	if r, ok := e.regs[v]; ok {
		return allocRegs[r]
	}
	cell, ok := e.cells[v]
	if !ok {
		e.fail(fmt.Errorf("v%d has no location", v.ID))
	}
	return fmt.Sprintf("%d(%%rbp)", e.fr.ValueOffset(cell))
}

// op emits one instruction.
func (e *emitter) op(format string, args ...any) {
	e.b.WriteByte('\t')
	fmt.Fprintf(e.b, format, args...)
	e.b.WriteByte('\n')
}

// instr emits in, an instruction of block blk.
func (e *emitter) instr(blk *ir.Block, in *ir.Instr) error {
	b, fr, f, label, tuples, val, op := e.b, e.fr, e.f, e.label, e.tuples, e.val, e.op
	res := func() string { return val(in) }
	if e.sel != nil && e.selected(blk, in) {
		return e.err
	}
	switch in.Op {
	case ir.Const:
		if in.Const >= math.MinInt32 && in.Const <= math.MaxInt32 {
			// The 32-bit immediate is sign-extended to 64 bits.
			op("movq\t$%d, %s", in.Const, res())
		} else {
			op("movabsq\t$%d, %%rax", in.Const)
			op("movq\t%%rax, %s", res())
		}
	case ir.Load:
		op("movq\t%d(%%rbp), %%rax", fr.SlotOffset(in.Slot.ID))
		op("movq\t%%rax, %s", res())
	case ir.Store:
		e.put(in.Args[0], fmt.Sprintf("%d(%%rbp)", fr.SlotOffset(in.Slot.ID)))
	case ir.GLoad:
		op("movq\t%s(%%rip), %%rax", globalSym(in.Global))
		op("movq\t%%rax, %s", res())
	case ir.GStore:
		e.put(in.Args[0], globalSym(in.Global)+"(%rip)")
	case ir.Add, ir.Sub, ir.Mul, ir.And, ir.Or, ir.Xor:
		// addq, subq, and the two-operand imulq keep the low 64 bits of
		// the result, whose low width bits are exact, and so do the bitwise
		// operations; canonicalizing then gives the IR's wrapping
		// semantics at every width. The flags they set are not used.
		mnemonic := map[ir.Op]string{ir.Add: "addq", ir.Sub: "subq", ir.Mul: "imulq", ir.And: "andq", ir.Or: "orq", ir.Xor: "xorq"}[in.Op]
		e.load(in.Args[0], "%rax")
		op("%s\t%s, %%rax", mnemonic, val(in.Args[1]))
		canon(op, in.Type)
		op("movq\t%%rax, %s", res())
	case ir.AndNot:
		e.load(in.Args[1], "%rcx")
		op("notq\t%%rcx")
		e.load(in.Args[0], "%rax")
		op("andq\t%%rcx, %%rax")
		canon(op, in.Type)
		op("movq\t%%rax, %s", res())
	case ir.Div, ir.Rem:
		// Lowering guarantees a nonzero divisor. A signed division by -1
		// is done without idivq, which faults on the minimum divided by
		// -1 (E10): the quotient is the negation, wrapping, and the
		// remainder 0. Numeric local labels keep each sequence's own.
		e.load(in.Args[1], "%rcx")
		e.load(in.Args[0], "%rax")
		if in.Type.Signed() {
			op("cmpq\t$-1, %%rcx")
			op("jne\t1f")
			if in.Op == ir.Div {
				op("negq\t%%rax")
			} else {
				op("xorl\t%%eax, %%eax")
			}
			op("jmp\t2f")
			b.WriteString("1:\n")
			op("cqto")
			op("idivq\t%%rcx")
		} else {
			op("xorl\t%%edx, %%edx")
			op("divq\t%%rcx")
		}
		if in.Op == ir.Rem {
			op("movq\t%%rdx, %%rax")
		}
		if in.Type.Signed() {
			b.WriteString("2:\n")
		}
		canon(op, in.Type)
		op("movq\t%%rax, %s", res())
	case ir.Shl, ir.Shr:
		// x86 shifts use the count modulo 64 (E10), so a count at or above
		// the width selects 0, or for an arithmetic right shift the sign
		// fill, instead. The count is unsigned, so the comparison is too.
		e.load(in.Args[1], "%rcx")
		e.load(in.Args[0], "%rax")
		switch {
		case in.Op == ir.Shl:
			op("shlq\t%%cl, %%rax")
			op("xorl\t%%edx, %%edx")
		case in.Type.Signed():
			op("movq\t%%rax, %%rdx")
			op("sarq\t$63, %%rdx")
			op("sarq\t%%cl, %%rax")
		default:
			op("shrq\t%%cl, %%rax")
			op("xorl\t%%edx, %%edx")
		}
		op("cmpq\t$%d, %%rcx", in.Args[0].Type.Width())
		op("cmovaeq\t%%rdx, %%rax")
		canon(op, in.Type)
		op("movq\t%%rax, %s", res())
	case ir.Panic:
		op("call\t%s", panicSyms[in.Index])
	case ir.Neg, ir.Com, ir.Conv:
		// negq maps the minimum to itself, as the IR requires; a conversion
		// is only the canonicalization to the new type.
		e.load(in.Args[0], "%rax")
		switch in.Op {
		case ir.Neg:
			op("negq\t%%rax")
		case ir.Com:
			op("notq\t%%rax")
		}
		canon(op, in.Type)
		op("movq\t%%rax, %s", res())
	case ir.Eq, ir.Ne, ir.Lt, ir.Le, ir.Gt, ir.Ge:
		// Bool operands are 0 or 1, so the same signed test is exact for
		// Eq and Ne on them.
		cc := setcc[in.Op]
		if t := in.Args[0].Type; t.IsInteger() && !t.Signed() {
			cc = setccUnsigned[in.Op]
		}
		e.load(in.Args[0], "%rax")
		op("cmpq\t%s, %%rax", val(in.Args[1]))
		op("%s\t%%al", cc)
		op("movzbl\t%%al, %%eax")
		op("movq\t%%rax, %s", res())
	case ir.Not:
		e.load(in.Args[0], "%rax")
		op("xorq\t$1, %%rax")
		op("movq\t%%rax, %s", res())
	case ir.Phi:
		// Its cell is written on each edge into the block (edgeCopies).
	case ir.Jump:
		e.edgeCopies(ir.Edge{From: blk, Index: 0})
		op("jmp\t%s", label(in.Targets[0]))
	case ir.Branch:
		// An edge whose target has phis gets its copies on the path taken
		// only along that edge: after the conditional jump, or under a
		// label of its own. This splits critical and parallel edges in the
		// emitted code without changing the CFG. The copies do not change
		// the flags.
		then, els := ir.Edge{From: blk, Index: 0}, ir.Edge{From: blk, Index: 1}
		e.load(in.Args[0], "%rax")
		op("testq\t%%rax, %%rax")
		switch {
		case !hasPhis(in.Targets[0]):
			op("jnz\t%s", label(in.Targets[0]))
			e.edgeCopies(els)
			op("jmp\t%s", label(in.Targets[1]))
		case !hasPhis(in.Targets[1]):
			op("jz\t%s", label(in.Targets[1]))
			e.edgeCopies(then)
			op("jmp\t%s", label(in.Targets[0]))
		default:
			elsLabel := fmt.Sprintf("%s.%d", label(blk), 1)
			op("jz\t%s", elsLabel)
			e.edgeCopies(then)
			op("jmp\t%s", label(in.Targets[0]))
			fmt.Fprintf(b, "%s:\n", elsLabel)
			e.edgeCopies(els)
			op("jmp\t%s", label(in.Targets[1]))
		}
	case ir.Println, ir.Print:
		// The operands are already in the frame, which the helpers do not
		// touch, so they survive the calls. Each call is made with %rsp
		// aligned (see Frame). Print has no separators and no newline.
		for i, a := range in.Args {
			if i > 0 && in.Op == ir.Println {
				op("call\t%s", symPrintSpace)
			}
			e.load(a, "%rdi")
			switch {
			case a.Type == ir.Bool:
				op("call\t%s", symPrintBool)
			case a.Type.Signed():
				op("call\t%s", symPrintInt)
			default:
				op("call\t%s", symPrintUint)
			}
		}
		if in.Op == ir.Print {
			break
		}
		op("call\t%s", symPrintNL)
	case ir.Param:
		if e.r1 != nil {
			e.r1Param(in)
			break
		}
		op("movq\t%d(%%rbp), %%rax", 16+8*in.Index)
		op("movq\t%%rax, %s", res())
	case ir.Call:
		if e.r1 != nil {
			e.r1Call(in)
			break
		}
		// Arguments and results go through the outgoing area at the
		// bottom of the frame (D-011); %rsp is aligned (see Frame).
		for i, a := range in.Args {
			e.put(a, fmt.Sprintf("%d(%%rsp)", 8*i))
		}
		op("call\t%s", funcSym(in.Callee))
		// Results are copied out before any other call can reuse the
		// outgoing area.
		switch in.Type {
		case ir.NoType:
		case ir.Tuple:
			for j := range in.Callee.Results {
				op("movq\t%d(%%rsp), %%rax", 8*(len(in.Args)+j))
				op("movq\t%%rax, %d(%%rbp)", fr.ValueOffset(tuples[in]+j))
			}
		default:
			op("movq\t%d(%%rsp), %%rax", 8*len(in.Args))
			op("movq\t%%rax, %s", res())
		}
	case ir.Extract:
		op("movq\t%d(%%rbp), %%rax", fr.ValueOffset(tuples[in.Args[0]]+in.Index))
		op("movq\t%%rax, %s", res())
	case ir.Ret:
		if e.r1 != nil {
			e.r1Ret(in)
			break
		}
		for j, a := range in.Args {
			e.put(a, fmt.Sprintf("%d(%%rbp)", 16+8*(len(f.Params)+j)))
		}
		op("movq\t%%rbp, %%rsp")
		op("popq\t%%rbp")
		op("ret")
	default:
		return fmt.Errorf("no instruction selection for %s", in.Op)
	}
	return nil
}

// canon emits the canonicalization of %rax to the integer type t, if t is
// narrower than 64 bits.
func canon(op func(string, ...any), t ir.Type) {
	if c, ok := canonical[t]; ok {
		op("%s", c)
	}
}

// indexOfEdge returns the index of phi's operand for edge. Verify guarantees
// there is exactly one.
func indexOfEdge(phi *ir.Instr, edge ir.Edge) int { return slices.Index(phi.Edges, edge) }

// hasPhis reports whether blk begins with a phi.
func hasPhis(blk *ir.Block) bool { return len(blk.Instrs) > 0 && blk.Instrs[0].Op == ir.Phi }

// edgeCopies emits the copies that give the phis of edge's target their
// values along it (D-025). The phis take their values together, so this is
// a parallel copy: a phi's operand may be another phi of the same block,
// whose old value must be read before its cell is overwritten. A copy is
// emitted once no remaining copy reads its destination. If none is ready,
// the destinations of the remaining copies are a permutation of their
// sources, a set of cycles: saving one destination in %rcx breaks its
// cycle. A copy of a cell to itself is omitted.
func (e *emitter) edgeCopies(edge ir.Edge) {
	type move struct{ dst, src string }
	var moves []move
	blk := edge.From.Instrs[len(edge.From.Instrs)-1].Targets[edge.Index]
	for _, phi := range blk.Instrs {
		if phi.Op != ir.Phi {
			break
		}
		// Verify guarantees exactly one operand for the edge.
		src, dst := e.val(phi.Args[indexOfEdge(phi, edge)]), e.val(phi)
		if src != dst {
			moves = append(moves, move{dst, src})
		}
	}
	for len(moves) > 0 {
		i := slices.IndexFunc(moves, func(m move) bool {
			return !slices.ContainsFunc(moves, func(n move) bool { return n.src == m.dst })
		})
		if i < 0 {
			d := moves[0].dst
			e.op("movq\t%s, %%rcx", d)
			for j := range moves {
				if moves[j].src == d {
					moves[j].src = "%rcx"
				}
			}
			continue
		}
		if m := moves[i]; m.src == "%rcx" {
			e.op("movq\t%%rcx, %s", m.dst)
		} else if e.sel != nil && (isReg(m.src) || isReg(m.dst) || isImm(m.src)) {
			// After the conditional jump: the flags are dead here.
			e.move(m.src, m.dst)
		} else {
			e.op("movq\t%s, %%rax", m.src)
			e.op("movq\t%%rax, %s", m.dst)
		}
		moves = slices.Delete(moves, i, i+1)
	}
}
