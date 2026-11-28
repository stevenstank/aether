package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stevenstank/aether/internal/diag"
)

// workspace writes files, path to content, under a new directory.
func workspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, data := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var testFiles = map[string]string{
	"go.mod":                "module example.com/ws\n\ngo 1.26\n",
	"bad/main.go":           "package main\n\nfunc main() {\n\tprintln(y)\n}\n",
	"hello/main.go":         "package main\n\nfunc main() {\n\tprintln(greet())\n}\n",
	"hello/greet.go":        "package main\n\nfunc greet() string { return \"hi\" }\n",
	"hello/greet_test.go":   "package main\n",
	"lib/lib.go":            "package lib\n",
	"testdata/a/main.go":    "package main\n",
	".hidden/main.go":       "package main\n",
	"nested/deep/x/main.go": "package main\n\nfunc main() {}\n",
}

// fakeRun returns a runner that reports every stage of the request's mode,
// logs one line, and returns res.
func fakeRun(res Result, calls *[]Request) Runner {
	return func(r Request, p Progress) Result {
		*calls = append(*calls, r)
		last := StageCheck
		if r.Mode == Build {
			last = StageWrite
		}
		for s := StageRead; s <= last; s++ {
			p.Stage(s, 0, 0)
			if s == StageAssemble {
				p.Stage(s, 1, 2)
			}
		}
		p.Log("tool output")
		return res
	}
}

func newTestModel(t *testing.T, root string, run Runner) *model {
	t.Helper()
	m := newModel(Config{Root: root, Run: run, CommandLine: func(r Request) []string {
		return append([]string{r.Mode.String(), "-o", r.Output}, r.Files...)
	}})
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { clock = clock.Add(10 * time.Millisecond); return clock }
	m.update(resizeMsg{120, 36})
	return m
}

func press(m *model, keys ...any) {
	for _, k := range keys {
		switch k := k.(type) {
		case string:
			for _, r := range k {
				m.update(keyMsg(runeKey(r)))
			}
		case KeyCode:
			m.update(keyMsg(Key{Code: k}))
		case Key:
			m.update(keyMsg(k))
		}
	}
}

// waitDone applies the runner's events until it finishes.
func waitDone(t *testing.T, m *model) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-m.events:
			m.update(ev)
			if _, ok := ev.(doneMsg); ok {
				return
			}
		case <-timeout:
			t.Fatal("the run did not finish")
		}
	}
}

func screenHas(t *testing.T, m *model, wants ...string) {
	t.Helper()
	text := m.view().String()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("screen lacks %q:\n%s", w, text)
		}
	}
}

func TestDiscover(t *testing.T) {
	root := workspace(t, testFiles)
	var got []string
	for _, p := range discover(root) {
		var files []string
		for _, f := range p.Files {
			files = append(files, rel(root, f))
		}
		got = append(got, p.Name(root)+" "+strings.Join(files, ","))
	}
	want := []string{
		"bad/ bad/main.go",
		"hello/ hello/greet.go,hello/main.go", // no test file
		"nested/deep/x/ nested/deep/x/main.go",
	}
	if !slices.Equal(got, want) {
		t.Errorf("discover = %q, want %q", got, want)
	}
}

func TestInitialState(t *testing.T) {
	root := workspace(t, testFiles)
	m := newTestModel(t, root, nil)
	if m.target.Name(root) != "bad/" || m.src == nil || m.src.path != filepath.Join(root, "bad/main.go") {
		t.Fatalf("target %q, source %v", m.target.Name(root), m.src)
	}
	screenHas(t, m, "◆ aether", "▸ bad/ (1 file)", "ready", "Explorer", "bad/main.go", "println(y)",
		"Nothing built yet", "Press b to build or c to check bad/", "b build", "q quit")
	// Hidden directories are not shown; testdata is shown but not a target.
	text := m.view().String()
	if strings.Contains(text, ".hidden") || !strings.Contains(text, "testdata") {
		t.Errorf("explorer:\n%s", text)
	}
}

func TestBuildSucceeds(t *testing.T) {
	root := workspace(t, testFiles)
	var calls []Request
	m := newTestModel(t, root, fakeRun(Result{Exit: diag.ExitOK}, &calls))
	press(m, "p", "hello", KeyEnter)
	if m.target.Name(root) != "hello/" || m.modal != modalNone {
		t.Fatalf("target %q, modal %d", m.target.Name(root), m.modal)
	}
	press(m, "b")
	if !m.running {
		t.Fatal("b did not start a build")
	}
	screenHas(t, m, "Building hello/", "building")
	press(m, "b")
	if m.note != "A build is already running" {
		t.Errorf("second b: note %q", m.note)
	}
	waitDone(t, m)
	want := Request{Mode: Build, Files: []string{filepath.Join(root, "hello/greet.go"), filepath.Join(root, "hello/main.go")},
		Output: filepath.Join(root, "hello/hello")}
	if len(calls) != 1 || !slices.Equal(calls[0].Files, want.Files) || calls[0].Output != want.Output || calls[0].Mode != Build {
		t.Fatalf("requests %+v, want %+v", calls, want)
	}
	for _, r := range m.stages {
		if r.state != stDone {
			t.Errorf("stage %s: state %d", stageNames[r.stage], r.state)
		}
	}
	screenHas(t, m, "✓ Build succeeded  →  hello/hello", "✓ build ok", "✓ Assemble", "GNU as · 1/2 files", "✓ Write executable")
	press(m, "]", "]")
	if m.tab != tabOutput {
		t.Fatalf("tab %d", m.tab)
	}
	screenHas(t, m, "$ aether build -o hello/hello hello/greet.go hello/main.go", "tool output", "exit status 0 (success)")
}

func TestCheckFails(t *testing.T) {
	root := workspace(t, testFiles)
	bad := filepath.Join(root, "bad/main.go")
	ds := []diag.Diagnostic{
		{Class: diag.Invalid, Pos: diag.Pos{File: bad, Line: 4, Col: 10}, Message: "undefined: y"},
		{Class: diag.Unsupported, Pos: diag.Pos{File: bad, Line: 3, Col: 1}, Message: "this is not supported", Feature: "some-feature"},
		{Class: diag.Internal, Message: "no position"},
	}
	var calls []Request
	m := newTestModel(t, root, fakeRun(Result{Diags: ds, Exit: diag.ExitInternal}, &calls))
	press(m, "c")
	waitDone(t, m)
	if calls[0].Mode != Check || calls[0].Output != "" {
		t.Errorf("request %+v", calls[0])
	}
	if m.tab != tabDiags || m.focus != paneBottom {
		t.Fatalf("after a failure: tab %d, focus %d", m.tab, m.focus)
	}
	for i, r := range m.stages {
		if want := []stageState{stDone, stFailed}[i]; r.state != want {
			t.Errorf("stage %s: state %d, want %d", stageNames[r.stage], r.state, want)
		}
	}
	screenHas(t, m, "✗ check failed · exit 4", "Diagnostics 3", "3 diagnostics: 1 invalid · 1 unsupported · 1 internal",
		"bad/main.go:4:10", "undefined: y", "[some-feature]", "no position")
	// The explorer counts the file's diagnostics.
	screenHas(t, m, "main.go               2││")

	press(m, KeyDown, KeyEnter) // the second diagnostic, at line 3
	if m.focus != paneSource || m.srcCur != 2 {
		t.Fatalf("after ⏎: focus %d, line %d", m.focus, m.srcCur+1)
	}
	screenHas(t, m, "◆ unsupported this is not supported [some-feature]")
	press(m, "n") // wraps to the diagnostic without a position
	if m.diagCur != 2 || m.note != "This diagnostic has no source position" {
		t.Errorf("n: diagnostic %d, note %q", m.diagCur, m.note)
	}
	press(m, "n")
	if m.diagCur != 0 || m.srcCur != 3 {
		t.Errorf("n: diagnostic %d, line %d", m.diagCur, m.srcCur+1)
	}
	press(m, "N", "N")
	if m.diagCur != 1 {
		t.Errorf("N N: diagnostic %d", m.diagCur)
	}
}

func TestUsageErrorAndPanic(t *testing.T) {
	root := workspace(t, testFiles)
	var calls []Request
	m := newTestModel(t, root, fakeRun(Result{Err: errors.New("open x.go: no such file"), Exit: diag.ExitUsage}, &calls))
	press(m, "b")
	waitDone(t, m)
	screenHas(t, m, "✗ Build failed: usage or file error", "open x.go: no such file · exit status 2")

	m.cfg.Run = func(Request, Progress) Result { panic("boom") }
	press(m, "c")
	waitDone(t, m)
	if len(m.diags) != 1 || m.diags[0].Class != diag.Internal || m.result.Exit != diag.ExitInternal {
		t.Errorf("after a panic: %v, exit %d", m.diags, m.result.Exit)
	}
}

func TestQuit(t *testing.T) {
	root := workspace(t, testFiles)
	m := newTestModel(t, root, nil)
	press(m, "q")
	if !m.quit {
		t.Error("q did not quit")
	}

	release := make(chan struct{})
	m = newTestModel(t, root, func(Request, Progress) Result { <-release; return Result{} })
	press(m, "b", "q")
	if m.quit || !m.quitAfterBuild {
		t.Fatal("q during a build quit at once")
	}
	close(release)
	waitDone(t, m)
	if !m.quit {
		t.Error("did not quit after the build")
	}

	m = newTestModel(t, root, func(Request, Progress) Result { select {} })
	press(m, Key{Code: KeyCtrl, Rune: 'c'}, "q")
	if !m.quit {
		t.Error("ctrl+c q during a build did not quit")
	}
}

func TestOptions(t *testing.T) {
	root := workspace(t, testFiles)
	var calls []Request
	m := newTestModel(t, root, fakeRun(Result{}, &calls))
	press(m, "o")
	screenHas(t, m, "Build options", "bad/bad", "$ aether build -o bad/bad bad/main.go")
	press(m, KeyEnter, Key{Code: KeyCtrl, Rune: 'u'}, "out/x", KeyLeft, "y", KeyEnter)
	if m.opts.Output != "out/yx" || m.editing {
		t.Fatalf("output %q, editing %v", m.opts.Output, m.editing)
	}
	press(m, KeyDown, " ", "j", KeyEnter, "j", " ")
	if !m.opts.O0 || !m.opts.Commands || !m.opts.KeepWork {
		t.Errorf("options %+v", m.opts)
	}
	press(m, KeyUp, KeyUp, KeyUp, KeyEnter, "zz", KeyEsc) // esc cancels the edit
	if m.opts.Output != "out/yx" {
		t.Errorf("esc kept the edit: %q", m.opts.Output)
	}
	press(m, "b") // b builds from the dialog
	waitDone(t, m)
	if m.modal != modalNone || len(calls) != 1 {
		t.Fatal("b in the options did not build")
	}
	if r := calls[0]; r.Output != filepath.Join(root, "out/yx") || r.Options != m.opts {
		t.Errorf("request %+v", r)
	}
	m.opts.Output = "/abs/out"
	if m.output() != "/abs/out" {
		t.Errorf("absolute output %q", m.output())
	}
}

func TestExplorer(t *testing.T) {
	root := workspace(t, testFiles)
	m := newTestModel(t, root, nil)
	m.focus = paneExplorer
	names := func() []string {
		var s []string
		for _, n := range m.vis {
			s = append(s, strings.Repeat(" ", n.depth)+n.name)
		}
		return s
	}
	want := []string{"bad", " main.go", "hello", "lib", "nested", "testdata", "go.mod"}
	if !slices.Equal(names(), want) {
		t.Fatalf("explorer %q, want %q", names(), want)
	}
	press(m, "g", "jj", KeyEnter) // open hello
	press(m, "j", KeyEnter)       // and open greet.go
	if m.src.path != filepath.Join(root, "hello/greet.go") || m.focus != paneSource {
		t.Fatalf("open: %s, focus %d", m.src.path, m.focus)
	}
	screenHas(t, m, "hello/greet.go", `return "hi"`)
	press(m, "1", "t") // greet.go alone as the target
	if m.target.Name(root) != "hello/greet.go" || len(m.target.Files) != 1 {
		t.Errorf("file target %q", m.target.Name(root))
	}
	press(m, "h", "t") // to hello/, and it as the target
	if m.target.Name(root) != "hello/" || len(m.target.Files) != 2 {
		t.Errorf("directory target %q", m.target.Name(root))
	}
	press(m, "h") // collapse hello
	if slices.Contains(names(), "  greet.go") {
		t.Errorf("collapsed: %q", names())
	}
	press(m, "G", "t") // go.mod is not a program
	if !strings.Contains(m.note, "not a program source file") {
		t.Errorf("note %q", m.note)
	}
	press(m, "k", "t") // nor is testdata, which has no files itself
	if !strings.Contains(m.note, "no files of package main") {
		t.Errorf("note %q", m.note)
	}
}

func TestProjectsFilter(t *testing.T) {
	root := workspace(t, testFiles)
	m := newTestModel(t, root, nil)
	press(m, "p", "zz")
	screenHas(t, m, "No program matches “zz”")
	press(m, KeyBackspace, KeyBackspace, "deep")
	if len(m.pickItems) != 1 {
		t.Fatalf("filter deep: %d items", len(m.pickItems))
	}
	press(m, KeyEnter)
	if m.target.Name(root) != "nested/deep/x/" || m.src.path != filepath.Join(root, "nested/deep/x/main.go") {
		t.Errorf("target %q, source %s", m.target.Name(root), m.src.path)
	}
	press(m, "p", KeyEsc)
	if m.modal != modalNone {
		t.Error("esc did not close the picker")
	}
}

func TestEmptyWorkspace(t *testing.T) {
	root := workspace(t, map[string]string{"lib/lib.go": "package lib\n"})
	m := newTestModel(t, root, nil)
	press(m, "b")
	if m.running || !strings.Contains(m.note, "No target") {
		t.Errorf("build without a target: running %v, note %q", m.running, m.note)
	}
	screenHas(t, m, "no target", "Press p to choose a program to build")
	press(m, "p")
	screenHas(t, m, "No directory here has files of package main")
}

// TestLayouts draws every view at many sizes: nothing may panic, and the
// header must name the target at every supported size.
func TestLayouts(t *testing.T) {
	root := workspace(t, testFiles)
	var calls []Request
	ds := []diag.Diagnostic{{Class: diag.Invalid, Pos: diag.Pos{File: filepath.Join(root, "bad/main.go"), Line: 4, Col: 10},
		Message: strings.Repeat("a long message ", 20), Feature: "f"}}
	m := newTestModel(t, root, fakeRun(Result{Diags: ds, Exit: diag.ExitInvalid}, &calls))
	press(m, "b")
	waitDone(t, m)
	for _, size := range [][2]int{{60, 16}, {61, 17}, {80, 24}, {89, 30}, {90, 30}, {132, 43}, {250, 80}} {
		m.update(resizeMsg{size[0], size[1]})
		for _, md := range []modal{modalNone, modalHelp, modalOptions, modalProjects} {
			for tb := tab(0); tb < numTabs; tb++ {
				m.modal, m.tab = md, tb
				s := m.view()
				if md == modalNone && !strings.Contains(s.Line(0), "bad/") {
					t.Errorf("%dx%d: header %q", size[0], size[1], s.Line(0))
				}
			}
		}
	}
	m.modal = modalNone
	m.update(resizeMsg{59, 30})
	screenHas(t, m, "Terminal too small: 59×30", "at least 60×16")
	m.update(resizeMsg{3, 2})
	m.view()
}

func TestRunNeedsTerminal(t *testing.T) {
	if isTerminal(os.Stdout) {
		t.Skip("standard output is a terminal")
	}
	err := Run(Config{Root: t.TempDir()})
	if !errors.Is(err, ErrNoTerminal) {
		t.Errorf("Run = %v, want ErrNoTerminal", err)
	}
}

func TestFormat(t *testing.T) {
	for d, want := range map[time.Duration]string{420 * time.Millisecond: "0.42s", 12340 * time.Millisecond: "12.3s", 125 * time.Second: "2m05s"} {
		if got := fmtDur(d); got != want {
			t.Errorf("fmtDur(%v) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int64]string{812: "812 B", 27136: "26.5 KB", 3 << 20: "3.0 MB"} {
		if got := fmtSize(n); got != want {
			t.Errorf("fmtSize(%d) = %q, want %q", n, got, want)
		}
	}
	if got := progressBar(1, 4, 8); got != "━━──────" {
		t.Errorf("progressBar = %q", got)
	}
}
