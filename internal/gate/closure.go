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
	if r := e.Type.Results; r.NumFields() > 0 && len(r.List[0].Names) > 0 {
		c.reject(r, FeatFuncDecl, "function literal with named results")
		return
	}
	mark := len(c.ds)
	c.lits = append(c.lits, e)
	c.stmts(e.Body.List)
	c.lits = c.lits[:len(c.lits)-1]
	if len(c.ds) == mark {
		if n := c.frameBytes(e.Body); n > maxFrameObjects {
			c.reject(e, FeatFrameSize, fmt.Sprintf("function literal, whose arrays and structs may need %d bytes of stack (more than %d)", n, maxFrameObjects))
		}
	}
}

// capture reports whether the use id of the variable v is allowed: not
// inside a function literal that captures v, if v is declared in a for
// statement's init. It rejects it if not.
func (c *checker) capture(id *ast.Ident, v *types.Var) bool {
	if len(c.lits) == 0 || !c.forVars[v] {
		return true
	}
	if lit := c.lits[len(c.lits)-1]; v.Pos() >= lit.Pos() && v.Pos() < lit.End() {
		return true // declared in the literal itself
	}
	c.reject(id, FeatLoopVarAddr, "the capture of "+id.Name+", a variable declared in a for statement's init, by a function literal")
	return false
}
