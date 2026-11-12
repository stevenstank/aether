// Package interp executes IR directly (D-005).
//
// It is a test oracle, not a product feature: it gives the IR an executable
// meaning that lowering is checked against gc with, and that later stages
// (the backend, optimization passes) are checked against in turn. It
// implements the semantics documented on the operations in package ir and
// nothing else; it has no performance goals.
package interp

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"

	"github.com/stevenstank/aether/internal/ir"
)

// MaxDepth is the call-depth limit (D-023): the most frames, main's
// included, that may be active at once. A call that would exceed it is a
// stack overflow. It bounds the frame stack's heap memory to MaxDepth frames
// (D-023 records the bound).
const MaxDepth = 100_000

// stackOverflow is what the native runtime writes for a stack overflow
// (D-009, SEMANTICS.md V-2).
const stackOverflow = "fatal error: stack overflow\n"

// Run executes the program's Init function, if it has one, and then its
// entry function, main, with the globals starting at zero, writes the output
// of println to stderr, and returns the program's exit status: 0 when main
// returns, 2 after a stack overflow or a panic. Nothing is ever written to
// standard output.
//
// The program is verified first. Malformed IR is an error, and nothing is
// executed. Errors writing to stderr are ignored, as the IR requires.
func Run(prog *ir.Program, stderr io.Writer) (int, error) {
	return RunSteps(prog, stderr, 0)
}

// ErrSteps reports that a program exceeded its step budget.
var ErrSteps = errors.New("interp: step budget exhausted")

// RunSteps is Run with a step budget, for tests (D-023): if the program
// executes more than steps instructions, it stops with ErrSteps, so a
// lowering bug that loops fails instead of hanging. A budget of 0 means
// none.
func RunSteps(prog *ir.Program, stderr io.Writer, steps int64) (int, error) {
	if errs := ir.Verify(prog); len(errs) > 0 {
		return 0, fmt.Errorf("interp: malformed IR: %w", errors.Join(errs...))
	}
	globals := make([]int64, len(prog.Globals))
	mem := newMemory(prog)
	var executed int64
	if prog.Init != nil {
		if code, err := run(prog.Init, prog.Strings, globals, mem, stderr, steps, &executed); code != 0 || err != nil {
			return code, err
		}
	}
	for _, f := range prog.Funcs {
		if f.Name == "main" {
			return run(f, prog.Strings, globals, mem, stderr, steps, &executed)
		}
	}
	return 0, errors.New("interp: no function main") // excluded by Verify
}

// frame is one active call.
type frame struct {
	f      *ir.Func
	blk    *ir.Block
	pc     int     // index of the next instruction in blk
	args   []int64 // the parameters
	slots  []int64
	values []int64         // indexed by value number
	tuples map[int][]int64 // the results of Tuple calls, by value number
	mems   []int64         // the handle of each local memory object, 0 until its first addr
}

// run executes entry (Init or main) and every call it makes on an explicit
// frame stack in one loop, never with a Go call per IR call, so the
// interpreter's own Go stack does not grow with the IR call depth (D-023).
// It counts the instructions it executes in *executed, against the budget
// steps.
//
// A Ptr is held as the index of its program string plus 1, so that the
// constant 0 is the empty string's address (D-033), or as the handle of a
// memory object (D-034).
func run(entry *ir.Func, strs []string, globals []int64, mem *memory, stderr io.Writer, steps int64, executed *int64) (int, error) {
	nvalues := map[*ir.Func]int{}
	newFrame := func(f *ir.Func, args []int64) frame {
		n, ok := nvalues[f]
		if !ok {
			for _, b := range f.Blocks {
				for _, in := range b.Instrs {
					n = max(n, in.ID+1)
				}
			}
			nvalues[f] = n
		}
		return frame{f: f, blk: f.Entry(), args: args, slots: make([]int64, len(f.Slots)), values: make([]int64, n), mems: make([]int64, len(f.Mems))}
	}
	stack := []frame{newFrame(entry, nil)}
	for {
		if steps > 0 && *executed == steps {
			return 0, ErrSteps
		}
		*executed++
		fr := &stack[len(stack)-1]
		// Verify guarantees that every operand was computed earlier in
		// the block, or in SSA form in a dominating block, that every slot
		// is stored on every path before it is loaded, and that every block
		// ends in a terminator.
		in := fr.blk.Instrs[fr.pc]
		fr.pc++
		arg := func(i int) int64 { return fr.values[in.Args[i].ID] }
		switch in.Op {
		case ir.Const:
			fr.values[in.ID] = in.Const
		case ir.Load:
			fr.values[in.ID] = fr.slots[in.Slot.ID]
		case ir.Store:
			fr.slots[in.Slot.ID] = arg(0)
		case ir.GLoad:
			fr.values[in.ID] = globals[in.Global.ID]
		case ir.GStore:
			globals[in.Global.ID] = arg(0)
		// Go's int64 arithmetic wraps in two's complement (spec §Integer
		// overflow), so the low width bits of each result are exact, and
		// Canonical reduces them to the instruction's type.
		case ir.Add:
			fr.values[in.ID] = in.Type.Canonical(arg(0) + arg(1))
		case ir.Sub:
			fr.values[in.ID] = in.Type.Canonical(arg(0) - arg(1))
		case ir.Mul:
			fr.values[in.ID] = in.Type.Canonical(arg(0) * arg(1))
		case ir.Neg:
			fr.values[in.ID] = in.Type.Canonical(-arg(0))
		case ir.And:
			fr.values[in.ID] = in.Type.Canonical(arg(0) & arg(1))
		case ir.Or:
			fr.values[in.ID] = in.Type.Canonical(arg(0) | arg(1))
		case ir.Xor:
			fr.values[in.ID] = in.Type.Canonical(arg(0) ^ arg(1))
		case ir.AndNot:
			fr.values[in.ID] = in.Type.Canonical(arg(0) &^ arg(1))
		case ir.Com:
			fr.values[in.ID] = in.Type.Canonical(^arg(0))
		case ir.Conv:
			fr.values[in.ID] = in.Type.Canonical(arg(0))
		case ir.Div, ir.Rem:
			x, y := arg(0), arg(1)
			if y == 0 {
				// Lowering checks every divisor first (D-023).
				return 0, fmt.Errorf("interp: func %s: %s by zero", fr.f.Name, in.Op)
			}
			fr.values[in.ID] = in.Type.Canonical(divide(in.Op, in.Type, x, y))
		case ir.Shl, ir.Shr:
			fr.values[in.ID] = in.Type.Canonical(shift(in.Op, in.Type, arg(0), uint64(arg(1))))
		case ir.Panic:
			msg := ir.PanicMessages[in.Index]
			if in.Index == ir.PanicIndex {
				msg = ir.IndexMessage(arg(0), in.Args[0].Type.Signed(), arg(1))
			}
			_, _ = io.WriteString(stderr, "panic: runtime error: "+msg+"\n")
			return 2, nil
		case ir.Str:
			fr.values[in.ID] = int64(in.Index) + 1
		case ir.LoadB:
			p, i := arg(0), arg(1)
			if p < 1 || p > int64(len(strs)) || i < 0 || i >= int64(len(strs[p-1])) {
				// Lowering checks every index first (D-033).
				return 0, fmt.Errorf("interp: func %s: %s out of range", fr.f.Name, in)
			}
			fr.values[in.ID] = int64(strs[p-1][i])
		case ir.Addr:
			m := in.Mem
			switch {
			case m.Global:
				fr.values[in.ID] = mem.globals[m.ID]
			case fr.mems[m.ID] != 0:
				fr.values[in.ID] = fr.mems[m.ID]
			default:
				if mem.live+m.Size > MaxMemory {
					// The frames' memory, like the native stack, is
					// bounded (D-034).
					_, _ = io.WriteString(stderr, stackOverflow)
					return 2, nil
				}
				fr.mems[m.ID] = mem.alloc(m.Size, false)
				mem.live += m.Size
				fr.values[in.ID] = fr.mems[m.ID]
			}
		case ir.LdM:
			v, err := mem.load(in.Type, arg(0), arg(1))
			if err != nil {
				return 0, fmt.Errorf("interp: func %s: %s: %v", fr.f.Name, in, err)
			}
			fr.values[in.ID] = v
		case ir.StM:
			if err := mem.store(in.Args[2].Type, arg(0), arg(1), arg(2)); err != nil {
				return 0, fmt.Errorf("interp: func %s: %s: %v", fr.f.Name, in, err)
			}
		case ir.Eq, ir.Ne, ir.Lt, ir.Le, ir.Gt, ir.Ge:
			fr.values[in.ID] = b2i(compare(in.Op, in.Args[0].Type, arg(0), arg(1)))
		case ir.Not:
			fr.values[in.ID] = 1 - arg(0)
		case ir.FAdd, ir.FSub, ir.FMul, ir.FDiv:
			fr.values[in.ID] = floatArith(in.Op, in.Type, arg(0), arg(1))
		case ir.FNeg:
			// The sign bit, as for gc; NaNs keep their payload.
			fr.values[in.ID] = arg(0) ^ math.MinInt64
			if in.Type == ir.F32 {
				fr.values[in.ID] = arg(0) ^ 1<<31
			}
		case ir.FEq, ir.FNe, ir.FLt, ir.FLe, ir.FGt, ir.FGe:
			fr.values[in.ID] = b2i(floatCompare(in.Op, toFloat(in.Args[0].Type, arg(0)), toFloat(in.Args[1].Type, arg(1))))
		case ir.IToF:
			fr.values[in.ID] = intToFloat(in.Type, in.Args[0].Type, arg(0))
		case ir.FToF:
			fr.values[in.ID] = fromFloat(in.Type, toFloat(in.Args[0].Type, arg(0)))
		case ir.FToI:
			fr.values[in.ID] = in.Type.Canonical(floatToInt(in.Type, toFloat(in.Args[0].Type, arg(0))))
		case ir.Jump:
			enter(fr, ir.Edge{From: fr.blk, Index: 0})
		case ir.Branch:
			enter(fr, ir.Edge{From: fr.blk, Index: int(1 - arg(0))})
		case ir.Println, ir.Print:
			var line []byte
			for i := 0; i < len(in.Args); i++ {
				a := in.Args[i]
				if i > 0 && in.Op == ir.Println {
					line = append(line, ' ')
				}
				switch {
				case a.Type == ir.Ptr:
					// A string: its address and then its length (D-033).
					p, n := arg(i), arg(i+1)
					i++
					switch {
					case p == 0 && n == 0:
					case p >= 1 && p <= int64(len(strs)) && n >= 0 && n <= int64(len(strs[p-1])):
						line = append(line, strs[p-1][:n]...)
					default:
						return 0, fmt.Errorf("interp: func %s: %s prints a string out of range", fr.f.Name, in)
					}
				case a.Type == ir.F64:
					line = strconv.AppendFloat(line, toFloat(a.Type, arg(i)), 'g', -1, 64)
				case a.Type == ir.F32:
					line = strconv.AppendFloat(line, toFloat(a.Type, arg(i)), 'g', -1, 32)
				case a.Type == ir.Bool:
					line = strconv.AppendBool(line, arg(i) == 1)
				case a.Type.Signed():
					line = strconv.AppendInt(line, arg(i), 10)
				default:
					line = strconv.AppendUint(line, uint64(arg(i)), 10)
				}
			}
			if in.Op == ir.Println {
				line = append(line, '\n')
			}
			_, _ = stderr.Write(line)
		case ir.Param:
			fr.values[in.ID] = fr.args[in.Index]
		case ir.Call:
			if len(stack) == MaxDepth {
				_, _ = io.WriteString(stderr, stackOverflow)
				return 2, nil
			}
			args := make([]int64, len(in.Args))
			for i := range args {
				args[i] = arg(i)
			}
			stack = append(stack, newFrame(in.Callee, args))
		case ir.Ret:
			results := make([]int64, len(in.Args))
			for i := range results {
				results[i] = arg(i)
			}
			// The frame's memory objects end with it (D-034).
			for i, h := range fr.mems {
				if h != 0 {
					mem.free(h)
					mem.live -= fr.f.Mems[i].Size
				}
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return 0, nil
			}
			caller := &stack[len(stack)-1]
			switch call := caller.blk.Instrs[caller.pc-1]; call.Type {
			case ir.NoType:
			case ir.Tuple:
				if caller.tuples == nil {
					caller.tuples = map[int][]int64{}
				}
				caller.tuples[call.ID] = results
			default:
				caller.values[call.ID] = results[0]
			}
		case ir.Extract:
			fr.values[in.ID] = fr.tuples[in.Args[0].ID][in.Index]
		default:
			return 0, fmt.Errorf("interp: func %s: cannot execute %s", fr.f.Name, in)
		}
	}
}

// enter continues fr along edge e and executes the phis at the start of its
// target (D-025): each takes its value for e, all of them read before any is
// written. Phis do not count as steps.
func enter(fr *frame, e ir.Edge) {
	b := e.From.Instrs[len(e.From.Instrs)-1].Targets[e.Index]
	n := 0
	for n < len(b.Instrs) && b.Instrs[n].Op == ir.Phi {
		n++
	}
	if n > 0 {
		vals := make([]int64, n)
		for i, phi := range b.Instrs[:n] {
			// Verify guarantees exactly one value for each incoming edge.
			j := slices.Index(phi.Edges, e)
			vals[i] = fr.values[phi.Args[j].ID]
		}
		for i, phi := range b.Instrs[:n] {
			fr.values[phi.ID] = vals[i]
		}
	}
	fr.blk, fr.pc = b, n
}

// compare applies a comparison to canonical values of type t: as signed or
// unsigned numbers by t, or as 0 or 1 for Bool, where only Eq and Ne apply.
func compare(op ir.Op, t ir.Type, x, y int64) bool {
	if t.IsInteger() && !t.Signed() {
		// Zero-extended values compare as unsigned 64-bit numbers; flipping
		// the sign bit maps that order onto int64's.
		x, y = x^math.MinInt64, y^math.MinInt64
	}
	switch op {
	case ir.Eq:
		return x == y
	case ir.Ne:
		return x != y
	case ir.Lt:
		return x < y
	case ir.Le:
		return x <= y
	case ir.Gt:
		return x > y
	}
	return x >= y // ir.Ge
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// divide applies Div or Rem to canonical values of t, with y not zero. Go's
// int64 division truncates toward zero, and MinInt64 / -1 is MinInt64 with
// remainder 0 (spec §Integer operators), as the IR requires; for a narrower
// signed type the quotient is reduced by the caller.
func divide(op ir.Op, t ir.Type, x, y int64) int64 {
	if !t.Signed() {
		if op == ir.Div {
			return int64(uint64(x) / uint64(y))
		}
		return int64(uint64(x) % uint64(y))
	}
	if op == ir.Div {
		return x / y
	}
	return x % y
}

// shift applies Shl or Shr to a canonical value of t: a count at or above
// the width gives 0, or the sign fill for Shr of a signed type. Below it,
// shifting the 64-bit canonical form is exact; Shl's result is reduced by
// the caller.
func shift(op ir.Op, t ir.Type, x int64, count uint64) int64 {
	if count >= uint64(t.Width()) {
		if op == ir.Shr && t.Signed() && x < 0 {
			return -1
		}
		return 0
	}
	switch {
	case op == ir.Shl:
		return x << count
	case t.Signed():
		return x >> count
	}
	return int64(uint64(x) >> count)
}

// toFloat returns the value of x, the canonical word of a float of type t.
// An F32 converts to float64 exactly.
func toFloat(t ir.Type, x int64) float64 {
	if t == ir.F32 {
		return float64(math.Float32frombits(uint32(x)))
	}
	return math.Float64frombits(uint64(x))
}

// fromFloat returns the canonical word of v as a float of type t, rounding
// it to nearest, ties to even, for F32.
func fromFloat(t ir.Type, v float64) int64 {
	if t == ir.F32 {
		return int64(math.Float32bits(float32(v)))
	}
	return int64(math.Float64bits(v))
}

// floatArith applies FAdd, FSub, FMul, or FDiv to canonical floats of type
// t. Each result is assigned on its own, at its type's precision, which
// keeps Go from fusing operations (spec §Arithmetic operators).
func floatArith(op ir.Op, t ir.Type, x, y int64) int64 {
	if t == ir.F32 {
		a, b := math.Float32frombits(uint32(x)), math.Float32frombits(uint32(y))
		var r float32
		switch op {
		case ir.FAdd:
			r = a + b
		case ir.FSub:
			r = a - b
		case ir.FMul:
			r = a * b
		default:
			r = a / b
		}
		return int64(math.Float32bits(r))
	}
	a, b := math.Float64frombits(uint64(x)), math.Float64frombits(uint64(y))
	var r float64
	switch op {
	case ir.FAdd:
		r = a + b
	case ir.FSub:
		r = a - b
	case ir.FMul:
		r = a * b
	default:
		r = a / b
	}
	return int64(math.Float64bits(r))
}

// floatCompare applies a float comparison: Go's are IEEE 754's.
func floatCompare(op ir.Op, x, y float64) bool {
	switch op {
	case ir.FEq:
		return x == y
	case ir.FNe:
		return x != y
	case ir.FLt:
		return x < y
	case ir.FLe:
		return x <= y
	case ir.FGt:
		return x > y
	}
	return x >= y // ir.FGe
}

// intToFloat converts x, a canonical integer of type from, to the float
// type t. Go's conversions round to nearest, ties to even, directly from
// the integer (spec §Conversions), so an int64 converts to float32 with one
// rounding.
func intToFloat(t, from ir.Type, x int64) int64 {
	switch {
	case from == ir.U64 && t == ir.F32:
		return int64(math.Float32bits(float32(uint64(x))))
	case from == ir.U64:
		return int64(math.Float64bits(float64(uint64(x))))
	case t == ir.F32:
		return int64(math.Float32bits(float32(x)))
	}
	return int64(math.Float64bits(float64(x)))
}

// trunc32 and trunc64 are x86's cvttsd2si into 32 and 64 bits: x truncated
// toward zero if the result is representable, and the type's minimum
// otherwise, NaN included.
func trunc32(x float64) int64 {
	if x > -2147483649 && x < 2147483648 {
		return int64(x)
	}
	return math.MinInt32
}

func trunc64(x float64) int64 {
	if x >= -0x1p63 && x < 0x1p63 {
		return int64(x)
	}
	return math.MinInt64
}

// floatToInt converts x to the integer type t as FToI defines (D-032), before
// reduction to t's canonical form.
func floatToInt(t ir.Type, x float64) int64 {
	switch t {
	case ir.I64, ir.U32:
		return trunc64(x)
	case ir.U64:
		if x < 0x1p63 {
			return trunc64(x)
		}
		return trunc64(x-0x1p63) | math.MinInt64
	}
	return trunc32(x)
}
