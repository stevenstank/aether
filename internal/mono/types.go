package mono

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/stevenstank/aether/internal/gate"
)

// subst returns t with the type parameters of e replaced by their
// arguments. t, at n, may have no other type parameters.
func (m *mono) subst(n ast.Node, t types.Type, e env) types.Type {
	switch t := t.(type) {
	case *types.TypeParam:
		if a, ok := e[t]; ok {
			return a
		}
		m.internal(n, "type parameter %s without an argument", t)
		return t
	case *types.Basic:
		return t
	case *types.Alias:
		if t.Obj().Pkg() == nil {
			return t // any
		}
		return m.subst(n, types.Unalias(t), e)
	case *types.Named:
		if t.TypeArgs().Len() == 0 {
			return t
		}
		args := make([]types.Type, t.TypeArgs().Len())
		for i := range args {
			args[i] = m.subst(n, t.TypeArgs().At(i), e)
		}
		r, err := types.Instantiate(m.ctxt, t.Origin(), args, false)
		if err != nil {
			m.internal(n, "instantiating %s: %v", t, err)
			return t
		}
		return r
	case *types.Pointer:
		return types.NewPointer(m.subst(n, t.Elem(), e))
	case *types.Slice:
		return types.NewSlice(m.subst(n, t.Elem(), e))
	case *types.Array:
		return types.NewArray(m.subst(n, t.Elem(), e), t.Len())
	case *types.Map:
		return types.NewMap(m.subst(n, t.Key(), e), m.subst(n, t.Elem(), e))
	case *types.Chan:
		return types.NewChan(t.Dir(), m.subst(n, t.Elem(), e))
	case *types.Signature:
		return types.NewSignatureType(nil, nil, nil, m.substTuple(n, t.Params(), e), m.substTuple(n, t.Results(), e), t.Variadic())
	case *types.Struct:
		var fs []*types.Var
		var tags []string
		for i := range t.NumFields() {
			f := t.Field(i)
			fs = append(fs, types.NewField(f.Pos(), f.Pkg(), f.Name(), m.subst(n, f.Type(), e), f.Embedded()))
			tags = append(tags, t.Tag(i))
		}
		return types.NewStruct(fs, tags)
	case *types.Interface:
		var ms []*types.Func
		for i := range t.NumExplicitMethods() {
			f := t.ExplicitMethod(i)
			ms = append(ms, types.NewFunc(f.Pos(), f.Pkg(), f.Name(), m.subst(n, f.Type(), e).(*types.Signature)))
		}
		var es []types.Type
		for i := range t.NumEmbeddeds() {
			es = append(es, m.subst(n, t.EmbeddedType(i), e))
		}
		return types.NewInterfaceType(ms, es).Complete()
	}
	m.internal(n, "unexpected type %s", t)
	return t
}

func (m *mono) substTuple(n ast.Node, t *types.Tuple, e env) *types.Tuple {
	var vs []*types.Var
	for v := range t.Variables() {
		vs = append(vs, types.NewParam(v.Pos(), v.Pkg(), v.Name(), m.subst(n, v.Type(), e)))
	}
	return types.NewTuple(vs...)
}

// typeExpr returns a type expression at pos for t, which has no type
// parameters, for the type parameter at n. root records it for the check
// after the second type check.
func (m *mono) typeExpr(n ast.Node, t types.Type, pos token.Pos, root bool) ast.Expr {
	x := m.typeExpr1(n, t, pos)
	if root && x != nil {
		m.written = append(m.written, written{x, typeName(t, false)})
	}
	if x == nil {
		// Never nil in a tree: the copy is not used after a rejection.
		x = &ast.BadExpr{From: pos, To: pos}
	}
	return x
}

func (m *mono) typeExpr1(n ast.Node, t types.Type, pos token.Pos) ast.Expr {
	ident := func(name string) ast.Expr { return &ast.Ident{NamePos: pos, Name: name} }
	sub := func(t types.Type) ast.Expr {
		if x := m.typeExpr1(n, t, pos); x != nil {
			return x
		}
		return &ast.BadExpr{From: pos, To: pos}
	}
	switch t := t.(type) {
	case *types.Basic:
		if t.Info()&types.IsUntyped == 0 {
			return ident(t.Name())
		}
	case *types.Alias:
		if t.Obj().Pkg() == nil {
			return ident(t.Obj().Name())
		}
		return m.typeExpr1(n, types.Unalias(t), pos)
	case *types.Named:
		obj := t.Obj()
		switch {
		case obj.Pkg() == nil:
			return ident(obj.Name()) // error, comparable
		case t.TypeArgs().Len() > 0:
			args := make([]types.Type, t.TypeArgs().Len())
			for i := range args {
				args[i] = t.TypeArgs().At(i)
			}
			return ident(m.instance(n, t.Origin().Obj(), args).name)
		case obj.Parent() == obj.Pkg().Scope():
			return ident(obj.Name())
		}
		m.rejectAt(n, gate.FeatGenericInstance, "type argument "+obj.Name()+" declared in a function")
		return nil
	case *types.Pointer:
		return &ast.StarExpr{Star: pos, X: sub(t.Elem())}
	case *types.Slice:
		return &ast.ArrayType{Lbrack: pos, Elt: sub(t.Elem())}
	case *types.Array:
		return &ast.ArrayType{Lbrack: pos, Len: &ast.BasicLit{ValuePos: pos, Kind: token.INT, Value: strconv.FormatInt(t.Len(), 10)}, Elt: sub(t.Elem())}
	case *types.Map:
		return &ast.MapType{Map: pos, Key: sub(t.Key()), Value: sub(t.Elem())}
	case *types.Signature:
		return m.funcType(n, t, pos)
	case *types.Struct:
		fl := &ast.FieldList{Opening: pos, Closing: pos}
		for i := range t.NumFields() {
			f := t.Field(i)
			if t.Tag(i) != "" {
				m.rejectAt(n, gate.FeatGenericInstance, "type argument "+t.String()+" with a field tag")
				return nil
			}
			field := &ast.Field{Type: sub(f.Type())}
			if !f.Embedded() {
				field.Names = []*ast.Ident{{NamePos: pos, Name: f.Name()}}
			}
			fl.List = append(fl.List, field)
		}
		return &ast.StructType{Struct: pos, Fields: fl}
	case *types.Interface:
		if !t.IsMethodSet() {
			break
		}
		fl := &ast.FieldList{Opening: pos, Closing: pos}
		for i := range t.NumExplicitMethods() {
			f := t.ExplicitMethod(i)
			fl.List = append(fl.List, &ast.Field{Names: []*ast.Ident{{NamePos: pos, Name: f.Name()}}, Type: m.funcType(n, f.Type().(*types.Signature), pos)})
		}
		for i := range t.NumEmbeddeds() {
			fl.List = append(fl.List, &ast.Field{Type: sub(t.EmbeddedType(i))})
		}
		return &ast.InterfaceType{Interface: pos, Methods: fl}
	}
	m.rejectAt(n, gate.FeatGenericInstance, "type argument "+t.String())
	return nil
}

// funcType returns a func type expression at pos for s.
func (m *mono) funcType(n ast.Node, s *types.Signature, pos token.Pos) *ast.FuncType {
	list := func(t *types.Tuple, variadic bool) *ast.FieldList {
		fl := &ast.FieldList{Opening: pos, Closing: pos}
		for i := range t.Len() {
			var x ast.Expr
			if variadic && i == t.Len()-1 {
				x = &ast.Ellipsis{Ellipsis: pos, Elt: m.typeExpr(n, t.At(i).Type().(*types.Slice).Elem(), pos, false)}
			} else {
				x = m.typeExpr(n, t.At(i).Type(), pos, false)
			}
			fl.List = append(fl.List, &ast.Field{Type: x})
		}
		return fl
	}
	return &ast.FuncType{Func: pos, Params: list(s.Params(), s.Variadic()), Results: list(s.Results(), false)}
}

// typeName returns the name of t as gc writes it: in a type argument
// (arg), with unexported field and method names qualified by the package.
func typeName(t types.Type, arg bool) string {
	field := func(v types.Object) string {
		if arg && !v.Exported() && v.Pkg() != nil {
			return v.Pkg().Name() + "." + v.Name()
		}
		return v.Name()
	}
	switch t := t.(type) {
	case *types.Alias:
		return typeName(types.Unalias(t), arg)
	case *types.Basic:
		return types.Typ[t.Kind()].Name()
	case *types.Named:
		name := t.Obj().Name()
		if t.TypeArgs().Len() > 0 {
			var as []string
			for a := range t.TypeArgs().Types() {
				as = append(as, typeName(a, true))
			}
			name += "[" + strings.Join(as, ",") + "]"
		}
		if t.Obj().Pkg() == nil {
			return name
		}
		return t.Obj().Pkg().Name() + "." + name
	case *types.Pointer:
		return "*" + typeName(t.Elem(), arg)
	case *types.Slice:
		return "[]" + typeName(t.Elem(), arg)
	case *types.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), typeName(t.Elem(), arg))
	case *types.Map:
		return "map[" + typeName(t.Key(), arg) + "]" + typeName(t.Elem(), arg)
	case *types.Struct:
		if t.NumFields() == 0 {
			return "struct {}"
		}
		var fs []string
		for f := range t.Fields() {
			if f.Embedded() {
				fs = append(fs, typeName(f.Type(), arg))
			} else {
				fs = append(fs, field(f)+" "+typeName(f.Type(), arg))
			}
		}
		return "struct { " + strings.Join(fs, "; ") + " }"
	case *types.Signature:
		return "func" + sigName(t, arg)
	case *types.Interface:
		if t.NumMethods() == 0 {
			return "interface {}"
		}
		var ms []string
		for f := range t.Methods() {
			ms = append(ms, field(f)+sigName(f.Type().(*types.Signature), arg))
		}
		return "interface { " + strings.Join(ms, "; ") + " }"
	}
	return t.String()
}

// sigName returns a signature as gc writes it after "func".
func sigName(s *types.Signature, arg bool) string {
	var ps []string
	for i := range s.Params().Len() {
		pt := s.Params().At(i).Type()
		if s.Variadic() && i == s.Params().Len()-1 {
			ps = append(ps, "..."+typeName(pt.(*types.Slice).Elem(), arg))
			continue
		}
		ps = append(ps, typeName(pt, arg))
	}
	r := "(" + strings.Join(ps, ", ") + ")"
	switch s.Results().Len() {
	case 0:
	case 1:
		r += " " + typeName(s.Results().At(0).Type(), arg)
	default:
		var rs []string
		for v := range s.Results().Variables() {
			rs = append(rs, typeName(v.Type(), arg))
		}
		r += " (" + strings.Join(rs, ", ") + ")"
	}
	return r
}
