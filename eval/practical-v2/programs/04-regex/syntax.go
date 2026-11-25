package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type nodeKind int

const (
	nLit nodeKind = iota
	nClass
	nAny
	nConcat
	nAlt
	nRepeat
	nGroup
	nBegin
	nEnd
	nWordB
	nNotWordB
	nEmpty
)

type runeRange struct{ lo, hi rune }

type node struct {
	kind     nodeKind
	r        rune
	ranges   []runeRange // nClass, sorted and merged
	negate   bool
	subs     []*node
	min, max int // nRepeat; max -1 means unbounded
	greedy   bool
	capIndex int // nGroup: 0 for non-capturing
	name     string
}

// SyntaxError reports a malformed pattern.
type SyntaxError struct {
	Pattern string
	Offset  int
	Msg     string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("bad pattern at offset %d: %s: %q", e.Offset, e.Msg, e.Pattern)
}

type flags struct {
	foldCase  bool
	multiLine bool
	dotNL     bool
}

type parser struct {
	src   string
	pos   int
	ncap  int
	names []string
	fl    flags
}

const maxRepeat = 1000

func parse(pattern string, fl flags) (*node, int, []string, error) {
	p := &parser{src: pattern, fl: fl, names: []string{""}}
	n, err := p.alternation()
	if err != nil {
		return nil, 0, nil, err
	}
	if p.pos < len(p.src) {
		return nil, 0, nil, p.errf("unexpected )")
	}
	return n, p.ncap, p.names, nil
}

func (p *parser) errf(format string, args ...any) error {
	return &SyntaxError{Pattern: p.src, Offset: p.pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) peek() (rune, bool) {
	if p.pos >= len(p.src) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(p.src[p.pos:])
	return r, true
}

func (p *parser) next() rune {
	r, n := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += n
	return r
}

func (p *parser) alternation() (*node, error) {
	var alts []*node
	for {
		c, err := p.concat()
		if err != nil {
			return nil, err
		}
		alts = append(alts, c)
		if r, ok := p.peek(); !ok || r != '|' {
			break
		}
		p.next()
	}
	if len(alts) == 1 {
		return alts[0], nil
	}
	return &node{kind: nAlt, subs: alts}, nil
}

func (p *parser) concat() (*node, error) {
	var items []*node
	for {
		r, ok := p.peek()
		if !ok || r == '|' || r == ')' {
			break
		}
		atom, err := p.atom()
		if err != nil {
			return nil, err
		}
		if atom == nil {
			continue // flag group like (?i)
		}
		atom, err = p.repeat(atom)
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	switch len(items) {
	case 0:
		return &node{kind: nEmpty}, nil
	case 1:
		return items[0], nil
	}
	return &node{kind: nConcat, subs: items}, nil
}

func (p *parser) repeat(atom *node) (*node, error) {
	for {
		r, ok := p.peek()
		if !ok {
			return atom, nil
		}
		start := p.pos
		min, max := 0, 0
		switch r {
		case '*':
			p.next()
			min, max = 0, -1
		case '+':
			p.next()
			min, max = 1, -1
		case '?':
			p.next()
			min, max = 0, 1
		case '{':
			lo, hi, ok := p.braces()
			if !ok {
				p.pos = start
				return atom, nil // literal '{'
			}
			if lo > maxRepeat || hi > maxRepeat || (hi >= 0 && hi < lo) {
				p.pos = start
				return nil, p.errf("invalid repeat count")
			}
			min, max = lo, hi
		default:
			return atom, nil
		}
		switch atom.kind {
		case nBegin, nEnd, nWordB, nNotWordB, nEmpty:
			p.pos = start
			return nil, p.errf("missing argument to repetition operator")
		case nRepeat:
			p.pos = start
			return nil, p.errf("invalid nested repetition operator")
		}
		greedy := true
		if r, ok := p.peek(); ok && r == '?' {
			p.next()
			greedy = false
		}
		atom = &node{kind: nRepeat, subs: []*node{atom}, min: min, max: max, greedy: greedy}
	}
}

// braces parses {n}, {n,}, or {n,m}; ok is false if it is not a repeat.
func (p *parser) braces() (lo, hi int, ok bool) {
	end := strings.IndexByte(p.src[p.pos:], '}')
	if end < 0 {
		return 0, 0, false
	}
	body := p.src[p.pos+1 : p.pos+end]
	a, b, comma := strings.Cut(body, ",")
	lo, err := strconv.Atoi(a)
	if err != nil || a == "" || a[0] == '+' || a[0] == '-' {
		return 0, 0, false
	}
	hi = lo
	if comma {
		if b == "" {
			hi = -1
		} else if hi, err = strconv.Atoi(b); err != nil || b[0] == '+' || b[0] == '-' {
			return 0, 0, false
		}
	}
	p.pos += end + 1
	return lo, hi, true
}

func (p *parser) atom() (*node, error) {
	r := p.next()
	switch r {
	case '(':
		return p.group()
	case '[':
		return p.class()
	case '.':
		if p.fl.dotNL {
			return &node{kind: nAny}, nil
		}
		return &node{kind: nClass, ranges: []runeRange{{'\n', '\n'}}, negate: true}, nil
	case '^':
		if p.fl.multiLine {
			return &node{kind: nBegin, r: 'm'}, nil
		}
		return &node{kind: nBegin}, nil
	case '$':
		if p.fl.multiLine {
			return &node{kind: nEnd, r: 'm'}, nil
		}
		return &node{kind: nEnd}, nil
	case '\\':
		return p.escape(false)
	case '*', '+', '?':
		p.pos--
		return nil, p.errf("missing argument to repetition operator")
	}
	return p.literal(r), nil
}

func (p *parser) literal(r rune) *node {
	if p.fl.foldCase {
		if rs := foldSet(r); len(rs) > 1 {
			return &node{kind: nClass, ranges: normalize(rs)}
		}
	}
	return &node{kind: nLit, r: r}
}

func foldSet(r rune) []runeRange {
	out := []runeRange{{r, r}}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, runeRange{f, f})
	}
	return out
}

func (p *parser) group() (*node, error) {
	start := p.pos - 1
	capIndex, name := 0, ""
	if strings.HasPrefix(p.src[p.pos:], "?") {
		p.next()
		switch {
		case strings.HasPrefix(p.src[p.pos:], "P<") || strings.HasPrefix(p.src[p.pos:], "<"):
			if p.src[p.pos] == 'P' {
				p.pos++
			}
			end := strings.IndexByte(p.src[p.pos:], '>')
			if end < 2 {
				return nil, p.errf("invalid named capture")
			}
			name = p.src[p.pos+1 : p.pos+end]
			for _, c := range name {
				if !(c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c)) {
					return nil, p.errf("invalid named capture")
				}
			}
			for _, n := range p.names {
				if n == name {
					return nil, p.errf("duplicate capture group name %q", name)
				}
			}
			p.pos += end + 1
			p.ncap++
			capIndex = p.ncap
			p.names = append(p.names, name)
		default:
			// Flags: (?flags) or (?flags:re)
			saved := p.fl
			on := true
			for {
				r, ok := p.peek()
				if !ok {
					return nil, p.errf("missing closing )")
				}
				p.next()
				switch r {
				case 'i':
					p.fl.foldCase = on
				case 'm':
					p.fl.multiLine = on
				case 's':
					p.fl.dotNL = on
				case '-':
					on = false
				case ')':
					return nil, nil // flags apply to the rest of the group
				case ':':
					sub, err := p.alternation()
					if err != nil {
						return nil, err
					}
					if r, ok := p.peek(); !ok || r != ')' {
						p.pos = start
						return nil, p.errf("missing closing )")
					}
					p.next()
					p.fl = saved
					return &node{kind: nGroup, subs: []*node{sub}}, nil
				default:
					return nil, p.errf("invalid or unsupported Perl syntax")
				}
			}
		}
	} else {
		p.ncap++
		capIndex = p.ncap
		p.names = append(p.names, "")
	}
	saved := p.fl
	sub, err := p.alternation()
	if err != nil {
		return nil, err
	}
	p.fl = saved
	if r, ok := p.peek(); !ok || r != ')' {
		p.pos = start
		return nil, p.errf("missing closing )")
	}
	p.next()
	return &node{kind: nGroup, subs: []*node{sub}, capIndex: capIndex, name: name}, nil
}

var (
	digitRanges = []runeRange{{'0', '9'}}
	wordRanges  = []runeRange{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}
	spaceRanges = []runeRange{{'\t', '\n'}, {'\f', '\r'}, {' ', ' '}}
)

// escape parses the character after a backslash. In a class, assertions
// are not allowed.
func (p *parser) escape(inClass bool) (*node, error) {
	if p.pos >= len(p.src) {
		return nil, p.errf("trailing backslash at end of expression")
	}
	r := p.next()
	switch r {
	case 'd', 'D':
		return &node{kind: nClass, ranges: digitRanges, negate: r == 'D'}, nil
	case 'w', 'W':
		return &node{kind: nClass, ranges: wordRanges, negate: r == 'W'}, nil
	case 's', 'S':
		return &node{kind: nClass, ranges: spaceRanges, negate: r == 'S'}, nil
	case 'b', 'B', 'A', 'z':
		if inClass {
			return nil, p.errf("invalid escape sequence \\%c", r)
		}
		switch r {
		case 'b':
			return &node{kind: nWordB}, nil
		case 'B':
			return &node{kind: nNotWordB}, nil
		case 'A':
			return &node{kind: nBegin, r: 'A'}, nil
		}
		return &node{kind: nEnd, r: 'z'}, nil
	case 'n':
		return p.literal('\n'), nil
	case 't':
		return p.literal('\t'), nil
	case 'r':
		return p.literal('\r'), nil
	case 'f':
		return p.literal('\f'), nil
	case 'v':
		return p.literal('\v'), nil
	case 'x':
		if strings.HasPrefix(p.src[p.pos:], "{") {
			end := strings.IndexByte(p.src[p.pos:], '}')
			if end < 0 {
				return nil, p.errf("invalid escape sequence")
			}
			v, err := strconv.ParseUint(p.src[p.pos+1:p.pos+end], 16, 32)
			if err != nil || v > unicode.MaxRune {
				return nil, p.errf("invalid escape sequence")
			}
			p.pos += end + 1
			return p.literal(rune(v)), nil
		}
		if p.pos+2 > len(p.src) {
			return nil, p.errf("invalid escape sequence")
		}
		v, err := strconv.ParseUint(p.src[p.pos:p.pos+2], 16, 8)
		if err != nil {
			return nil, p.errf("invalid escape sequence")
		}
		p.pos += 2
		return p.literal(rune(v)), nil
	}
	if r < utf8.RuneSelf && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return p.literal(r), nil
	}
	p.pos -= utf8.RuneLen(r) + 1
	return nil, p.errf("invalid escape sequence \\%c", r)
}

func (p *parser) class() (*node, error) {
	start := p.pos - 1
	n := &node{kind: nClass}
	if strings.HasPrefix(p.src[p.pos:], "^") {
		p.next()
		n.negate = true
	}
	var rs []runeRange
	first := true
	for {
		if p.pos >= len(p.src) {
			p.pos = start
			return nil, p.errf("missing closing ]")
		}
		r := p.next()
		if r == ']' && !first {
			break
		}
		first = false
		if r == '[' && strings.HasPrefix(p.src[p.pos:], ":") {
			end := strings.Index(p.src[p.pos:], ":]")
			if end > 0 {
				name := p.src[p.pos+1 : p.pos+end]
				cls, ok := posixClasses[strings.TrimPrefix(name, "^")]
				if !ok {
					return nil, p.errf("invalid character class range")
				}
				if strings.HasPrefix(name, "^") {
					cls = invert(normalize(cls))
				}
				rs = append(rs, cls...)
				p.pos += end + 2
				continue
			}
		}
		lo := r
		if r == '\\' {
			e, err := p.escape(true)
			if err != nil {
				return nil, err
			}
			if e.kind == nClass {
				cls := e.ranges
				if e.negate {
					cls = invert(cls)
				}
				rs = append(rs, cls...)
				continue
			}
			lo = e.r
			if len(e.ranges) > 0 { // case-folded literal
				lo = e.ranges[0].lo
				rs = append(rs, e.ranges...)
			}
		}
		hi := lo
		if strings.HasPrefix(p.src[p.pos:], "-") && !strings.HasPrefix(p.src[p.pos:], "-]") && p.pos+1 < len(p.src) {
			p.next()
			hr := p.next()
			if hr == '\\' {
				e, err := p.escape(true)
				if err != nil {
					return nil, err
				}
				if e.kind == nClass {
					return nil, p.errf("invalid character class range")
				}
				hr = e.r
			}
			hi = hr
			if hi < lo {
				return nil, p.errf("invalid character class range")
			}
		}
		rs = append(rs, runeRange{lo, hi})
		if p.fl.foldCase {
			rs = append(rs, foldRange(lo, hi)...)
		}
	}
	n.ranges = normalize(rs)
	return n, nil
}

var posixClasses = map[string][]runeRange{
	"alpha":  {{'A', 'Z'}, {'a', 'z'}},
	"digit":  {{'0', '9'}},
	"alnum":  {{'0', '9'}, {'A', 'Z'}, {'a', 'z'}},
	"space":  {{'\t', '\r'}, {' ', ' '}},
	"upper":  {{'A', 'Z'}},
	"lower":  {{'a', 'z'}},
	"punct":  {{'!', '/'}, {':', '@'}, {'[', '`'}, {'{', '~'}},
	"xdigit": {{'0', '9'}, {'A', 'F'}, {'a', 'f'}},
	"word":   {{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}},
}

func foldRange(lo, hi rune) []runeRange {
	var out []runeRange
	if hi-lo > 5000 {
		hi = lo + 5000
	}
	for r := lo; r <= hi; r++ {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			out = append(out, runeRange{f, f})
		}
	}
	return out
}

func normalize(rs []runeRange) []runeRange {
	if len(rs) == 0 {
		return nil
	}
	s := append([]runeRange(nil), rs...)
	// insertion sort is fine for the small classes patterns use
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].lo < s[j-1].lo; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	out := s[:1]
	for _, r := range s[1:] {
		last := &out[len(out)-1]
		if r.lo <= last.hi+1 {
			last.hi = max(last.hi, r.hi)
		} else {
			out = append(out, r)
		}
	}
	return out
}

func invert(rs []runeRange) []runeRange {
	var out []runeRange
	next := rune(0)
	for _, r := range rs {
		if r.lo > next {
			out = append(out, runeRange{next, r.lo - 1})
		}
		next = r.hi + 1
	}
	if next <= unicode.MaxRune {
		out = append(out, runeRange{next, unicode.MaxRune})
	}
	return out
}
