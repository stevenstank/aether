package tui

import (
	"strconv"
	"strings"
	"unicode"
)

// A Color is the default color (0) or a 24-bit RGB color; the terminal's
// color mode decides how it is written.
type Color uint32

const rgbFlag = 1 << 24

// RGB returns the color with the given components.
func RGB(r, g, b uint8) Color { return rgbFlag | Color(r)<<16 | Color(g)<<8 | Color(b) }

func (c Color) rgb() (r, g, b int) { return int(c>>16) & 0xff, int(c>>8) & 0xff, int(c) & 0xff }

// A Style is how a cell is drawn.
type Style struct {
	Fg, Bg                        Color
	Bold, Dim, Italic, Under, Rev bool
}

// ColorMode is how many colors the terminal shows.
type ColorMode int

const (
	ColorNone ColorMode = iota // attributes only (NO_COLOR)
	Color256                   // the xterm 256-color palette
	ColorTrue                  // 24-bit color
)

// A cell is one column of the screen. A wide rune occupies its cell and
// the next, which holds the rune 0.
type cell struct {
	r rune
	s Style
}

// A Screen is a grid of cells that the view draws into.
type Screen struct {
	W, H  int
	cells []cell
}

// NewScreen returns a blank screen of w columns and h rows.
func NewScreen(w, h int) *Screen {
	s := &Screen{W: w, H: h, cells: make([]cell, w*h)}
	for i := range s.cells {
		s.cells[i].r = ' '
	}
	return s
}

// runeWidth returns the number of columns r occupies: 0 for control and
// combining characters, 2 for East Asian wide and fullwidth characters and
// emoji, 1 otherwise.
func runeWidth(r rune) int {
	switch {
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200b:
		return 0
	case r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f64f) ||
		(r >= 0x1f900 && r <= 0x1f9ff) ||
		(r >= 0x20000 && r <= 0x3fffd)):
		return 2
	}
	return 1
}

// textWidth returns the number of columns s occupies.
func textWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// Set draws r at column x of row y, if it is on the screen.
func (s *Screen) Set(x, y int, r rune, st Style) {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return
	}
	s.cells[y*s.W+x] = cell{r, st}
}

// Fill paints the rectangle with spaces of style st.
func (s *Screen) Fill(x, y, w, h int, st Style) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			s.Set(i, j, ' ', st)
		}
	}
}

// Text draws str at (x, y) in at most max columns and returns the number
// of columns drawn. Text that does not fit ends in an ellipsis. Tabs are
// drawn as spaces and other control characters are dropped.
func (s *Screen) Text(x, y, max int, str string, st Style) int {
	if max <= 0 {
		return 0
	}
	str = strings.ReplaceAll(str, "\t", "    ")
	fits := textWidth(str) <= max
	col := 0
	for _, r := range str {
		w := runeWidth(r)
		if w == 0 {
			continue
		}
		if !fits && col+w > max-1 {
			s.Set(x+col, y, '…', st)
			return col + 1
		}
		s.Set(x+col, y, r, st)
		if w == 2 {
			s.Set(x+col+1, y, 0, st)
		}
		col += w
	}
	return col
}

// Right draws str ending at column x+max, and returns its start column.
func (s *Screen) Right(x, y, max int, str string, st Style) int {
	w := min(textWidth(str), max)
	s.Text(x+max-w, y, w, str, st)
	return x + max - w
}

// Box draws a rounded border around the rectangle, with title in it.
func (s *Screen) Box(x, y, w, h int, border Style, title string, titleSt Style) {
	if w < 2 || h < 2 {
		return
	}
	for i := x + 1; i < x+w-1; i++ {
		s.Set(i, y, '─', border)
		s.Set(i, y+h-1, '─', border)
	}
	for j := y + 1; j < y+h-1; j++ {
		s.Set(x, j, '│', border)
		s.Set(x+w-1, j, '│', border)
	}
	s.Set(x, y, '╭', border)
	s.Set(x+w-1, y, '╮', border)
	s.Set(x, y+h-1, '╰', border)
	s.Set(x+w-1, y+h-1, '╯', border)
	if title != "" && w > 6 {
		s.Text(x+2, y, w-6, " "+title+" ", titleSt)
	}
}

// Line returns row y as plain text, for tests.
func (s *Screen) Line(y int) string {
	var b strings.Builder
	for _, c := range s.cells[y*s.W : (y+1)*s.W] {
		if c.r != 0 {
			b.WriteRune(c.r)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// String returns the whole screen as plain text, for tests.
func (s *Screen) String() string {
	lines := make([]string, s.H)
	for y := range lines {
		lines[y] = s.Line(y)
	}
	return strings.Join(lines, "\n")
}

// StyleAt returns the style of the cell at (x, y), for tests.
func (s *Screen) StyleAt(x, y int) Style { return s.cells[y*s.W+x].s }

// renderLine encodes row y as escape sequences and text in color mode m,
// starting and ending in the default style.
func (s *Screen) renderLine(y int, m ColorMode) string {
	var b strings.Builder
	cur := Style{}
	for _, c := range s.cells[y*s.W : (y+1)*s.W] {
		if c.r == 0 {
			continue
		}
		if c.s != cur {
			b.WriteString(sgr(c.s, m))
			cur = c.s
		}
		b.WriteRune(c.r)
	}
	if cur != (Style{}) {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// sgr returns the sequence that selects style st from any other.
func sgr(st Style, m ColorMode) string {
	p := []string{"0"}
	if m == ColorNone && (st.Bg == cSel || st.Bg == cSelDim || st.Bg == cBarHi) {
		st.Rev = true // without colors, a selection is reversed
	}
	if st.Bold {
		p = append(p, "1")
	}
	if st.Dim {
		p = append(p, "2")
	}
	if st.Italic {
		p = append(p, "3")
	}
	if st.Under {
		p = append(p, "4")
	}
	if st.Rev {
		p = append(p, "7")
	}
	p = append(p, colorParams(st.Fg, "38", m)...)
	p = append(p, colorParams(st.Bg, "48", m)...)
	return "\x1b[" + strings.Join(p, ";") + "m"
}

func colorParams(c Color, sel string, m ColorMode) []string {
	if c == 0 || m == ColorNone {
		return nil
	}
	r, g, b := c.rgb()
	if m == ColorTrue {
		return []string{sel, "2", strconv.Itoa(r), strconv.Itoa(g), strconv.Itoa(b)}
	}
	return []string{sel, "5", strconv.Itoa(xterm256(r, g, b))}
}

// xterm256 returns the index of the closest color of the xterm palette's
// 6x6x6 cube and gray ramp (indexes 16 to 255).
func xterm256(r, g, b int) int {
	level := func(v int) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (v - 35) / 40
	}
	steps := [6]int{0, 95, 135, 175, 215, 255}
	cr, cg, cb := level(r), level(g), level(b)
	cube := 16 + 36*cr + 6*cg + cb
	dist := func(x, y, z int) int { return (x-r)*(x-r) + (y-g)*(y-g) + (z-b)*(z-b) }
	dc := dist(steps[cr], steps[cg], steps[cb])
	avg := (r + g + b) / 3
	gi := 23
	if avg < 238 {
		gi = max(0, (avg-3)/10)
	}
	gv := 8 + 10*gi
	if dist(gv, gv, gv) < dc {
		return 232 + gi
	}
	return cube
}

// A painter writes screens to the terminal, redrawing only changed rows.
type painter struct {
	mode ColorMode
	prev []string // the rows last written; nil after a resize
	w, h int
}

// frame returns the bytes that change the terminal from the previous frame
// to s.
func (p *painter) frame(s *Screen) string {
	var b strings.Builder
	b.WriteString("\x1b[?2026h") // synchronized update, where supported
	if p.w != s.W || p.h != s.H || p.prev == nil {
		p.prev = make([]string, s.H)
		p.w, p.h = s.W, s.H
		b.WriteString("\x1b[0m\x1b[2J")
		for i := range p.prev {
			p.prev[i] = "\x00" // differs from every row
		}
	}
	for y := 0; y < s.H; y++ {
		line := s.renderLine(y, p.mode)
		if line == p.prev[y] {
			continue
		}
		p.prev[y] = line
		b.WriteString("\x1b[")
		b.WriteString(strconv.Itoa(y + 1))
		b.WriteString(";1H")
		b.WriteString(line)
	}
	b.WriteString("\x1b[?2026l")
	return b.String()
}
