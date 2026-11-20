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

// FeatImport is the feature of an import Aether cannot resolve (D-049).
const FeatImport = "import"

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
	// Precheck checks a file that parsed, of main or not, before type
	// checking.
	Precheck func(fset *token.FileSet, f *ast.File, main bool) []diag.Diagnostic
	// Prepare transforms a package after type checking, before the
	// packages that import it are type-checked.
	Prepare func(*Package) (*Package, []diag.Diagnostic)
	// Reject returns the Unsupported diagnostic of an import.
	Reject func(pos diag.Pos, feature, what string) diag.Diagnostic
}

// Load parses and type-checks the program whose package main is srcs, with
// the packages of its main module that it imports (D-049). It stops at the
// first package with diagnostics. The packages are loaded depth first, each
// in the order D-003 gives its steps, with stages.
func Load(fset *token.FileSet, srcs []Source, stages Stages) (*Program, []diag.Diagnostic) {
	ld := &loader{fset: fset, stages: stages, pkgs: map[string]*Package{}, loading: map[string]bool{}}
	ld.findModule(filepath.Dir(srcs[0].Name))
	main, ds := ld.load("main", srcs)
	if len(ds) > 0 {
		return nil, ds
	}
	// Spec §Package initialization: repeatedly the first package, sorted
	// by import path, whose imports are all initialized.
	prog := &Program{Fset: fset}
	done := map[*types.Package]bool{}
	for len(prog.Packages) < len(ld.order) {
		for _, p := range ld.order {
			if !done[p.Types] && !slices.ContainsFunc(p.Types.Imports(), func(q *types.Package) bool { return !done[q] }) {
				done[p.Types] = true
				prog.Packages = append(prog.Packages, p)
				break
			}
		}
	}
	prog.Packages = append(prog.Packages, main)
	return prog, nil
}

type loader struct {
	fset    *token.FileSet
	stages  Stages
	modPath string // the main module's path, "" without one
	modDir  string // its directory, relative to the main package's as named
	pkgs    map[string]*Package
	loading map[string]bool // the packages being loaded: an import of one is a cycle
	order   []*Package      // the packages besides main, sorted by path
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

// Import returns a loaded package; it is the importer of go/types.
func (ld *loader) Import(path string) (*types.Package, error) {
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
	for _, f := range files {
		ds = append(ds, ld.stages.Precheck(ld.fset, f, path == "main")...)
	}
	if len(ds) > 0 {
		return nil, ds
	}
	ld.loading[path] = true
	for _, f := range files {
		for _, imp := range f.Imports {
			if ds := ld.importSpec(imp); len(ds) > 0 {
				return nil, ds
			}
		}
	}
	delete(ld.loading, path)
	var imp types.Importer
	if len(ld.pkgs) > 0 {
		imp = ld
	}
	pkg, ds := CheckPackage(ld.fset, path, files, imp)
	if len(ds) == 0 && ld.stages.Prepare != nil {
		pkg, ds = ld.stages.Prepare(pkg)
	}
	if len(ds) > 0 {
		return nil, ds
	}
	unqualify(pkg)
	return pkg, nil
}

// importSpec loads the package imp imports, unless it is loaded: a package
// of the main module. Other imports are unsupported (until increment 11.5,
// the standard library).
func (ld *loader) importSpec(imp *ast.ImportSpec) []diag.Diagnostic {
	path, err := strconv.Unquote(imp.Path.Value)
	if err != nil || path == "C" || ld.pkgs[path] != nil {
		return nil // go/types reports a malformed path; Precheck, cgo
	}
	at := Position(ld.fset, imp.Pos())
	rel, ok := strings.CutPrefix(path, ld.modPath)
	if ld.modPath == "" || !ok || rel != "" && rel[0] != '/' {
		return []diag.Diagnostic{ld.stages.Reject(at, FeatImport, "import "+imp.Path.Value)}
	}
	if ld.loading[path] {
		return []diag.Diagnostic{{Class: diag.Invalid, Pos: at, Message: "import cycle not allowed"}}
	}
	dir := filepath.Join(ld.modDir, filepath.FromSlash(rel))
	srcs, err := packageFiles(dir)
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
	pkg, ds := ld.load(path, srcs)
	if len(ds) > 0 {
		return ds
	}
	ld.pkgs[path] = pkg
	i, _ := slices.BinarySearchFunc(ld.order, path, func(p *Package, path string) int { return strings.Compare(p.Types.Path(), path) })
	ld.order = slices.Insert(ld.order, i, pkg)
	return nil
}

// packageFiles reads the files of the package in dir that the go command
// builds for linux/amd64 without cgo, sorted by name (D-049).
func packageFiles(dir string) ([]Source, error) {
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
			srcs = append(srcs, Source{Name: filepath.Join(dir, name), Data: data})
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
}
