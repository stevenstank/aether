package tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseKeys(t *testing.T) {
	tests := []struct {
		in   string
		want []Key
	}{
		{"ab", []Key{runeKey('a'), runeKey('b')}},
		{"é世", []Key{runeKey('é'), runeKey('世')}},
		{"\r\t\x7f", []Key{{Code: KeyEnter}, {Code: KeyTab}, {Code: KeyBackspace}}},
		{"\x03\x15", []Key{{Code: KeyCtrl, Rune: 'c'}, {Code: KeyCtrl, Rune: 'u'}}},
		{"\x1b", []Key{{Code: KeyEsc}}},
		{"\x1b[A\x1b[B\x1b[C\x1b[D", []Key{{Code: KeyUp}, {Code: KeyDown}, {Code: KeyRight}, {Code: KeyLeft}}},
		{"\x1bOA\x1bOH\x1b[F", []Key{{Code: KeyUp}, {Code: KeyHome}, {Code: KeyEnd}}},
		{"\x1b[5~\x1b[6~\x1b[3~\x1b[1~\x1b[4~", []Key{{Code: KeyPgUp}, {Code: KeyPgDn}, {Code: KeyDelete}, {Code: KeyHome}, {Code: KeyEnd}}},
		{"\x1b[1;5A", []Key{{Code: KeyUp}}}, // ctrl+up: the modifier is ignored
		{"\x1b[Z", []Key{{Code: KeyBacktab}}},
		{"\x1b[99~x", []Key{runeKey('x')}},             // unknown sequences are dropped whole
		{"\x1bq", []Key{{Code: KeyEsc}, runeKey('q')}}, // alt+q
		{"\x1b[", []Key{{Code: KeyEsc}, runeKey('[')}}, // a truncated sequence
		{"\xff" + "a", []Key{runeKey('a')}},            // invalid UTF-8 is dropped
	}
	for _, tt := range tests {
		if got := parseKeys([]byte(tt.in)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseKeys(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestScreenText(t *testing.T) {
	s := NewScreen(12, 4)
	if n := s.Text(0, 0, 12, "short", stText); n != 5 {
		t.Errorf("Text returned %d, want 5", n)
	}
	s.Text(0, 1, 8, "much too long", stText)
	s.Text(0, 2, 12, "a\tb世界", stText)
	s.Text(10, 3, 10, "clipped at the edge", stText)
	want := []string{"short", "much to…", "a    b世界", "          cl"}
	for y, w := range want {
		if got := s.Line(y); got != w {
			t.Errorf("line %d = %q, want %q", y, got, w)
		}
	}
	if w := textWidth("a世́"); w != 3 {
		t.Errorf("textWidth = %d, want 3", w)
	}
}

func TestScreenRightAndBox(t *testing.T) {
	s := NewScreen(10, 3)
	s.Box(0, 0, 10, 3, stBorder, "T", stTitle)
	s.Right(1, 1, 8, "end", stText)
	want := "╭─ T ────╮\n│     end│\n╰────────╯"
	if got := s.String(); got != want {
		t.Errorf("screen:\n%s\nwant:\n%s", got, want)
	}
}

func TestPainter(t *testing.T) {
	p := &painter{mode: ColorTrue}
	s := NewScreen(4, 3)
	s.Text(0, 0, 4, "ab", Style{Fg: RGB(1, 2, 3), Bold: true})
	first := p.frame(s)
	for _, want := range []string{"\x1b[2J", "\x1b[1;1H\x1b[0;1;38;2;1;2;3mab\x1b[0m  ", "\x1b[3;1H    "} {
		if !strings.Contains(first, want) {
			t.Errorf("first frame %q lacks %q", first, want)
		}
	}
	s.Text(0, 1, 4, "x", stText)
	second := p.frame(s)
	if strings.Contains(second, "\x1b[2J") || strings.Contains(second, "\x1b[1;1H") || strings.Contains(second, "\x1b[3;1H") {
		t.Errorf("second frame redraws unchanged rows: %q", second)
	}
	if !strings.Contains(second, "\x1b[2;1H") {
		t.Errorf("second frame does not redraw row 2: %q", second)
	}
	if third := p.frame(s); third != "\x1b[?2026h\x1b[?2026l" {
		t.Errorf("unchanged frame = %q", third)
	}
	if p.frame(NewScreen(5, 3)) == "" || p.prev == nil || p.w != 5 {
		t.Error("a resize does not redraw")
	}
}

func TestColors(t *testing.T) {
	st := Style{Fg: RGB(255, 0, 0), Bg: RGB(128, 128, 128)}
	if got, want := sgr(st, Color256), "\x1b[0;38;5;196;48;5;244m"; got != want {
		t.Errorf("256 colors: %q, want %q", got, want)
	}
	if got, want := sgr(st, ColorNone), "\x1b[0m"; got != want {
		t.Errorf("no colors: %q, want %q", got, want)
	}
	if got, want := sgr(Style{Bg: cSel}, ColorNone), "\x1b[0;7m"; got != want {
		t.Errorf("selection without colors: %q, want %q", got, want)
	}
	for _, tt := range []struct {
		env  map[string]string
		want ColorMode
	}{
		{map[string]string{"TERM": "xterm-256color"}, Color256},
		{map[string]string{"TERM": "xterm", "COLORTERM": "truecolor"}, ColorTrue},
		{map[string]string{"TERM": "xterm", "COLORTERM": "truecolor", "NO_COLOR": "1"}, ColorNone},
		{map[string]string{"TERM": "linux"}, ColorNone},
	} {
		if got := colorMode(func(k string) string { return tt.env[k] }); got != tt.want {
			t.Errorf("colorMode(%v) = %d, want %d", tt.env, got, tt.want)
		}
	}
	for term, want := range map[string]bool{"": false, "dumb": false, "xterm": true, "screen-256color": true} {
		if supported(term) != want {
			t.Errorf("supported(%q) = %v", term, !want)
		}
	}
}

func TestHighlight(t *testing.T) {
	lines := []string{
		"package main // c",
		"var s = `raw",
		"text` + \"x\"",
		"func f() int { return len(s) + 42 }",
	}
	spans := highlight(lines)
	word := func(line int, sp span) string { return lines[line][sp.start:sp.end] }
	find := func(line int, text string) (Style, bool) {
		for _, sp := range spans[line] {
			if word(line, sp) == text {
				return sp.st, true
			}
		}
		return Style{}, false
	}
	checks := []struct {
		line int
		text string
		fg   Color
	}{
		{0, "package", cKeyword}, {0, "main", cIdent}, {0, "// c", cComment},
		{1, "`raw", cString}, {2, "text`", cString}, {2, `"x"`, cString},
		{3, "func", cKeyword}, {3, "int", cBuiltin}, {3, "len", cBuiltin}, {3, "42", cNumber},
	}
	for _, c := range checks {
		st, ok := find(c.line, c.text)
		if !ok || st.Fg != c.fg {
			t.Errorf("line %d: %q colored %v (found %v), want %v; spans %v", c.line, c.text, st.Fg, ok, c.fg, spans[c.line])
		}
	}
	// Source that does not scan is still colored as far as it goes.
	if got := highlight([]string{"x := \"unterminated", "y"}); len(got) != 2 || len(got[1]) == 0 {
		t.Errorf("highlight of a bad string: %v", got)
	}
}
