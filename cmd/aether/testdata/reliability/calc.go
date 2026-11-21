// A tokenizer, recursive-descent parser, and evaluator for a small
// expression language with variables, functions, and errors.
package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

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

func (k tokKind) String() string {
	return [...]string{"num", "ident", "op", "(", ")", ",", "eof"}[k]
}

type token struct {
	kind tokKind
	text string
	pos  int
}

func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			toks = append(toks, token{tNum, src[i:j], i})
			i = j
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			toks = append(toks, token{tIdent, src[i:j], i})
			i = j
		case strings.ContainsRune("+-*/%^=<>", rune(c)):
			toks = append(toks, token{tOp, string(c), i})
			i++
		case c == '(':
			toks = append(toks, token{tLParen, "(", i})
			i++
		case c == ')':
			toks = append(toks, token{tRParen, ")", i})
			i++
		case c == ',':
			toks = append(toks, token{tComma, ",", i})
			i++
		default:
			return nil, fmt.Errorf("lex: unexpected %q at %d", c, i)
		}
	}
	return append(toks, token{tEOF, "", len(src)}), nil
}

type node interface {
	eval(env *env) float64
	String() string
}

type num float64
type ident string
type binary struct {
	op   byte
	l, r node
}
type unary struct {
	op byte
	x  node
}
type call struct {
	fn   string
	args []node
}
type assign struct {
	name string
	x    node
}

type evalError struct{ msg string }

func (e *evalError) Error() string { return "eval: " + e.msg }

type env struct {
	vars  map[string]float64
	funcs map[string]func(args []float64) float64
	depth int
}

func fail(format string, args ...any) { panic(&evalError{fmt.Sprintf(format, args...)}) }

func (n num) eval(*env) float64 { return float64(n) }
func (n num) String() string    { return strconv.FormatFloat(float64(n), 'g', -1, 64) }
func (n ident) eval(e *env) float64 {
	v, ok := e.vars[string(n)]
	if !ok {
		fail("undefined %s", string(n))
	}
	return v
}
func (n ident) String() string { return string(n) }

func pow(x float64, n int) float64 {
	r := 1.0
	for n > 0 {
		if n&1 == 1 {
			r *= x
		}
		x *= x
		n >>= 1
	}
	return r
}

func (b *binary) eval(e *env) float64 {
	l, r := b.l.eval(e), b.r.eval(e)
	switch b.op {
	case '+':
		return l + r
	case '-':
		return l - r
	case '*':
		return l * r
	case '/':
		if r == 0 {
			fail("division by zero")
		}
		return l / r
	case '%':
		if int64(r) == 0 {
			fail("modulo by zero")
		}
		return float64(int64(l) % int64(r))
	case '^':
		return pow(l, int(r))
	case '<':
		if l < r {
			return 1
		}
		return 0
	case '>':
		if l > r {
			return 1
		}
		return 0
	}
	fail("bad op %c", b.op)
	return 0
}
func (b *binary) String() string {
	return "(" + b.l.String() + " " + string(b.op) + " " + b.r.String() + ")"
}

func (u *unary) eval(e *env) float64 { return -u.x.eval(e) }
func (u *unary) String() string      { return "(-" + u.x.String() + ")" }

func (c *call) eval(e *env) float64 {
	f, ok := e.funcs[c.fn]
	if !ok {
		fail("unknown function %s", c.fn)
	}
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > 50 {
		fail("too deep")
	}
	args := make([]float64, len(c.args))
	for i, a := range c.args {
		args[i] = a.eval(e)
	}
	return f(args)
}
func (c *call) String() string {
	parts := make([]string, len(c.args))
	for i, a := range c.args {
		parts[i] = a.String()
	}
	return c.fn + "(" + strings.Join(parts, ", ") + ")"
}

func (a *assign) eval(e *env) float64 {
	v := a.x.eval(e)
	e.vars[a.name] = v
	return v
}
func (a *assign) String() string { return a.name + " = " + a.x.String() }

type parser struct {
	toks []token
	i    int
}

var errSyntax = errors.New("syntax error")

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	p.i++
	return t
}

func prec(op string) int {
	switch op {
	case "<", ">":
		return 1
	case "+", "-":
		return 2
	case "*", "/", "%":
		return 3
	case "^":
		return 4
	}
	return 0
}

func (p *parser) primary() (node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: number %q: %v", errSyntax, t.text, err)
		}
		return num(f), nil
	case tIdent:
		if p.peek().kind != tLParen {
			return ident(t.text), nil
		}
		p.next()
		c := &call{fn: t.text}
		for p.peek().kind != tRParen {
			a, err := p.expr(1)
			if err != nil {
				return nil, err
			}
			c.args = append(c.args, a)
			if p.peek().kind == tComma {
				p.next()
			} else if p.peek().kind != tRParen {
				return nil, fmt.Errorf("%w at %d: want , or )", errSyntax, p.peek().pos)
			}
		}
		p.next()
		return c, nil
	case tLParen:
		x, err := p.expr(1)
		if err != nil {
			return nil, err
		}
		if p.next().kind != tRParen {
			return nil, fmt.Errorf("%w: missing )", errSyntax)
		}
		return x, nil
	case tOp:
		if t.text == "-" {
			x, err := p.primary()
			if err != nil {
				return nil, err
			}
			return &unary{'-', x}, nil
		}
	}
	return nil, fmt.Errorf("%w at %d: unexpected %v %q", errSyntax, t.pos, t.kind, t.text)
}

func (p *parser) expr(min int) (node, error) {
	l, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		pr := prec(t.text)
		if t.kind != tOp || pr < min || t.text == "=" {
			return l, nil
		}
		p.next()
		nextMin := pr + 1
		if t.text == "^" {
			nextMin = pr
		}
		r, err := p.expr(nextMin)
		if err != nil {
			return nil, err
		}
		l = &binary{t.text[0], l, r}
	}
}

func parse(src string) (node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if len(toks) > 2 && toks[0].kind == tIdent && toks[1].text == "=" {
		p.i = 2
		x, err := p.expr(1)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tEOF {
			return nil, fmt.Errorf("%w: trailing %q", errSyntax, p.peek().text)
		}
		return &assign{toks[0].text, x}, nil
	}
	x, err := p.expr(1)
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, fmt.Errorf("%w: trailing %q", errSyntax, p.peek().text)
	}
	return x, nil
}

func run(e *env, src string) (v float64, err error) {
	defer func() {
		if r := recover(); r != nil {
			ee, ok := r.(*evalError)
			if !ok {
				panic(r)
			}
			err = ee
		}
	}()
	n, err := parse(src)
	if err != nil {
		return 0, err
	}
	fmt.Printf("  ast: %v\n", n)
	return n.eval(e), nil
}

func main() {
	e := &env{vars: map[string]float64{"pi": 3.14159}, funcs: map[string]func([]float64) float64{}}
	e.funcs["max"] = func(a []float64) float64 {
		m := a[0]
		for _, x := range a[1:] {
			if x > m {
				m = x
			}
		}
		return m
	}
	e.funcs["sum"] = func(a []float64) float64 {
		s := 0.0
		for _, x := range a {
			s += x
		}
		return s
	}
	var fib func([]float64) float64
	fib = func(a []float64) float64 {
		n := int(a[0])
		if n < 2 {
			return float64(n)
		}
		return fib([]float64{float64(n - 1)}) + fib([]float64{float64(n - 2)})
	}
	e.funcs["fib"] = fib
	e.funcs["rec"] = func(a []float64) float64 {
		c := &call{fn: "rec", args: []node{num(a[0] + 1)}}
		return c.eval(e)
	}
	lines := []string{
		"1 + 2 * 3",
		"(1 + 2) * 3",
		"2 ^ 3 ^ 2",
		"x = 10",
		"y = x * pi",
		"max(x, y, 7, -3)",
		"sum(1, 2, 3, 4.5) / 2",
		"fib(15)",
		"17 % 5 + -x",
		"x / (y - y)",
		"undefinedvar + 1",
		"nope(1)",
		"3 +",
		"(1 + 2",
		"1 $ 2",
		"rec(0)",
		"1 < 2 > 0",
		"1.2.3",
	}
	for i, l := range lines {
		fmt.Printf("%2d> %s\n", i, l)
		v, err := run(e, l)
		switch {
		case errors.Is(err, errSyntax):
			fmt.Println("  syntax:", err)
		case err != nil:
			_, ok := err.(*evalError)
			fmt.Println("  error:", err, ok)
		default:
			fmt.Printf("  = %.6g\n", v)
		}
	}
	fmt.Println("depth after errors:", e.depth)
	keys := []string{}
	for k := range e.vars {
		keys = append(keys, k)
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		fmt.Printf("%s=%v ", k, e.vars[k])
	}
	fmt.Println()
}
