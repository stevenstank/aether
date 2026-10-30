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
	var executed int64
	if prog.Init != nil {
		if code, err := run(prog.Init, globals, stderr, steps, &executed); code != 0 || err != nil {
			return code, err
		}
	}
	for _, f := range prog.Funcs {
		if f.Name == "main" {
			return run(f, globals, stderr, steps, &executed)
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
}

// run executes entry (Init or main) and every call it makes on an explicit
// frame stack in one loop, never with a Go call per IR call, so the
// interpreter's own Go stack does not grow with the IR call depth (D-023).
// It counts the instructions it executes in *executed, against the budget
// steps.
func run(entry *ir.Func, globals []int64, stderr io.Writer, steps int64, executed *int64) (int, error) {
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
		return frame{f: f, blk: f.Entry(), args: args, slots: make([]int64, len(f.Slots)), values: make([]int64, n)}
	}
	stack := []frame{newFrame(entry, nil)}
	for {
		if steps > 0 && *executed == steps {
			return 0, ErrSteps
		}
		*executed++
		fr := &stack[len(stack)-1]
		// Verify guarantees that every operand was computed earlier in
		// the block, that every slot is stored on every path before it is
		// loaded, and that every block ends in a terminator.
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
			_, _ = io.WriteString(stderr, "panic: runtime error: "+ir.PanicMessages[in.Index]+"\n")
			return 2, nil
		case ir.Eq, ir.Ne, ir.Lt, ir.Le, ir.Gt, ir.Ge:
			fr.values[in.ID] = b2i(compare(in.Op, in.Args[0].Type, arg(0), arg(1)))
		case ir.Not:
			fr.values[in.ID] = 1 - arg(0)
		case ir.Jump:
			fr.blk, fr.pc = in.Targets[0], 0
		case ir.Branch:
			fr.blk, fr.pc = in.Targets[1-arg(0)], 0
		case ir.Println, ir.Print:
			var line []byte
			for i, a := range in.Args {
				if i > 0 && in.Op == ir.Println {
					line = append(line, ' ')
				}
				switch {
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
