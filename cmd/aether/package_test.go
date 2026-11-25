package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/diag"
)

// TestPackagePrograms builds each program of the module testdata/packages,
// made of several files and packages (D-049, increment 11.4), with gc and
// with Aether, with and without -O0, and compares what they do.
func TestPackagePrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	for _, tool := range []string{"as", "ld"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	mod := filepath.Join("testdata", "packages")
	bin := t.TempDir() + string(filepath.Separator)
	cmd := exec.Command(gobin, "build", "-o", bin, "./cmd/...")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=", "GOAMD64=v1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gc build failed: %v\n%s", err, out)
	}
	progs, err := filepath.Glob(filepath.Join(mod, "cmd", "*"))
	if err != nil || len(progs) == 0 {
		t.Fatal(progs, err)
	}
	for _, dir := range progs {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			files, err := filepath.Glob(filepath.Join(dir, "*.go"))
			if err != nil {
				t.Fatal(err)
			}
			want := runExe(t, filepath.Join(bin, name))
			for _, o0 := range []bool{false, true} {
				out := filepath.Join(t.TempDir(), name)
				args := []string{"build", "-o", out}
				if o0 {
					args = append(args, "-O0")
				}
				if stderr, code := runAether(append(args, files...)...); code != 0 {
					t.Fatalf("aether %v: exit %d\n%s", args, code, stderr)
				}
				if got := runExe(t, out); got != want {
					t.Errorf("-O0 %v: aether %+v, gc %+v", o0, got, want)
				}
			}
		})
	}
}

// TestPackageRejects checks how programs of several files and packages that
// Aether does not compile are classified: the first diagnostic's class,
// feature, and message.
func TestPackageRejects(t *testing.T) {
	const mod = "go.mod:module m.test/x\n"
	tests := []struct {
		name  string
		files string // "name:content" lines, files separated by "--\n"
		main  []string
		class diag.Class
		want  string
	}{
		{"cycle", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package a\nimport \"m.test/x/b\"\nfunc F() { b.G() }\n" +
			"--\nb/b.go:package b\nimport \"m.test/x/a\"\nfunc G() { a.F() }\n",
			[]string{"main.go"}, diag.Invalid, "b/b.go:2:8: invalid: import cycle not allowed"},
		{"standard library", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package a\nimport \"runtime\"\nfunc F() { runtime.Goexit() }\n",
			[]string{"main.go"}, diag.Unsupported, `a/a.go:3:20: unsupported: runtime.Goexit, which Aether's package runtime does not have,`},
		{"unreachable in a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { println(a.F()) }\n" +
			"--\na/a.go:package a\nvar v = g()\nfunc g() int { return 2 }\nfunc F() int { return v }\nfunc H() { println(1i) }\nvar w = 1i\n",
			[]string{"main.go"}, 0, ""},
		{"root initializer in a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package a\nvar _ = h()\nfunc h() struct{ error } { return struct{ error }{} }\nfunc F() {}\n",
			[]string{"main.go"}, diag.Unsupported, "a/a.go:2:9: unsupported"},
		{"no module", "main.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n",
			[]string{"main.go"}, diag.Unsupported, `main.go:2:8: unsupported: import "m.test/x/a"`},
		{"outside the module", mod + "--\nmain.go:package main\nimport \"m.test/xy\"\nfunc main() { xy.F() }\n",
			[]string{"main.go"}, diag.Unsupported, `main.go:2:8: unsupported: import "m.test/xy"`},
		{"missing package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n",
			[]string{"main.go"}, diag.Invalid, "main.go:2:8: invalid: package m.test/x/a: no such package directory"},
		{"excluded files", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a_windows.go:package a\nfunc F() {}\n--\na/b.go://go:build gc\n\npackage a\nfunc F() {}\n",
			[]string{"main.go"}, diag.Invalid, "package m.test/x/a: no Go files for linux/amd64"},
		{"two package names", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package a\nfunc F() {}\n--\na/b.go:package b\n",
			[]string{"main.go"}, diag.Invalid, "found packages a and b in"},
		{"import of a program", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package main\nfunc F() {}\nfunc main() {}\n",
			[]string{"main.go"}, diag.Unsupported, `import "m.test/x/a" of a program`},
		{"type error in a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package a\nfunc F() { var s string = 1; _ = s }\n",
			[]string{"main.go"}, diag.Invalid, "a/a.go:2:27: invalid: cannot use 1"},
		{"unsupported in a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package a\nfunc F() { println(1i) }\n",
			[]string{"main.go"}, diag.Unsupported, "a/a.go:2:20: unsupported"},
		// Another package's generic code is instantiated by the last, in
		// initialization order, of its package and those of the type
		// arguments, which must name them all (11.7, D-052).
		{"type argument of main", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\ntype T int\nfunc main() { println(a.Id(T(1)) == 1) }\n" +
			"--\na/a.go:package a\nfunc Id[T any](x T) T { return x }\n",
			[]string{"main.go"}, 0, ""},
		{"local type argument", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { type T int; println(a.Id(T(1)) == 1) }\n" +
			"--\na/a.go:package a\nfunc Id[T any](x T) T { return x }\n",
			[]string{"main.go"}, diag.Unsupported, "main.go:3:35: unsupported: type argument main.T declared in a function is not supported in subset S1 [generic-instance]"},
		{"unexported type argument", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nimport \"m.test/x/b\"\nfunc main() { println(b.F() == 1, a.Id(1) == 1) }\n" +
			"--\na/a.go:package a\nfunc Id[T any](x T) T { return x }\n" +
			"--\nb/b.go:package b\nimport \"m.test/x/a\"\ntype t int\nfunc F() t { return a.Id(t(1)) }\n",
			[]string{"main.go"}, 0, ""},
		{"unexported type arguments of two packages", mod + "--\nmain.go:package main\nimport (\"m.test/x/a\"; \"m.test/x/b\"; \"m.test/x/c\")\nfunc main() { _ = a.Two(b.F(), c.G()) }\n" +
			"--\na/a.go:package a\nfunc Two[A, B any](x A, y B) int { return 2 }\n" +
			"--\nb/b.go:package b\ntype t int\nfunc F() t { return 1 }\n" +
			"--\nc/c.go:package c\ntype u int\nfunc G() u { return 1 }\n",
			[]string{"main.go"}, diag.Unsupported, "main.go:3:19: unsupported: type argument m.test/x/b.t, which package m.test/x/c cannot name, is not supported in subset S1 [generic-instance]"},
		{"hosted embedding of an unexported type", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\ntype T int\nfunc main() { var b a.Box[T]; println(b.V) }\n" +
			"--\na/a.go:package a\ntype inner struct{}\ntype Box[T any] struct {\n\tinner\n\tV T\n}\n",
			[]string{"main.go"}, diag.Unsupported, "a/a.go:4:2: unsupported: embedded unexported type inner of another package in a hosted instance is not supported in subset S1 [generic-instance]"},
		{"hosted use of an unexported field", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\ntype T int\nfunc main() { println(a.Get(T(1))) }\n" +
			"--\na/a.go:package a\ntype cfg struct{ n int }\nvar c = cfg{2}\nfunc Get[T any](x T) int { return c.n }\n",
			[]string{"main.go"}, diag.Unsupported, "a/a.go:4:37: unsupported: generic code whose instance does not type-check (c.n undefined"},
		{"type argument of an imported package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc id[T any](x T) T { return x }\nfunc main() { println(id(a.T(1)) == 1) }\n" +
			"--\na/a.go:package a\ntype T int\n",
			[]string{"main.go"}, 0, ""},
		{"files in two directories", "main.go:package main\nfunc main() { f() }\n--\nd/f.go:package main\nfunc f() {}\n",
			[]string{"main.go", "d/f.go"}, diag.Unsupported, "source files in more than one directory"},
		{"main in another file", "main.go:package main\nfunc f() {}\n--\nm.go:package main\nfunc main() { f() }\n",
			[]string{"main.go", "m.go"}, 0, ""},
		{"package name", "main.go:package main\nfunc main() {}\n--\np.go:package p\n",
			[]string{"main.go", "p.go"}, diag.Unsupported, "p.go:1:9: unsupported: package p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range strings.Split(tt.files, "--\n") {
				name, data, _ := strings.Cut(f, ":")
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var srcs []source
			for _, m := range tt.main {
				data, err := os.ReadFile(filepath.Join(dir, m))
				if err != nil {
					t.Fatal(err)
				}
				srcs = append(srcs, source{name: filepath.Join(dir, m), data: data})
			}
			ds := check(srcs)
			if tt.want == "" {
				if len(ds) != 0 {
					t.Fatalf("rejected: %v", ds)
				}
				return
			}
			diag.Sort(ds)
			if len(ds) == 0 || ds[0].Class != tt.class || !strings.Contains(ds[0].String(), tt.want) {
				t.Fatalf("diagnostics %v, want first %v containing %q", ds, tt.class, tt.want)
			}
		})
	}
}

// TestStdPrograms builds each program of testdata/std, which import the
// standard-library packages Aether compiles from GOROOT source (D-049,
// increment 11.5), with gc and with Aether, with and without -O0, and
// compares what they do.
func TestStdPrograms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	for _, tool := range []string{"as", "ld"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	files, err := filepath.Glob(filepath.Join("testdata", "std", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatal(files, err)
	}
	progs := map[string][]byte{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(f), ".go")] = src
	}
	want := gcRun(t, gobin, progs)
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".go")
		t.Run(name, func(t *testing.T) {
			// A fatal error is compared by the panic rule.
			if q, ok, err := panicPrefix(want[name]); err != nil {
				t.Fatalf("gc output does not fit the panic rule: %v\n%+v", err, want[name])
			} else if ok {
				want[name] = result{stderr: q, exit: 2}
			}
			got, ds, err := interpret(f, progs[name])
			if len(ds) > 0 || err != nil {
				t.Fatalf("not interpreted: %v %v", ds, err)
			}
			if got != want[name] {
				t.Errorf("interpreter %+v, gc %+v", got, want[name])
			}
			for _, o0 := range []bool{false, true} {
				out := filepath.Join(t.TempDir(), name)
				args := []string{"build", "-o", out}
				if o0 {
					args = append(args, "-O0")
				}
				if stderr, code := runAether(append(args, f)...); code != 0 {
					t.Fatalf("aether %v: exit %d\n%s", args, code, stderr)
				}
				if got := runExe(t, out); got != want[name] {
					t.Errorf("-O0 %v: aether %+v, gc %+v", o0, got, want[name])
				}
			}
		})
	}
}

// TestStdin checks a program reading its standard input with fmt and
// os.Stdin against gc, in the interpreter and natively, with and without
// -O0 (increment 11.8).
func TestStdin(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	for _, tool := range []string{"as", "ld"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	src := []byte(`package main

import (
	"fmt"
	"os"
)

func main() {
	var a, b int
	var s string
	n, err := fmt.Scan(&a, &b)
	fmt.Println(n, err, a+b)
	n, err = fmt.Scanln(&s)
	fmt.Println(n, err, s)
	var f float64
	n, err = fmt.Scanf("%g\n", &f)
	fmt.Println(n, err, f)
	buf := make([]byte, 4)
	m, err := os.Stdin.Read(buf)
	fmt.Println(m, err, string(buf[:m]))
	m, err = os.Stdin.Read(nil)
	fmt.Println(m, err)
	n, err = fmt.Scan(&a)
	fmt.Println(n, err)
	var nf *os.File
	_, err = nf.Read(buf)
	fmt.Println(err)
}
`)
	const input = "3 4\nword\n2.5\nrest"
	bin := gcBuild(t, gobin, map[string][]byte{"stdin": src})
	want := runExeIn(t, filepath.Join(bin, "stdin"), nil, strings.NewReader(input))
	if want.exit != 0 || !strings.Contains(want.stdout, "4 <nil> rest") {
		t.Fatalf("gc: %+v", want)
	}
	f := filepath.Join(t.TempDir(), "stdin.go")
	if err := os.WriteFile(f, src, 0o666); err != nil {
		t.Fatal(err)
	}
	got, ds, err := interpretIn(f, src, strings.NewReader(input))
	if len(ds) > 0 || err != nil || got != want {
		t.Errorf("interpreter %+v %v %v, gc %+v", got, ds, err, want)
	}
	for _, o0 := range []bool{false, true} {
		out := filepath.Join(t.TempDir(), "stdin")
		args := []string{"build", "-o", out}
		if o0 {
			args = append(args, "-O0")
		}
		if stderr, code := runAether(append(args, f)...); code != 0 {
			t.Fatalf("aether %v: exit %d\n%s", args, code, stderr)
		}
		if got := runExeIn(t, out, nil, strings.NewReader(input)); got != want {
			t.Errorf("-O0 %v: aether %+v, gc %+v", o0, got, want)
		}
	}
}

// TestStdRejects checks how programs importing the standard library that
// Aether does not compile are classified, and that only what main reaches
// of an imported package is checked (D-049).
func TestStdRejects(t *testing.T) {
	tests := []struct {
		name, src string
		class     diag.Class
		want      string
	}{
		{"assembly", "package main\nimport \"math/big\"\nfunc main() { println(new(big.Int).Add(big.NewInt(1), big.NewInt(1)).String()) }\n",
			diag.Unsupported, "$GOROOT/src/math/big/arith_decl.go:25:1: unsupported: function addVV without a body (implemented outside Go)"},
		// gc's runtime, os, sync, and sync/atomic have more than Aether's.
		{"runtime name", "package main\nimport \"runtime\"\nfunc main() { runtime.Goexit() }\n",
			diag.Unsupported, "p.go:3:23: unsupported: runtime.Goexit, which Aether's package runtime does not have, is not supported in subset S1 [stdlib-package]"},
		{"os method", "package main\nimport \"os\"\nfunc main() { os.Stdout.Stat() }\n",
			diag.Unsupported, "p.go:3:25: unsupported: os.File.Stat, which Aether's package os does not have,"},
		{"os function", "package main\nimport \"os\"\nfunc main() { os.ReadDir(\".\") }\n",
			diag.Unsupported, "p.go:3:18: unsupported: os.ReadDir, which Aether's package os does not have,"},
		{"sync type", "package main\nimport \"sync\"\nvar c sync.Cond\nfunc main() {}\n",
			diag.Unsupported, "p.go:3:12: unsupported: sync.Cond, which Aether's package sync does not have,"},
		// Declared for type checking, but unsupported if reached (Batch 1).
		{"runtime.Callers", "package main\nimport \"runtime\"\nfunc main() { pc := make([]uintptr, 4); println(runtime.Callers(0, pc)) }\n",
			diag.Unsupported, "$AETHER/runtime/extern.go:55:1: unsupported: function Callers without a body (implemented outside Go)"},
		{"runtime.Caller", "package main\nimport \"runtime\"\nfunc main() { _, file, _, _ := runtime.Caller(0); println(file) }\n",
			diag.Unsupported, "$AETHER/runtime/extern.go:63:1: unsupported: function Caller without a body (implemented outside Go)"},
		{"unexported", "package main\nimport \"sync\"\nfunc main() { var m sync.Mutex; println(m.locked) }\n",
			diag.Invalid, "p.go:3:43: invalid: m.locked undefined"},
		{"still invalid", "package main\nimport \"os\"\nfunc main() { os.Exit(\"1\") }\n",
			diag.Invalid, "p.go:3:23: invalid: cannot use \"1\""},
		{"internal", "package main\nimport \"internal/strconv\"\nfunc main() { println(strconv.Itoa(1)) }\n",
			diag.Invalid, "p.go:2:8: invalid: use of internal package internal/strconv not allowed"},
		{"not in std", "package main\nimport \"nosuch/pkg\"\nfunc main() { pkg.F() }\n",
			diag.Invalid, "p.go:2:8: invalid: package nosuch/pkg is not in std"},
		{"unsafe.Slice of an unsupported type", "package main\nimport \"unsafe\"\nfunc main() { var c struct{ error }; println(len(unsafe.Slice(&c, 1))) }\n",
			diag.Unsupported, "p.go:3:21: unsupported: type struct{error}"},
		{"println of unsafe.Pointer", "package main\nimport \"unsafe\"\nfunc main() { println(unsafe.Pointer(nil)) }\n",
			diag.Unsupported, "p.go:3:30: unsupported: println argument of type unsafe.Pointer"},
		// Another package's generic code, instantiated with main's types,
		// which main hosts (D-052).
		{"errors.AsType", "package main\nimport \"errors\"\ntype E struct{}\nfunc (*E) Error() string { return \"\" }\nfunc main() { _, ok := errors.AsType[*E](errors.New(\"x\")); println(ok) }\n", 0, ""},
		{"type argument of main", "package main\nimport \"slices\"\ntype T int\nfunc main() { slices.Sort([]T{2, 1}) }\n", 0, ""},
		{"sort.Reverse", "package main\nimport \"sort\"\nfunc main() { sort.Sort(sort.Reverse(sort.IntSlice{2, 1})) }\n",
			diag.Unsupported, "$GOROOT/src/sort/sort.go:90:14: unsupported: type struct{Interface}"},
		{"iter.Pull", "package main\nimport \"iter\"\nfunc main() { next, stop := iter.Pull(iter.Seq[int](nil)); _, _ = next, stop }\n",
			diag.Unsupported, "p.go:3:34: unsupported: iter.Pull, which Aether's package iter does not have,"},
		{"range over func in slices", "package main\nimport \"slices\"\nfunc main() { println(len(slices.Collect(slices.Values([]int{1})))) }\n",
			diag.Unsupported, "$GOROOT/src/slices/iter.go:51:2: unsupported: for-range statement"},
		{"unreachable", "package main\nimport \"strconv\"\nfunc main() { println(strconv.Itoa(7)) }\n", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := check([]source{{name: "p.go", data: []byte(tt.src)}})
			if tt.want == "" {
				if len(ds) != 0 {
					t.Fatalf("rejected: %v", ds)
				}
				return
			}
			diag.Sort(ds)
			if len(ds) == 0 || ds[0].Class != tt.class || !strings.Contains(ds[0].String(), tt.want) {
				t.Fatalf("diagnostics %v, want first %v containing %q", ds, tt.class, tt.want)
			}
		})
	}
}

// TestGOROOTVersion checks that a GOROOT of another Go version is a usage
// error for a program importing the standard library, and is not read by
// one that imports nothing (D-049).
func TestGOROOTVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("go1.25.3\ntime x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOROOT", root)
	dir := t.TempDir()
	for name, src := range map[string]string{
		"std.go":  "package main\nimport \"unicode/utf8\"\nfunc main() { println(utf8.RuneLen('x')) }\n",
		"none.go": "package main\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stderr, code := runAether("check", filepath.Join(dir, "std.go"))
	if code != diag.ExitUsage || !strings.Contains(stderr, "is go1.25.3; Aether needs go1.26") {
		t.Errorf("std: exit %d, %q", code, stderr)
	}
	if stderr, code := runAether("check", filepath.Join(dir, "none.go")); code != 0 {
		t.Errorf("no imports: exit %d, %q", code, stderr)
	}
}

// TestStdoutErrors checks writes of os against gc's when the standard
// output is closed, which gc's runtime and Aether's replace by /dev/null,
// and when it is /dev/full, where a write fails with ENOSPC.
func TestStdoutErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	src := []byte("package main\nimport \"os\"\nfunc main() {\n\tn, err := os.Stdout.WriteString(\"lost\")\n" +
		"\tif err != nil {\n\t\tprintln(n, err.Error())\n\t\treturn\n\t}\n\tprintln(n)\n}\n")
	bin := gcBuild(t, gobin, map[string][]byte{"w": src})
	dir := t.TempDir()
	f := filepath.Join(dir, "w.go")
	if err := os.WriteFile(f, src, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "w")
	if stderr, code := runAether("build", "-o", out, f); code != 0 {
		t.Fatalf("aether build: exit %d\n%s", code, stderr)
	}
	for redirect, want := range map[string]string{">&-": "4\n", ">/dev/full": "0 write /dev/stdout: no space left on device\n"} {
		run := func(exe string) string {
			b, err := exec.Command("/bin/sh", "-c", exe+" "+redirect).CombinedOutput()
			if err != nil {
				t.Fatalf("%s %s: %v\n%s", exe, redirect, err, b)
			}
			return string(b)
		}
		if gc := run(filepath.Join(bin, "w")); gc != want {
			t.Fatalf("%s: gc %q, want %q", redirect, gc, want)
		}
		if got := run(out); got != want {
			t.Errorf("%s: aether %q, gc %q", redirect, got, want)
		}
	}
}
