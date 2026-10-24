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
	"strconv"

	"github.com/stevenstank/aether/internal/ir"
)

// Run executes the program's entry function, main, and writes the output of
// println to stderr. An S1 program writes nothing to standard output and
// always exits with status 0, so Run has nothing else to report.
//
// The program is verified first. Malformed IR is an error, and nothing is
// executed. Errors writing to stderr are ignored, as the IR requires.
func Run(prog *ir.Program, stderr io.Writer) error {
	if errs := ir.Verify(prog); len(errs) > 0 {
		return fmt.Errorf("interp: malformed IR: %w", errors.Join(errs...))
	}
	for _, f := range prog.Funcs {
		if f.Name == "main" {
			return run(f, stderr)
		}
	}
	return errors.New("interp: no function main") // excluded by Verify
}

func run(f *ir.Func, stderr io.Writer) error {
	// Verify guarantees that every operand was computed earlier in the
	// block and every slot was stored before it is loaded, so neither map
	// lookup below can miss.
	values := map[*ir.Instr]int64{}
	slots := make([]int64, len(f.Slots))
	// Verify also guarantees a single block ending in ret: the IR has no
	// branches.
	for _, in := range f.Entry().Instrs {
		arg := func(i int) int64 { return values[in.Args[i]] }
		switch in.Op {
		case ir.Const:
			values[in] = in.Const
		case ir.Load:
			values[in] = slots[in.Slot.ID]
		case ir.Store:
			slots[in.Slot.ID] = arg(0)
		// Go's int64 arithmetic wraps in two's complement (spec §Integer
		// overflow), which is exactly the IR's semantics.
		case ir.Add:
			values[in] = arg(0) + arg(1)
		case ir.Sub:
			values[in] = arg(0) - arg(1)
		case ir.Mul:
			values[in] = arg(0) * arg(1)
		case ir.Neg:
			values[in] = -arg(0)
		case ir.Println:
			var line []byte
			for i := range in.Args {
				if i > 0 {
					line = append(line, ' ')
				}
				line = strconv.AppendInt(line, arg(i), 10)
			}
			line = append(line, '\n')
			_, _ = stderr.Write(line)
		case ir.Ret:
			return nil
		default:
			return fmt.Errorf("interp: func %s: cannot execute %s", f.Name, in)
		}
	}
	return fmt.Errorf("interp: func %s: block b0 has no terminator", f.Name) // excluded by Verify
}
