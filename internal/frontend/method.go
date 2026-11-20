package frontend

import (
	"go/ast"
	"go/types"
)

// TakesAddress reports whether a call through the method selection sel,
// of kind MethodVal, takes the address of its operand x or of an embedded
// field of it: the method has a pointer receiver, and neither x nor any
// embedded field on the path to the method is a pointer. The gate and
// lowering both use it (D-038).
func TakesAddress(sel *types.Selection) bool {
	if _, ok := sel.Obj().Type().(*types.Signature).Recv().Type().(*types.Pointer); !ok {
		return false
	}
	t := sel.Recv()
	path := sel.Index()
	for i := 0; ; i++ {
		if _, ok := t.Underlying().(*types.Pointer); ok {
			return false
		}
		if i == len(path)-1 {
			return true
		}
		s, ok := t.Underlying().(*types.Struct)
		if !ok {
			return false
		}
		t = s.Field(path[i]).Type()
	}
}

// Generic reports whether n, a top-level function declaration or type
// specification, is generic code that only its instances compile: a
// generic function, a method of a generic type, a generic type, or an
// interface that can only be a constraint. Packages besides main keep it
// after monomorphization, for the packages importing them (D-049).
func Generic(info *types.Info, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.FuncDecl:
		if n.Type.TypeParams != nil {
			return true
		}
		if fn, ok := info.Defs[n.Name].(*types.Func); ok && n.Recv != nil {
			t := fn.Type().(*types.Signature).Recv().Type()
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			nt, ok := types.Unalias(t).(*types.Named)
			return ok && nt.TypeParams().Len() > 0
		}
	case *ast.TypeSpec:
		if n.TypeParams != nil {
			return true
		}
		if tn, ok := info.Defs[n.Name].(*types.TypeName); ok {
			i, ok := tn.Type().Underlying().(*types.Interface)
			return ok && !i.IsMethodSet()
		}
	}
	return false
}
