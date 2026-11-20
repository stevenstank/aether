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
			"--\na/a.go:package a\nimport \"runtime\"\nfunc F() { runtime.GC() }\n",
			[]string{"main.go"}, diag.Unsupported, `a/a.go:2:8: unsupported: import "runtime" is not supported in subset S1 [import]`},
		{"unreachable in a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { println(a.F()) }\n" +
			"--\na/a.go:package a\nvar v = g()\nfunc g() int { return 2 }\nfunc F() int { return v }\nfunc H() { println(1i) }\nvar w = 1i\n",
			[]string{"main.go"}, 0, ""},
		{"root initializer in a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { a.F() }\n" +
			"--\na/a.go:package a\nvar _ = h()\nfunc h() complex128 { return 1i }\nfunc F() {}\n",
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
		{"generic instance of a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc main() { println(a.Id(1)) }\n" +
			"--\na/a.go:package a\nfunc Id[T any](x T) T { return x }\n",
			[]string{"main.go"}, diag.Unsupported, "main.go:3:25: unsupported: instance func(x int) int of another package's generic code"},
		{"type argument of a package", mod + "--\nmain.go:package main\nimport \"m.test/x/a\"\nfunc id[T any](x T) T { return x }\nfunc main() { println(id(a.T(1)) == 1) }\n" +
			"--\na/a.go:package a\ntype T int\n",
			[]string{"main.go"}, diag.Unsupported, "type argument T of another package"},
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

// TestStdRejects checks how programs importing the standard library that
// Aether does not compile are classified, and that only what main reaches
// of an imported package is checked (D-049).
func TestStdRejects(t *testing.T) {
	tests := []struct {
		name, src string
		class     diag.Class
		want      string
	}{
		{"assembly", "package main\nimport \"math\"\nfunc main() { println(math.Exp(1)) }\n",
			diag.Unsupported, "$GOROOT/src/math/exp_asm.go:11:1: unsupported: function archExp without a body (implemented outside Go)"},
		{"runtime", "package main\nimport \"os\"\nfunc main() { os.Exit(1) }\n",
			diag.Unsupported, `unsupported: import "runtime"`},
		{"internal", "package main\nimport \"internal/strconv\"\nfunc main() { println(strconv.Itoa(1)) }\n",
			diag.Invalid, "p.go:2:8: invalid: use of internal package internal/strconv not allowed"},
		{"not in std", "package main\nimport \"nosuch/pkg\"\nfunc main() { pkg.F() }\n",
			diag.Invalid, "p.go:2:8: invalid: package nosuch/pkg is not in std"},
		{"unsafe", "package main\nimport \"unsafe\"\nfunc main() { println(unsafe.Sizeof(1)) }\n",
			diag.Unsupported, `p.go:2:8: unsupported: import "unsafe"`},
		{"errors.Is", "package main\nimport \"errors\"\nfunc main() { println(errors.Is(nil, nil)) }\n",
			diag.Unsupported, "$AETHER/errors/wrap.go:8:1: unsupported: function Is without a body"},
		// sync/atomic imports unsafe, which has nothing to initialize: the
		// initialization order once never ended.
		{"imports unsafe", "package main\nimport \"sync/atomic\"\nfunc main() { var x atomic.Int32; println(x.Add(1)) }\n",
			diag.Unsupported, "$GOROOT/src/sync/atomic/doc.go:71:1: unsupported: function SwapInt32 without a body"},
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
