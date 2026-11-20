package gate

// Goroutines and channels (Phase 10 increment 10.2, D-048): go statements
// of the calls defer allows, channel types of supported element types,
// make, send, receive (also comma-ok and as a statement), close, len, cap,
// comparison, and range over a channel. select is still rejected.

import (
	"go/ast"
	"go/token"
	"go/types"
)

// maxChanElem is the largest channel element gc accepts: it rejects larger
// ones ("channel element type too large (>64kB)").
const maxChanElem = 1<<16 - 1

func isChan(t types.Type) bool {
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

// isChan reports whether the expression e has a channel type.
func (c *checker) isChan(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	return ok && !tv.IsType() && isChan(tv.Type)
}

// chanBuiltin checks a call of make, close, len, or cap on a channel,
// reporting whether it is one.
func (c *checker) chanBuiltin(call *ast.CallExpr, name string) bool {
	args := call.Args
	switch {
	case call.Ellipsis.IsValid() || len(args) == 0:
		return false
	case name == "make" && len(args) <= 2 && c.info.Types[args[0]].IsType() && isChan(c.info.Types[args[0]].Type):
		if !c.typeExpr(args[0]) {
			return true
		}
		args = args[1:]
	case (name == "close" || name == "len" || name == "cap") && len(args) == 1 && c.isChan(args[0]):
	default:
		return false
	}
	for _, a := range args {
		c.expr(a)
	}
	return true
}

// goStmt checks a go statement: the calls a defer statement allows.
func (c *checker) goStmt(s *ast.GoStmt) {
	if id, ok := ast.Unparen(s.Call.Fun).(*ast.Ident); ok {
		if b, ok := c.info.Uses[id].(*types.Builtin); ok {
			if b.Name() == "print" || b.Name() == "println" {
				c.exprStmt(&ast.ExprStmt{X: s.Call})
				return
			}
			c.reject(s, "go", "go of builtin "+b.Name())
			return
		}
	}
	c.call(s.Call)
}

// isRecv reports whether e is a receive, <-ch.
func isRecv(e ast.Expr) bool {
	u, ok := ast.Unparen(e).(*ast.UnaryExpr)
	return ok && u.Op == token.ARROW
}
