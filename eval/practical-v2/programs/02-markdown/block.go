package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Node is a block-level element of a parsed document.
type Node interface {
	render(r *renderer)
}

type (
	Heading struct {
		Level int
		Text  string
		ID    string
	}
	Paragraph struct{ Text string }
	CodeBlock struct{ Lang, Code string }
	Quote     struct{ Children []Node }
	Rule      struct{}
	HTMLBlock struct{ Raw string }
	ListItem  struct {
		Children []Node
		Task     int // 0: not a task, 1: unchecked, 2: checked
	}
	List struct {
		Ordered bool
		Start   int
		Tight   bool
		Items   []*ListItem
	}
	Table struct {
		Align  []string
		Header []string
		Rows   [][]string
	}
)

type linkDef struct{ URL, Title string }

type Document struct {
	Blocks []Node
	Refs   map[string]linkDef
	ids    map[string]int
}

var (
	atxRE      = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	ruleRE     = regexp.MustCompile(`^ {0,3}((?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	fenceRE    = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})[ \t]*([^`]*)$")
	bulletRE   = regexp.MustCompile(`^( {0,3})([-+*])([ \t]+|$)`)
	orderedRE  = regexp.MustCompile(`^( {0,3})(\d{1,9})([.)])([ \t]+|$)`)
	refDefRE   = regexp.MustCompile(`^ {0,3}\[([^\]]+)\]:[ \t]*<?(\S+?)>?(?:[ \t]+(?:"([^"]*)"|'([^']*)'|\(([^)]*)\)))?[ \t]*$`)
	tableSepRE = regexp.MustCompile(`^\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	htmlRE     = regexp.MustCompile(`^ {0,3}<(/?)(div|p|table|pre|section|details|summary|ul|ol|blockquote|h[1-6]|!--)`)
	taskRE     = regexp.MustCompile(`^\[([ xX])\][ \t]+`)
)

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

// expandTabs replaces tabs with spaces to the next multiple of four.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

func indentOf(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

func normalizeLabel(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func Parse(src string) *Document {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, l := range lines {
		lines[i] = expandTabs(l)
	}
	d := &Document{Refs: map[string]linkDef{}, ids: map[string]int{}}
	d.Blocks = d.parseBlocks(lines)
	return d
}

func (d *Document) slug(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(stripMarkup(text)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case r == ' ' || r == '-' || r == '_':
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	id := strings.TrimSuffix(b.String(), "-")
	if id == "" {
		id = "section"
	}
	n := d.ids[id]
	d.ids[id] = n + 1
	if n > 0 {
		id += "-" + strconv.Itoa(n)
	}
	return id
}

func (d *Document) parseBlocks(lines []string) []Node {
	var out []Node
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, &Paragraph{Text: strings.TrimSpace(strings.Join(para, "\n"))})
			para = nil
		}
	}
	for i := 0; i < len(lines); {
		line := lines[i]
		if isBlank(line) {
			flush()
			i++
			continue
		}
		// Setext headings underline a paragraph.
		if len(para) > 0 && indentOf(line) < 4 {
			t := strings.TrimSpace(line)
			if strings.Trim(t, "=") == "" || (strings.Trim(t, "-") == "" && len(para) > 0) {
				level := 1
				if t[0] == '-' {
					level = 2
				}
				text := strings.TrimSpace(strings.Join(para, "\n"))
				para = nil
				out = append(out, &Heading{Level: level, Text: text, ID: d.slug(text)})
				i++
				continue
			}
		}
		if m := atxRE.FindStringSubmatch(line); m != nil {
			flush()
			out = append(out, &Heading{Level: len(m[1]), Text: m[2], ID: d.slug(m[2])})
			i++
			continue
		}
		if ruleRE.MatchString(line) {
			flush()
			out = append(out, Rule{})
			i++
			continue
		}
		if m := fenceRE.FindStringSubmatch(line); m != nil {
			flush()
			indent, fence := len(m[1]), m[2]
			var code []string
			i++
			for ; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				if strings.HasPrefix(t, fence[:1]) && strings.Trim(t, fence[:1]) == "" && len(t) >= len(fence) {
					i++
					break
				}
				l := lines[i]
				k := min(indent, indentOf(l))
				code = append(code, l[k:])
			}
			lang := strings.Fields(m[3] + " ")
			cb := &CodeBlock{Code: strings.Join(code, "\n")}
			if len(code) > 0 {
				cb.Code += "\n"
			}
			if len(lang) > 0 {
				cb.Lang = lang[0]
			}
			out = append(out, cb)
			continue
		}
		if indentOf(line) >= 4 && len(para) == 0 {
			var code []string
			for i < len(lines) && (indentOf(lines[i]) >= 4 || isBlank(lines[i])) {
				if isBlank(lines[i]) {
					code = append(code, "")
				} else {
					code = append(code, lines[i][4:])
				}
				i++
			}
			for len(code) > 0 && code[len(code)-1] == "" {
				code = code[:len(code)-1]
			}
			out = append(out, &CodeBlock{Code: strings.Join(code, "\n") + "\n"})
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(line, " "), ">") {
			flush()
			var inner []string
			for i < len(lines) && !isBlank(lines[i]) {
				t := strings.TrimLeft(lines[i], " ")
				if strings.HasPrefix(t, ">") {
					t = strings.TrimPrefix(t[1:], " ")
				} else if len(inner) == 0 {
					break
				}
				inner = append(inner, t)
				i++
			}
			out = append(out, &Quote{Children: d.parseBlocks(inner)})
			continue
		}
		if htmlRE.MatchString(line) && len(para) == 0 {
			var raw []string
			for i < len(lines) && !isBlank(lines[i]) {
				raw = append(raw, lines[i])
				i++
			}
			out = append(out, &HTMLBlock{Raw: strings.Join(raw, "\n")})
			continue
		}
		if bulletRE.MatchString(line) || orderedRE.MatchString(line) {
			flush()
			var list *List
			list, i = d.parseList(lines, i)
			out = append(out, list)
			continue
		}
		if len(para) == 0 {
			if m := refDefRE.FindStringSubmatch(line); m != nil {
				label := normalizeLabel(m[1])
				if _, dup := d.Refs[label]; !dup {
					d.Refs[label] = linkDef{URL: m[2], Title: m[3] + m[4] + m[5]}
				}
				i++
				continue
			}
			if strings.Contains(line, "|") && i+1 < len(lines) && tableSepRE.MatchString(lines[i+1]) {
				var t *Table
				t, i = parseTable(lines, i)
				out = append(out, t)
				continue
			}
		}
		para = append(para, strings.TrimLeft(line, " "))
		i++
	}
	flush()
	return out
}

func listMarker(line string) (ordered bool, start, width int, delim string, ok bool) {
	if m := bulletRE.FindStringSubmatch(line); m != nil {
		w := len(m[0])
		if m[3] == "" {
			w++
		}
		return false, 0, w, m[2], true
	}
	if m := orderedRE.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[2])
		w := len(m[0])
		if m[4] == "" {
			w++
		}
		return true, n, w, m[3], true
	}
	return
}

func (d *Document) parseList(lines []string, i int) (*List, int) {
	ordered, start, _, delim, _ := listMarker(lines[i])
	list := &List{Ordered: ordered, Start: start, Tight: true}
	sawBlank := false
	for i < len(lines) {
		o, _, width, dl, ok := listMarker(lines[i])
		if !ok || o != ordered || dl != delim {
			break
		}
		if sawBlank {
			list.Tight = false
		}
		var body []string
		first := ""
		if width <= len(lines[i]) {
			first = lines[i][width:]
		}
		item := &ListItem{}
		if m := taskRE.FindStringSubmatch(first); m != nil {
			item.Task = 1
			if m[1] != " " {
				item.Task = 2
			}
			first = first[len(m[0]):]
		}
		body = append(body, first)
		i++
		sawBlank = false
		for i < len(lines) {
			l := lines[i]
			if isBlank(l) {
				sawBlank = true
				body = append(body, "")
				i++
				continue
			}
			if indentOf(l) >= width {
				if sawBlank {
					list.Tight = false
				}
				body = append(body, l[width:])
				sawBlank = false
				i++
				continue
			}
			if sawBlank {
				break
			}
			if _, _, _, _, isItem := listMarker(l); isItem || ruleRE.MatchString(l) {
				break
			}
			body = append(body, strings.TrimLeft(l, " ")) // lazy continuation
			i++
		}
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		item.Children = d.parseBlocks(body)
		list.Items = append(list.Items, item)
		if sawBlank && (i >= len(lines) || isBlank(lines[i])) {
			break
		}
	}
	return list, i
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cur strings.Builder
	for j := 0; j < len(line); j++ {
		if line[j] == '\\' && j+1 < len(line) && line[j+1] == '|' {
			cur.WriteByte('|')
			j++
			continue
		}
		if line[j] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(line[j])
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func parseTable(lines []string, i int) (*Table, int) {
	t := &Table{Header: splitRow(lines[i])}
	for _, spec := range splitRow(lines[i+1]) {
		left, right := strings.HasPrefix(spec, ":"), strings.HasSuffix(spec, ":")
		switch {
		case left && right:
			t.Align = append(t.Align, "center")
		case right:
			t.Align = append(t.Align, "right")
		case left:
			t.Align = append(t.Align, "left")
		default:
			t.Align = append(t.Align, "")
		}
	}
	i += 2
	for i < len(lines) && !isBlank(lines[i]) && strings.Contains(lines[i], "|") {
		row := splitRow(lines[i])
		for len(row) < len(t.Header) {
			row = append(row, "")
		}
		t.Rows = append(t.Rows, row[:len(t.Header)])
		i++
	}
	return t, i
}
