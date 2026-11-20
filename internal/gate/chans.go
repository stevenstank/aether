package gate

// Goroutines and channels (Phase 10 increment 10.2, D-048): go statements
// of the calls defer allows, channel types of supported element types,
// make, send, receive (also comma-ok and as a statement), close, len, cap,
// comparison, and range over a channel; select (increment 10.3).

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
	if c.variadic(s.Call) {
		c.reject(s.Call, FeatVariadic, "variadic call in a go statement")
		return
	}
	c.call(s.Call)
}

// isRecv reports whether e is a receive, <-ch.
func isRecv(e ast.Expr) bool {
	u, ok := ast.Unparen(e).(*ast.UnaryExpr)
	return ok && u.Op == token.ARROW
}

// selectStmt checks a select statement: each case's send or receive, and
// the variables a receive declares or the targets it assigns, as a range
// over a map's, then its statements.
func (c *checker) selectStmt(s *ast.SelectStmt) {
	for _, st := range s.Body.List {
		cc := st.(*ast.CommClause)
		switch comm := cc.Comm.(type) {
		case nil:
		case *ast.SendStmt:
			c.expr(comm.Chan)
			c.expr(comm.Value)
		case *ast.ExprStmt:
			c.expr(comm.X)
		case *ast.AssignStmt:
			u, ok := ast.Unparen(comm.Rhs[0]).(*ast.UnaryExpr)
			if !ok || u.Op != token.ARROW || len(comm.Rhs) != 1 {
				c.internal(comm, "select case that is not a receive")
				continue
			}
			c.expr(u.X)
			for _, e := range comm.Lhs {
				if id, ok := e.(*ast.Ident); ok && id.Name == "_" {
					continue
				}
				if comm.Tok == token.DEFINE {
					id, ok := e.(*ast.Ident)
					if !ok {
						c.internal(e, "non-identifier declared by a select case")
						continue
					}
					c.intVar(id, c.info.Defs[id], true)
					continue
				}
				c.target(e)
			}
		default:
			c.internal(cc, "unexpected select case %T", comm)
		}
		c.stmts(cc.Body)
	}
}
