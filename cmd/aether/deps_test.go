package main

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/stevenstank/aether"

// The dependency rules of docs/ARCHITECTURE.md §Candidate package
// boundaries, checked on the real import graph reported by "go list -deps"
// (non-test code only), not by searching source text.
//
//   - Nothing may depend on go/importer or go/build, directly or transitively
//     (they can run the go command; D-002, E9).
//   - os/exec may enter the dependency graph only through internal/toolchain.
//   - Packages listed in noGoPackages must not import any go/* package.
//   - Only internal/toolchain may start processes by other means
//     (os.StartProcess, or the syscall package).
const toolchainPkg = modulePath + "/internal/toolchain"

var forbiddenEverywhere = []string{"go/importer", "go/build"}

var noGoPackages = []string{
	modulePath + "/internal/diag",
	modulePath + "/internal/ir",
	modulePath + "/internal/opt",
	modulePath + "/internal/amd64",
}

// importGraph returns the import graph of the given patterns and all their
// dependencies, as reported by the go command.
func importGraph(t *testing.T, patterns ...string) map[string][]string {
	t.Helper()
	args := append([]string{"list", "-deps", "-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}"}, patterns...)
	cmd := exec.Command("go", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	graph := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) > 0 {
			graph[f[0]] = f[1:]
		}
	}
	return graph
}

// reaches reports whether target is reachable from pkg, not following edges
// into the packages in skip, and returns a path to it.
func reaches(graph map[string][]string, pkg, target string, skip map[string]bool) []string {
	prev := map[string]string{pkg: ""}
	queue := []string{pkg}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p == target {
			var path []string
			for ; p != ""; p = prev[p] {
				path = append([]string{p}, path...)
			}
			return path
		}
		for _, q := range graph[p] {
			if _, seen := prev[q]; seen || skip[q] {
				continue
			}
			prev[q] = p
			queue = append(queue, q)
		}
	}
	return nil
}

func TestDependencyRules(t *testing.T) {
	graph := importGraph(t, modulePath+"/...")
	var own []string
	for p := range graph {
		if p == modulePath || strings.HasPrefix(p, modulePath+"/") {
			own = append(own, p)
		}
	}
	if len(own) == 0 {
		t.Fatal("no Aether packages found")
	}
	skipToolchain := map[string]bool{toolchainPkg: true}
	for _, p := range own {
		for _, bad := range forbiddenEverywhere {
			if path := reaches(graph, p, bad, nil); path != nil {
				t.Errorf("%s depends on %s: %s", p, bad, strings.Join(path, " -> "))
			}
		}
		if p != toolchainPkg {
			if path := reaches(graph, p, "os/exec", skipToolchain); path != nil {
				t.Errorf("%s reaches os/exec outside internal/toolchain: %s", p, strings.Join(path, " -> "))
			}
		}
	}
	for _, p := range noGoPackages {
		for _, imp := range graph[p] {
			if strings.HasPrefix(imp, "go/") {
				t.Errorf("%s imports %s", p, imp)
			}
		}
	}
}

// TestDependencyRulesDetectViolations guards against the rules passing
// vacuously: the same check must find the os/exec dependency that
// go/importer is known to have (E9).
func TestDependencyRulesDetectViolations(t *testing.T) {
	graph := importGraph(t, "go/importer")
	if reaches(graph, "go/importer", "os/exec", nil) == nil {
		t.Fatal("reachability check did not find go/importer -> os/exec")
	}
}

// TestNoProcessStartOutsideToolchain checks the non-test source of every
// Aether package except internal/toolchain for other ways of starting a
// process: importing syscall, or calling os.StartProcess. The files
// allowed syscall are those of syscallAllowed, and each may use only the
// names listed for it.
func TestNoProcessStartOutsideToolchain(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}} {{.Dir}}{{range .GoFiles}} {{.}}{{end}}", modulePath+"/...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	checked := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] == toolchainPkg {
			continue
		}
		for _, name := range f[2:] {
			checkNoProcessStart(t, filepath.Join(f[1], name))
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no source files checked")
	}
}

// syscallAllowed are the files that may import syscall, and the names of
// syscall each may use; none starts a process.
//   - The interpreter's file system calls for a program (D-051): its
//     Syscall6 calls make only the system calls of Aether's os package.
//   - The TUI's terminal (D-061): its Syscall calls make only the ioctls
//     TCGETS, TCSETS, and TIOCGWINSZ.
var syscallAllowed = map[string]map[string]bool{
	"internal/interp/syscall_linux_amd64.go": {
		"Syscall6": true, "Errno": true, "Open": true, "Close": true, "Faccessat": true,
		"O_CLOEXEC": true, "O_DIRECTORY": true, "EBADF": true, "ERANGE": true,
	},
	"internal/tui/term_linux.go": {
		"Syscall": true, "SYS_IOCTL": true, "TCGETS": true, "TCSETS": true, "TIOCGWINSZ": true,
		"Termios": true, "SIGWINCH": true, "SIGTERM": true, "SIGHUP": true, "SIGINT": true,
		"IGNBRK": true, "BRKINT": true, "PARMRK": true, "ISTRIP": true, "INLCR": true,
		"IGNCR": true, "ICRNL": true, "IXON": true, "OPOST": true, "ECHO": true,
		"ECHONL": true, "ICANON": true, "ISIG": true, "IEXTEN": true, "CSIZE": true,
		"PARENB": true, "CS8": true, "VMIN": true, "VTIME": true,
	},
}

// syscallNames returns the names of syscall that path may use, or nil if it
// may not import syscall.
func syscallNames(path string) map[string]bool {
	path = filepath.ToSlash(path)
	for file, names := range syscallAllowed {
		if path == file || strings.HasSuffix(path, "/"+file) {
			return names
		}
	}
	return nil
}

func checkNoProcessStart(t *testing.T, path string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	osName := ""
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		switch p {
		case "syscall":
			allowed := syscallNames(path)
			if allowed == nil {
				t.Errorf("%s imports syscall", path)
				break
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "syscall" && !allowed[sel.Sel.Name] {
						t.Errorf("%s uses syscall.%s", path, sel.Sel.Name)
					}
				}
				return true
			})
		case "os":
			osName = "os"
			if imp.Name != nil {
				osName = imp.Name.Name
			}
		}
	}
	if osName == "" {
		return
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "StartProcess" {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == osName {
				t.Errorf("%s calls os.StartProcess", path)
			}
		}
		return true
	})
}
