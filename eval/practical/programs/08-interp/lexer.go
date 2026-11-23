package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Kind int

const (
	EOF Kind = iota
	Ident
	Int
	Str
	Punct
	Keyword
)

var kindNames = [...]string{"end of input", "identifier", "integer", "string", "punctuation", "keyword"}

func (k Kind) String() string { return kindNames[k] }

type Token struct {
	Kind Kind
	Text string
	Line int
}

func (t Token) String() string {
	if t.Kind == EOF {
		return "end of input"
	}
	return fmt.Sprintf("%q", t.Text)
}

var keywords = map[string]bool{
	"let": true, "fn": true, "if": true, "else": true, "while": true, "for": true, "in": true,
	"return": true, "print": true, "true": true, "false": true, "nil": true,
	"and": true, "or": true, "not": true, "break": true, "continue": true,
}

// Error is a compile-time or run-time error at a line.
type Error struct {
	Phase string
	Line  int
	Msg   string
}

func (e *Error) Error() string { return fmt.Sprintf("%s error on line %d: %s", e.Phase, e.Line, e.Msg) }

func Lex(src string) ([]Token, error) {
	var toks []Token
	line := 1
	i := 0
	errf := func(format string, args ...any) error {
		return &Error{"syntax", line, fmt.Sprintf(format, args...)}
	}
	for i < len(src) {
		r, size := utf8.DecodeRuneInString(src[i:])
		switch {
		case r == '\n':
			line++
			i++
		case unicode.IsSpace(r):
			i += size
		case r == '/' && strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case r == '_' || unicode.IsLetter(r):
			start := i
			for i < len(src) {
				r, size := utf8.DecodeRuneInString(src[i:])
				if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				i += size
			}
			word := src[start:i]
			k := Ident
			if keywords[word] {
				k = Keyword
			}
			toks = append(toks, Token{k, word, line})
		case r >= '0' && r <= '9':
			start := i
			for i < len(src) && src[i] >= '0' && src[i] <= '9' {
				i++
			}
			toks = append(toks, Token{Int, src[start:i], line})
		case r == '"':
			i++
			var b strings.Builder
			for {
				if i >= len(src) || src[i] == '\n' {
					return nil, errf("unterminated string")
				}
				c := src[i]
				if c == '"' {
					i++
					break
				}
				if c == '\\' && i+1 < len(src) {
					i++
					switch src[i] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case '"', '\\':
						b.WriteByte(src[i])
					default:
						return nil, errf("unknown escape \\%c", src[i])
					}
					i++
					continue
				}
				b.WriteByte(c)
				i++
			}
			toks = append(toks, Token{Str, b.String(), line})
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "+=", "-=":
				toks = append(toks, Token{Punct, two, line})
				i += 2
				continue
			}
			if strings.ContainsRune("+-*/%<>=(){}[],;:.", r) {
				toks = append(toks, Token{Punct, string(r), line})
				i += size
				continue
			}
			return nil, errf("unexpected character %q", r)
		}
	}
	toks = append(toks, Token{EOF, "", line})
	return toks, nil
}
