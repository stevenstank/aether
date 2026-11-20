package frontend

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
)

// Features of the loader's Unsupported diagnostics (D-049): an import
// Aether cannot resolve, and a standard-library package that does not
// type-check as Aether selects its files.
const (
	FeatImport = "import"
	FeatStdlib = "stdlib-package"
)

// A Source is a file named on the command line.
type Source struct {
	Name string
	Data []byte
}

// A Program is the packages of a program, in the order they are
// initialized: main last (D-049).
type Program struct {
	Fset     *token.FileSet
	Packages []*Package
}

// Main returns the package main.
func (p *Program) Main() *Package { return p.Packages[len(p.Packages)-1] }

// Stages are the steps of D-003 that the loader runs on each package.
type Stages struct {
	// Precheck checks a file that parsed, of main, of the standard
	// library, or neither, before type checking.
	Precheck func(fset *token.FileSet, f *ast.File, main, std bool) []diag.Diagnostic
	// Prepare transforms a package after type checking, before the
	// packages that import it are type-checked.
	Prepare func(*Package) (*Package, []diag.Diagnostic)
	// Reject returns the Unsupported diagnostic of an import.
	Reject func(pos diag.Pos, feature, what string) diag.Diagnostic
}

// Load parses and type-checks the program whose package main is srcs, with
// the packages of its main module that it imports (D-049). It stops at the
// first package with diagnostics. The packages are loaded depth first, each
// in the order D-003 gives its steps, with stages. The packages besides
// main are then pruned to what main reaches (prune). The error is a
// problem with GOROOT, which only a program with a standard-library import
// reads.
func Load(fset *token.FileSet, srcs []Source, stages Stages) (*Program, []diag.Diagnostic, error) {
	ld := &loader{fset: fset, stages: stages, pkgs: map[string]*Package{}, loading: map[string]bool{}}
	ld.findModule(filepath.Dir(srcs[0].Name))
	main, ds := ld.load("main", srcs)
	if ld.err != nil {
		return nil, nil, ld.err
	}
	if len(ds) > 0 {
		return nil, ds, nil
	}
	prog := &Program{Fset: fset, Packages: append(initOrder(ld.order), main)}
	prune(prog)
	return prog, nil, nil
}

// initOrder returns the packages of order, sorted by import path, in the
// order of spec §Package initialization: repeatedly the first package
// whose imports are all initialized.
func initOrder(order []*Package) []*Package {
	var out []*Package
	done := map[*types.Package]bool{}
	for len(out) < len(order) {
		for _, p := range order {
			// unsafe is go/types' own, with nothing to initialize.
			if !done[p.Types] && !slices.ContainsFunc(p.Types.Imports(), func(q *types.Package) bool { return !done[q] && q != types.Unsafe }) {
				done[p.Types] = true
				out = append(out, p)
				break
			}
		}
	}
	return out
}

type loader struct {
	fset    *token.FileSet
	stages  Stages
	modPath string // the main module's path, "" without one
	modDir  string // its directory, relative to the main package's as named
	pkgs    map[string]*Package
	loading map[string]bool // the packages being loaded: an import of one is a cycle
	order   []*Package      // the packages besides main, sorted by path
	root    string          // GOROOT, once a standard-library package is imported
	err     error           // a problem with GOROOT
}

// findModule finds the main module: the nearest go.mod at or above dir,
// read for its module line only.
func (ld *loader) findModule(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	for up := "."; ; up = filepath.Join(up, "..") {
		data, err := os.ReadFile(filepath.Join(abs, "go.mod"))
		if err == nil {
			ld.modPath, ld.modDir = modulePath(data), filepath.Join(dir, up)
			return
		}
		if filepath.Dir(abs) == abs {
			return
		}
		abs = filepath.Dir(abs)
	}
}

// modulePath returns the path of a go.mod file's module line, or "".
func modulePath(data []byte) string {
	for line := range strings.Lines(string(data)) {
		line, _, _ = strings.Cut(line, "//")
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "module" {
			if p, err := strconv.Unquote(f[1]); err == nil {
				return p
			}
			return f[1]
		}
	}
	return ""
}

// Import returns a loaded package, or unsafe; it is the importer of
// go/types.
func (ld *loader) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if p := ld.pkgs[path]; p != nil {
		return p.Types, nil
	}
	return nil, errors.New("package not loaded: " + path)
}

// load loads the package path from srcs: parses its files, prechecks them,
// loads its imports, type-checks it, and prepares it.
func (ld *loader) load(path string, srcs []Source) (*Package, []diag.Diagnostic) {
	var files []*ast.File
	var ds []diag.Diagnostic
	for _, s := range srcs {
		f, errs := Parse(ld.fset, s.Name, s.Data)
		ds = append(ds, errs...)
		if f != nil {
			files = append(files, f)
		}
	}
	if len(ds) > 0 {
		return nil, ds
	}
	std := path != "main" && isStd(path)
	for _, f := range files {
		ds = append(ds, ld.stages.Precheck(ld.fset, f, path == "main", std)...)
	}
	if len(ds) > 0 {
		return nil, ds
	}
	ld.loading[path] = true
	for _, f := range files {
		for _, imp := range f.Imports {
			if ds := ld.importSpec(path, imp); len(ds) > 0 || ld.err != nil {
				return nil, ds
			}
		}
	}
	delete(ld.loading, path)
	var imp types.Importer
	for _, f := range files {
		if len(f.Imports) > 0 {
			imp = ld
		}
	}
	pkg, ds := CheckPackage(ld.fset, path, files, imp)
	if len(ds) > 0 && std {
		// Files written for gc or gccgo, not Aether's subset.
		return nil, []diag.Diagnostic{ld.stages.Reject(ds[0].Pos, FeatStdlib,
			"package "+path+", which does not type-check as Aether builds it ("+ds[0].Message+"),")}
	}
	if len(ds) > 0 {
		if d, ok := ld.missing(pkg); ok {
			// Valid Go, but beyond what Aether's replacement declares.
			return nil, []diag.Diagnostic{d}
		}
	}
	if len(ds) == 0 && ld.stages.Prepare != nil {
		pkg, ds = ld.stages.Prepare(pkg)
	}
	if len(ds) > 0 {
		return nil, ds
	}
	unqualify(pkg)
	return pkg, nil
}

// missing finds a selector of pkg naming what a package Aether replaces
// (D-049) does not declare: an exported package-level name, or an
// exported field or method of one of its types. gc's package may have it,
// so the program is not invalid.
func (ld *loader) missing(pkg *Package) (diag.Diagnostic, bool) {
	var d diag.Diagnostic
	found := false
	for _, f := range pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || found || !token.IsExported(sel.Sel.Name) {
				return !found // gc's unexported names are not accessible either
			}
			var p *types.Package
			var what string
			if id, ok := sel.X.(*ast.Ident); ok {
				if pn, ok := pkg.Info.Uses[id].(*types.PkgName); ok {
					if pn.Imported().Scope().Lookup(sel.Sel.Name) != nil {
						return true
					}
					p, what = pn.Imported(), pn.Imported().Path()+"."+sel.Sel.Name
				}
			}
			if t := pkg.Info.TypeOf(sel.X); p == nil && t != nil && pkg.Info.Selections[sel] == nil {
				if ptr, ok := t.(*types.Pointer); ok {
					t = ptr.Elem()
				}
				if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil {
					p, what = n.Obj().Pkg(), n.Obj().Pkg().Path()+"."+n.Obj().Name()+"."+sel.Sel.Name
				}
			}
			if p != nil && patches[p.Path()].replace {
				d, found = ld.stages.Reject(Position(ld.fset, sel.Sel.Pos()), FeatStdlib, what+", which Aether's package "+p.Path()+" does not have,"), true
			}
			return !found
		})
	}
	return d, found
}

// importSpec loads the package that imp, of the package from, imports,
// unless it is loaded: a package of the main module or of the standard
// library. Other imports are unsupported.
func (ld *loader) importSpec(from string, imp *ast.ImportSpec) []diag.Diagnostic {
	path, err := strconv.Unquote(imp.Path.Value)
	if err != nil || path == "C" || ld.pkgs[path] != nil {
		return nil // go/types reports a malformed path; Precheck, cgo
	}
	at := Position(ld.fset, imp.Pos())
	fromStd := from != "main" && isStd(from)
	if ld.loading[path] {
		return []diag.Diagnostic{{Class: diag.Invalid, Pos: at, Message: "import cycle not allowed"}}
	}
	rel, inModule := strings.CutPrefix(path, ld.modPath)
	inModule = inModule && ld.modPath != "" && (rel == "" || rel[0] == '/')
	var srcs []Source
	switch {
	case path == "unsafe" && fromStd:
		return nil // go/types' own package; the gate rejects what it does not support
	case !inModule && isStd(path) && path != "unsafe":
		if !fromStd && (strings.HasPrefix(path, "internal/") || strings.Contains(path, "/internal/") || strings.HasSuffix(path, "/internal")) {
			return []diag.Diagnostic{{Class: diag.Invalid, Pos: at, Message: "use of internal package " + path + " not allowed"}}
		}
		if ld.root == "" {
			if ld.root, ld.err = GOROOT(); ld.err != nil {
				return nil
			}
		}
		if srcs, err = stdFiles(ld.root, path); err != nil {
			return []diag.Diagnostic{{Class: diag.Invalid, Pos: at, Message: fmt.Sprintf("package %s is not in std: %v", path, err)}}
		}
		return ld.loaded(path, srcs)
	case !inModule:
		return []diag.Diagnostic{ld.stages.Reject(at, FeatImport, "import "+imp.Path.Value)}
	}
	dir := filepath.Join(ld.modDir, filepath.FromSlash(rel))
	srcs, err = packageFiles(dir, dir)
	if err != nil {
		return []diag.Diagnostic{{Class: diag.Invalid, Pos: at, Message: fmt.Sprintf("package %s: %v", path, err)}}
	}
	name := ""
	for _, s := range srcs {
		f, err := parser.ParseFile(token.NewFileSet(), "", s.Data, parser.PackageClauseOnly)
		switch {
		case err != nil:
			// Load reports it.
		case name != "" && f.Name.Name != name:
			return []diag.Diagnostic{{Class: diag.Invalid, Pos: at, Message: fmt.Sprintf("found packages %s and %s in %s", name, f.Name.Name, dir)}}
		default:
			name = f.Name.Name
		}
	}
	if name == "main" {
		return []diag.Diagnostic{ld.stages.Reject(at, FeatImport, "import "+imp.Path.Value+" of a program")}
	}
	return ld.loaded(path, srcs)
}

// loaded loads the package path from srcs and records it.
func (ld *loader) loaded(path string, srcs []Source) []diag.Diagnostic {
	pkg, ds := ld.load(path, srcs)
	if len(ds) > 0 || ld.err != nil {
		return ds
	}
	ld.pkgs[path] = pkg
	i, _ := slices.BinarySearchFunc(ld.order, path, func(p *Package, path string) int { return strings.Compare(p.Types.Path(), path) })
	ld.order = slices.Insert(ld.order, i, pkg)
	return nil
}

// packageFiles reads the files of the package in dir that the go command
// builds for linux/amd64 without cgo, sorted by name, and names them in
// the directory display (D-049).
func packageFiles(dir, display string) ([]Source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.New("no such package directory")
	}
	var srcs []Source
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") || !goodOSArch(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if buildable(data) {
			srcs = append(srcs, Source{Name: display + string(filepath.Separator) + name, Data: data})
		}
	}
	if len(srcs) == 0 {
		return nil, errors.New("no Go files for linux/amd64")
	}
	return srcs, nil
}

// knownOS and knownArch are go1.26's GOOS and GOARCH values, which a file
// name may end with (go/build's goodOSArchFile).
var knownOS = strings.Fields("aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos")
var knownArch = strings.Fields("386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm")

// goodOSArch reports whether a file name's _GOOS, _GOARCH, or
// _GOOS_GOARCH suffix, if it has one, matches linux/amd64.
func goodOSArch(name string) bool {
	parts := strings.Split(strings.TrimSuffix(name, ".go"), "_")
	n := len(parts)
	if n >= 3 && slices.Contains(knownOS, parts[n-2]) && slices.Contains(knownArch, parts[n-1]) {
		return parts[n-2] == "linux" && parts[n-1] == "amd64"
	}
	if n >= 2 && slices.Contains(knownOS, parts[n-1]) {
		return parts[n-1] == "linux"
	}
	if n >= 2 && slices.Contains(knownArch, parts[n-1]) {
		return parts[n-1] == "amd64"
	}
	return true
}

// buildable evaluates the build constraints of a file's header, the
// comments before its package clause: the //go:build line, or the
// // +build lines if it has none. The tags are linux, unix, amd64, purego,
// and go1.1 to go1.26; gc, gccgo, and cgo are not set (D-049).
func buildable(src []byte) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return true // the parse error is reported
	}
	var goBuild constraint.Expr
	var plus []constraint.Expr
	for _, g := range f.Comments {
		if g.Pos() >= f.Package || g == f.Doc {
			continue
		}
		for _, c := range g.List {
			x, err := constraint.Parse(c.Text)
			switch {
			case err != nil:
			case constraint.IsGoBuild(c.Text):
				goBuild = x
			default:
				plus = append(plus, x)
			}
		}
	}
	if goBuild != nil {
		plus = []constraint.Expr{goBuild}
	}
	for _, x := range plus {
		if !x.Eval(buildTag) {
			return false
		}
	}
	return true
}

func buildTag(tag string) bool {
	switch tag {
	case "linux", "unix", "amd64", "purego":
		return true
	}
	v, ok := strings.CutPrefix(tag, "go1.")
	n, err := strconv.Atoi(v)
	return ok && err == nil && strconv.Itoa(n) == v && n >= 1 && n <= 26
}

// unqualify replaces each qualified identifier p.X of pkg's files by its
// identifier X, recording its type and value there, so that the later
// stages treat a name declared in another package as one declared in this
// one. The object of X, in Info.Uses, is still the other package's.
func unqualify(pkg *Package) {
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() && v.Elem().Kind() == reflect.Struct {
				walk(v.Elem())
			}
		case reflect.Interface:
			if v.IsNil() {
				return
			}
			if se, ok := v.Interface().(*ast.SelectorExpr); ok && v.CanSet() {
				if id, ok := se.X.(*ast.Ident); ok {
					if _, ok := pkg.Info.Uses[id].(*types.PkgName); ok {
						if tv, ok := pkg.Info.Types[se]; ok {
							pkg.Info.Types[se.Sel] = tv
						}
						v.Set(reflect.ValueOf(se.Sel))
						return
					}
				}
			}
			walk(v.Elem())
		case reflect.Struct:
			for i := range v.NumField() {
				if f := v.Field(i); f.CanSet() && v.Type().Field(i).Type != reflect.TypeFor[*ast.Object]() {
					walk(f)
				}
			}
		case reflect.Slice:
			for i := range v.Len() {
				walk(v.Index(i))
			}
		}
	}
	for _, f := range pkg.Files {
		walk(reflect.ValueOf(f).Elem().FieldByName("Decls"))
	}
	// InitOrder holds the initializers themselves, not their places.
	for _, in := range pkg.Info.InitOrder {
		walk(reflect.ValueOf(in).Elem().FieldByName("Rhs"))
	}
}
