package main

import (
	"fmt"
	"strconv"
)

type Parser struct {
	toks []Token
	pos  int
}

// bail aborts parsing; Parse recovers it.
type bail struct{ err *Error }

func (p *Parser) fail(t Token, format string, args ...any) {
	panic(bail{&Error{"syntax", t.Line, fmt.Sprintf(format, args...)}})
}

func (p *Parser) peek() Token { return p.toks[p.pos] }
func (p *Parser) advance() Token {
	t := p.toks[p.pos]
	if t.Kind != EOF {
		p.pos++
	}
	return t
}

func (p *Parser) is(text string) bool {
	t := p.peek()
	return (t.Kind == Punct || t.Kind == Keyword) && t.Text == text
}

func (p *Parser) accept(text string) bool {
	if p.is(text) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) expect(text string) Token {
	if !p.is(text) {
		p.fail(p.peek(), "expected %q, found %v", text, p.peek())
	}
	return p.advance()
}

func (p *Parser) ident() string {
	t := p.peek()
	if t.Kind != Ident {
		p.fail(t, "expected identifier, found %v", t)
	}
	p.advance()
	return t.Text
}

func Parse(toks []Token) (prog []Stmt, err error) {
	p := &Parser{toks: toks}
	defer func() {
		if r := recover(); r != nil {
			b, ok := r.(bail)
			if !ok {
				panic(r)
			}
			prog, err = nil, b.err
		}
	}()
	for p.peek().Kind != EOF {
		prog = append(prog, p.statement())
	}
	return prog, nil
}

func (p *Parser) block() []Stmt {
	p.expect("{")
	var body []Stmt
	for !p.is("}") {
		if p.peek().Kind == EOF {
			p.fail(p.peek(), "unexpected end of input in block")
		}
		body = append(body, p.statement())
	}
	p.expect("}")
	return body
}

func (p *Parser) statement() Stmt {
	t := p.peek()
	switch {
	case p.accept("let"):
		name := p.ident()
		p.expect("=")
		init := p.expr()
		p.expect(";")
		return &LetStmt{t.Line, name, init}
	case p.is("fn") && p.toks[p.pos+1].Kind == Ident:
		p.advance()
		name := p.ident()
		f := p.funcRest(t.Line, name)
		return &LetStmt{t.Line, name, f}
	case p.accept("print"):
		var args []Expr
		if !p.is(";") {
			args = append(args, p.expr())
			for p.accept(",") {
				args = append(args, p.expr())
			}
		}
		p.expect(";")
		return &PrintStmt{t.Line, args}
	case p.accept("if"):
		return p.ifRest(t.Line)
	case p.accept("while"):
		cond := p.expr()
		return &WhileStmt{t.Line, cond, p.block()}
	case p.accept("for"):
		v := p.ident()
		p.expect("in")
		seq := p.expr()
		return &ForStmt{t.Line, v, seq, p.block()}
	case p.accept("return"):
		var v Expr
		if !p.is(";") {
			v = p.expr()
		}
		p.expect(";")
		return &ReturnStmt{t.Line, v}
	case p.accept("break"):
		p.expect(";")
		return &BreakStmt{t.Line}
	case p.accept("continue"):
		p.expect(";")
		return &ContinueStmt{t.Line}
	}
	x := p.expr()
	for _, op := range []string{"=", "+=", "-="} {
		if p.accept(op) {
			switch x.(type) {
			case *Name, *Index:
			default:
				p.fail(t, "cannot assign to this expression")
			}
			v := p.expr()
			p.expect(";")
			return &AssignStmt{t.Line, x, op, v}
		}
	}
	p.expect(";")
	return &ExprStmt{x}
}

func (p *Parser) ifRest(line int) Stmt {
	cond := p.expr()
	s := &IfStmt{Line: line, Cond: cond, Then: p.block()}
	if p.accept("else") {
		if t := p.peek(); p.accept("if") {
			s.Else = []Stmt{p.ifRest(t.Line)}
		} else {
			s.Else = p.block()
		}
	}
	return s
}

func (p *Parser) funcRest(line int, name string) *FuncLit {
	p.expect("(")
	var params []string
	if !p.is(")") {
		params = append(params, p.ident())
		for p.accept(",") {
			params = append(params, p.ident())
		}
	}
	p.expect(")")
	return &FuncLit{line, name, params, p.block()}
}

var precedence = map[string]int{
	"or": 1, "and": 2,
	"==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4,
	"+": 5, "-": 5, "*": 6, "/": 6, "%": 6,
}

func (p *Parser) expr() Expr { return p.binary(1) }

func (p *Parser) binary(minPrec int) Expr {
	x := p.unary()
	for {
		t := p.peek()
		prec, ok := precedence[t.Text]
		if !ok || (t.Kind != Punct && t.Kind != Keyword) || prec < minPrec {
			return x
		}
		p.advance()
		y := p.binary(prec + 1)
		x = &Binary{t.Line, t.Text, x, y}
	}
}

func (p *Parser) unary() Expr {
	t := p.peek()
	if p.accept("-") || p.accept("not") {
		return &Unary{t.Line, t.Text, p.unary()}
	}
	return p.postfix()
}

func (p *Parser) postfix() Expr {
	x := p.primary()
	for {
		t := p.peek()
		switch {
		case p.accept("("):
			var args []Expr
			if !p.is(")") {
				args = append(args, p.expr())
				for p.accept(",") {
					args = append(args, p.expr())
				}
			}
			p.expect(")")
			x = &Call{t.Line, x, args}
		case p.accept("["):
			i := p.expr()
			p.expect("]")
			x = &Index{t.Line, x, i}
		case p.accept("."):
			x = &Field{t.Line, x, p.ident()}
		default:
			return x
		}
	}
}

func (p *Parser) primary() Expr {
	t := p.advance()
	switch t.Kind {
	case Int:
		v, err := strconv.ParseInt(t.Text, 10, 64)
		if err != nil {
			p.fail(t, "integer %s out of range", t.Text)
		}
		return &IntLit{t.Line, v}
	case Str:
		return &StrLit{t.Line, t.Text}
	case Ident:
		return &Name{t.Line, t.Text}
	case Keyword:
		switch t.Text {
		case "true", "false":
			return &BoolLit{t.Line, t.Text == "true"}
		case "nil":
			return &NilLit{t.Line}
		case "fn":
			return p.funcRest(t.Line, "")
		}
	case Punct:
		switch t.Text {
		case "(":
			x := p.expr()
			p.expect(")")
			return x
		case "[":
			l := &ListLit{Line: t.Line}
			for !p.is("]") {
				l.Elems = append(l.Elems, p.expr())
				if !p.accept(",") {
					break
				}
			}
			p.expect("]")
			return l
		case "{":
			m := &MapLit{Line: t.Line}
			for !p.is("}") {
				m.Keys = append(m.Keys, p.expr())
				p.expect(":")
				m.Vals = append(m.Vals, p.expr())
				if !p.accept(",") {
					break
				}
			}
			p.expect("}")
			return m
		}
	}
	p.fail(t, "unexpected %v", t)
	return nil
}
