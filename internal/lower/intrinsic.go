package lower

import (
	"go/ast"
	"go/types"
)

// intrinsicKey returns the key of fd in frontend.Intrinsics: its package
// path and name.
func intrinsicKey(info *types.Info, fd *ast.FuncDecl) string {
	obj := info.Defs[fd.Name]
	if obj == nil || fd.Recv != nil {
		return ""
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

// intrinsic generates the body of fd, a function an overlay file declares
// without one (D-049).
func (l *lowerer) intrinsic(fd *ast.FuncDecl) {
	obj := l.pkg.Info.Defs[fd.Name].(*types.Func)
	sig := obj.Type().(*types.Signature)
	l.fn = l.funcs[obj]
	b, pos := l.fn.Entry(), l.fn.Pos
	switch intrinsicKey(l.pkg.Info, fd) {
	case "math.Float32bits", "math.Float32frombits", "math.Float64bits", "math.Float64frombits",
		"internal/strconv.float32bits", "internal/strconv.float32frombits",
		"internal/strconv.float64bits", "internal/strconv.float64frombits":
		// The bits of the parameter, as the result's type: stored in a
		// memory object and loaded as the other type of the same size.
		in, out := sig.Params().At(0).Type(), sig.Results().At(0).Type()
		if sizes.Sizeof(in) != sizes.Sizeof(out) || len(l.fn.Params) != 1 || len(l.fn.Results) != 1 {
			l.fail(fd, "intrinsic %s of types %s and %s", obj.Name(), in, out)
		}
		x := l.fn.Param(b, 0, pos) // the parameter first (ABI-R1)
		m := l.fn.NewMem("bits", sizes.Sizeof(in), sizes.Alignof(in), pos)
		a, zero := l.fn.Addr(b, m, pos), l.fn.Const(b, 0, pos)
		l.fn.StM(b, a, zero, x, pos)
		l.fn.Ret(b, pos, l.fn.LdM(b, l.fn.Results[0], a, zero, pos))
	default:
		l.fail(fd, "unknown intrinsic %s", obj.Name())
	}
	l.fn = nil
}
