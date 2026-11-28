// Package tui is Aether's terminal user interface (D-061): a workspace
// explorer, a source viewer, build configuration, live build progress, and
// structured diagnostics. It compiles nothing itself; the driver supplies a
// Runner that calls the same pipeline as aether check and aether build.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stevenstank/aether/internal/diag"
)

// Mode is the command a request runs.
type Mode int

const (
	Check Mode = iota // aether check
	Build             // aether build
)

func (m Mode) String() string {
	if m == Build {
		return "build"
	}
	return "check"
}

// Options are the build flags the TUI configures.
type Options struct {
	Output   string // -o, as entered: relative to the workspace root, or absolute; "" is the default
	O0       bool   // -O0
	Commands bool   // -x
	KeepWork bool   // -work
}

// A Request is one check or build of a target.
type Request struct {
	Mode    Mode
	Files   []string // absolute paths
	Output  string   // absolute path; Build only
	Options Options
}

// Stage is a step of a check or build, in the order they run.
type Stage int

const (
	StageRead     Stage = iota // read the source files
	StageCheck                 // parse, type-check, gate, lower, verify
	StageEmit                  // optimize and emit assembly
	StageAssemble              // GNU as, one file at a time
	StageLink                  // GNU ld
	StageWrite                 // publish the executable
	numStages
)

// Progress receives a Runner's progress; its methods may be called from any
// goroutine.
type Progress interface {
	// Stage reports that stage s has started, or, with total > 0, that done
	// of its total units are finished.
	Stage(s Stage, done, total int)
	// Log reports one line of output, such as a command run.
	Log(line string)
}

// Result is the outcome of a request.
type Result struct {
	Diags []diag.Diagnostic // sorted, as the CLI reports them
	Err   error             // a usage or file error, which has no position
	Exit  int               // the exit status aether check or build would have
}

// A Runner runs a request on its own goroutine and reports progress.
type Runner func(Request, Progress) Result

// Config configures the TUI.
type Config struct {
	Root        string                 // the workspace directory, absolute
	Run         Runner                 // runs checks and builds
	CommandLine func(Request) []string // the equivalent aether command line
}

type pane int

const (
	paneExplorer pane = iota
	paneSource
	paneBottom
)

type tab int

const (
	tabBuild tab = iota
	tabDiags
	tabOutput
	numTabs
)

type modal int

const (
	modalNone modal = iota
	modalHelp
	modalOptions
	modalProjects
)

type stageState int

const (
	stPending stageState = iota
	stRunning
	stDone
	stFailed
	stSkipped
)

type stageRow struct {
	stage       Stage
	state       stageState
	start       time.Time
	dur         time.Duration
	done, total int
}

// Messages of the event loop.
type (
	keyMsg    Key
	resizeMsg struct{ w, h int }
	tickMsg   struct{}
	quitMsg   struct{}
	stageMsg  struct {
		stage       Stage
		done, total int
	}
	logMsg  string
	doneMsg Result
)

// maxLog bounds the output pane's lines.
const maxLog = 10000

// model is the state of the TUI. Only the event loop's goroutine touches
// it; runners talk to it through events.
type model struct {
	cfg    Config
	events chan any
	now    func() time.Time
	w, h   int
	quit   bool

	tree         *node
	vis          []*node
	exCur, exOff int

	projects []Target
	target   Target
	opts     Options

	src            *source
	srcCur, srcOff int
	srcCol         int // horizontal scroll, in columns

	focus pane
	tab   tab
	modal modal

	diags          []diag.Diagnostic
	diagCur        int
	diagOff        int
	log            []string
	logOff         int // lines scrolled up from the bottom
	running        bool
	mode           Mode
	req            Request
	stages         []stageRow
	started        time.Time
	elapsed        time.Duration
	result         *Result
	outSize        int64
	quitAfterBuild bool
	frame          int

	optCur  int
	editing bool
	edit    []rune
	editPos int

	filter    string
	pickCur   int
	pickOff   int
	pickItems []Target

	note   string
	noteSt Style
}

func newModel(cfg Config) *model {
	m := &model{cfg: cfg, events: make(chan any, 1024), now: time.Now}
	m.tree = &node{path: cfg.Root, name: filepath.Base(cfg.Root), dir: true, open: true, depth: -1}
	m.reload()
	if len(m.projects) > 0 {
		m.setTarget(m.projects[0])
	}
	m.openFirst()
	return m
}

// reload rereads the tree, the projects, and the open file from disk.
func (m *model) reload() {
	open := map[string]bool{}
	var mark func(n *node)
	mark = func(n *node) {
		for _, c := range n.children {
			if c.dir && c.open {
				open[c.path] = true
				mark(c)
			}
		}
	}
	mark(m.tree)
	var load func(n *node)
	load = func(n *node) {
		n.load()
		for _, c := range n.children {
			if open[c.path] {
				c.open = true
				load(c)
			}
		}
	}
	load(m.tree)
	m.refreshVisible()
	m.projects = discover(m.cfg.Root)
	if m.src != nil {
		m.src = loadSource(m.src.path)
		m.clampSource()
	}
	if m.target.Dir != "" && m.target.whole {
		if t, ok := dirTarget(m.target.Dir); ok {
			m.target = t
		}
	}
}

func (m *model) refreshVisible() {
	m.vis = m.tree.visible(nil)
	m.exCur = clamp(m.exCur, 0, len(m.vis)-1)
}

// openFirst opens the target's first file, or else the first Go file shown.
func (m *model) openFirst() {
	if len(m.target.Files) > 0 {
		m.openFile(m.target.Files[0], 0)
		return
	}
	for _, n := range m.vis {
		if !n.dir && strings.HasSuffix(n.name, ".go") {
			m.openFile(n.path, 0)
			return
		}
	}
}

func (m *model) setTarget(t Target) {
	m.target = t
	m.tree.reveal(t.Files[0])
	m.refreshVisible()
}

// output returns the absolute output path of a build of the target.
func (m *model) output() string {
	if o := m.opts.Output; o != "" {
		if filepath.IsAbs(o) {
			return o
		}
		return filepath.Join(m.cfg.Root, o)
	}
	if m.target.Dir == "" {
		return ""
	}
	if !m.target.whole {
		f := m.target.Files[0]
		return strings.TrimSuffix(f, ".go")
	}
	return filepath.Join(m.target.Dir, filepath.Base(m.target.Dir))
}

func (m *model) request(mode Mode) Request {
	r := Request{Mode: mode, Files: m.target.Files, Options: m.opts}
	if mode == Build {
		r.Output = m.output()
	}
	return r
}

// commandLine returns the aether command line of r, relative to the root.
func (m *model) commandLine(r Request) string {
	if m.cfg.CommandLine == nil {
		return "aether " + r.Mode.String()
	}
	args := m.cfg.CommandLine(r)
	for i, a := range args {
		if filepath.IsAbs(a) {
			a = rel(m.cfg.Root, a)
		}
		if a == "" || strings.ContainsAny(a, " \t'\"$\\") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		args[i] = a
	}
	return "aether " + strings.Join(args, " ")
}

func (m *model) notify(text string, st Style) {
	m.note, m.noteSt = text, st
}

// start runs a check or build of the target on a new goroutine.
func (m *model) start(mode Mode) {
	switch {
	case m.running:
		m.notify("A "+m.mode.String()+" is already running", stWarn)
		return
	case len(m.target.Files) == 0:
		m.notify("No target: press p to choose a program", stWarn)
		return
	}
	req := m.request(mode)
	m.running, m.mode, m.req = true, mode, req
	m.result, m.diags, m.diagCur, m.diagOff, m.outSize = nil, nil, 0, 0, 0
	m.started = m.now()
	m.elapsed = 0
	last := StageCheck
	if mode == Build {
		last = StageWrite
	}
	m.stages = m.stages[:0]
	for s := StageRead; s <= last; s++ {
		m.stages = append(m.stages, stageRow{stage: s})
	}
	m.tab = tabBuild
	m.note = ""
	if len(m.log) > 0 {
		m.appendLog("")
	}
	m.appendLog("$ " + m.commandLine(req))
	m.logOff = 0
	p := progress{m.events}
	go func() {
		var res Result
		defer func() {
			if r := recover(); r != nil {
				res = Result{Diags: []diag.Diagnostic{{Class: diag.Internal, Message: fmt.Sprintf("internal compiler error: %v", r)}}, Exit: diag.ExitInternal}
			}
			m.events <- doneMsg(res)
		}()
		res = m.cfg.Run(req, p)
	}()
}

// progress forwards a runner's progress to the event loop.
type progress struct{ events chan<- any }

func (p progress) Stage(s Stage, done, total int) { p.events <- stageMsg{s, done, total} }
func (p progress) Log(line string)                { p.events <- logMsg(line) }

func (m *model) appendLog(line string) {
	m.log = append(m.log, line)
	if len(m.log) > maxLog {
		m.log = append(m.log[:0], m.log[len(m.log)-maxLog:]...)
	}
	if m.logOff > 0 {
		m.logOff++ // keep the lines in view where they are
	}
}

func (m *model) onStage(msg stageMsg) {
	now := m.now()
	for i := range m.stages {
		r := &m.stages[i]
		switch {
		case r.stage < msg.stage && r.state == stRunning:
			r.state, r.dur = stDone, now.Sub(r.start)
		case r.stage < msg.stage && r.state == stPending:
			r.state = stSkipped
		case r.stage == msg.stage:
			if r.state == stPending {
				r.state, r.start = stRunning, now
			}
			r.done, r.total = msg.done, msg.total
		}
	}
}

func (m *model) onDone(res Result) {
	now := m.now()
	m.running = false
	m.elapsed = now.Sub(m.started)
	m.result = &res
	failed := res.Exit != diag.ExitOK
	for i := range m.stages {
		r := &m.stages[i]
		switch {
		case r.state == stRunning && failed:
			r.state, r.dur = stFailed, now.Sub(r.start)
		case r.state == stRunning:
			r.state, r.dur = stDone, now.Sub(r.start)
		case r.state == stPending:
			r.state = stSkipped
		}
	}
	m.diags = res.Diags
	if res.Err != nil {
		m.appendLog("aether " + m.mode.String() + ": " + res.Err.Error())
	}
	for _, d := range res.Diags {
		m.appendLog(d.String())
	}
	m.appendLog(fmt.Sprintf("exit status %d (%s) in %s", res.Exit, exitMeaning(res.Exit), fmtDur(m.elapsed)))
	if !failed && m.mode == Build {
		if fi, err := os.Stat(m.req.Output); err == nil {
			m.outSize = fi.Size()
		}
	}
	if len(m.diags) > 0 {
		m.tab = tabDiags
		m.focus = paneBottom
	}
	if m.src != nil {
		m.src = loadSource(m.src.path) // the build saw the file as it is now
		m.clampSource()
	}
	if m.quitAfterBuild {
		m.quit = true
	}
}

// update applies one message.
func (m *model) update(msg any) {
	switch msg := msg.(type) {
	case keyMsg:
		m.key(Key(msg))
	case resizeMsg:
		m.w, m.h = msg.w, msg.h
	case tickMsg:
		m.frame++
		if m.running {
			m.elapsed = m.now().Sub(m.started)
		}
	case quitMsg:
		m.quit = true
	case stageMsg:
		m.onStage(msg)
	case logMsg:
		m.appendLog(string(msg))
	case doneMsg:
		m.onDone(Result(msg))
	}
}

func (m *model) key(k Key) {
	if k.Code == KeyCtrl && k.Rune == 'c' {
		m.requestQuit()
		return
	}
	switch m.modal {
	case modalHelp:
		if k.Code == KeyEsc || k.Code == KeyEnter || k.Rune == '?' || k.Rune == 'q' {
			m.modal = modalNone
		}
		return
	case modalOptions:
		m.optionsKey(k)
		return
	case modalProjects:
		m.projectsKey(k)
		return
	}
	if m.paneKey(k) {
		return
	}
	switch {
	case k.Code == KeyTab:
		m.focus = (m.focus + 1) % 3
	case k.Code == KeyBacktab:
		m.focus = (m.focus + 2) % 3
	case k.Code == KeyEsc:
		m.note = ""
	case k.Code != KeyRune:
	case k.Rune == 'q':
		m.requestQuit()
	case k.Rune == 'b':
		m.start(Build)
	case k.Rune == 'c':
		m.start(Check)
	case k.Rune == 'o':
		m.modal, m.optCur, m.editing = modalOptions, 0, false
	case k.Rune == 'p':
		m.openProjects()
	case k.Rune == '?':
		m.modal = modalHelp
	case k.Rune == 'r':
		m.reload()
		m.notify(fmt.Sprintf("Reloaded · %d programs found", len(m.projects)), stMuted)
	case k.Rune == '1':
		m.focus = paneExplorer
	case k.Rune == '2':
		m.focus = paneSource
	case k.Rune == '3':
		m.focus = paneBottom
	case k.Rune == ']':
		m.tab = (m.tab + 1) % numTabs
	case k.Rune == '[':
		m.tab = (m.tab + numTabs - 1) % numTabs
	case k.Rune == 'n':
		m.nextDiag(1)
	case k.Rune == 'N':
		m.nextDiag(-1)
	}
}

func (m *model) requestQuit() {
	switch {
	case !m.running || m.quitAfterBuild:
		m.quit = true
	default:
		m.quitAfterBuild = true
		m.notify("Quitting when the "+m.mode.String()+" finishes · press q again to quit now", stWarn)
	}
}

// paneKey handles a key of the focused pane and reports whether it did.
func (m *model) paneKey(k Key) bool {
	switch m.focus {
	case paneExplorer:
		return m.explorerKey(k)
	case paneSource:
		return m.sourceKey(k)
	}
	return m.bottomKey(k)
}

// move returns the change of a selection that a navigation key makes.
func move(k Key, page int) (int, bool) {
	switch {
	case k.Code == KeyUp || k.Rune == 'k':
		return -1, true
	case k.Code == KeyDown || k.Rune == 'j':
		return 1, true
	case k.Code == KeyPgUp || (k.Code == KeyCtrl && k.Rune == 'u'):
		return -max(1, page), true
	case k.Code == KeyPgDn || (k.Code == KeyCtrl && k.Rune == 'd'):
		return max(1, page), true
	case k.Code == KeyHome || k.Rune == 'g':
		return -1 << 30, true
	case k.Code == KeyEnd || k.Rune == 'G':
		return 1 << 30, true
	}
	return 0, false
}

func (m *model) explorerKey(k Key) bool {
	if d, ok := move(k, m.layout().top.h-2); ok {
		m.exCur = clamp(m.exCur+d, 0, len(m.vis)-1)
		return true
	}
	if len(m.vis) == 0 {
		return false
	}
	n := m.vis[m.exCur]
	switch {
	case k.Code == KeyEnter || k.Code == KeyRight || k.Rune == 'l':
		if n.dir {
			if !n.loaded {
				n.load()
			}
			n.open = k.Code == KeyRight || k.Rune == 'l' || !n.open
			m.refreshVisible()
		} else {
			m.openFile(n.path, 0)
			if k.Code == KeyEnter {
				m.focus = paneSource
			}
		}
	case k.Code == KeyLeft || k.Rune == 'h':
		if n.dir && n.open {
			n.open = false
			m.refreshVisible()
			break
		}
		for i := m.exCur - 1; i >= 0; i-- { // to the parent directory
			if m.vis[i].dir && m.vis[i].depth < n.depth {
				m.exCur = i
				break
			}
		}
	case k.Rune == 't' || k.Rune == ' ':
		m.targetNode(n)
	default:
		return false
	}
	return true
}

// targetNode makes the program of node n the target.
func (m *model) targetNode(n *node) {
	if n.dir {
		t, ok := dirTarget(n.path)
		if !ok {
			m.notify(rel(m.cfg.Root, n.path)+" has no files of package main", stWarn)
			return
		}
		m.target = t
	} else {
		if !isSource(n.name) {
			m.notify(n.name+" is not a program source file", stWarn)
			return
		}
		m.target = fileTarget(n.path)
	}
	m.notify("Target: "+m.target.Name(m.cfg.Root), Style{Fg: cAccent})
}

func (m *model) sourceKey(k Key) bool {
	if m.src == nil {
		return false
	}
	if d, ok := move(k, m.layout().top.h-3); ok {
		m.srcCur = clamp(m.srcCur+d, 0, len(m.src.lines)-1)
		return true
	}
	switch {
	case k.Code == KeyLeft || k.Rune == 'h':
		m.srcCol = max(0, m.srcCol-8)
	case k.Code == KeyRight || k.Rune == 'l':
		m.srcCol += 8
	default:
		return false
	}
	return true
}

func (m *model) bottomKey(k Key) bool {
	page := m.layout().bottom.h - 4
	switch {
	case k.Code == KeyLeft:
		m.tab = (m.tab + numTabs - 1) % numTabs
		return true
	case k.Code == KeyRight:
		m.tab = (m.tab + 1) % numTabs
		return true
	}
	switch m.tab {
	case tabDiags:
		if d, ok := move(k, page); ok {
			m.diagCur = clamp(m.diagCur+d, 0, len(m.diags)-1)
			return true
		}
		if k.Code == KeyEnter && len(m.diags) > 0 {
			m.jump(m.diags[m.diagCur])
			return true
		}
	case tabOutput:
		if d, ok := move(k, page); ok {
			m.logOff = clamp(m.logOff-d, 0, max(0, len(m.log)-1))
			return true
		}
	}
	return false
}

// nextDiag selects the next diagnostic in direction dir and shows it.
func (m *model) nextDiag(dir int) {
	if len(m.diags) == 0 {
		m.notify("No diagnostics", stMuted)
		return
	}
	m.diagCur = (m.diagCur + dir + len(m.diags)) % len(m.diags)
	m.tab = tabDiags
	m.jump(m.diags[m.diagCur])
}

// jump opens the source of d at its line.
func (m *model) jump(d diag.Diagnostic) {
	if d.Pos.File == "" {
		m.notify("This diagnostic has no source position", stMuted)
		return
	}
	path := d.Pos.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.cfg.Root, path)
	}
	m.openFile(path, max(0, d.Pos.Line-1))
	if m.src.err == nil {
		m.focus = paneSource
	}
}

// openFile shows path in the source pane at line (0-based).
func (m *model) openFile(path string, line int) {
	if m.src == nil || m.src.path != path {
		m.src = loadSource(path)
		m.srcCol = 0
		if m.tree.reveal(path) {
			m.refreshVisible()
			for i, n := range m.vis {
				if n.path == path {
					m.exCur = i
				}
			}
		}
	}
	m.srcCur = line
	m.clampSource()
	m.srcOff = max(0, m.srcCur-(m.layout().top.h-3)/3) // the line a third of the way down
}

func (m *model) clampSource() {
	m.srcCur = clamp(m.srcCur, 0, len(m.src.lines)-1)
}

func (m *model) openProjects() {
	m.modal, m.filter, m.pickCur, m.pickOff = modalProjects, "", 0, 0
	m.filterProjects()
	for i, t := range m.pickItems {
		if t.Dir == m.target.Dir && t.whole == m.target.whole {
			m.pickCur = i
		}
	}
}

func (m *model) filterProjects() {
	m.pickItems = m.pickItems[:0]
	f := strings.ToLower(m.filter)
	for _, t := range m.projects {
		if strings.Contains(strings.ToLower(t.Name(m.cfg.Root)), f) {
			m.pickItems = append(m.pickItems, t)
		}
	}
	m.pickCur = clamp(m.pickCur, 0, len(m.pickItems)-1)
}

func (m *model) projectsKey(k Key) {
	if d, ok := move(Key{Code: k.Code}, 8); ok && k.Code != KeyRune {
		m.pickCur = clamp(m.pickCur+d, 0, len(m.pickItems)-1)
		return
	}
	switch k.Code {
	case KeyEsc:
		m.modal = modalNone
	case KeyEnter:
		if len(m.pickItems) > 0 {
			m.setTarget(m.pickItems[m.pickCur])
			m.openFile(m.target.Files[0], 0)
			m.notify("Target: "+m.target.Name(m.cfg.Root), Style{Fg: cAccent})
		}
		m.modal = modalNone
	case KeyBackspace:
		if m.filter != "" {
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
			m.filterProjects()
		}
	case KeyRune:
		m.filter += string(k.Rune)
		m.pickCur = 0
		m.filterProjects()
	}
}

// The rows of the options dialog.
const (
	optOutput = iota
	optO0
	optCommands
	optKeepWork
	numOpts
)

func (m *model) optionsKey(k Key) {
	if m.editing {
		m.editKey(k)
		return
	}
	switch {
	case k.Code == KeyEsc || k.Rune == 'o' || k.Rune == 'q':
		m.modal = modalNone
	case k.Code == KeyUp || k.Rune == 'k':
		m.optCur = (m.optCur + numOpts - 1) % numOpts
	case k.Code == KeyDown || k.Rune == 'j' || k.Code == KeyTab:
		m.optCur = (m.optCur + 1) % numOpts
	case k.Code == KeyEnter || k.Rune == ' ':
		switch m.optCur {
		case optOutput:
			m.editing = true
			cur := m.opts.Output
			if cur == "" {
				cur = rel(m.cfg.Root, m.output())
			}
			m.edit = []rune(cur)
			m.editPos = len(m.edit)
		case optO0:
			m.opts.O0 = !m.opts.O0
		case optCommands:
			m.opts.Commands = !m.opts.Commands
		case optKeepWork:
			m.opts.KeepWork = !m.opts.KeepWork
		}
	case k.Rune == 'b':
		m.modal = modalNone
		m.start(Build)
	}
}

func (m *model) editKey(k Key) {
	switch k.Code {
	case KeyEsc:
		m.editing = false
	case KeyEnter:
		m.editing = false
		m.opts.Output = strings.TrimSpace(string(m.edit))
	case KeyBackspace:
		if m.editPos > 0 {
			m.edit = append(m.edit[:m.editPos-1], m.edit[m.editPos:]...)
			m.editPos--
		}
	case KeyDelete:
		if m.editPos < len(m.edit) {
			m.edit = append(m.edit[:m.editPos], m.edit[m.editPos+1:]...)
		}
	case KeyLeft:
		m.editPos = max(0, m.editPos-1)
	case KeyRight:
		m.editPos = min(len(m.edit), m.editPos+1)
	case KeyHome:
		m.editPos = 0
	case KeyEnd:
		m.editPos = len(m.edit)
	case KeyCtrl:
		if k.Rune == 'u' {
			m.edit, m.editPos = m.edit[:0], 0
		}
	case KeyRune:
		m.edit = append(m.edit[:m.editPos], append([]rune{k.Rune}, m.edit[m.editPos:]...)...)
		m.editPos++
	}
}

func clamp(v, lo, hi int) int {
	if v > hi {
		v = hi
	}
	if v < lo {
		v = lo
	}
	return v
}

// fmtDur formats a duration for display: 0.42s, 12.3s, 2m05s.
func fmtDur(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// fmtSize formats a file size: 812 B, 1.3 MB.
func fmtSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
