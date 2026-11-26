package frontend

import (
	"go/ast"
	"go/token"
	"go/types"
)

// prune replaces each package of prog besides main by the declarations
// main can reach (D-049, amending D-003), so that the gate and lowering see
// only those:
//
//   - all of main, the init functions, and the package variables whose
//     initializers may have an effect (impure) are roots;
//   - a declaration is reachable when reachable code names it, or names
//     an object whose type involves it;
//   - a method of a reachable defined type is reachable when reachable
//     code names it, or when a reachable interface type has a method of
//     its name (and package, if unexported): only then can it be called,
//     through an itab (v2 evaluation; Aether's reflect calls no method).
//     Error and String are always kept, for the printing of panics.
//
// The pruned packages share their syntax and types.Info maps with the
// unpruned ones; only their files' declaration lists and InitOrder differ.
func prune(prog *Program) {
	if len(prog.Packages) == 1 {
		return
	}
	r := &reacher{decls: map[types.Object]ast.Node{}, methods: map[*types.TypeName][]types.Object{},
		seen: map[types.Object]bool{}, types: map[types.Type]bool{},
		ifaceIDs: map[string]bool{"Error": true, "String": true}, pending: map[string][]types.Object{}}
	main := prog.Main()
	var roots []ast.Node
	for _, p := range prog.Packages[:len(prog.Packages)-1] {
		r.info = p.Info
		for _, f := range p.Files {
			for _, d := range f.Decls {
				roots = append(roots, r.index(d)...)
			}
		}
	}
	// Info's maps are shared by every walk: their keys are distinct in
	// each package, and the merged lookups below go to the right one.
	r.infos = make([]*types.Info, len(prog.Packages))
	for i, p := range prog.Packages {
		r.infos[i] = p.Info
	}
	for _, f := range main.Files {
		r.walk(f)
	}
	for _, n := range roots {
		if s, ok := n.(*ast.ValueSpec); ok {
			for _, id := range s.Names {
				for _, info := range r.infos {
					if obj := info.Defs[id]; obj != nil {
						r.seen[obj] = true
					}
				}
			}
		}
		r.walk(n)
	}
	// The functions lowering's scheduler calls are reachable if the
	// program can make what they serve (D-054).
	for _, p := range prog.Packages {
		hooks := RuntimeHooks[p.Types.Path()]
		if trigger := p.Types.Scope().Lookup(hooks.Trigger); trigger == nil || !r.seen[trigger] {
			continue
		}
		for _, name := range hooks.Funcs {
			if obj := p.Types.Scope().Lookup(name); obj != nil {
				r.mark(obj)
			}
		}
	}
	for i, p := range prog.Packages[:len(prog.Packages)-1] {
		prog.Packages[i] = r.pruned(p)
	}
}

// Hooks are a package's functions lowering's scheduler calls, if Trigger
// is reachable.
type Hooks struct {
	Trigger string
	Funcs   []string
}

// RuntimeHooks are the functions of time that lowering's scheduler and
// channels call, by package path, if the program can make a timer: the
// part of gc's runtime that is Aether's time/runtime.go (D-054).
var RuntimeHooks = map[string]Hooks{
	"time": {Trigger: "newRuntimeTimer", Funcs: []string{"runTimers", "maybeRunChan", "blockTimerChan", "unblockTimerChan"}},
}

type reacher struct {
	info    *types.Info   // while indexing
	infos   []*types.Info // every package's
	decls   map[types.Object]ast.Node
	methods map[*types.TypeName][]types.Object
	seen    map[types.Object]bool
	types   map[types.Type]bool
	// The method identities of the reachable interfaces, and the methods
	// of reachable types not yet reachable, by identity.
	ifaceIDs map[string]bool
	pending  map[string][]types.Object
}

// index records the declarations of d by object, and returns those that
// are roots.
func (r *reacher) index(d ast.Decl) []ast.Node {
	var roots []ast.Node
	switch d := d.(type) {
	case *ast.FuncDecl:
		obj := r.info.Defs[d.Name]
		if obj == nil {
			return nil
		}
		r.decls[obj] = d
		if d.Recv == nil && d.Name.Name == "init" {
			roots = append(roots, d)
		}
		if sig := obj.Type().(*types.Signature); sig.Recv() != nil {
			t := sig.Recv().Type()
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			if n, ok := types.Unalias(t).(*types.Named); ok {
				r.methods[n.Obj()] = append(r.methods[n.Obj()], obj)
			}
		}
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.ValueSpec:
				for _, id := range s.Names {
					if obj := r.info.Defs[id]; obj != nil {
						r.decls[obj] = s
					}
				}
				for _, v := range s.Values {
					if !pure(r.info, v) {
						roots = append(roots, s)
						break
					}
				}
			case *ast.TypeSpec:
				if obj := r.info.Defs[s.Name]; obj != nil {
					r.decls[obj] = s
				}
			}
		}
	}
	return roots
}

// pure reports whether evaluating e can have no effect: no call, receive,
// or operation that may panic.
func pure(info *types.Info, e ast.Expr) bool {
	if tv, ok := info.Types[e]; ok && tv.Value != nil {
		return true
	}
	switch e := e.(type) {
	case *ast.Ident, *ast.FuncLit:
		return true
	case *ast.ParenExpr:
		return pure(info, e.X)
	case *ast.UnaryExpr:
		return e.Op == token.AND && pure(info, e.X)
	case *ast.KeyValueExpr:
		return pure(info, e.Key) && pure(info, e.Value)
	case *ast.CompositeLit:
		for _, x := range e.Elts {
			if !pure(info, x) {
				return false
			}
		}
		return true
	case *ast.CallExpr:
		// A conversion, except of a slice to an array or a pointer to
		// one, which may panic (D-055).
		if tv := info.Types[e.Fun]; tv.IsType() && len(e.Args) == 1 {
			if at := info.Types[e.Args[0]].Type; at != nil {
				if _, sl := at.Underlying().(*types.Slice); sl && !isSliceType(tv.Type) {
					return false
				}
			}
			return pure(info, e.Args[0])
		}
	}
	return false
}

// walk marks every object n names, and the types of its expressions.
func (r *reacher) walk(n ast.Node) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			for _, info := range r.infos {
				if obj := info.Uses[n]; obj != nil {
					r.mark(obj)
					break
				}
			}
		case *ast.SelectorExpr:
			for _, info := range r.infos {
				if sel := info.Selections[n]; sel != nil {
					r.mark(sel.Obj())
					break
				}
			}
		}
		if e, ok := n.(ast.Expr); ok {
			for _, info := range r.infos {
				if tv, ok := info.Types[e]; ok {
					r.markType(tv.Type)
					break
				}
			}
		}
		return true
	})
}

// mark makes obj reachable, with its declaration, its type, and, for a
// type, its methods.
func (r *reacher) mark(obj types.Object) {
	if r.seen[obj] {
		return
	}
	r.seen[obj] = true
	if obj.Type() != nil {
		r.markType(obj.Type())
	}
	if tn, ok := obj.(*types.TypeName); ok {
		for _, m := range r.methods[tn] {
			if id := m.Id(); r.ifaceIDs[id] {
				r.mark(m)
			} else {
				r.pending[id] = append(r.pending[id], m)
			}
		}
	}
	if d := r.decls[obj]; d != nil {
		r.walk(d)
	}
}

// markType marks the defined types t involves.
func (r *reacher) markType(t types.Type) {
	if t == nil || r.types[t] {
		return
	}
	r.types[t] = true
	switch t := t.(type) {
	case *types.Named:
		r.mark(t.Obj())
		for a := range t.TypeArgs().Types() {
			r.markType(a)
		}
		if _, ok := t.Underlying().(*types.Interface); ok {
			r.markType(t.Underlying()) // error's, which has no declaration
		}
	case *types.Interface:
		// Its methods may be called on any reachable type that has them.
		for m := range t.Methods() {
			id := m.Id()
			if r.ifaceIDs[id] {
				continue
			}
			r.ifaceIDs[id] = true
			ms := r.pending[id]
			delete(r.pending, id)
			for _, m := range ms {
				r.mark(m)
			}
		}
	case *types.Alias:
		r.markType(types.Unalias(t))
	case *types.Pointer:
		r.markType(t.Elem())
	case *types.Slice:
		r.markType(t.Elem())
	case *types.Array:
		r.markType(t.Elem())
	case *types.Chan:
		r.markType(t.Elem())
	case *types.Map:
		r.markType(t.Key())
		r.markType(t.Elem())
	case *types.Struct:
		for f := range t.Fields() {
			r.markType(f.Type())
		}
	case *types.Tuple:
		for v := range t.Variables() {
			r.markType(v.Type())
		}
	case *types.Signature:
		r.markType(t.Params())
		r.markType(t.Results())
	}
}

// pruned returns p with only its reachable declarations.
func (r *reacher) pruned(p *Package) *Package {
	out := *p
	info := *p.Info
	info.InitOrder = nil
	for _, in := range p.Info.InitOrder {
		for _, v := range in.Lhs {
			if r.seen[v] {
				info.InitOrder = append(info.InitOrder, in)
				break
			}
		}
	}
	out.Info = &info
	out.Files = nil
	for _, f := range p.Files {
		nf := *f
		nf.Decls = nil
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if r.seen[p.Info.Defs[d.Name]] || d.Recv == nil && d.Name.Name == "init" {
					nf.Decls = append(nf.Decls, d)
				}
			case *ast.GenDecl:
				g := *d
				g.Specs = nil
				for _, s := range d.Specs {
					if d.Tok == token.IMPORT || r.specReached(p.Info, s) {
						g.Specs = append(g.Specs, s)
					}
				}
				if len(g.Specs) > 0 {
					nf.Decls = append(nf.Decls, &g)
				}
			}
		}
		out.Files = append(out.Files, &nf)
	}
	return &out
}

// specReached reports whether a value or type specification declares a
// reachable object or is a root.
func (r *reacher) specReached(info *types.Info, s ast.Spec) bool {
	switch s := s.(type) {
	case *ast.ValueSpec:
		for _, v := range s.Values {
			if !pure(info, v) {
				return true
			}
		}
		for _, id := range s.Names {
			if r.seen[info.Defs[id]] {
				return true
			}
		}
	case *ast.TypeSpec:
		return r.seen[info.Defs[s.Name]]
	}
	return false
}

func isSliceType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Slice)
	return ok
}
