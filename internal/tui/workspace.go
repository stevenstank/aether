package tui

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A Target is what a check or build compiles: the files of package main of
// one directory, or one file.
type Target struct {
	Dir   string   // absolute
	Files []string // absolute, sorted
	whole bool     // Files are all of Dir's files of package main
}

// Name is the target's directory, or its file, relative to root.
func (t Target) Name(root string) string {
	if len(t.Files) == 1 && !t.whole {
		return rel(root, t.Files[0])
	}
	return rel(root, t.Dir) + string(filepath.Separator)
}

// rel returns path relative to root, or path if it is not under root.
func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(r, "..") {
		return path
	}
	return r
}

// isSource reports whether name is a Go file that a program's build
// includes: not a test.
func isSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// mainFiles returns the sorted non-test Go files of package main in dir.
func mainFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !isSource(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if err == nil && f.Name.Name == "main" {
			files = append(files, path)
		}
	}
	return files
}

// dirTarget returns the target of all files of package main in dir.
func dirTarget(dir string) (Target, bool) {
	files := mainFiles(dir)
	return Target{Dir: dir, Files: files, whole: true}, len(files) > 0
}

// fileTarget returns the target of the one file path.
func fileTarget(path string) Target {
	return Target{Dir: filepath.Dir(path), Files: []string{path}}
}

// skipDir reports whether discovery skips the directory name: hidden
// directories, and those whose Go files are not one program.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules"
}

// Bounds on discovery, so that a TUI started in a large tree stays fast.
const (
	maxScanDirs  = 4000
	maxScanDepth = 8
)

// discover returns the directories under root, root first and then in
// lexical order, that contain a program: files of package main.
func discover(root string) []Target {
	var found []Target
	dirs := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if dirs >= maxScanDirs {
			return
		}
		dirs++
		if t, ok := dirTarget(dir); ok {
			found = append(found, t)
		}
		if depth == maxScanDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() && !skipDir(e.Name()) {
				walk(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	walk(root, 0)
	return found
}

// A node is a directory or Go file of the explorer's tree.
type node struct {
	path     string
	name     string
	dir      bool
	depth    int
	open     bool
	loaded   bool
	children []*node
}

// load reads the children of a directory node: directories first, then Go
// files and go.mod, each sorted by name; hidden entries are left out.
func (n *node) load() {
	n.loaded = true
	n.children = nil
	entries, err := os.ReadDir(n.path)
	if err != nil {
		return
	}
	var dirs, files []*node
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		c := &node{path: filepath.Join(n.path, name), name: name, depth: n.depth + 1}
		switch {
		case e.IsDir():
			c.dir = true
			dirs = append(dirs, c)
		case strings.HasSuffix(name, ".go") || name == "go.mod":
			files = append(files, c)
		}
	}
	byName := func(s []*node) { sort.Slice(s, func(i, j int) bool { return s[i].name < s[j].name }) }
	byName(dirs)
	byName(files)
	n.children = append(dirs, files...)
}

// visible returns the nodes shown below n, in order, n excluded.
func (n *node) visible(out []*node) []*node {
	for _, c := range n.children {
		out = append(out, c)
		if c.dir && c.open {
			out = c.visible(out)
		}
	}
	return out
}

// reveal opens the directories from n down to path, loading them as
// needed, and reports whether path was found.
func (n *node) reveal(path string) bool {
	for _, c := range n.children {
		if c.path == path {
			return true
		}
		if c.dir && strings.HasPrefix(path, c.path+string(filepath.Separator)) {
			if !c.loaded {
				c.load()
			}
			c.open = true
			return c.reveal(path)
		}
	}
	return false
}

// A source is a file loaded for viewing.
type source struct {
	path  string
	lines []string
	err   error
	// spans are the highlighted tokens of each line.
	spans [][]span
}

var errTooLarge = errors.New("file too large to view")

// maxViewBytes bounds the size of a file the viewer loads.
const maxViewBytes = 4 << 20

func loadSource(path string) *source {
	s := &source{path: path}
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > maxViewBytes {
		s.err = errTooLarge
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.err = err
		return s
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	s.lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if strings.HasSuffix(path, ".go") {
		s.spans = highlight(s.lines)
	}
	return s
}
