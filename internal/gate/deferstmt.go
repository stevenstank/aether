package gate

import (
	"go/ast"
	"go/types"
)

// deferStmt checks a defer statement (D-043): of a call the gate accepts
// as a statement, of a function, method, func value, function literal, or
// interface method, or of print or println. Deferring any other builtin,
// panic and recover included, stays unsupported.
func (c *checker) deferStmt(s *ast.DeferStmt) {
	if id, ok := ast.Unparen(s.Call.Fun).(*ast.Ident); ok {
		if b, ok := c.info.Uses[id].(*types.Builtin); ok {
			if b.Name() == "print" || b.Name() == "println" {
				c.exprStmt(&ast.ExprStmt{X: s.Call})
				return
			}
			c.reject(s, "defer", "defer of builtin "+b.Name())
			return
		}
	}
	c.call(s.Call)
}
