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
			"--\na/a.go:package a\nimport \"os\"\nfunc F() { os.Exit(0) }\n",
			[]string{"main.go"}, diag.Unsupported, `a/a.go:2:8: unsupported: import "os" is not supported in subset S1 [import]`},
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
