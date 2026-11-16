package frontend

import "go/types"

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
