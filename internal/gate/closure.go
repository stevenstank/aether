package gate

// Closures (Phase 8 increment 8.3, D-040, docs/SEMANTICS.md §Closures): a
// function literal, whose body is checked as a function's, and which may
// use and assign the variables of the functions around it. A variable
// declared in a for statement's init cannot be captured: Go 1.22 gives
// each iteration its own copy of it, as for its address (D-035).

import (
	"fmt"
	"go/ast"
	"go/types"
)

// funcLit checks a function literal: its signature, as a function
// declaration's, and its body, with its own frame bound.
func (c *checker) funcLit(e *ast.FuncLit) {
	if !c.typed(e) {
		return
	}
	mark := len(c.ds)
	c.lits = append(c.lits, e)
	c.stmts(e.Body.List)
	c.lits = c.lits[:len(c.lits)-1]
	if len(c.ds) == mark {
		if n := c.funcBytes(e.Type, e.Body); n > maxFrameObjects {
			c.reject(e, FeatFrameSize, fmt.Sprintf("function literal, whose arrays and structs may need %d bytes of stack (more than %d)", n, maxFrameObjects))
		}
	}
}

// capture reports whether the use id of the variable v is allowed. A
// variable declared in a for statement's init is each iteration's own
// (Go 1.22): lowering gives each a new heap object when a literal captures
// it (v2 evaluation), so every capture is.
func (c *checker) capture(id *ast.Ident, v *types.Var) bool { return true }
