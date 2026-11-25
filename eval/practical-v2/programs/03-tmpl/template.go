package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Position is a 1-based line and column in a template source.
type Position struct {
	Name      string
	Line, Col int
}

func (p Position) String() string { return fmt.Sprintf("%s:%d:%d", p.Name, p.Line, p.Col) }

// TemplateError is returned for both parse and execution failures.
type TemplateError struct {
	Pos  Position
	Msg  string
	Err  error
	Exec bool
}

func (e *TemplateError) Error() string {
	kind := "parse"
	if e.Exec {
		kind = "exec"
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s error: %s: %v", e.Pos, kind, e.Msg, e.Err)
	}
	return fmt.Sprintf("%s: %s error: %s", e.Pos, kind, e.Msg)
}

func (e *TemplateError) Unwrap() error { return e.Err }

var ErrUnknownFilter = errors.New("unknown filter")

type node interface{ pos() Position }

type textNode struct {
	p    Position
	text string
}

type filterCall struct {
	name string
	args []expr
}

// expr is a path like user.name or items.0, or a literal.
type expr struct {
	path    []string
	literal any
	isLit   bool
}

type outputNode struct {
	p       Position
	value   expr
	filters []filterCall
	raw     bool
}

type ifNode struct {
	p        Position
	negate   bool
	cond     expr
	filters  []filterCall
	then     []node
	elseBody []node
}

type eachNode struct {
	p     Position
	list  expr
	as    string
	body  []node
	empty []node
}

type partialNode struct {
	p    Position
	name string
	ctx  *expr
}

type setNode struct {
	p       Position
	name    string
	value   expr
	filters []filterCall
}

func (n *textNode) pos() Position    { return n.p }
func (n *outputNode) pos() Position  { return n.p }
func (n *ifNode) pos() Position      { return n.p }
func (n *eachNode) pos() Position    { return n.p }
func (n *partialNode) pos() Position { return n.p }
func (n *setNode) pos() Position     { return n.p }

type Template struct {
	Name string
	root []node
}

type tag struct {
	p     Position
	body  string
	raw   bool
	trimL bool
	trimR bool
}

type parser struct {
	name string
	src  string
	off  int
	line int
	col  int
}

func (ps *parser) advance(n int) {
	for _, r := range ps.src[ps.off : ps.off+n] {
		if r == '\n' {
			ps.line++
			ps.col = 1
		} else {
			ps.col++
		}
	}
	ps.off += n
}

func (ps *parser) here() Position { return Position{ps.name, ps.line, ps.col} }

func (ps *parser) errf(p Position, format string, args ...any) error {
	return &TemplateError{Pos: p, Msg: fmt.Sprintf(format, args...)}
}

// lex splits the source into text and tag items.
func (ps *parser) lex() ([]any, error) {
	var items []any
	for ps.off < len(ps.src) {
		i := strings.Index(ps.src[ps.off:], "{{")
		if i < 0 {
			items = append(items, &textNode{p: ps.here(), text: ps.src[ps.off:]})
			ps.advance(len(ps.src) - ps.off)
			break
		}
		if i > 0 {
			items = append(items, &textNode{p: ps.here(), text: ps.src[ps.off : ps.off+i]})
			ps.advance(i)
		}
		start := ps.here()
		raw := strings.HasPrefix(ps.src[ps.off:], "{{{")
		open, closer := "{{", "}}"
		if raw {
			open, closer = "{{{", "}}}"
		}
		end := strings.Index(ps.src[ps.off+len(open):], closer)
		if end < 0 {
			return nil, ps.errf(start, "unclosed tag")
		}
		body := ps.src[ps.off+len(open) : ps.off+len(open)+end]
		t := &tag{p: start, raw: raw}
		if strings.HasPrefix(body, "-") {
			t.trimL, body = true, body[1:]
		}
		if strings.HasSuffix(body, "-") {
			t.trimR, body = true, body[:len(body)-1]
		}
		t.body = strings.TrimSpace(body)
		items = append(items, t)
		ps.advance(len(open) + end + len(closer))
	}
	// Apply whitespace trimming markers to neighboring text.
	for i, it := range items {
		t, ok := it.(*tag)
		if !ok {
			continue
		}
		if t.trimL && i > 0 {
			if tn, ok := items[i-1].(*textNode); ok {
				tn.text = strings.TrimRightFunc(tn.text, unicode.IsSpace)
			}
		}
		if t.trimR && i+1 < len(items) {
			if tn, ok := items[i+1].(*textNode); ok {
				tn.text = strings.TrimLeftFunc(tn.text, unicode.IsSpace)
			}
		}
	}
	return items, nil
}

// tokenize splits a tag body into words, keeping quoted strings intact.
func tokenize(s string) ([]string, error) {
	var toks []string
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				return nil, errors.New("unterminated string")
			}
			toks = append(toks, s[i:j+1])
			i = j + 1
		case c == '|':
			toks = append(toks, "|")
			i++
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t\n|\"", rune(s[j])) {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks, nil
}

func parseExpr(tok string) (expr, error) {
	switch {
	case strings.HasPrefix(tok, `"`):
		s, err := strconv.Unquote(tok)
		if err != nil {
			return expr{}, fmt.Errorf("bad string %s", tok)
		}
		return expr{literal: s, isLit: true}, nil
	case tok == "true" || tok == "false":
		return expr{literal: tok == "true", isLit: true}, nil
	case tok == "nil":
		return expr{isLit: true}, nil
	}
	if f, err := strconv.ParseFloat(tok, 64); err == nil && (tok[0] == '-' || unicode.IsDigit(rune(tok[0]))) {
		return expr{literal: f, isLit: true}, nil
	}
	if tok == "." || tok == "this" {
		return expr{}, nil
	}
	for _, part := range strings.Split(tok, ".") {
		if part == "" {
			return expr{}, fmt.Errorf("bad path %q", tok)
		}
	}
	return expr{path: strings.Split(tok, ".")}, nil
}

// parsePipeline parses "value | filter arg | filter".
func parsePipeline(toks []string) (expr, []filterCall, error) {
	if len(toks) == 0 {
		return expr{}, nil, errors.New("empty expression")
	}
	v, err := parseExpr(toks[0])
	if err != nil {
		return expr{}, nil, err
	}
	var fs []filterCall
	rest := toks[1:]
	for len(rest) > 0 {
		if rest[0] != "|" || len(rest) < 2 {
			return expr{}, nil, fmt.Errorf("expected '| filter' near %q", strings.Join(rest, " "))
		}
		fc := filterCall{name: rest[1]}
		rest = rest[2:]
		for len(rest) > 0 && rest[0] != "|" {
			a, err := parseExpr(rest[0])
			if err != nil {
				return expr{}, nil, err
			}
			fc.args = append(fc.args, a)
			rest = rest[1:]
		}
		if _, ok := filters[fc.name]; !ok {
			return expr{}, nil, fmt.Errorf("%w %q", ErrUnknownFilter, fc.name)
		}
		fs = append(fs, fc)
	}
	return v, fs, nil
}

func Parse(name, src string) (*Template, error) {
	ps := &parser{name: name, src: src, line: 1, col: 1}
	items, err := ps.lex()
	if err != nil {
		return nil, err
	}
	pos := 0
	root, stop, err := ps.parseList(items, &pos)
	if err != nil {
		return nil, err
	}
	if stop != nil {
		return nil, ps.errf(stop.p, "unexpected {{%s}}", stop.body)
	}
	return &Template{Name: name, root: root}, nil
}

// parseList parses nodes until a closing or else tag, which it returns.
func (ps *parser) parseList(items []any, pos *int) ([]node, *tag, error) {
	var out []node
	for *pos < len(items) {
		it := items[*pos]
		*pos++
		if tn, ok := it.(*textNode); ok {
			if tn.text != "" {
				out = append(out, tn)
			}
			continue
		}
		t := it.(*tag)
		if strings.HasPrefix(t.body, "!") {
			continue
		}
		if strings.HasPrefix(t.body, "/") || t.body == "else" {
			return out, t, nil
		}
		toks, err := tokenize(t.body)
		if err != nil {
			return nil, nil, ps.errf(t.p, "%v", err)
		}
		if len(toks) == 0 {
			return nil, nil, ps.errf(t.p, "empty tag")
		}
		switch toks[0] {
		case "#if", "#unless":
			cond, fs, err := parsePipeline(toks[1:])
			if err != nil {
				return nil, nil, ps.errf(t.p, "%s: %v", toks[0], err)
			}
			n := &ifNode{p: t.p, cond: cond, filters: fs, negate: toks[0] == "#unless"}
			var end *tag
			n.then, end, err = ps.parseList(items, pos)
			if err != nil {
				return nil, nil, err
			}
			if end != nil && end.body == "else" {
				n.elseBody, end, err = ps.parseList(items, pos)
				if err != nil {
					return nil, nil, err
				}
			}
			if end == nil || end.body != "/"+toks[0][1:] {
				return nil, nil, ps.errf(t.p, "%s is not closed with {{/%s}}", toks[0], toks[0][1:])
			}
			out = append(out, n)
		case "#each":
			if len(toks) != 2 && !(len(toks) == 4 && toks[2] == "as") {
				return nil, nil, ps.errf(t.p, "usage: {{#each list}} or {{#each list as name}}")
			}
			list, err := parseExpr(toks[1])
			if err != nil {
				return nil, nil, ps.errf(t.p, "%v", err)
			}
			n := &eachNode{p: t.p, list: list}
			if len(toks) == 4 {
				n.as = toks[3]
			}
			var end *tag
			n.body, end, err = ps.parseList(items, pos)
			if err != nil {
				return nil, nil, err
			}
			if end != nil && end.body == "else" {
				n.empty, end, err = ps.parseList(items, pos)
				if err != nil {
					return nil, nil, err
				}
			}
			if end == nil || end.body != "/each" {
				return nil, nil, ps.errf(t.p, "#each is not closed with {{/each}}")
			}
			out = append(out, n)
		case ">":
			if len(toks) < 2 || len(toks) > 3 {
				return nil, nil, ps.errf(t.p, "usage: {{> partial [context]}}")
			}
			n := &partialNode{p: t.p, name: toks[1]}
			if len(toks) == 3 {
				e, err := parseExpr(toks[2])
				if err != nil {
					return nil, nil, ps.errf(t.p, "%v", err)
				}
				n.ctx = &e
			}
			out = append(out, n)
		case "set":
			if len(toks) < 4 || toks[2] != "=" {
				return nil, nil, ps.errf(t.p, "usage: {{set name = value}}")
			}
			v, fs, err := parsePipeline(toks[3:])
			if err != nil {
				return nil, nil, ps.errf(t.p, "set: %v", err)
			}
			out = append(out, &setNode{p: t.p, name: toks[1], value: v, filters: fs})
		default:
			if strings.HasPrefix(toks[0], "#") {
				return nil, nil, ps.errf(t.p, "unknown block %s", toks[0])
			}
			v, fs, err := parsePipeline(toks)
			if err != nil {
				return nil, nil, ps.errf(t.p, "%v", err)
			}
			out = append(out, &outputNode{p: t.p, value: v, filters: fs, raw: t.raw})
		}
	}
	return out, nil, nil
}
