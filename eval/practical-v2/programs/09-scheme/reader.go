package main

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

type ReadError struct {
	Line int
	Msg  string
}

func (e *ReadError) Error() string { return fmt.Sprintf("read error on line %d: %s", e.Line, e.Msg) }

type reader struct {
	src  []rune
	pos  int
	line int
}

func (r *reader) fail(format string, args ...any) {
	panic(&ReadError{r.line, fmt.Sprintf(format, args...)})
}

func (r *reader) skip() {
	for r.pos < len(r.src) {
		c := r.src[r.pos]
		switch {
		case c == '\n':
			r.line++
			r.pos++
		case unicode.IsSpace(c):
			r.pos++
		case c == ';':
			for r.pos < len(r.src) && r.src[r.pos] != '\n' {
				r.pos++
			}
		case c == '#' && r.pos+1 < len(r.src) && r.src[r.pos+1] == '|':
			depth := 0
			for r.pos < len(r.src) {
				if strings.HasPrefix(string(r.src[r.pos:min(r.pos+2, len(r.src))]), "#|") {
					depth++
					r.pos += 2
				} else if strings.HasPrefix(string(r.src[r.pos:min(r.pos+2, len(r.src))]), "|#") {
					depth--
					r.pos += 2
					if depth == 0 {
						break
					}
				} else {
					if r.src[r.pos] == '\n' {
						r.line++
					}
					r.pos++
				}
			}
			if depth != 0 {
				r.fail("unterminated block comment")
			}
		case c == '#' && r.pos+1 < len(r.src) && r.src[r.pos+1] == ';':
			r.pos += 2
			r.read() // datum comment
		default:
			return
		}
	}
}

func isDelimiter(c rune) bool {
	return unicode.IsSpace(c) || strings.ContainsRune("()\";'`,", c)
}

// readAll parses every datum in src.
func readAll(src string) (out []Value, err error) {
	defer func() {
		if x := recover(); x != nil {
			re, ok := x.(*ReadError)
			if !ok {
				panic(x)
			}
			err = re
		}
	}()
	r := &reader{src: []rune(src), line: 1}
	for {
		r.skip()
		if r.pos >= len(r.src) {
			return out, nil
		}
		out = append(out, r.read())
	}
}

func (r *reader) read() Value {
	r.skip()
	if r.pos >= len(r.src) {
		r.fail("unexpected end of input")
	}
	c := r.src[r.pos]
	switch c {
	case '(', '[':
		r.pos++
		closer := ')'
		if c == '[' {
			closer = ']'
		}
		var items []Value
		var tail Value = Empty
		for {
			r.skip()
			if r.pos >= len(r.src) {
				r.fail("missing %c", closer)
			}
			if r.src[r.pos] == closer {
				r.pos++
				break
			}
			if r.src[r.pos] == ')' || r.src[r.pos] == ']' {
				r.fail("mismatched %c", r.src[r.pos])
			}
			if r.src[r.pos] == '.' && r.pos+1 < len(r.src) && isDelimiter(r.src[r.pos+1]) && len(items) > 0 {
				r.pos++
				tail = r.read()
				r.skip()
				if r.pos >= len(r.src) || r.src[r.pos] != closer {
					r.fail("bad dotted list")
				}
				r.pos++
				break
			}
			items = append(items, r.read())
		}
		for i := len(items) - 1; i >= 0; i-- {
			tail = &Pair{items[i], tail}
		}
		return tail
	case ')', ']':
		r.pos++
		r.fail("unexpected %c", c)
	case '\'':
		r.pos++
		return list(Symbol("quote"), r.read())
	case '`':
		r.pos++
		return list(Symbol("quasiquote"), r.read())
	case ',':
		r.pos++
		if r.pos < len(r.src) && r.src[r.pos] == '@' {
			r.pos++
			return list(Symbol("unquote-splicing"), r.read())
		}
		return list(Symbol("unquote"), r.read())
	case '"':
		r.pos++
		var s []rune
		for {
			if r.pos >= len(r.src) {
				r.fail("unterminated string")
			}
			ch := r.src[r.pos]
			r.pos++
			if ch == '"' {
				break
			}
			if ch == '\n' {
				r.line++
			}
			if ch == '\\' && r.pos < len(r.src) {
				esc := r.src[r.pos]
				r.pos++
				switch esc {
				case 'n':
					ch = '\n'
				case 't':
					ch = '\t'
				default:
					ch = esc
				}
			}
			s = append(s, ch)
		}
		return &MString{s}
	case '#':
		if r.pos+1 < len(r.src) {
			switch r.src[r.pos+1] {
			case '(':
				r.pos++
				return &Vector{listToSlice(r.read())}
			case '\\':
				r.pos += 2
				start := r.pos
				r.pos++
				for r.pos < len(r.src) && !isDelimiter(r.src[r.pos]) {
					r.pos++
				}
				name := string(r.src[start:r.pos])
				if len([]rune(name)) == 1 {
					return Char([]rune(name)[0])
				}
				for ch, n := range charNames {
					if n == name {
						return Char(ch)
					}
				}
				r.fail("unknown character #\\%s", name)
			}
		}
	}
	start := r.pos
	for r.pos < len(r.src) && !isDelimiter(r.src[r.pos]) {
		r.pos++
	}
	tok := string(r.src[start:r.pos])
	return parseAtom(r, tok)
}

func parseAtom(r *reader, tok string) Value {
	switch tok {
	case "#t", "#true":
		return true
	case "#f", "#false":
		return false
	}
	if strings.HasPrefix(tok, "#") {
		r.fail("bad syntax %s", tok)
	}
	if n, err := strconv.ParseInt(tok, 10, 64); err == nil {
		return n
	}
	if b, ok := new(big.Int).SetString(tok, 10); ok {
		return b
	}
	if strings.Contains(tok, "/") {
		if q, ok := new(big.Rat).SetString(tok); ok {
			return normRat(q)
		}
	}
	if f, err := strconv.ParseFloat(tok, 64); err == nil && (unicode.IsDigit(rune(tok[len(tok)-1])) || tok[len(tok)-1] == '.') {
		return f
	}
	return Symbol(strings.ToLower(tok))
}
