// Package mono instantiates the generic functions and types of a
// type-checked program at the source level (D-045, Phase 8 increment 8.8).
//
// Starting from the program's non-generic declarations, it copies each
// declaration's syntax tree, and for every instance it reaches a copy of
// the generic declaration in which each type parameter is replaced by a
// type expression of its argument; a generic type's copy brings a copy of
// each of its methods. References to instances, explicit or inferred,
// become identifiers naming the copy, F[int] or Box[main.P], the type
// arguments written as gc writes them, so that a copied type has gc's
// name. The copies keep the source positions. The program made of the
// copies, without generic declarations and without the interfaces that can
// only be constraints, is type-checked again; the gate and lowering see
// only it.
package mono

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
)

// maxInstances bounds the instances of a program, which a generic function
// that instantiates itself with ever larger types could make endless.
const maxInstances = 1000

// env maps the type parameters of the declaration being copied to their
// arguments, which have no type parameters.
type env map[*types.TypeParam]types.Type

// instance is a generic function or type with its type arguments.
type instance struct {
	name    string       // the copy's identifier: F[int,main.P]
	obj     types.Object // the generic *types.Func or *types.TypeName
	args    []types.Type
	fn      *ast.FuncDecl                   // the copy of a function
	spec    *ast.TypeSpec                   // the copy of a type
	methods map[*ast.FuncDecl]*ast.FuncDecl // the copies of a type's methods
}

// written is a type expression written for a type argument, and the name
// of the type it must denote.
type written struct {
	e    ast.Expr
	name string
}

type mono struct {
	pkg     *frontend.Package
	info    *types.Info
	ctxt    *types.Context
	funcs   map[*types.Func]*ast.FuncDecl       // the generic functions
	types   map[*types.TypeName]*ast.TypeSpec   // the generic types
	methods map[*types.TypeName][]*ast.FuncDecl // their methods, in source order
	insts   map[string]*instance
	order   []*instance // in order of creation
	queue   []*instance // not yet copied
	copies  map[ast.Node]ast.Node
	written []written
	ds      []diag.Diagnostic
	seen    map[diag.Pos]bool
}

// Instantiate returns pkg with its generic declarations replaced by their
// instances, type-checked again, or Unsupported diagnostics. A program
// without generic declarations is returned as it is.
func Instantiate(pkg *frontend.Package) (*frontend.Package, []diag.Diagnostic) {
	m := &mono{pkg: pkg, info: pkg.Info, ctxt: types.NewContext(),
		funcs: map[*types.Func]*ast.FuncDecl{}, types: map[*types.TypeName]*ast.TypeSpec{},
		methods: map[*types.TypeName][]*ast.FuncDecl{}, insts: map[string]*instance{},
		copies: map[ast.Node]ast.Node{}, seen: map[diag.Pos]bool{}}
	// An instance is made in its generic declaration's package, which
	// is not yet supported for another package's (D-049, increment 11.7).
	for id, in := range pkg.Info.Instances {
		if obj := pkg.Info.Uses[id]; obj != nil && obj.Pkg() != pkg.Types {
			m.rejectAt(id, gate.FeatGenericInstance, "instance "+in.Type.String()+" of another package's generic code")
		}
	}
	if len(m.ds) > 0 {
		diag.Sort(m.ds)
		return nil, m.ds
	}
	if !m.collect() {
		return pkg, nil
	}
	if len(m.ds) > 0 {
		return nil, m.ds
	}
	// The non-generic declarations, which instantiate the first instances.
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if m.genericFunc(d) == nil && m.genericRecv(d) == nil {
					m.copies[d] = m.copy(d, nil)
				}
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					m.copies[d] = m.copy(d, nil)
					continue
				}
				for _, s := range d.Specs {
					if s := s.(*ast.TypeSpec); m.genericType(s) == nil && !m.constraint(s) {
						m.copies[s] = m.copy(s, nil)
					}
				}
			}
		}
	}
	for len(m.queue) > 0 && len(m.ds) == 0 {
		in := m.queue[0]
		m.queue = m.queue[1:]
		m.build(in)
	}
	if len(m.ds) > 0 {
		return nil, m.ds
	}
	// Another package's generic code may be instantiated only by the
	// packages importing it, which are rejected until increment 11.7
	// (D-049); the instances leave it out.
	if pkg.Types.Path() == "main" {
		m.unused()
	}
	if len(m.ds) > 0 {
		return nil, m.ds
	}
	out, ds := frontend.CheckPackage(pkg.Fset, pkg.Types.Path(), m.files(), pkg.Importer)
	if len(ds) > 0 {
		// The first error only: the others often follow from it.
		m.reject(ds[0].Pos, gate.FeatGenericInstance, "generic code whose instance does not type-check ("+ds[0].Message+")")
	}
	if len(m.ds) > 0 {
		return nil, m.ds
	}
	// A type expression written for a type argument must denote it: a
	// local declaration could shadow the name it is written with.
	for _, w := range m.written {
		if tv, ok := out.Info.Types[w.e]; !ok || !tv.IsType() || typeName(tv.Type, false) != w.name {
			m.reject(frontend.Position(pkg.Fset, w.e.Pos()), gate.FeatGenericInstance,
				"type argument "+w.name+" whose name is shadowed in generic code")
		}
	}
	if len(m.ds) > 0 {
		return nil, m.ds
	}
	return out, nil
}

func (m *mono) reject(pos diag.Pos, feature, what string) {
	if m.seen[pos] {
		return // one diagnostic per position, however many instances
	}
	m.seen[pos] = true
	m.ds = append(m.ds, gate.Reject(pos, feature, what))
}

func (m *mono) rejectAt(n ast.Node, feature, what string) {
	m.reject(frontend.Position(m.pkg.Fset, n.Pos()), feature, what)
}

// internal reports a violated assumption of this package.
func (m *mono) internal(n ast.Node, format string, args ...any) {
	m.ds = append(m.ds, diag.Diagnostic{Class: diag.Internal, Pos: frontend.Position(m.pkg.Fset, n.Pos()),
		Message: "monomorphization: " + fmt.Sprintf(format, args...)})
}

// collect finds the generic declarations, and reports whether there are
// any, or interfaces that can only be constraints.
func (m *mono) collect() bool {
	any := false
	for _, f := range m.pkg.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Type.TypeParams != nil {
					any = true
					if fn, ok := m.info.Defs[d.Name].(*types.Func); ok && d.Body != nil {
						m.funcs[fn] = d
					} else {
						m.rejectAt(d.Name, gate.FeatNoBody, "generic function "+d.Name.Name+" without a body")
					}
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					s, ok := s.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if s.TypeParams == nil {
						// An interface that can only be a constraint is
						// left out, also from a program without generic
						// code.
						any = any || m.constraint(s)
						continue
					}
					any = true
					tn, ok := m.info.Defs[s.Name].(*types.TypeName)
					switch {
					case s.Assign.IsValid():
						m.rejectAt(s, gate.FeatPackageDecl, "generic type alias "+s.Name.Name)
					case !ok:
						m.internal(s, "no type name for %s", s.Name.Name)
					default:
						m.types[tn] = s
					}
				}
			}
		}
	}
	for _, f := range m.pkg.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if tn := m.genericRecv(fd); tn != nil {
					m.methods[tn] = append(m.methods[tn], fd)
				}
			}
		}
	}
	return any
}

// genericFunc returns the generic function d declares, or nil.
func (m *mono) genericFunc(d *ast.FuncDecl) *types.Func {
	fn, _ := m.info.Defs[d.Name].(*types.Func)
	if m.funcs[fn] == nil {
		return nil
	}
	return fn
}

// genericType returns the generic type s declares, or nil.
func (m *mono) genericType(s *ast.TypeSpec) *types.TypeName {
	tn, _ := m.info.Defs[s.Name].(*types.TypeName)
	if m.types[tn] == nil {
		return nil
	}
	return tn
}

// genericRecv returns the generic type of which d is a method, or nil.
func (m *mono) genericRecv(d *ast.FuncDecl) *types.TypeName {
	if d.Recv == nil || len(d.Recv.List) != 1 {
		return nil
	}
	fn, ok := m.info.Defs[d.Name].(*types.Func)
	if !ok {
		return nil
	}
	t := fn.Type().(*types.Signature).Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.TypeParams().Len() > 0 && m.types[n.Origin().Obj()] != nil {
		return n.Origin().Obj()
	}
	return nil
}

// constraint reports whether s declares an interface that is not a method
// set, which can only be used as a constraint.
func (m *mono) constraint(s *ast.TypeSpec) bool {
	tn, ok := m.info.Defs[s.Name].(*types.TypeName)
	if !ok {
		return false
	}
	i, ok := tn.Type().Underlying().(*types.Interface)
	return ok && !i.IsMethodSet()
}

// unused rejects the generic functions and types the program never
// instantiates, whose code would be neither checked nor compiled. A
// generic interface has no code, and may be instantiated only in
// constraints, which the copies do not have.
func (m *mono) unused() {
	used := map[types.Object]bool{}
	for _, in := range m.order {
		used[in.obj] = true
	}
	for _, f := range m.pkg.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if fn := m.genericFunc(d); fn != nil && !used[fn] {
					m.rejectAt(d.Name, gate.FeatGenericUnused, "generic function "+d.Name.Name+", never instantiated,")
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					s, ok := s.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if tn := m.genericType(s); tn != nil && !used[tn] && !types.IsInterface(tn.Type()) {
						m.rejectAt(s.Name, gate.FeatGenericUnused, "generic type "+s.Name.Name+", never instantiated,")
					}
				}
			}
		}
	}
}

// instance returns the instance of the generic obj with args, creating it
// and queuing its copy if it is new.
func (m *mono) instance(n ast.Node, obj types.Object, args []types.Type) *instance {
	names := make([]string, len(args))
	for i, a := range args {
		names[i] = typeName(a, true)
	}
	name := obj.Name() + "[" + strings.Join(names, ",") + "]"
	if in := m.insts[name]; in != nil {
		for i, a := range in.args {
			if !types.Identical(a, args[i]) {
				m.rejectAt(n, gate.FeatGenericInstance, "instance "+name+" of different type arguments with the same name")
			}
		}
		return in
	}
	if len(m.insts) == maxInstances {
		m.rejectAt(n, gate.FeatGenericInstance, fmt.Sprintf("a program of more than %d instances of generic code", maxInstances))
	}
	in := &instance{name: name, obj: obj, args: args}
	m.insts[name] = in
	m.order = append(m.order, in)
	m.queue = append(m.queue, in)
	return in
}

// build makes the copies of the instance in.
func (m *mono) build(in *instance) {
	switch obj := in.obj.(type) {
	case *types.Func:
		fd := m.funcs[obj]
		e := bind(obj.Type().(*types.Signature).TypeParams(), in.args)
		c := *fd
		c.Doc = nil
		c.Name = &ast.Ident{NamePos: fd.Name.NamePos, Name: in.name}
		ft := *fd.Type
		ft.TypeParams = nil
		ft.Params = m.copy(fd.Type.Params, e).(*ast.FieldList)
		if fd.Type.Results != nil {
			ft.Results = m.copy(fd.Type.Results, e).(*ast.FieldList)
		}
		c.Type = &ft
		c.Body = m.copy(fd.Body, e).(*ast.BlockStmt)
		in.fn = &c
	case *types.TypeName:
		s := m.types[obj]
		e := bind(obj.Type().(*types.Named).TypeParams(), in.args)
		in.spec = &ast.TypeSpec{Name: &ast.Ident{NamePos: s.Name.NamePos, Name: in.name}, Type: m.copy(s.Type, e).(ast.Expr)}
		in.methods = map[*ast.FuncDecl]*ast.FuncDecl{}
		for _, md := range m.methods[obj] {
			fn := m.info.Defs[md.Name].(*types.Func)
			e := bind(fn.Type().(*types.Signature).RecvTypeParams(), in.args)
			c := *md
			c.Doc = nil
			c.Name = &ast.Ident{NamePos: md.Name.NamePos, Name: md.Name.Name}
			r := md.Recv.List[0]
			var rt ast.Expr = &ast.Ident{NamePos: r.Type.Pos(), Name: in.name}
			if _, ok := ast.Unparen(r.Type).(*ast.StarExpr); ok {
				rt = &ast.StarExpr{Star: r.Type.Pos(), X: rt}
			}
			var names []*ast.Ident
			for _, id := range r.Names {
				names = append(names, &ast.Ident{NamePos: id.NamePos, Name: id.Name})
			}
			c.Recv = &ast.FieldList{Opening: md.Recv.Opening, List: []*ast.Field{{Names: names, Type: rt}}, Closing: md.Recv.Closing}
			c.Type = m.copy(md.Type, e).(*ast.FuncType)
			c.Body = m.copy(md.Body, e).(*ast.BlockStmt)
			in.methods[md] = &c
		}
	}
}

// bind maps the type parameters ps to args.
func bind(ps *types.TypeParamList, args []types.Type) env {
	e := env{}
	for i := range ps.Len() {
		e[ps.At(i)] = args[i]
	}
	return e
}

// files assembles the copies in the order of the declarations they come
// from, each generic declaration replaced by its instances' copies.
//
// A package besides main keeps its generic declarations, as they are, for
// the packages importing it; the gate and lowering skip them (D-049).
func (m *mono) files() []*ast.File {
	keep := m.pkg.Types.Path() != "main"
	var files []*ast.File
	for _, f := range m.pkg.Files {
		nf := &ast.File{Package: f.Package, Name: f.Name, FileStart: f.FileStart, FileEnd: f.FileEnd, GoVersion: f.GoVersion}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				fn, tn := m.genericFunc(d), m.genericRecv(d)
				switch {
				case fn != nil:
					for _, in := range m.order {
						if in.obj == fn {
							nf.Decls = append(nf.Decls, in.fn)
						}
					}
					if keep {
						nf.Decls = append(nf.Decls, d)
					}
				case tn != nil:
					for _, in := range m.order {
						if in.obj == tn {
							nf.Decls = append(nf.Decls, in.methods[d])
						}
					}
					if keep {
						nf.Decls = append(nf.Decls, d)
					}
				default:
					nf.Decls = append(nf.Decls, m.copies[d].(ast.Decl))
				}
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					nf.Decls = append(nf.Decls, m.copies[d].(ast.Decl))
					continue
				}
				g := &ast.GenDecl{TokPos: d.TokPos, Tok: d.Tok, Lparen: d.Lparen, Rparen: d.Rparen}
				for _, s := range d.Specs {
					s := s.(*ast.TypeSpec)
					if tn := m.genericType(s); tn != nil {
						for _, in := range m.order {
							if in.obj == tn {
								g.Specs = append(g.Specs, in.spec)
							}
						}
						if keep {
							g.Specs = append(g.Specs, s)
						}
					} else if c := m.copies[s]; c != nil {
						g.Specs = append(g.Specs, c.(ast.Spec))
					} else if keep {
						g.Specs = append(g.Specs, s) // a constraint
					}
				}
				if len(g.Specs) > 0 {
					nf.Decls = append(nf.Decls, g)
				}
			}
		}
		files = append(files, nf)
	}
	return files
}

var commentGroup = reflect.TypeFor[*ast.CommentGroup]()

// copy returns a deep copy of the syntax tree n under e, rewritten by
// rewrite. Comments are left out.
func (m *mono) copy(n ast.Node, e env) ast.Node {
	if r := m.rewrite(n, e); r != nil {
		return r
	}
	v := reflect.ValueOf(n).Elem()
	c := reflect.New(v.Type())
	c.Elem().Set(v)
	for i := range v.NumField() {
		f := c.Elem().Field(i)
		f.Set(m.value(n, f, e))
	}
	return c.Interface().(ast.Node)
}

// value returns a copy of the field v of the node n.
func (m *mono) value(n ast.Node, v reflect.Value, e env) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || v.Type() == commentGroup {
			return reflect.Zero(v.Type())
		}
		x, ok := v.Interface().(ast.Node)
		if !ok {
			return v
		}
		c := reflect.ValueOf(m.copy(x, e))
		if !c.Type().AssignableTo(v.Type()) {
			// Only an expression is replaced by one of another kind.
			m.internal(n, "replacement %s for a field of type %s", c.Type(), v.Type())
			return v
		}
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		r := reflect.New(v.Type()).Elem()
		r.Set(reflect.ValueOf(m.copy(v.Interface().(ast.Node), e)))
		return r
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		r := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			r.Index(i).Set(m.value(n, v.Index(i), e))
		}
		return r
	}
	return v
}

// rewrite returns the replacement of n in a copy under e, or nil: a type
// expression for a type parameter, an identifier for an instance, and a
// non-constant form of a constant conversion to a type parameter.
func (m *mono) rewrite(n ast.Node, e env) ast.Node {
	switch n := n.(type) {
	case *ast.Ident:
		if tn, ok := m.info.Uses[n].(*types.TypeName); ok {
			if tp, ok := tn.Type().(*types.TypeParam); ok {
				return m.typeExpr(n, m.subst(n, tp, e), n.Pos(), true)
			}
		}
		if in, ok := m.info.Instances[n]; ok {
			return m.ref(n, n, in, e)
		}
	case *ast.IndexExpr:
		if id, ok := ast.Unparen(n.X).(*ast.Ident); ok {
			if in, ok := m.info.Instances[id]; ok {
				return m.ref(n, id, in, e)
			}
		}
	case *ast.IndexListExpr:
		if id, ok := ast.Unparen(n.X).(*ast.Ident); ok {
			if in, ok := m.info.Instances[id]; ok {
				return m.ref(n, id, in, e)
			}
		}
	case *ast.CallExpr:
		// Under a type parameter, T(c) is not a constant: arithmetic on
		// it happens at run time. [1]T{T(c)}[0] has its value and is not
		// a constant either.
		if tv := m.info.Types[n.Fun]; len(e) > 0 && tv.IsType() && len(n.Args) == 1 && m.info.Types[n.Args[0]].Value != nil {
			if _, ok := tv.Type.(*types.TypeParam); ok {
				p, end := n.Pos(), n.End()-1
				conv := &ast.CallExpr{Fun: m.copy(n.Fun, e).(ast.Expr), Lparen: n.Lparen, Args: []ast.Expr{m.copy(n.Args[0], e).(ast.Expr)}, Rparen: n.Rparen}
				lit := &ast.CompositeLit{
					Type:   &ast.ArrayType{Lbrack: p, Len: &ast.BasicLit{ValuePos: p, Kind: token.INT, Value: "1"}, Elt: m.copy(n.Fun, e).(ast.Expr)},
					Lbrace: p, Elts: []ast.Expr{conv}, Rbrace: end}
				return &ast.IndexExpr{X: lit, Lbrack: end, Index: &ast.BasicLit{ValuePos: end, Kind: token.INT, Value: "0"}, Rbrack: end}
			}
		}
	}
	return nil
}

// ref returns the identifier of the instance that id, in the expression
// n, refers to.
func (m *mono) ref(n ast.Node, id *ast.Ident, in types.Instance, e env) ast.Node {
	obj := m.info.Uses[id]
	switch obj := obj.(type) {
	case *types.Func:
		if m.funcs[obj] == nil {
			m.internal(n, "instance of %s, not a generic function", obj.Name())
			return nil
		}
	case *types.TypeName:
		if m.types[obj] == nil {
			m.internal(n, "instance of %s, not a generic type", obj.Name())
			return nil
		}
	default:
		m.internal(n, "instance of %v", obj)
		return nil
	}
	args := make([]types.Type, in.TypeArgs.Len())
	for i := range args {
		args[i] = m.subst(n, in.TypeArgs.At(i), e)
	}
	return &ast.Ident{NamePos: n.Pos(), Name: m.instance(n, obj, args).name}
}
