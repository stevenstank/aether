package gate

// Maps (Phase 8 increment 8.5, D-042, docs/SEMANTICS.md §Maps): map types
// of supported key and element types, make, literals, indexing, delete,
// len, clear, and comparison with nil. A range over a map is increment
// 8.5b.

import (
	"go/ast"
	"go/types"
)

func isMap(t types.Type) bool {
	_, ok := t.Underlying().(*types.Map)
	return ok
}

// isMap reports whether the expression e has a map type.
func (c *checker) isMap(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	return ok && !tv.IsType() && isMap(tv.Type)
}

// mapBuiltin checks a call of make, delete, or clear on a map, reporting
// whether it is one: make of a map type with perhaps a size hint, delete
// of a key, and clear.
func (c *checker) mapBuiltin(call *ast.CallExpr, name string) bool {
	args := call.Args
	switch {
	case call.Ellipsis.IsValid() || len(args) == 0:
		return false
	case name == "make" && len(args) <= 2 && c.info.Types[args[0]].IsType() && isMap(c.info.Types[args[0]].Type):
		if !c.typeExpr(args[0]) {
			return true
		}
		args = args[1:]
	case name == "delete" && len(args) == 2 && c.isMap(args[0]),
		name == "clear" && len(args) == 1 && c.isMap(args[0]):
	default:
		return false
	}
	for _, a := range args {
		c.expr(a)
	}
	return true
}
