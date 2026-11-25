package main

import (
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const punctuation = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

// inlineParser converts inline Markdown to HTML.
type inlineParser struct {
	doc *Document
	src string
	pos int
	out strings.Builder
}

func (d *Document) inline(s string) string {
	p := &inlineParser{doc: d, src: s}
	p.run()
	return p.out.String()
}

// stripMarkup returns the plain text of inline Markdown, for slugs and
// alt text.
func stripMarkup(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '*', '_', '`', '[', ']':
			continue
		case '\\':
			if i+1 < len(s) && strings.IndexByte(punctuation, s[i+1]) >= 0 {
				i++
				b.WriteByte(s[i])
				continue
			}
		case '(':
			// Drop "(url)" right after a closing bracket.
			if i > 0 && s[i-1] == ']' {
				if j := strings.IndexByte(s[i:], ')'); j >= 0 {
					i += j
					continue
				}
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "#"
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https", "mailto", "ftp":
		return u.String()
	}
	return "#"
}

func (p *inlineParser) run() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\\' && p.pos+1 < len(p.src) && strings.IndexByte(punctuation, p.src[p.pos+1]) >= 0:
			p.out.WriteString(html.EscapeString(p.src[p.pos+1 : p.pos+2]))
			p.pos += 2
		case c == '\\' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '\n':
			p.out.WriteString("<br />\n")
			p.pos += 2
		case c == '`':
			p.codeSpan()
		case c == '*' || c == '_':
			p.emphasis()
		case c == '!' && strings.HasPrefix(p.src[p.pos:], "!["):
			if !p.link(true) {
				p.out.WriteString("!")
				p.pos++
			}
		case c == '[':
			if !p.link(false) {
				p.out.WriteString("[")
				p.pos++
			}
		case c == '<':
			p.angle()
		case c == '~' && strings.HasPrefix(p.src[p.pos:], "~~"):
			if end := strings.Index(p.src[p.pos+2:], "~~"); end > 0 {
				p.out.WriteString("<del>")
				p.out.WriteString(p.doc.inline(p.src[p.pos+2 : p.pos+2+end]))
				p.out.WriteString("</del>")
				p.pos += end + 4
			} else {
				p.out.WriteString("~")
				p.pos++
			}
		case c == '\n':
			// Two trailing spaces make a hard break.
			text := p.out.String()
			if strings.HasSuffix(text, "  ") {
				trimmed := strings.TrimRight(text, " ")
				p.out.Reset()
				p.out.WriteString(trimmed)
				p.out.WriteString("<br />\n")
			} else {
				p.out.WriteByte('\n')
			}
			p.pos++
		case c == '&':
			// Keep entity references, escape a bare ampersand.
			if end := strings.IndexByte(p.src[p.pos:], ';'); end > 1 && end < 10 && isEntityName(p.src[p.pos+1:p.pos+end]) {
				p.out.WriteString(p.src[p.pos : p.pos+end+1])
				p.pos += end + 1
			} else {
				p.out.WriteString("&amp;")
				p.pos++
			}
		default:
			r, size := utf8.DecodeRuneInString(p.src[p.pos:])
			if r == utf8.RuneError && size == 1 {
				p.out.WriteString("�")
			} else {
				p.out.WriteString(html.EscapeString(p.src[p.pos : p.pos+size]))
			}
			p.pos += size
		}
	}
}

func isEntityName(s string) bool {
	if strings.HasPrefix(s, "#") {
		s = strings.TrimPrefix(strings.TrimPrefix(s[1:], "x"), "X")
		if s == "" {
			return false
		}
		for _, r := range s {
			if !unicode.Is(unicode.ASCII_Hex_Digit, r) {
				return false
			}
		}
		return true
	}
	for _, r := range s {
		if !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r))) {
			return false
		}
	}
	return s != ""
}

func (p *inlineParser) codeSpan() {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] == '`' {
		p.pos++
	}
	ticks := p.src[start:p.pos]
	for j := p.pos; j < len(p.src); {
		k := strings.Index(p.src[j:], ticks)
		if k < 0 {
			break
		}
		k += j
		end := k + len(ticks)
		if end < len(p.src) && p.src[end] == '`' {
			for end < len(p.src) && p.src[end] == '`' {
				end++
			}
			j = end
			continue
		}
		code := strings.ReplaceAll(p.src[p.pos:k], "\n", " ")
		if len(code) > 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.Trim(code, " ") != "" {
			code = code[1 : len(code)-1]
		}
		p.out.WriteString("<code>" + html.EscapeString(code) + "</code>")
		p.pos = end
		return
	}
	p.out.WriteString(ticks)
}

// emphasis handles *em*, **strong**, and ***both*** with a simple
// matching-delimiter search; underscores do not open emphasis inside words.
func (p *inlineParser) emphasis() {
	c := p.src[p.pos]
	n := 0
	for p.pos+n < len(p.src) && p.src[p.pos+n] == c && n < 3 {
		n++
	}
	if c == '_' && p.pos > 0 {
		prev, _ := utf8.DecodeLastRuneInString(p.src[:p.pos])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) {
			p.out.WriteString(p.src[p.pos : p.pos+n])
			p.pos += n
			return
		}
	}
	after := p.pos + n
	if after >= len(p.src) || p.src[after] == ' ' || p.src[after] == '\n' {
		p.out.WriteString(p.src[p.pos : p.pos+n])
		p.pos += n
		return
	}
	delim := strings.Repeat(string(c), n)
	for n > 0 {
		delim = strings.Repeat(string(c), n)
		if end := findCloser(p.src, after, delim); end >= 0 {
			inner := p.doc.inline(p.src[after:end])
			switch n {
			case 1:
				p.out.WriteString("<em>" + inner + "</em>")
			case 2:
				p.out.WriteString("<strong>" + inner + "</strong>")
			case 3:
				p.out.WriteString("<em><strong>" + inner + "</strong></em>")
			}
			p.pos = end + n
			return
		}
		n--
		after--
		p.out.WriteByte(c)
		p.pos++
	}
}

func findCloser(s string, from int, delim string) int {
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
			continue
		case '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				i += j + 1
			}
			continue
		}
		if !strings.HasPrefix(s[i:], delim) || s[i-1] == ' ' || s[i-1] == '\n' {
			continue
		}
		end := i + len(delim)
		if end < len(s) && s[end] == delim[0] {
			// Part of a longer run; skip it.
			for end < len(s) && s[end] == delim[0] {
				end++
			}
			if end-i != len(delim) {
				i = end - 1
				continue
			}
		}
		if delim[0] == '_' && end < len(s) {
			r, _ := utf8.DecodeRuneInString(s[end:])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
		}
		return i
	}
	return -1
}

// link parses [text](url "title"), [text][ref], [ref], and the image forms.
func (p *inlineParser) link(image bool) bool {
	start := p.pos
	open := p.pos
	if image {
		open++
	}
	depth := 0
	closeAt := -1
	for j := open; j < len(p.src); j++ {
		switch p.src[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				closeAt = j
			}
		}
		if closeAt >= 0 {
			break
		}
	}
	if closeAt < 0 {
		return false
	}
	text := p.src[open+1 : closeAt]
	rest := p.src[closeAt+1:]
	var dest, title string
	consumed := 0
	switch {
	case strings.HasPrefix(rest, "("):
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return false
		}
		inside := strings.TrimSpace(rest[1:end])
		dest = inside
		if k := strings.IndexAny(inside, " \t"); k >= 0 {
			dest = inside[:k]
			t := strings.TrimSpace(inside[k:])
			if len(t) >= 2 && (t[0] == '"' || t[0] == '\'') && t[len(t)-1] == t[0] {
				title = t[1 : len(t)-1]
			} else {
				return false
			}
		}
		dest = strings.TrimSuffix(strings.TrimPrefix(dest, "<"), ">")
		consumed = end + 1
	case strings.HasPrefix(rest, "["):
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return false
		}
		label := rest[1:end]
		if label == "" {
			label = text
		}
		def, ok := p.doc.Refs[normalizeLabel(label)]
		if !ok {
			return false
		}
		dest, title = def.URL, def.Title
		consumed = end + 1
	default:
		def, ok := p.doc.Refs[normalizeLabel(text)]
		if !ok {
			return false
		}
		dest, title = def.URL, def.Title
	}
	p.pos = closeAt + 1 + consumed
	attrTitle := ""
	if title != "" {
		attrTitle = ` title="` + html.EscapeString(title) + `"`
	}
	if image {
		p.out.WriteString(`<img src="` + html.EscapeString(safeURL(dest)) + `" alt="` + html.EscapeString(stripMarkup(text)) + `"` + attrTitle + ` />`)
	} else {
		p.out.WriteString(`<a href="` + html.EscapeString(safeURL(dest)) + `"` + attrTitle + `>` + p.doc.inline(text) + `</a>`)
	}
	_ = start
	return true
}

// angle handles autolinks like <https://example.com> and <a@b.org>;
// anything else is an escaped '<'.
func (p *inlineParser) angle() {
	end := strings.IndexByte(p.src[p.pos:], '>')
	if end > 1 {
		inner := p.src[p.pos+1 : p.pos+end]
		if !strings.ContainsAny(inner, " <\n") {
			if u, err := url.Parse(inner); err == nil && u.Scheme != "" && u.Host != "" {
				p.out.WriteString(`<a href="` + html.EscapeString(u.String()) + `">` + html.EscapeString(inner) + `</a>`)
				p.pos += end + 1
				return
			}
			if at := strings.IndexByte(inner, '@'); at > 0 && strings.Contains(inner[at:], ".") {
				p.out.WriteString(`<a href="mailto:` + html.EscapeString(inner) + `">` + html.EscapeString(inner) + `</a>`)
				p.pos += end + 1
				return
			}
		}
	}
	p.out.WriteString("&lt;")
	p.pos++
}
