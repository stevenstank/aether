package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type opcode uint8

const (
	opChar opcode = iota
	opClass
	opAny
	opSplit // try x first, then y
	opJmp
	opSave
	opAssert
	opMatch
)

type inst struct {
	op     opcode
	r      rune
	ranges []runeRange
	negate bool
	x, y   int
	n      int      // opSave slot
	assert nodeKind // opAssert
	multi  bool
}

// Regexp is a compiled pattern, executed by a Pike VM with leftmost-first
// (Perl-like) match priorities.
type Regexp struct {
	expr   string
	prog   []inst
	ncap   int
	names  []string
	prefix string // literal prefix for fast scanning, if anchored to none
}

func Compile(expr string, foldCase bool) (*Regexp, error) {
	n, ncap, names, err := parse(expr, flags{foldCase: foldCase})
	if err != nil {
		return nil, err
	}
	c := &compiler{}
	c.emit(inst{op: opSave, n: 0})
	c.compile(n)
	c.emit(inst{op: opSave, n: 1})
	c.emit(inst{op: opMatch})
	re := &Regexp{expr: expr, prog: c.prog, ncap: ncap, names: names}
	re.prefix = literalPrefix(n)
	return re, nil
}

func literalPrefix(n *node) string {
	var b strings.Builder
	var walk func(n *node) bool
	walk = func(n *node) bool {
		switch n.kind {
		case nLit:
			b.WriteRune(n.r)
			return true
		case nConcat:
			for _, s := range n.subs {
				if !walk(s) {
					return false
				}
			}
			return true
		case nGroup:
			return walk(n.subs[0])
		}
		return false
	}
	walk(n)
	return b.String()
}

type compiler struct{ prog []inst }

func (c *compiler) emit(i inst) int {
	c.prog = append(c.prog, i)
	return len(c.prog) - 1
}

func (c *compiler) compile(n *node) {
	switch n.kind {
	case nLit:
		c.emit(inst{op: opChar, r: n.r})
	case nClass:
		c.emit(inst{op: opClass, ranges: n.ranges, negate: n.negate})
	case nAny:
		c.emit(inst{op: opAny})
	case nEmpty:
	case nBegin, nEnd, nWordB, nNotWordB:
		c.emit(inst{op: opAssert, assert: n.kind, multi: n.r == 'm'})
	case nConcat:
		for _, s := range n.subs {
			c.compile(s)
		}
	case nGroup:
		if n.capIndex > 0 {
			c.emit(inst{op: opSave, n: 2 * n.capIndex})
			c.compile(n.subs[0])
			c.emit(inst{op: opSave, n: 2*n.capIndex + 1})
		} else {
			c.compile(n.subs[0])
		}
	case nAlt:
		var jumps []int
		for i, s := range n.subs {
			if i < len(n.subs)-1 {
				split := c.emit(inst{op: opSplit})
				c.prog[split].x = len(c.prog)
				c.compile(s)
				jumps = append(jumps, c.emit(inst{op: opJmp}))
				c.prog[split].y = len(c.prog)
			} else {
				c.compile(s)
			}
		}
		for _, j := range jumps {
			c.prog[j].x = len(c.prog)
		}
	case nRepeat:
		sub := n.subs[0]
		for i := 0; i < n.min; i++ {
			c.compile(sub)
		}
		switch {
		case n.max < 0:
			// L: split body, out; body; jmp L
			l := c.emit(inst{op: opSplit})
			c.compile(sub)
			c.emit(inst{op: opJmp, x: l})
			c.setSplit(l, l+1, len(c.prog), n.greedy)
		default:
			var splits []int
			for i := n.min; i < n.max; i++ {
				s := c.emit(inst{op: opSplit})
				splits = append(splits, s)
				c.compile(sub)
			}
			for _, s := range splits {
				c.setSplit(s, s+1, len(c.prog), n.greedy)
			}
		}
	}
}

func (c *compiler) setSplit(at, body, out int, greedy bool) {
	if greedy {
		c.prog[at].x, c.prog[at].y = body, out
	} else {
		c.prog[at].x, c.prog[at].y = out, body
	}
}

func isWordByte(r rune) bool {
	return r == '_' || ('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
}

func (in *inst) matches(r rune) bool {
	switch in.op {
	case opChar:
		return r == in.r
	case opAny:
		return true
	case opClass:
		found := false
		lo, hi := 0, len(in.ranges)
		for lo < hi {
			m := (lo + hi) / 2
			switch {
			case r < in.ranges[m].lo:
				hi = m
			case r > in.ranges[m].hi:
				lo = m + 1
			default:
				found = true
				lo = hi
			}
		}
		return found != in.negate
	}
	return false
}

type thread struct {
	pc  int
	cap []int
}

type queue struct {
	sparse []int
	dense  []thread
}

func newQueue(n int) *queue { return &queue{sparse: make([]int, n), dense: make([]thread, 0, n)} }

func (q *queue) contains(pc int) bool {
	i := q.sparse[pc]
	return i < len(q.dense) && q.dense[i].pc == pc
}

func (q *queue) clear() { q.dense = q.dense[:0] }

// machine holds the state of one search.
type machine struct {
	re      *Regexp
	input   string
	clist   *queue
	nlist   *queue
	matched []int
	steps   int
}

func (m *machine) assertOK(in *inst, pos int) bool {
	var before, after rune = -1, -1
	if pos > 0 {
		before, _ = utf8.DecodeLastRuneInString(m.input[:pos])
	}
	if pos < len(m.input) {
		after, _ = utf8.DecodeRuneInString(m.input[pos:])
	}
	switch in.assert {
	case nBegin:
		return pos == 0 || (in.multi && before == '\n')
	case nEnd:
		return pos == len(m.input) || (in.multi && after == '\n')
	case nWordB, nNotWordB:
		b := isWordByte(before) != isWordByte(after)
		return b == (in.assert == nWordB)
	}
	return false
}

// add follows empty-width instructions from pc, adding runnable threads to
// q in priority order.
func (m *machine) add(q *queue, pc, pos int, cap []int) {
	if q.contains(pc) {
		return
	}
	q.sparse[pc] = len(q.dense)
	q.dense = append(q.dense, thread{pc: pc})
	in := &m.re.prog[pc]
	switch in.op {
	case opJmp:
		m.add(q, in.x, pos, cap)
	case opSplit:
		m.add(q, in.x, pos, cap)
		m.add(q, in.y, pos, cap)
	case opSave:
		old := cap[in.n]
		cap[in.n] = pos
		m.add(q, pc+1, pos, cap)
		cap[in.n] = old
	case opAssert:
		if m.assertOK(in, pos) {
			m.add(q, pc+1, pos, cap)
		}
	default:
		t := &q.dense[len(q.dense)-1]
		t.cap = append(t.cap[:0], cap...)
	}
}

// run searches for the leftmost-first match starting at or after start.
func (m *machine) run(start int) []int {
	ncap := 2 * (m.re.ncap + 1)
	m.clist.clear()
	m.nlist.clear()
	m.matched = nil
	cap := make([]int, ncap)
	for i := range cap {
		cap[i] = -1
	}
	pos := start
	if m.re.prefix != "" && m.re.prog[1].op == opChar {
		i := strings.Index(m.input[pos:], m.re.prefix)
		if i < 0 {
			return nil
		}
		pos += i
	}
	for {
		if m.matched == nil {
			m.add(m.clist, 0, pos, cap)
		}
		if len(m.clist.dense) == 0 {
			break
		}
		r, width := rune(-1), 0
		if pos < len(m.input) {
			r, width = utf8.DecodeRuneInString(m.input[pos:])
		}
		for i := 0; i < len(m.clist.dense); i++ {
			t := &m.clist.dense[i]
			if t.cap == nil {
				continue
			}
			m.steps++
			in := &m.re.prog[t.pc]
			switch in.op {
			case opMatch:
				m.matched = append(m.matched[:0], t.cap...)
				m.matched[1] = pos
				i = len(m.clist.dense) // cut lower-priority threads
			case opChar, opClass, opAny:
				if width > 0 && in.matches(r) {
					m.add(m.nlist, t.pc+1, pos+width, t.cap)
				}
			}
		}
		for i := range m.clist.dense {
			m.clist.dense[i].cap = nil
		}
		m.clist, m.nlist = m.nlist, m.clist
		m.nlist.clear()
		if width == 0 {
			break
		}
		pos += width
	}
	return m.matched
}

func (re *Regexp) newMachine(s string) *machine {
	return &machine{re: re, input: s, clist: newQueue(len(re.prog)), nlist: newQueue(len(re.prog))}
}

// FindSubmatchIndex returns the byte offsets of the leftmost match and its
// groups, or nil.
func (re *Regexp) FindSubmatchIndex(s string) []int {
	return re.newMachine(s).run(0)
}

// FindAllSubmatchIndex follows the stdlib rules for successive matches: an
// empty match directly after the previous match is skipped.
func (re *Regexp) FindAllSubmatchIndex(s string, n int) [][]int {
	m := re.newMachine(s)
	var out [][]int
	prevEnd := -1
	for pos := 0; pos <= len(s) && (n < 0 || len(out) < n); {
		loc := m.run(pos)
		if loc == nil {
			break
		}
		accept := true
		if loc[1] == loc[0] {
			if loc[0] == prevEnd {
				accept = false
			}
			if loc[1] < len(s) {
				_, w := utf8.DecodeRuneInString(s[loc[1]:])
				pos = loc[1] + w
			} else {
				pos = len(s) + 1
			}
		} else {
			pos = loc[1]
		}
		prevEnd = loc[1]
		if accept {
			out = append(out, append([]int(nil), loc...))
		}
	}
	return out
}

func (re *Regexp) MatchString(s string) bool { return re.FindSubmatchIndex(s) != nil }

// Expand appends template to dst with $1, ${1}, ${name}, and $$ replaced.
func (re *Regexp) expand(dst []byte, template, src string, loc []int) []byte {
	for {
		i := strings.IndexByte(template, '$')
		if i < 0 {
			break
		}
		dst = append(dst, template[:i]...)
		template = template[i+1:]
		if strings.HasPrefix(template, "$") {
			dst = append(dst, '$')
			template = template[1:]
			continue
		}
		var name string
		if strings.HasPrefix(template, "{") {
			end := strings.IndexByte(template, '}')
			if end < 0 {
				dst = append(dst, '$')
				continue
			}
			name, template = template[1:end], template[end+1:]
		} else {
			j := 0
			for j < len(template) && (isWordByte(rune(template[j]))) {
				j++
			}
			if j == 0 {
				dst = append(dst, '$')
				continue
			}
			name, template = template[:j], template[j:]
		}
		g := -1
		if k, err := strconv.Atoi(name); err == nil {
			g = k
		} else {
			for idx, nm := range re.names {
				if nm == name {
					g = idx
				}
			}
		}
		if g >= 0 && 2*g+1 < len(loc) && loc[2*g] >= 0 {
			dst = append(dst, src[loc[2*g]:loc[2*g+1]]...)
		}
	}
	return append(dst, template...)
}

func (re *Regexp) ReplaceAllString(src, repl string) string {
	var out []byte
	last := 0
	for _, loc := range re.FindAllSubmatchIndex(src, -1) {
		out = append(out, src[last:loc[0]]...)
		out = re.expand(out, repl, src, loc)
		last = loc[1]
	}
	out = append(out, src[last:]...)
	return string(out)
}
