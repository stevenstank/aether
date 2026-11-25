package frontend

import (
	"embed"
	"errors"
	"fmt"
	"go/ast"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// overlayFS holds Aether's overlay files, _overlay/IMPORTPATH/NAME.go: each
// replaces the file NAME.go of the standard-library package IMPORTPATH, or
// is added to it (D-049).
//
//go:embed all:_overlay
var overlayFS embed.FS

// A patch is how Aether changes a standard-library package besides its
// overlay files.
type patch struct {
	replace bool              // the package is only its overlay files
	exclude []string          // GOROOT files left out
	use     map[string]string // GOROOT files replaced by other GOROOT files, built whatever their constraints
	// bodies gives GOROOT functions declared without a body, which gc's
	// runtime implements (//go:linkname), the body "return target(its
	// parameters)", target an overlay function of the package (D-054).
	bodies map[string]string
}

// patches are the standard-library packages Aether changes (D-049). Each
// "use" file is gc's own portable version of a file written for gc: its
// _noasm or compiler_bootstrap variant.
var patches = map[string]patch{
	"math": {
		// useFMA, for Exp, is in Aether's exp_asm.go, which, with
		// log_asm.go, is gc's amd64 assembly of Exp and Log in Go (D-053).
		exclude: []string{"exp_amd64.go"},
		use: map[string]string{
			// Floor, Ceil, Trunc, Max, Min, and Hypot compute the same
			// results in Go as gc's assembly.
			"floor_asm.go": "floor_noasm.go", "dim_asm.go": "dim_noasm.go", "hypot_asm.go": "hypot_noasm.go",
		},
	},
	"math/bits": {use: map[string]string{"bits_errors.go": "bits_errors_bootstrap.go"}},
	// The table-driven CRC of gc's other architectures, in place of its
	// SSE4.2 and carry-less multiplication assembly: a CRC is defined
	// exactly, so the results are the same (Batch 1).
	"hash/crc32": {use: map[string]string{"crc32_amd64.go": "crc32_otherarch.go"}},
	"strconv":    {use: map[string]string{"bytealg.go": "bytealg_bootstrap.go"}},
	// gc's portable files, and Aether's for its assembly and runtime (11.7).
	"internal/bytealg": {exclude: []string{"equal_native.go", "index_native.go"}, use: map[string]string{
		"compare_native.go": "compare_generic.go", "count_native.go": "count_generic.go", "indexbyte_native.go": "indexbyte_generic.go"}},
	"internal/abi": {replace: true}, // gc's type metadata and calling convention; NoEscape (11.7)
	"reflect":      {replace: true}, // over Aether's type descriptors (11.8)
	// gc's runtime, and packages that depend on its internals (11.6).
	"runtime": {replace: true}, "os": {replace: true}, "sync": {replace: true}, "sync/atomic": {replace: true},
	"iter":                 {replace: true}, // gc's Pull and Pull2 use its runtime's coroutines (11.7)
	"internal/reflectlite": {replace: true}, // ValueOf, Len, and Swapper, for sort (R.6)
	"internal/godebug":     {replace: true}, // $GODEBUG at startup, without gc's runtime hooks (D-053)
	"syscall":              {replace: true}, // what time and os use, over D-051's primitive (D-054)
	// gc's time, its runtime's part in Aether's runtime.go (D-054).
	"time": {bodies: map[string]string{"now": "runtimeClock", "runtimeNow": "runtimeClock", "runtimeNano": "nanotime",
		"runtimeIsBubbled": "isBubbled", "Sleep": "sleep", "newTimer": "newRuntimeTimer", "stopTimer": "stopRuntimeTimer",
		"resetTimer": "resetRuntimeTimer"}},
}

// Intrinsics are the functions of overlay files declared without a body
// whose bodies lowering generates, by package path and name (D-049).
var Intrinsics = map[string]bool{
	"math.Float32bits": true, "math.Float32frombits": true, "math.Float64bits": true, "math.Float64frombits": true,
	"internal/strconv.float32bits": true, "internal/strconv.float32frombits": true,
	"internal/strconv.float64bits": true, "internal/strconv.float64frombits": true,
	"runtime.GC": true, "runtime.Gosched": true, "runtime.NumGoroutine": true,
	"os.argc": true, "os.arg": true, "os.Exit": true, "os.write": true, "os.read": true,
	"os.syscall6": true, "sync.fatal": true,
	"syscall.rawSyscall6": true, "syscall.envc": true, "syscall.envString": true,
	"time.syscall6": true, "time.setChanTimer": true, "runtime.envc": true, "runtime.envString": true,
	"math.cpuFMA": true, "math.envc": true, "math.env": true,
	"internal/godebug.envc": true, "internal/godebug.env": true, "math/rand.runtime_rand": true,
	"internal/reflectlite.implements": true,
}

// GOROOTIntrinsics are the intrinsics declared without a body in GOROOT's
// own files, not Aether's: math/rand's runtime_rand, which gc's linker
// makes runtime.rand (D-053).
var GOROOTIntrinsics = map[string]bool{"math/rand.runtime_rand": true}

// isStd reports whether an import path names a standard-library package:
// its first element has no dot.
func isStd(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// GOROOT returns the Go tree whose source Aether compiles the standard
// library from: $GOROOT, or else the GOROOT of the toolchain that built
// Aether. Its VERSION must be go1.26 (D-049).
func GOROOT() (string, error) {
	root := os.Getenv("GOROOT")
	if root == "" {
		root = runtime.GOROOT()
	}
	if root == "" {
		return "", errors.New("no GOROOT: set GOROOT to a go1.26 tree")
	}
	data, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return "", fmt.Errorf("GOROOT %s has no VERSION file: %v", root, err)
	}
	v, _, _ := strings.Cut(string(data), "\n")
	if v != "go1.26" && !strings.HasPrefix(v, "go1.26.") && !strings.HasPrefix(v, "go1.26rc") {
		return "", fmt.Errorf("GOROOT %s is %s; Aether needs go1.26", root, v)
	}
	return root, nil
}

// stdFiles returns the files Aether compiles of the standard-library
// package p: those of $GOROOT/src/p the go command builds for linux/amd64,
// patched, with the overlay files. GOROOT files are named
// $GOROOT/src/..., overlay files $AETHER/... (D-049).
func stdFiles(root, p string) ([]Source, error) {
	pt := patches[p]
	var srcs []Source
	if !pt.replace {
		files, err := packageFiles(filepath.Join(root, "src", filepath.FromSlash(p)), "$GOROOT/src/"+p)
		if err != nil {
			return nil, err
		}
		for _, s := range files {
			if name := path.Base(s.Name); !slices.Contains(pt.exclude, name) && pt.use[name] == "" && !hasOverlay(p, name) {
				srcs = append(srcs, s)
			}
		}
		for _, other := range slices.Sorted(maps.Values(pt.use)) {
			data, err := os.ReadFile(filepath.Join(root, "src", filepath.FromSlash(p), other))
			if err != nil {
				return nil, err
			}
			srcs = append(srcs, Source{Name: "$GOROOT/src/" + p + "/" + other, Data: data})
		}
	}
	entries, _ := fs.ReadDir(overlayFS, "_overlay/"+p)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := overlayFS.ReadFile("_overlay/" + p + "/" + e.Name())
		if err != nil {
			return nil, err
		}
		srcs = append(srcs, Source{Name: "$AETHER/" + p + "/" + e.Name(), Data: data})
	}
	if len(srcs) == 0 {
		return nil, errors.New("no Go files for linux/amd64")
	}
	return srcs, nil
}

func hasOverlay(p, name string) bool {
	_, err := fs.Stat(overlayFS, "_overlay/"+p+"/"+name)
	return err == nil
}

// giveBodies gives each function of the file f of the standard-library
// package p that its patch's bodies lists, declared without a body, the
// body calling its target with its parameters, returning its results: as
// written in the source, at the declaration's name (D-054).
func giveBodies(p string, f *ast.File) {
	bodies := patches[p].bodies
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body != nil || fd.Recv != nil || bodies[fd.Name.Name] == "" {
			continue
		}
		pos := fd.Name.Pos()
		call := &ast.CallExpr{Fun: &ast.Ident{NamePos: pos, Name: bodies[fd.Name.Name]}, Lparen: pos, Rparen: pos}
		for i, field := range fd.Type.Params.List {
			if len(field.Names) == 0 {
				field.Names = []*ast.Ident{{NamePos: pos, Name: fmt.Sprintf("p%d", i)}}
			}
			for _, n := range field.Names {
				call.Args = append(call.Args, &ast.Ident{NamePos: pos, Name: n.Name})
			}
		}
		var s ast.Stmt = &ast.ExprStmt{X: call}
		if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
			s = &ast.ReturnStmt{Return: pos, Results: []ast.Expr{call}}
		}
		fd.Body = &ast.BlockStmt{Lbrace: pos, List: []ast.Stmt{s}, Rbrace: pos}
	}
}
