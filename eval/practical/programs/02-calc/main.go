// Command calc is a line-oriented calculator. Each input line is an
// expression or an assignment (name = expr). Supported: + - * / % ^,
// unary minus, parentheses, variables, and the functions abs, sqrt, min,
// max, floor, ceil, round, and hypot. The line "vars" lists variables.
//
// The exit status is 1 if any line failed.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var (
	ErrDivByZero = errors.New("division by zero")
	ErrUndefined = errors.New("undefined variable")
)

// SyntaxError reports a parse error at a column (1-based).
type SyntaxError struct {
	Col int
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("syntax error at column %d: %s", e.Col, e.Msg)
}

type tokKind int

const (
	tNum tokKind = iota
	tIdent
	tOp
	tLParen
	tRParen
	tComma
	tEOF
)

type token struct {
	kind tokKind
	text string
	num  float64
	pos  int
}

func tokenize(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := rune(s[i])
		switch {
		case c == ' ' || c == '\t':
			i++
		case unicode.IsDigit(c) || c == '.':
			start := i
			for i < len(s) && (unicode.IsDigit(rune(s[i])) || s[i] == '.') {
				i++
			}
			if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
				j := i + 1
				if j < len(s) && (s[j] == '+' || s[j] == '-') {
					j++
				}
				if j < len(s) && unicode.IsDigit(rune(s[j])) {
					i = j
					for i < len(s) && unicode.IsDigit(rune(s[i])) {
						i++
					}
				}
			}
			v, err := strconv.ParseFloat(s[start:i], 64)
			if err != nil {
				return nil, &SyntaxError{start + 1, fmt.Sprintf("bad number %q", s[start:i])}
			}
			toks = append(toks, token{kind: tNum, text: s[start:i], num: v, pos: start})
		case unicode.IsLetter(c) || c == '_':
			start := i
			for i < len(s) && (unicode.IsLetter(rune(s[i])) || unicode.IsDigit(rune(s[i])) || s[i] == '_') {
				i++
			}
			toks = append(toks, token{kind: tIdent, text: s[start:i], pos: start})
		case strings.ContainsRune("+-*/%^=", c):
			toks = append(toks, token{kind: tOp, text: string(c), pos: i})
			i++
		case c == '(':
			toks = append(toks, token{kind: tLParen, text: "(", pos: i})
			i++
		case c == ')':
			toks = append(toks, token{kind: tRParen, text: ")", pos: i})
			i++
		case c == ',':
			toks = append(toks, token{kind: tComma, text: ",", pos: i})
			i++
		default:
			return nil, &SyntaxError{i + 1, fmt.Sprintf("unexpected character %q", c)}
		}
	}
	toks = append(toks, token{kind: tEOF, pos: len(s)})
	return toks, nil
}

type parser struct {
	toks []token
	pos  int
	vars map[string]float64
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) errorf(t token, format string, args ...any) error {
	return &SyntaxError{t.pos + 1, fmt.Sprintf(format, args...)}
}

// expr := term (('+'|'-') term)*
func (p *parser) expr() (float64, error) {
	v, err := p.term()
	if err != nil {
		return 0, err
	}
	for p.peek().kind == tOp && (p.peek().text == "+" || p.peek().text == "-") {
		op := p.next().text
		r, err := p.term()
		if err != nil {
			return 0, err
		}
		if op == "+" {
			v += r
		} else {
			v -= r
		}
	}
	return v, nil
}

// term := unary (('*'|'/'|'%') unary)*
func (p *parser) term() (float64, error) {
	v, err := p.unary()
	if err != nil {
		return 0, err
	}
	for p.peek().kind == tOp && strings.Contains("*/%", p.peek().text) {
		op := p.next().text
		r, err := p.unary()
		if err != nil {
			return 0, err
		}
		switch op {
		case "*":
			v *= r
		case "/":
			if r == 0 {
				return 0, ErrDivByZero
			}
			v /= r
		case "%":
			if r == 0 {
				return 0, ErrDivByZero
			}
			v = math.Mod(v, r)
		}
	}
	return v, nil
}

// unary := '-' unary | power
func (p *parser) unary() (float64, error) {
	if t := p.peek(); t.kind == tOp && t.text == "-" {
		p.next()
		v, err := p.unary()
		return -v, err
	}
	return p.power()
}

// power := primary ('^' unary)?   (right associative)
func (p *parser) power() (float64, error) {
	base, err := p.primary()
	if err != nil {
		return 0, err
	}
	if t := p.peek(); t.kind == tOp && t.text == "^" {
		p.next()
		exp, err := p.unary()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *parser) primary() (float64, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return t.num, nil
	case tLParen:
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if p.peek().kind != tRParen {
			return 0, p.errorf(p.peek(), "expected )")
		}
		p.next()
		return v, nil
	case tIdent:
		if p.peek().kind == tLParen {
			p.next()
			var args []float64
			if p.peek().kind != tRParen {
				for {
					a, err := p.expr()
					if err != nil {
						return 0, err
					}
					args = append(args, a)
					if p.peek().kind != tComma {
						break
					}
					p.next()
				}
			}
			if p.peek().kind != tRParen {
				return 0, p.errorf(p.peek(), "expected ) after arguments")
			}
			p.next()
			return call(t, args)
		}
		switch t.text {
		case "pi":
			return math.Pi, nil
		case "e":
			return math.E, nil
		}
		v, ok := p.vars[t.text]
		if !ok {
			return 0, fmt.Errorf("%w: %s", ErrUndefined, t.text)
		}
		return v, nil
	case tEOF:
		return 0, p.errorf(t, "unexpected end of input")
	}
	return 0, p.errorf(t, "unexpected %q", t.text)
}

func call(name token, args []float64) (float64, error) {
	want := func(n int) error {
		if len(args) != n {
			return fmt.Errorf("%s expects %d argument(s), got %d", name.text, n, len(args))
		}
		return nil
	}
	switch name.text {
	case "abs", "sqrt", "floor", "ceil", "round":
		if err := want(1); err != nil {
			return 0, err
		}
		x := args[0]
		switch name.text {
		case "abs":
			return math.Abs(x), nil
		case "sqrt":
			if x < 0 {
				return 0, fmt.Errorf("sqrt of negative number %g", x)
			}
			return math.Sqrt(x), nil
		case "floor":
			return math.Floor(x), nil
		case "ceil":
			return math.Ceil(x), nil
		default:
			return math.Round(x), nil
		}
	case "hypot":
		if err := want(2); err != nil {
			return 0, err
		}
		return math.Hypot(args[0], args[1]), nil
	case "min", "max":
		if len(args) == 0 {
			return 0, fmt.Errorf("%s expects at least one argument", name.text)
		}
		r := args[0]
		for _, a := range args[1:] {
			if name.text == "min" {
				r = math.Min(r, a)
			} else {
				r = math.Max(r, a)
			}
		}
		return r, nil
	}
	return 0, fmt.Errorf("unknown function %s", name.text)
}

func evalLine(line string, vars map[string]float64) (string, float64, error) {
	toks, err := tokenize(line)
	if err != nil {
		return "", 0, err
	}
	target := ""
	start := 0
	if len(toks) >= 2 && toks[0].kind == tIdent && toks[1].kind == tOp && toks[1].text == "=" {
		target = toks[0].text
		if target == "pi" || target == "e" {
			return "", 0, fmt.Errorf("cannot assign to constant %s", target)
		}
		start = 2
	}
	p := &parser{toks: toks, pos: start, vars: vars}
	v, err := p.expr()
	if err != nil {
		return "", 0, err
	}
	if t := p.peek(); t.kind != tEOF {
		return "", 0, p.errorf(t, "unexpected %q", t.text)
	}
	return target, v, nil
}

func format(v float64) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return fmt.Sprint(v)
	}
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'g', 12, 64)
}

func main() {
	vars := map[string]float64{}
	failed := 0
	in := bufio.NewScanner(os.Stdin)
	lineNo := 0
	for in.Scan() {
		lineNo++
		line := strings.TrimSpace(in.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "vars" {
			names := make([]string, 0, len(vars))
			for k := range vars {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				fmt.Printf("  %s = %s\n", k, format(vars[k]))
			}
			continue
		}
		target, v, err := evalLine(line, vars)
		if err != nil {
			failed++
			var se *SyntaxError
			switch {
			case errors.As(err, &se):
				fmt.Printf("line %d: %v\n  %s\n  %s^\n", lineNo, err, line, strings.Repeat(" ", se.Col-1))
			case errors.Is(err, ErrDivByZero), errors.Is(err, ErrUndefined):
				fmt.Printf("line %d: math error: %v\n", lineNo, err)
			default:
				fmt.Printf("line %d: error: %v\n", lineNo, err)
			}
			continue
		}
		if target != "" {
			vars[target] = v
			fmt.Printf("%s = %s\n", target, format(v))
		} else {
			fmt.Println(format(v))
		}
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "calc: %d line(s) failed\n", failed)
		os.Exit(1)
	}
}
