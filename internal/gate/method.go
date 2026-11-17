package gate

// Methods (Phase 8 increment 8.1, D-038, docs/SEMANTICS.md §Methods): a
// method declaration on a defined type of the program, and a call x.M(...)
// of one, perhaps promoted through embedded fields, with the receiver's
// address taken or the pointer to it followed as the method's receiver
// needs. A method value x.M not called and a method expression T.M need
// function values, and are rejected.

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/frontend"
)

// method checks a method declaration as a function declaration whose first
// parameter is its receiver. go/types has checked that the receiver's base
// type is a defined type of the package, neither a pointer nor an interface,
// and that its methods' names are unique.
func (c *checker) method(d *ast.FuncDecl) {
	if d.Name.Name == "_" {
		c.reject(d.Name, FeatBlank, "method named _")
		return
	}
	c.funcDecl(d)
}

// methodCall checks a call of the method that sel selects: its receiver
// operand x, then its arguments as for a function. If the method has a
// pointer receiver and the path to it reaches a variable's memory without a
// pointer, the call takes that variable's address, which is checked as &x
// is: never of a variable declared in a for statement's init (D-035).
func (c *checker) methodCall(call *ast.CallExpr, x ast.Expr, sel *types.Selection) bool {
	if call.Ellipsis.IsValid() {
		c.reject(call, FeatVariadic, "variadic call")
		return false
	}
	if frontend.TakesAddress(sel) {
		c.address(x)
	} else {
		c.expr(x)
	}
	if !c.multiValue(call.Args) {
		for _, a := range call.Args {
			c.expr(a)
		}
	}
	return true
}

// methodValue checks a method value x.M, not called (D-039): its receiver
// operand x, as for a call of M.
func (c *checker) methodValue(e *ast.SelectorExpr, sel *types.Selection) {
	if fn, ok := sel.Obj().(*types.Func); !ok || fn.Pkg() != c.pkg.Types {
		c.reject(e, FeatSelector, "selector "+types.ExprString(e))
		return
	}
	if !c.typed(e) {
		return
	}
	if frontend.TakesAddress(sel) {
		c.address(e.X)
	} else {
		c.expr(e.X)
	}
}
