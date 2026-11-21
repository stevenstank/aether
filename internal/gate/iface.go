package gate

// Interfaces (Phase 8 increment 8.4, D-041, docs/SEMANTICS.md §Interfaces):
// interface types of methods with supported signatures, declared or
// literal, any, and error; conversions to them; calls of their methods;
// type assertions and type switches. An interface embedded in a struct is
// rejected, since its methods would be promoted through it, as is a switch
// statement on an interface value or with a case value of interface type.

import (
	"go/ast"
	"go/types"
)

func isInterface(t types.Type) bool {
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

// ifaceType reports whether t is an interface type of methods only, with
// supported signatures: not a constraint's type set.
func (c *checker) ifaceType(t *types.Interface, seen map[*types.Named]bool) bool {
	if !t.IsMethodSet() {
		return false
	}
	for m := range t.Methods() {
		sig := m.Type().(*types.Signature)
		if sig.TypeParams().Len() > 0 {
			return false
		}
		for _, tuple := range []*types.Tuple{sig.Params(), sig.Results()} {
			for v := range tuple.Variables() {
				if !c.sizedType(v.Type(), seen) {
					return false
				}
			}
		}
	}
	return true
}

// ifaceTypeExpr checks an interface type literal: its methods' signatures
// and its embedded interfaces, rejecting an unsupported one.
func (c *checker) ifaceTypeExpr(t *ast.InterfaceType) bool {
	mark := len(c.ds)
	for _, f := range t.Methods.List {
		ft, ok := f.Type.(*ast.FuncType)
		if !ok {
			c.typeExpr(f.Type) // an embedded interface
			continue
		}
		// A method: its parameter and result types. go/types gives its
		// signature a receiver, which a func type has not.
		for _, fl := range []*ast.FieldList{ft.Params, ft.Results} {
			if fl == nil {
				continue
			}
			for _, p := range fl.List {
				c.typeExpr(elemOfVariadic(p.Type))
			}
		}
	}
	return len(c.ds) == mark
}

// typeAssert checks x.(T): its operand and the type asserted.
func (c *checker) typeAssert(e *ast.TypeAssertExpr) {
	if e.Type == nil {
		c.internal(e, "x.(type) outside a type switch")
		return
	}
	if c.typeExpr(e.Type) {
		c.expr(e.X)
	}
}

// typeSwitch checks a type switch: its init, the operand of its guard, the
// types of its cases, nil among them, and their bodies.
func (c *checker) typeSwitch(s *ast.TypeSwitchStmt) {
	if s.Init != nil {
		c.stmt(s.Init)
	}
	var guard ast.Expr
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		guard = a.X
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			guard = a.Rhs[0]
		}
	}
	ta, ok := guard.(*ast.TypeAssertExpr)
	if !ok {
		c.internal(s, "type switch without a guard")
		return
	}
	c.expr(ta.X)
	for _, cc := range s.Body.List {
		cc := cc.(*ast.CaseClause)
		for _, e := range cc.List {
			if c.info.Types[e].IsNil() {
				continue
			}
			c.typeExpr(e)
		}
		c.stmts(cc.Body)
	}
}
