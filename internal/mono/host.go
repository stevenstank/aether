package mono

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/gate"
)

// Hosted instances (D-052). An instance of another package's generic code
// is made by its host: of the generic's package and the packages
// declaring the types in its type arguments, the last in initialization
// order, which can name them all. A package that is not the host names the
// instance, and a hosted copy names every package-level declaration but
// its own, through a view: a package of its own, imported under a name no
// source can write, whose declarations stand for the prepared package's.
// After the type check, every use of a view's declaration is made a use of
// the declaration it stands for, so the later stages never see a view.

// view is the names a package's copies use of the prepared package path.
type view struct {
	local string            // the name the view is imported as
	names map[string]string // the view's name of each declaration
	order []string          // the declarations' names, in order of use
}

// source returns the generic declarations of the package path.
func (m *mono) source(path string) *source {
	if s := m.sources[path]; s != nil {
		return s
	}
	pkg := m.pkg.Others.Original(path)
	s := &source{pkg: pkg, funcs: map[*types.Func]*ast.FuncDecl{}, types: map[*types.TypeName]*ast.TypeSpec{},
		methods: map[*types.TypeName][]*ast.FuncDecl{}}
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if fn, ok := pkg.Info.Defs[d.Name].(*types.Func); ok && d.Type.TypeParams != nil && d.Body != nil {
					s.funcs[fn] = d
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					if sp, ok := sp.(*ast.TypeSpec); ok && sp.TypeParams != nil && !sp.Assign.IsValid() {
						if tn, ok := pkg.Info.Defs[sp.Name].(*types.TypeName); ok {
							s.types[tn] = sp
						}
					}
				}
			}
		}
	}
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && frontend.Generic(pkg.Info, fd) && fd.Recv != nil {
				if tn := recvType(pkg.Info, fd); tn != nil && s.types[tn] != nil {
					s.methods[tn] = append(s.methods[tn], fd)
				}
			}
		}
	}
	m.sources[path] = s
	return s
}

// recvType returns the generic type of which d is a method, or nil.
func recvType(info *types.Info, d *ast.FuncDecl) *types.TypeName {
	fn, ok := info.Defs[d.Name].(*types.Func)
	if !ok || fn.Type().(*types.Signature).Recv() == nil {
		return nil
	}
	t := fn.Type().(*types.Signature).Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok && n.TypeParams().Len() > 0 {
		return n.Origin().Obj()
	}
	return nil
}

// use makes s the package whose code is copied.
func (m *mono) use(s *source) {
	m.info, m.funcs, m.types, m.methods, m.srcTypes = s.pkg.Info, s.funcs, s.types, s.methods, s.pkg.Types
	m.hosting = s != m.own
}

// foreignInst returns the expression naming the instance of obj, another
// package's generic function or type, with args: made here if this
// package hosts it, or else wanted of its host. It returns nil if the
// instance is rejected.
func (m *mono) foreignInst(n ast.Node, obj types.Object, args []types.Type, pos token.Pos) ast.Expr {
	host := m.host(n, obj, args)
	switch host {
	case "":
		return nil
	case m.pkg.Types.Path():
		return &ast.Ident{NamePos: pos, Name: m.instance(n, obj, args).name}
	}
	name := instName(obj, args)
	if host != obj.Pkg().Path() {
		name = frontend.HostedName(name, obj.Pkg().Path())
	}
	found := false
	for _, w := range m.wants {
		found = found || w.Path == obj.Pkg().Path() && w.Name == name
	}
	if !found {
		m.wants = append(m.wants, frontend.Want{Path: obj.Pkg().Path(), Generic: obj.Name(), Name: name, Host: host, Args: args})
	}
	if m.pkg.Others == nil {
		// A package checked alone: as its file imports the generic's.
		return &ast.SelectorExpr{X: &ast.Ident{NamePos: pos, Name: obj.Pkg().Name()}, Sel: &ast.Ident{NamePos: pos, Name: name}}
	}
	return m.viewRef(host, name, pos)
}

// host returns the path of the package that makes the instance of obj with
// args, or "" after rejecting an argument the host cannot name.
func (m *mono) host(n ast.Node, obj types.Object, args []types.Type) string {
	path := obj.Pkg().Path()
	o := m.pkg.Others
	if o == nil {
		for _, a := range args {
			if bad := unnameable(a, obj.Pkg()); bad != "" {
				m.rejectAt(n, gate.FeatGenericInstance, "type argument "+bad+", which package "+path+" cannot name,")
				return ""
			}
		}
		return path
	}
	host := path
	for _, a := range args {
		for _, p := range owners(a, nil) {
			if o.Rank(p) > o.Rank(host) {
				host = p
			}
		}
	}
	for _, a := range args {
		if bad, local := unnameableIn(a, host); bad != "" {
			if local {
				m.rejectAt(n, gate.FeatGenericInstance, "type argument "+bad+" declared in a function")
			} else {
				m.rejectAt(n, gate.FeatGenericInstance, "type argument "+bad+", which package "+host+" cannot name,")
			}
			return ""
		}
	}
	return host
}

// owners appends to ps the paths of the packages declaring the defined
// types, and the unexported fields and methods, in t.
func owners(t types.Type, ps []string) []string {
	switch t := t.(type) {
	case *types.Alias:
		return owners(types.Unalias(t), ps)
	case *types.Named:
		if t.Obj().Pkg() != nil {
			ps = append(ps, t.Obj().Pkg().Path())
		}
		for a := range t.TypeArgs().Types() {
			ps = owners(a, ps)
		}
	case *types.Pointer:
		return owners(t.Elem(), ps)
	case *types.Slice:
		return owners(t.Elem(), ps)
	case *types.Array:
		return owners(t.Elem(), ps)
	case *types.Chan:
		return owners(t.Elem(), ps)
	case *types.Map:
		return owners(t.Elem(), owners(t.Key(), ps))
	case *types.Signature:
		for v := range t.Params().Variables() {
			ps = owners(v.Type(), ps)
		}
		for v := range t.Results().Variables() {
			ps = owners(v.Type(), ps)
		}
	case *types.Struct:
		for f := range t.Fields() {
			if !f.Exported() && f.Pkg() != nil {
				ps = append(ps, f.Pkg().Path())
			}
			ps = owners(f.Type(), ps)
		}
	case *types.Interface:
		for f := range t.Methods() {
			if !f.Exported() && f.Pkg() != nil {
				ps = append(ps, f.Pkg().Path())
			}
			ps = owners(f.Type(), ps)
		}
	}
	return ps
}

// unnameableIn returns the name of a type in t that a copy in the package
// host cannot write, or "": a type declared in a function (local), or an
// unexported type, field, or method of another package.
func unnameableIn(t types.Type, host string) (bad string, local bool) {
	switch t := t.(type) {
	case *types.Alias:
		return unnameableIn(types.Unalias(t), host)
	case *types.Named:
		obj := t.Obj()
		if obj.Pkg() != nil {
			if obj.Parent() != obj.Pkg().Scope() {
				return typeName(t, true), true
			}
			if !obj.Exported() && obj.Pkg().Path() != host {
				return typeName(t, true), false
			}
		}
		for a := range t.TypeArgs().Types() {
			if bad, local := unnameableIn(a, host); bad != "" {
				return bad, local
			}
		}
	case *types.Pointer:
		return unnameableIn(t.Elem(), host)
	case *types.Slice:
		return unnameableIn(t.Elem(), host)
	case *types.Array:
		return unnameableIn(t.Elem(), host)
	case *types.Chan:
		return unnameableIn(t.Elem(), host)
	case *types.Map:
		if bad, local := unnameableIn(t.Key(), host); bad != "" {
			return bad, local
		}
		return unnameableIn(t.Elem(), host)
	case *types.Signature:
		for _, tu := range []*types.Tuple{t.Params(), t.Results()} {
			for v := range tu.Variables() {
				if bad, local := unnameableIn(v.Type(), host); bad != "" {
					return bad, local
				}
			}
		}
	case *types.Struct:
		for f := range t.Fields() {
			if !f.Exported() && f.Pkg() != nil && f.Pkg().Path() != host {
				return typeName(t, true), false
			}
			if bad, local := unnameableIn(f.Type(), host); bad != "" {
				return bad, local
			}
		}
	case *types.Interface:
		for f := range t.Methods() {
			if !f.Exported() && f.Pkg() != nil && f.Pkg().Path() != host {
				return typeName(t, true), false
			}
			if bad, local := unnameableIn(f.Type(), host); bad != "" {
				return bad, local
			}
		}
	}
	return "", false
}

// viewPrefix begins the view's name of an unexported declaration, which a
// view must export; no source can write it, as "·" is not a letter.
const viewPrefix = "X·"

// viewRef returns the expression naming, through its view, the
// package-level declaration name of the package path.
func (m *mono) viewRef(path, name string, pos token.Pos) ast.Expr {
	v := m.views[path]
	if v == nil {
		v = &view{local: fmt.Sprintf("·view%d", len(m.vorder)), names: map[string]string{}}
		m.views[path] = v
		m.vorder = append(m.vorder, path)
	}
	vn, ok := v.names[name]
	if !ok {
		vn = name
		if !token.IsExported(name) {
			vn = viewPrefix + name
		}
		v.names[name] = vn
		v.order = append(v.order, name)
	}
	return &ast.SelectorExpr{X: &ast.Ident{NamePos: pos, Name: v.local}, Sel: &ast.Ident{NamePos: pos, Name: vn}}
}

// viewPath is the import path of the view of the package path.
func viewPath(path string) string { return "aether.view/" + path }

// viewPackages returns the views, by import path, and the prepared
// declaration each of their declarations stands for.
func (m *mono) viewPackages() (map[string]*types.Package, map[types.Object]types.Object) {
	pkgs, real := map[string]*types.Package{}, map[types.Object]types.Object{}
	for _, path := range m.vorder {
		p := m.pkg.Others.Prepared(path)
		if p == nil {
			m.ds = append(m.ds, m.internalDiag("view of %s, which is not prepared", path))
			return nil, nil
		}
		v := m.views[path]
		vp := types.NewPackage(viewPath(path), p.Name())
		for _, name := range v.order {
			obj := p.Scope().Lookup(name)
			vn := v.names[name]
			var s types.Object
			switch obj := obj.(type) {
			case *types.TypeName:
				// Not an alias: a name of the prepared type itself.
				s = types.NewTypeName(token.NoPos, vp, vn, obj.Type())
			case *types.Func:
				s = types.NewFunc(token.NoPos, vp, vn, obj.Type().(*types.Signature))
			case *types.Var:
				s = types.NewVar(token.NoPos, vp, vn, obj.Type())
			case *types.Const:
				s = types.NewConst(token.NoPos, vp, vn, obj.Type(), obj.Val())
			default:
				m.ds = append(m.ds, m.internalDiag("view of %s.%s, which the prepared package does not declare", path, name))
				return nil, nil
			}
			vp.Scope().Insert(s)
			real[s] = obj
		}
		vp.MarkComplete()
		pkgs[viewPath(path)] = vp
	}
	return pkgs, real
}

// viewImports returns a new declaration importing the views, for one
// file, or nil.
func (m *mono) viewImports() *ast.GenDecl {
	if len(m.vorder) == 0 {
		return nil
	}
	g := &ast.GenDecl{Tok: token.IMPORT, Lparen: 1}
	for _, path := range m.vorder {
		g.Specs = append(g.Specs, &ast.ImportSpec{Name: &ast.Ident{Name: m.views[path].local},
			Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(viewPath(path))}})
	}
	return g
}

// hostedFile returns a file of the copies of the hosted instances, or nil.
func (m *mono) hostedFile() *ast.File {
	var decls, funcs []ast.Decl
	g := &ast.GenDecl{Tok: token.TYPE, Lparen: 1}
	for _, in := range m.order {
		if in.src == m.own {
			continue
		}
		if in.spec != nil {
			g.Specs = append(g.Specs, in.spec)
			for _, md := range in.src.methods[in.obj.(*types.TypeName)] {
				funcs = append(funcs, in.methods[md])
			}
		} else {
			funcs = append(funcs, in.fn)
		}
	}
	if len(g.Specs) > 0 {
		decls = append(decls, g)
	}
	if len(decls)+len(funcs) == 0 {
		return nil
	}
	f0 := m.pkg.Files[0]
	unsafe := &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{&ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: `"unsafe"`}}}}
	decls = append(append([]ast.Decl{unsafe}, decls...), funcs...)
	return &ast.File{Name: &ast.Ident{Name: f0.Name.Name}, Decls: decls, GoVersion: f0.GoVersion}
}

// viewImporter imports the views, and other packages with the package's
// importer.
type viewImporter struct {
	base  types.Importer
	views map[string]*types.Package
}

func (v viewImporter) Import(path string) (*types.Package, error) {
	if p := v.views[path]; p != nil {
		return p, nil
	}
	if path == "unsafe" || v.base == nil {
		return types.Unsafe, nil
	}
	return v.base.Import(path)
}

// resolveViews makes each use of a view's declaration in out a use of the
// declaration it stands for, records the homes of the hosted instances'
// declarations, and rejects an embedded field a view names, whose name
// would be the view's.
func (m *mono) resolveViews(out *frontend.Package, real map[types.Object]types.Object) {
	for id, obj := range out.Info.Uses {
		if r, ok := real[obj]; ok {
			out.Info.Uses[id] = r
		}
	}
	// In source order, so that the diagnostics are deterministic.
	var embedded []*ast.Ident
	for id, obj := range out.Info.Defs {
		if v, ok := obj.(*types.Var); ok && v.Embedded() && strings.HasPrefix(v.Name(), viewPrefix) {
			embedded = append(embedded, id)
		}
	}
	slices.SortFunc(embedded, func(a, b *ast.Ident) int { return cmp.Compare(a.Pos(), b.Pos()) })
	for _, id := range embedded {
		m.reject(frontend.Position(out.Fset, id.Pos()), gate.FeatGenericInstance, "embedded unexported type "+strings.TrimPrefix(id.Name, viewPrefix)+" of another package in a hosted instance")
	}
	for _, in := range m.order {
		if in.src == m.own {
			continue
		}
		if out.Homes == nil {
			out.Homes = map[types.Object]*types.Package{}
		}
		home := in.src.pkg.Types
		var ids []*ast.Ident
		if in.spec != nil {
			ids = append(ids, in.spec.Name)
			for _, c := range in.methods {
				ids = append(ids, c.Name)
			}
		} else {
			ids = append(ids, in.fn.Name)
		}
		for _, id := range ids {
			obj := out.Info.Defs[id]
			if obj == nil {
				m.internal(id, "no object for the hosted %s", id.Name)
				continue
			}
			out.Homes[obj] = home
			if tn, ok := obj.(*types.TypeName); ok {
				homeFields(tn.Type().Underlying(), out.Types, home, out.Homes)
			}
		}
	}
}

// homeFields records home as the home of the unexported fields of t that
// p declares.
func homeFields(t types.Type, p, home *types.Package, homes map[types.Object]*types.Package) {
	s, ok := t.(*types.Struct)
	if !ok {
		return
	}
	for f := range s.Fields() {
		if f.Pkg() == p && !f.Exported() {
			homes[f] = home
		}
		homeFields(f.Type(), p, home, homes)
	}
}

// internalDiag returns an Internal diagnostic of this package.
func (m *mono) internalDiag(format string, args ...any) diag.Diagnostic {
	return diag.Diagnostic{Class: diag.Internal, Message: "monomorphization: " + fmt.Sprintf(format, args...)}
}

// viewName matches a view's qualifier, the prefix of a name it exports,
// and a hosted instance's home in its name (frontend.HostedName).
var viewName = regexp.MustCompile(`·view[0-9]+\.|` + viewPrefix + `|·of·[^\s(),\]]*`)

// sourceNames returns msg, a type checker's message about copies, with
// the names of views' declarations written as the source writes them.
func sourceNames(msg string) string { return viewName.ReplaceAllString(msg, "") }
