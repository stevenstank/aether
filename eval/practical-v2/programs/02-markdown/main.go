// Command markdown renders a practical subset of Markdown (CommonMark
// blocks plus GFM tables, task lists, and strikethrough) to HTML.
//
// Usage: markdown [-toc] [-page title] [-stats] [file]
package main

import (
	"bufio"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"slices"
	"strings"
)

type renderer struct {
	doc *Document
	w   *strings.Builder
}

func (h *Heading) render(r *renderer) {
	fmt.Fprintf(r.w, "<h%d id=\"%s\">%s</h%d>\n", h.Level, h.ID, r.doc.inline(h.Text), h.Level)
}

func (p *Paragraph) render(r *renderer) {
	r.w.WriteString("<p>" + r.doc.inline(p.Text) + "</p>\n")
}

func (c *CodeBlock) render(r *renderer) {
	if c.Lang != "" {
		fmt.Fprintf(r.w, "<pre><code class=\"language-%s\">", html.EscapeString(c.Lang))
	} else {
		r.w.WriteString("<pre><code>")
	}
	r.w.WriteString(html.EscapeString(c.Code))
	r.w.WriteString("</code></pre>\n")
}

func (q *Quote) render(r *renderer) {
	r.w.WriteString("<blockquote>\n")
	for _, c := range q.Children {
		c.render(r)
	}
	r.w.WriteString("</blockquote>\n")
}

func (Rule) render(r *renderer) { r.w.WriteString("<hr />\n") }

func (h *HTMLBlock) render(r *renderer) { r.w.WriteString(h.Raw + "\n") }

func (l *List) render(r *renderer) {
	tag := "ul"
	if l.Ordered {
		tag = "ol"
	}
	if l.Ordered && l.Start != 1 {
		fmt.Fprintf(r.w, "<ol start=\"%d\">\n", l.Start)
	} else {
		r.w.WriteString("<" + tag + ">\n")
	}
	for _, it := range l.Items {
		r.w.WriteString("<li>")
		switch it.Task {
		case 1:
			r.w.WriteString(`<input type="checkbox" disabled="" /> `)
		case 2:
			r.w.WriteString(`<input type="checkbox" checked="" disabled="" /> `)
		}
		for i, c := range it.Children {
			if p, ok := c.(*Paragraph); ok && l.Tight {
				if i > 0 {
					r.w.WriteString("\n")
				}
				r.w.WriteString(r.doc.inline(p.Text))
				continue
			}
			if i == 0 {
				r.w.WriteString("\n")
			}
			c.render(r)
		}
		r.w.WriteString("</li>\n")
	}
	r.w.WriteString("</" + tag + ">\n")
}

func (t *Table) render(r *renderer) {
	cell := func(tag, align, text string) {
		if align != "" {
			fmt.Fprintf(r.w, "<%s style=\"text-align: %s\">%s</%s>\n", tag, align, r.doc.inline(text), tag)
		} else {
			fmt.Fprintf(r.w, "<%s>%s</%s>\n", tag, r.doc.inline(text), tag)
		}
	}
	r.w.WriteString("<table>\n<thead>\n<tr>\n")
	for i, h := range t.Header {
		cell("th", alignAt(t.Align, i), h)
	}
	r.w.WriteString("</tr>\n</thead>\n")
	if len(t.Rows) > 0 {
		r.w.WriteString("<tbody>\n")
		for _, row := range t.Rows {
			r.w.WriteString("<tr>\n")
			for i, c := range row {
				cell("td", alignAt(t.Align, i), c)
			}
			r.w.WriteString("</tr>\n")
		}
		r.w.WriteString("</tbody>\n")
	}
	r.w.WriteString("</table>\n")
}

func alignAt(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}

func (d *Document) HTML() string {
	var sb strings.Builder
	r := &renderer{doc: d, w: &sb}
	for _, b := range d.Blocks {
		b.render(r)
	}
	return sb.String()
}

// headings walks the whole tree, including headings inside quotes and lists.
func headings(nodes []Node, visit func(*Heading)) {
	for _, n := range nodes {
		switch n := n.(type) {
		case *Heading:
			visit(n)
		case *Quote:
			headings(n.Children, visit)
		case *List:
			for _, it := range n.Items {
				headings(it.Children, visit)
			}
		}
	}
}

func (d *Document) TOC() string {
	var sb strings.Builder
	sb.WriteString("<nav class=\"toc\">\n<ul>\n")
	headings(d.Blocks, func(h *Heading) {
		if h.Level > 3 {
			return
		}
		fmt.Fprintf(&sb, "%s<li><a href=\"#%s\">%s</a></li>\n", strings.Repeat("  ", h.Level-1), h.ID, html.EscapeString(stripMarkup(h.Text)))
	})
	sb.WriteString("</ul>\n</nav>\n")
	return sb.String()
}

type stats struct {
	counts map[string]int
	words  int
}

func (s *stats) walk(nodes []Node) {
	for _, n := range nodes {
		name := fmt.Sprintf("%T", n)
		name = strings.TrimPrefix(strings.TrimPrefix(name, "*"), "main.")
		s.counts[name]++
		switch n := n.(type) {
		case *Paragraph:
			s.words += len(strings.Fields(stripMarkup(n.Text)))
		case *Heading:
			s.words += len(strings.Fields(stripMarkup(n.Text)))
		case *Quote:
			s.walk(n.Children)
		case *List:
			for _, it := range n.Items {
				s.walk(it.Children)
			}
		}
	}
}

func main() {
	toc := flag.Bool("toc", false, "prepend a table of contents")
	page := flag.String("page", "", "wrap the output in a full HTML page with this title")
	showStats := flag.Bool("stats", false, "print block statistics to stderr")
	flag.Parse()

	var in io.Reader = os.Stdin
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "usage: markdown [-toc] [-page title] [-stats] [file]")
		os.Exit(2)
	}
	if flag.NArg() == 1 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "markdown:", err)
			os.Exit(1)
		}
		defer f.Close()
		in = f
	}
	src, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "markdown:", err)
		os.Exit(1)
	}
	doc := Parse(string(src))
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if *page != "" {
		fmt.Fprintf(w, "<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\" />\n<title>%s</title>\n</head>\n<body>\n", html.EscapeString(*page))
	}
	if *toc {
		w.WriteString(doc.TOC())
	}
	w.WriteString(doc.HTML())
	if *page != "" {
		w.WriteString("</body>\n</html>\n")
	}
	if *showStats {
		s := &stats{counts: map[string]int{}}
		s.walk(doc.Blocks)
		keys := make([]string, 0, len(s.counts))
		for k := range s.counts {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			fmt.Fprintf(os.Stderr, "%-10s %d\n", k, s.counts[k])
		}
		fmt.Fprintf(os.Stderr, "%-10s %d\nrefs       %d\n", "words", s.words, len(doc.Refs))
	}
}
