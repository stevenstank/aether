package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
)

// The smallest terminal the layout fits.
const (
	minW = 60
	minH = 16
)

type rect struct{ x, y, w, h int }

type layout struct {
	explorer, top, bottom rect // top is the source pane
}

func (m *model) layout() layout {
	bodyY, bodyH := 1, max(0, m.h-2)
	bottomH := clamp(bodyH*2/5, 8, 16)
	if bodyH-bottomH < 6 {
		bottomH = max(0, bodyH-6)
	}
	topH := bodyH - bottomH
	exW := clamp(m.w/4, 24, 38)
	if m.w < 90 {
		exW = 22
	}
	return layout{
		explorer: rect{0, bodyY, exW, topH},
		top:      rect{exW, bodyY, m.w - exW, topH},
		bottom:   rect{0, bodyY + topH, m.w, bottomH},
	}
}

var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func (m *model) spin() rune { return spinner[m.frame%len(spinner)] }

// view draws the whole TUI.
func (m *model) view() *Screen {
	s := NewScreen(m.w, m.h)
	if m.w < minW || m.h < minH {
		msg := fmt.Sprintf("Terminal too small: %d×%d", m.w, m.h)
		need := fmt.Sprintf("Aether needs at least %d×%d", minW, minH)
		y := m.h/2 - 1
		s.Text(max(0, (m.w-textWidth(msg))/2), y, m.w, msg, stWarn)
		s.Text(max(0, (m.w-textWidth(need))/2), y+1, m.w, need, stMuted)
		return s
	}
	l := m.layout()
	m.header(s)
	m.explorer(s, l.explorer)
	m.source(s, l.top)
	m.bottom(s, l.bottom)
	m.footer(s)
	switch m.modal {
	case modalHelp:
		m.help(s)
	case modalOptions:
		m.options(s)
	case modalProjects:
		m.picker(s)
	}
	return s
}

func (m *model) header(s *Screen) {
	s.Fill(0, 0, s.W, 1, stBar)
	text, st := m.status()
	pill := " " + text + " "
	end := s.Right(0, 0, s.W-1, pill, st) - 1 // the left part ends here
	tgt := "no target"
	if len(m.target.Files) > 0 {
		tgt = m.target.Name(m.cfg.Root)
		if m.target.whole {
			tgt += fmt.Sprintf(" (%d %s)", len(m.target.Files), plural(len(m.target.Files), "file"))
		}
	}
	x := 1
	x += s.Text(x, 0, end-x, "◆ aether", Style{Fg: cAccent, Bg: cBar, Bold: true}) + 1
	// The target matters more than the workspace path: the path gets what
	// the target leaves, and at most a third of the bar.
	tgtW := min(textWidth(tgt)+2, end-x)
	rootW := min(s.W/3, end-x-tgtW-3)
	if rootW > 6 {
		x += s.Text(x, 0, rootW, "│ "+shortPath(m.cfg.Root), withBg(stMuted, cBar)) + 1
	}
	x += s.Text(x, 0, end-x, "│ ", withBg(stMuted, cBar))
	s.Text(x, 0, end-x, "▸ "+tgt, Style{Fg: cText, Bg: cBar, Bold: true})
}

// status returns the header's summary of the last or current run.
func (m *model) status() (string, Style) {
	switch {
	case m.running:
		return fmt.Sprintf("%c %sing %s", m.spin(), m.mode, fmtDur(m.elapsed)), Style{Fg: cBar, Bg: cAccent, Bold: true}
	case m.result == nil:
		return "ready", Style{Fg: cMuted, Bg: cBarHi}
	case m.result.Exit == diag.ExitOK:
		return fmt.Sprintf("✓ %s ok %s", m.mode, fmtDur(m.elapsed)), Style{Fg: cBar, Bg: cOK, Bold: true}
	}
	return fmt.Sprintf("✗ %s failed · exit %d", m.mode, m.result.Exit), Style{Fg: cBar, Bg: cErr, Bold: true}
}

// panel draws a pane's frame and returns its inside.
func (m *model) panel(s *Screen, r rect, p pane, title string) rect {
	border, tst := stBorder, stTitle
	if m.focus == p && m.modal == modalNone {
		border, tst = stFocus, stTitleOn
	}
	s.Box(r.x, r.y, r.w, r.h, border, title, tst)
	return rect{r.x + 1, r.y + 1, r.w - 2, r.h - 2}
}

func (m *model) explorer(s *Screen, r rect) {
	in := m.panel(s, r, paneExplorer, "Explorer")
	if len(m.vis) == 0 {
		s.Text(in.x+1, in.y, in.w-1, "(empty)", stFaint)
		return
	}
	m.exOff = scrollTo(m.exOff, m.exCur, in.h, len(m.vis))
	counts := m.diagCounts()
	for i := 0; i < in.h && m.exOff+i < len(m.vis); i++ {
		idx := m.exOff + i
		n := m.vis[idx]
		y := in.y + i
		st := stText
		bg := Color(0)
		if idx == m.exCur {
			bg = cSelDim
			if m.focus == paneExplorer {
				bg = cSel
			}
			s.Fill(in.x, y, in.w, 1, Style{Bg: bg})
		}
		indent := 1 + 2*n.depth
		icon := "  "
		switch {
		case n.dir && n.open:
			icon, st = "▾ ", Style{Fg: cAccent2}
		case n.dir:
			icon, st = "▸ ", Style{Fg: cAccent2}
		case !strings.HasSuffix(n.name, ".go"):
			st = stMuted
		}
		if m.inTarget(n) {
			st.Bold = true
		}
		if m.src != nil && n.path == m.src.path {
			st.Fg = cAccent
		}
		x := in.x + indent
		x += s.Text(x, y, in.w-indent, icon, withBg(stFaint, bg))
		badge := ""
		if c := counts[n.path]; c > 0 {
			badge = " " + strconv.Itoa(c)
		} else if m.inTarget(n) && !n.dir {
			badge = " •"
		}
		avail := in.x + in.w - x - textWidth(badge)
		s.Text(x, y, avail, n.name, withBg(st, bg))
		if badge != "" {
			bst := withBg(Style{Fg: cAccent}, bg)
			if counts[n.path] > 0 {
				bst = withBg(stErr, bg)
			}
			s.Right(in.x, y, in.w, badge, bst)
		}
	}
	scrollbar(s, rect{r.x + r.w - 1, in.y, 1, in.h}, m.exOff, in.h, len(m.vis))
}

// inTarget reports whether n is the target's directory or one of its files.
func (m *model) inTarget(n *node) bool {
	if n.dir {
		return m.target.whole && n.path == m.target.Dir
	}
	for _, f := range m.target.Files {
		if f == n.path {
			return true
		}
	}
	return false
}

// diagCounts returns the number of diagnostics in each file.
func (m *model) diagCounts() map[string]int {
	c := map[string]int{}
	for _, d := range m.diags {
		c[d.Pos.File]++
	}
	return c
}

func (m *model) source(s *Screen, r rect) {
	title := "Source"
	if m.src != nil {
		title = rel(m.cfg.Root, m.src.path)
	}
	in := m.panel(s, r, paneSource, title)
	if m.src == nil {
		emptyState(s, in, "No file open", "Select a .go file in the explorer and press ⏎")
		return
	}
	if m.src.err != nil {
		emptyState(s, in, "Cannot show this file", m.src.err.Error())
		return
	}
	lines := m.src.lines
	lineDiags := map[int][]diag.Diagnostic{}
	for _, d := range m.diags {
		if d.Pos.File == m.src.path && d.Pos.Line > 0 {
			lineDiags[d.Pos.Line-1] = append(lineDiags[d.Pos.Line-1], d)
		}
	}
	textH := in.h
	cur, hasCur := lineDiags[m.srcCur]
	if hasCur {
		textH-- // the cursor line's diagnostic goes below the text
	}
	m.srcOff = scrollTo(m.srcOff, m.srcCur, textH, len(lines))
	numW := len(strconv.Itoa(len(lines)))
	gutter := numW + 4
	for i := 0; i < textH && m.srcOff+i < len(lines); i++ {
		ln := m.srcOff + i
		y := in.y + i
		bg := Color(0)
		if ln == m.srcCur {
			bg = cSelDim
			if m.focus == paneSource {
				bg = cSel
			}
			s.Fill(in.x, y, in.w, 1, Style{Bg: bg})
		}
		numSt := withBg(stFaint, bg)
		if ln == m.srcCur {
			numSt = withBg(Style{Fg: cAccent}, bg)
		}
		s.Text(in.x+1, y, numW, fmt.Sprintf("%*d", numW, ln+1), numSt)
		if ds := lineDiags[ln]; len(ds) > 0 {
			worst := ds[0]
			for _, d := range ds {
				if d.Class > worst.Class {
					worst = d
				}
			}
			s.Set(in.x+numW+2, y, classIcon(worst.Class), withBg(classStyle(worst.Class), bg))
		}
		var spans []span
		if m.src.spans != nil {
			spans = m.src.spans[ln]
		}
		drawCode(s, in.x+gutter, y, in.w-gutter, lines[ln], spans, m.srcCol, bg)
	}
	if hasCur {
		d := cur[0]
		y := in.y + in.h - 1
		s.Fill(in.x, y, in.w, 1, Style{Bg: cBar})
		x := in.x + 1
		x += s.Text(x, y, in.w-2, string(classIcon(d.Class))+" "+d.Class.String()+" ", withBg(classStyle(d.Class), cBar))
		msg := d.Message
		if d.Feature != "" {
			msg += " [" + d.Feature + "]"
		}
		if len(cur) > 1 {
			msg += fmt.Sprintf("  (+%d more)", len(cur)-1)
		}
		s.Text(x, y, in.x+in.w-x-1, msg, withBg(stText, cBar))
	}
	scrollbar(s, rect{r.x + r.w - 1, in.y, 1, textH}, m.srcOff, textH, len(lines))
}

// drawCode draws one line of source, colored by spans, from column col on.
func drawCode(s *Screen, x, y, w int, line string, spans []span, col int, bg Color) {
	base := withBg(Style{Fg: cMuted}, bg)
	c := 0 // the column in the line
	si := 0
	for i, r := range line {
		for si < len(spans) && spans[si].end <= i {
			si++
		}
		st := base
		if si < len(spans) && spans[si].start <= i {
			st = withBg(spans[si].st, bg)
		}
		rw := runeWidth(r)
		if r == '\t' {
			rw = 4 - c%4
		}
		if rw == 0 {
			continue
		}
		if c >= col {
			dx := c - col
			if dx+rw > w {
				if w > 0 {
					s.Set(x+w-1, y, '…', withBg(stFaint, bg))
				}
				return
			}
			if r == '\t' {
				for k := 0; k < rw; k++ {
					s.Set(x+dx+k, y, ' ', st)
				}
			} else {
				s.Set(x+dx, y, r, st)
				if rw == 2 {
					s.Set(x+dx+1, y, 0, st)
				}
			}
		}
		c += rw
	}
}

func emptyState(s *Screen, in rect, title, detail string) {
	y := in.y + in.h/2 - 1
	s.Text(in.x+max(0, (in.w-textWidth(title))/2), y, in.w, title, Style{Fg: cText, Bold: true})
	s.Text(in.x+max(0, (in.w-textWidth(detail))/2), y+1, in.w, detail, stMuted)
}

// scrollTo returns the first shown row of a list of n rows, h at a time,
// that keeps row cur in view and moves as little as possible from off.
func scrollTo(off, cur, h, n int) int {
	if h <= 0 {
		return 0
	}
	if cur < off {
		off = cur
	}
	if cur >= off+h {
		off = cur - h + 1
	}
	return clamp(off, 0, max(0, n-h))
}

// scrollbar draws a thumb in the column r when not all n rows fit in h.
func scrollbar(s *Screen, r rect, off, h, n int) {
	if n <= h || h <= 0 {
		return
	}
	size := max(1, h*h/n)
	pos := (h - size) * off / max(1, n-h)
	for i := 0; i < size; i++ {
		s.Set(r.x, r.y+pos+i, '┃', Style{Fg: cFaint})
	}
}

func (m *model) bottom(s *Screen, r rect) {
	in := m.panel(s, r, paneBottom, "")
	// Tabs in the top border.
	names := []string{"Build", "Diagnostics", "Output"}
	if n := len(m.diags); n > 0 {
		names[1] += " " + strconv.Itoa(n)
	}
	x := r.x + 2
	for i, name := range names {
		st := stTitle
		label := " " + name + " "
		if tab(i) == m.tab {
			st = Style{Fg: cBar, Bg: cAccent, Bold: true}
			if m.focus != paneBottom {
				st = Style{Fg: cText, Bg: cBarHi, Bold: true}
			}
		}
		x += s.Text(x, r.y, r.w-4-(x-r.x), label, st) + 1
	}
	switch m.tab {
	case tabBuild:
		m.buildTab(s, in)
	case tabDiags:
		m.diagTab(s, in)
	case tabOutput:
		m.outputTab(s, in)
	}
}

var stageNames = [numStages]string{"Read sources", "Check", "Optimize & emit", "Assemble", "Link", "Write executable"}

func (m *model) stageDetail(r stageRow) string {
	switch r.stage {
	case StageRead:
		n := len(m.req.Files)
		return fmt.Sprintf("%d %s", n, plural(n, "file"))
	case StageCheck:
		return "parse · type-check · subset gate · lower · verify IR"
	case StageEmit:
		if m.req.Options.O0 {
			return "-O0 · every value in a stack cell"
		}
		return "SSA · fold · DCE · registers · x86-64 assembly"
	case StageAssemble:
		if r.total > 0 {
			return fmt.Sprintf("GNU as · %d/%d files", r.done, r.total)
		}
		return "GNU as"
	case StageLink:
		return "GNU ld · static, no libraries"
	}
	return rel(m.cfg.Root, m.req.Output)
}

func (m *model) buildTab(s *Screen, in rect) {
	if len(m.stages) == 0 {
		hint := "Press b to build or c to check " + m.targetLabel()
		if len(m.target.Files) == 0 {
			hint = "Press p to choose a program to build"
		}
		emptyState(s, in, "Nothing built yet", hint)
		return
	}
	x, y := in.x+1, in.y
	w := in.w - 2
	// The headline.
	head, st := "", stText
	switch {
	case m.running:
		head, st = fmt.Sprintf("%c %sing %s", m.spin(), capitalize(m.mode.String()), m.targetLabel()), Style{Fg: cAccent, Bold: true}
	case m.result.Exit == diag.ExitOK && m.mode == Build:
		head, st = fmt.Sprintf("✓ Build succeeded  →  %s", rel(m.cfg.Root, m.req.Output)), stOK
		if m.outSize > 0 {
			head += "  (" + fmtSize(m.outSize) + ")"
		}
	case m.result.Exit == diag.ExitOK:
		head, st = "✓ Check passed: the program is inside the supported subset", stOK
	default:
		head, st = fmt.Sprintf("✗ %s failed: %s", capitalize(m.mode.String()), exitMeaning(m.result.Exit)), stErr
	}
	s.Text(x, y, w-12, head, st)
	s.Right(x, y, w, fmtDur(m.elapsed), stMuted)
	y++
	if m.result != nil && m.result.Exit != diag.ExitOK {
		sum := m.diagSummary()
		if m.result.Err != nil {
			sum = m.result.Err.Error()
		}
		s.Text(x+2, y, w-2, sum+fmt.Sprintf(" · exit status %d", m.result.Exit), stMuted)
	}
	y++
	for _, r := range m.stages {
		if y >= in.y+in.h {
			break
		}
		icon, ist, nst := '·', stFaint, stFaint
		dur := ""
		switch r.state {
		case stRunning:
			icon, ist, nst = m.spin(), Style{Fg: cAccent, Bold: true}, Style{Fg: cText, Bold: true}
			dur = fmtDur(m.now().Sub(r.start))
		case stDone:
			icon, ist, nst = '✓', Style{Fg: cOK}, stText
			dur = fmtDur(r.dur)
		case stFailed:
			icon, ist, nst = '✗', stErr, Style{Fg: cErr}
			dur = fmtDur(r.dur)
		case stSkipped:
			icon = '-'
		}
		s.Set(x+1, y, icon, ist)
		s.Text(x+3, y, 18, stageNames[r.stage], nst)
		detailW := w - 3 - 19 - 10
		dx := x + 3 + 19
		if r.state == stRunning && r.total > 0 && detailW > 30 {
			bar := progressBar(r.done, r.total, 12)
			s.Text(dx, y, 12, bar, Style{Fg: cAccent})
			dx += 13
			detailW -= 13
		}
		s.Text(dx, y, detailW, m.stageDetail(r), stMuted)
		s.Right(x, y, w, dur, stFaint)
		y++
	}
}

func progressBar(done, total, w int) string {
	n := clamp(done*w/max(1, total), 0, w)
	return strings.Repeat("━", n) + strings.Repeat("─", w-n)
}

func (m *model) targetLabel() string {
	if len(m.target.Files) == 0 {
		return ""
	}
	return m.target.Name(m.cfg.Root)
}

// diagSummary counts the diagnostics by class: "2 invalid · 1 unsupported".
func (m *model) diagSummary() string {
	if len(m.diags) == 0 {
		return "no diagnostics"
	}
	var n [diag.Tool + 1]int
	for _, d := range m.diags {
		if d.Class >= diag.Invalid && d.Class <= diag.Tool {
			n[d.Class]++
		}
	}
	var parts []string
	for c := diag.Invalid; c <= diag.Tool; c++ {
		if n[c] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[c], c))
		}
	}
	return fmt.Sprintf("%d %s: %s", len(m.diags), plural(len(m.diags), "diagnostic"), strings.Join(parts, " · "))
}

func (m *model) diagTab(s *Screen, in rect) {
	if len(m.diags) == 0 {
		detail := "Run a check (c) or build (b) to see the compiler's findings here"
		if m.result != nil {
			detail = "The last " + m.mode.String() + " reported nothing"
		}
		emptyState(s, in, "No diagnostics", detail)
		return
	}
	s.Text(in.x+1, in.y, in.w-2, m.diagSummary(), stMuted)
	listY, listH := in.y+1, in.h-1
	m.diagOff = scrollTo(m.diagOff, m.diagCur, listH, len(m.diags))
	posW := 0
	for _, d := range m.diags {
		posW = max(posW, textWidth(m.diagPos(d)))
	}
	posW = min(posW, in.w/3)
	for i := 0; i < listH && m.diagOff+i < len(m.diags); i++ {
		idx := m.diagOff + i
		d := m.diags[idx]
		y := listY + i
		bg := Color(0)
		if idx == m.diagCur {
			bg = cSelDim
			if m.focus == paneBottom {
				bg = cSel
			}
			s.Fill(in.x, y, in.w, 1, Style{Bg: bg})
		}
		x := in.x + 1
		cst := withBg(classStyle(d.Class), bg)
		s.Set(x, y, classIcon(d.Class), cst)
		s.Text(x+2, y, 11, d.Class.String(), cst)
		x += 14
		s.Text(x, y, posW, m.diagPos(d), withBg(Style{Fg: cAccent2}, bg))
		x += posW + 2
		msg := d.Message
		end := in.x + in.w - 1
		if d.Feature != "" {
			tag := "[" + d.Feature + "]"
			if textWidth(tag) < (end-x)/3 {
				end = s.Right(x, y, end-x, tag, withBg(stFaint, bg)) - 1
			}
		}
		s.Text(x, y, end-x, strings.ReplaceAll(msg, "\n", " "), withBg(stText, bg))
	}
	scrollbar(s, rect{in.x + in.w, listY, 1, listH}, m.diagOff, listH, len(m.diags))
}

// diagPos is a diagnostic's position relative to the root.
func (m *model) diagPos(d diag.Diagnostic) string {
	if d.Pos.File == "" {
		return "—"
	}
	p := d.Pos
	p.File = rel(m.cfg.Root, p.File)
	return p.String()
}

func (m *model) outputTab(s *Screen, in rect) {
	if len(m.log) == 0 {
		emptyState(s, in, "No output yet", "Commands, tool versions, and results appear here; -x logs every as and ld run")
		return
	}
	end := len(m.log) - m.logOff
	start := max(0, end-in.h)
	for i, line := range m.log[start:end] {
		st := stText
		switch {
		case strings.HasPrefix(line, "$ "):
			st = Style{Fg: cAccent, Bold: true}
		case strings.HasPrefix(line, "exit status 0"):
			st = Style{Fg: cOK}
		case strings.HasPrefix(line, "exit status"):
			st = Style{Fg: cErr}
		case strings.HasPrefix(line, "#") || strings.HasPrefix(line, "cd ") || strings.HasPrefix(line, "WORK="):
			st = stMuted
		}
		s.Text(in.x+1, in.y+i, in.w-2, line, st)
	}
	if m.logOff > 0 {
		s.Right(in.x, in.y+in.h-1, in.w-1, fmt.Sprintf(" ↓ %d more ", m.logOff), Style{Fg: cBar, Bg: cMuted})
	}
}

// hints are the footer's key hints for the focused pane.
func (m *model) hints() [][2]string {
	var h [][2]string
	switch m.focus {
	case paneExplorer:
		h = [][2]string{{"↑↓", "move"}, {"⏎", "open"}, {"t", "target"}}
	case paneSource:
		h = [][2]string{{"↑↓", "scroll"}, {"←→", "pan"}, {"n/N", "next issue"}}
	case paneBottom:
		if m.tab == tabDiags {
			h = [][2]string{{"↑↓", "select"}, {"⏎", "go to source"}, {"←→", "tabs"}}
		} else {
			h = [][2]string{{"↑↓", "scroll"}, {"←→", "tabs"}}
		}
	}
	return append(h, [][2]string{{"b", "build"}, {"c", "check"}, {"o", "options"}, {"p", "programs"}, {"tab", "focus"}, {"?", "help"}, {"q", "quit"}}...)
}

func (m *model) footer(s *Screen) {
	y := s.H - 1
	s.Fill(0, y, s.W, 1, stBar)
	noteW := 0
	if m.note != "" {
		noteW = min(textWidth(m.note)+2, s.W/2)
		s.Right(0, y, s.W-1, m.note, withBg(m.noteSt, cBar))
	}
	x := 1
	limit := s.W - noteW - 2
	for _, h := range m.hints() {
		w := textWidth(h[0]) + 1 + textWidth(h[1]) + 2
		if x+w > limit {
			break
		}
		x += s.Text(x, y, w, h[0], stKey)
		x += s.Text(x+1, y, w, h[1], stBar) + 3
	}
}

// dialog draws a centered modal of the given size and returns its inside.
func dialog(s *Screen, w, h int, title string) rect {
	w, h = min(w, s.W-4), min(h, s.H-2)
	x, y := (s.W-w)/2, (s.H-h)/2
	s.Fill(x, y, w, h, Style{Bg: cBar})
	s.Box(x, y, w, h, Style{Fg: cAccent, Bg: cBar}, title, Style{Fg: cAccent, Bg: cBar, Bold: true})
	return rect{x + 2, y + 1, w - 4, h - 2}
}

var helpRows = [][2]string{
	{"", "Global"},
	{"b / c", "build / check the target"},
	{"o", "build options (-o, -O0, -x, -work)"},
	{"p", "choose a program in the workspace"},
	{"tab, 1 2 3", "focus explorer, source, results"},
	{"[ ]", "switch results tab"},
	{"n / N", "next / previous diagnostic"},
	{"r", "reload files from disk"},
	{"q, ctrl+c", "quit"},
	{"", "Explorer"},
	{"⏎ / → / ←", "open file or folder / expand / collapse"},
	{"t, space", "make the file or folder the target"},
	{"", "Source and lists"},
	{"↑↓ j k", "move"},
	{"pgup pgdn, g G", "page, top, bottom"},
	{"⏎", "go to a diagnostic's source line"},
}

func (m *model) help(s *Screen) {
	in := dialog(s, 62, len(helpRows)+4, "Keyboard shortcuts")
	bg := cBar
	for i, row := range helpRows {
		y := in.y + 1 + i
		if y >= in.y+in.h {
			break
		}
		if row[0] == "" {
			s.Text(in.x, y, in.w, row[1], Style{Fg: cMuted, Bg: bg, Bold: true})
			continue
		}
		s.Text(in.x+1, y, 16, row[0], Style{Fg: cAccent2, Bg: bg, Bold: true})
		s.Text(in.x+18, y, in.w-18, row[1], withBg(stText, bg))
	}
}

func (m *model) options(s *Screen) {
	in := dialog(s, 72, 14, "Build options")
	bg := cBar
	rows := []struct{ label, value, hint string }{
		{"Output", "", "-o: where the executable is written"},
		{"Optimization", onOff(!m.opts.O0, "on", "-O0"), "-O0: every value in a stack cell"},
		{"Show commands", onOff(m.opts.Commands, "on", "off"), "-x: log as and ld runs and versions"},
		{"Keep work dir", onOff(m.opts.KeepWork, "on", "off"), "-work: keep and print the temporary dir"},
	}
	out := rel(m.cfg.Root, m.output())
	if out == "" {
		out = "(no target)"
	}
	rows[0].value = out
	for i, r := range rows {
		y := in.y + 1 + i*2
		sel := i == m.optCur
		lst := withBg(stMuted, bg)
		if sel {
			s.Fill(in.x, y, in.w, 1, Style{Bg: cSel})
			lst = Style{Fg: cText, Bg: cSel, Bold: true}
			s.Set(in.x, y, '▸', Style{Fg: cAccent, Bg: cSel})
		}
		rowBg := bg
		if sel {
			rowBg = cSel
		}
		s.Text(in.x+2, y, 15, r.label, lst)
		vx := in.x + 18
		if i == optOutput && m.editing {
			m.drawEdit(s, vx, y, in.w-18)
		} else {
			vst := Style{Fg: cAccent2, Bg: rowBg, Bold: true}
			if r.value == "off" || r.value == "-O0" {
				vst = Style{Fg: cWarn, Bg: rowBg, Bold: true}
			}
			if r.value == "off" {
				vst.Fg = cMuted
			}
			s.Text(vx, y, in.w-18, r.value, vst)
		}
		s.Text(in.x+18, y+1, in.w-18, r.hint, withBg(stFaint, bg))
	}
	cmd := "$ " + m.commandLine(m.request(Build))
	if len(m.target.Files) == 0 {
		cmd = "$ aether build (no target)"
	}
	s.Text(in.x, in.y+in.h-2, in.w, cmd, Style{Fg: cMuted, Bg: bg, Italic: true})
	keys := "↑↓ select · ⏎/space change · b build · esc close"
	if m.editing {
		keys = "⏎ apply · esc cancel · ctrl+u clear"
	}
	s.Text(in.x, in.y+in.h-1, in.w, keys, withBg(stFaint, bg))
}

// drawEdit draws the output field being edited, with a block cursor.
func (m *model) drawEdit(s *Screen, x, y, w int) {
	field := Style{Fg: cText, Bg: cBarHi}
	s.Fill(x, y, w, 1, field)
	text := m.edit
	start := 0
	if m.editPos >= w-1 {
		start = m.editPos - w + 2
	}
	col := 0
	for i := start; i <= len(text) && col < w; i++ {
		r := ' '
		if i < len(text) {
			r = text[i]
		}
		st := field
		if i == m.editPos {
			st = Style{Fg: cBar, Bg: cAccent}
		}
		s.Set(x+col, y, r, st)
		col += max(1, runeWidth(r))
	}
}

func (m *model) picker(s *Screen) {
	h := clamp(len(m.projects)+6, 9, s.H-2)
	in := dialog(s, 70, h, "Programs in the workspace")
	bg := cBar
	s.Text(in.x, in.y, 2, "⌕", Style{Fg: cAccent, Bg: bg})
	if m.filter == "" {
		s.Text(in.x+2, in.y, in.w-2, "type to filter", withBg(stFaint, bg))
	} else {
		n := s.Text(in.x+2, in.y, in.w-3, m.filter, Style{Fg: cText, Bg: bg, Bold: true})
		s.Set(in.x+2+n, in.y, ' ', Style{Bg: cAccent})
	}
	listY, listH := in.y+2, in.h-3
	if len(m.pickItems) == 0 {
		msg := "No directory here has files of package main"
		if m.filter != "" {
			msg = "No program matches “" + m.filter + "”"
		}
		s.Text(in.x, listY, in.w, msg, withBg(stMuted, bg))
	}
	m.pickOff = scrollTo(m.pickOff, m.pickCur, listH, len(m.pickItems))
	for i := 0; i < listH && m.pickOff+i < len(m.pickItems); i++ {
		idx := m.pickOff + i
		t := m.pickItems[idx]
		y := listY + i
		rowBg := bg
		if idx == m.pickCur {
			rowBg = cSel
			s.Fill(in.x-1, y, in.w+2, 1, Style{Bg: rowBg})
		}
		name := t.Name(m.cfg.Root)
		if name == "."+string(filepath.Separator) {
			name = "." + string(filepath.Separator) + " (workspace root)"
		}
		st := Style{Fg: cText, Bg: rowBg}
		if t.Dir == m.target.Dir && t.whole == m.target.whole {
			st.Fg, st.Bold = cAccent, true
		}
		s.Text(in.x+1, y, in.w-14, name, st)
		n := len(t.Files)
		s.Right(in.x, y, in.w, fmt.Sprintf("%d %s", n, plural(n, "file")), withBg(stMuted, rowBg))
	}
	s.Text(in.x, in.y+in.h-1, in.w, "↑↓ select · ⏎ set target · esc close", withBg(stFaint, bg))
}

func onOff(on bool, yes, no string) string {
	if on {
		return yes
	}
	return no
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// shortPath abbreviates the home directory as ~.
func shortPath(p string) string {
	if home := homeDir; home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + p[len(home):]
	}
	return p
}
