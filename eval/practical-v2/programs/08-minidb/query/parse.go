// Package query parses minidb's SQL dialect.
package query

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tKeyword
	tInt
	tFloat
	tString
	tSym
)

type token struct {
	kind tokKind
	text string
	pos  int
}

var keywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`SELECT FROM WHERE AND OR NOT INSERT INTO VALUES UPDATE SET DELETE CREATE
		TABLE INDEX ON UNIQUE PRIMARY KEY ORDER BY ASC DESC LIMIT OFFSET GROUP AS NULL IS LIKE BEGIN COMMIT
		ROLLBACK EXPLAIN INT INTEGER FLOAT REAL TEXT DROP TRUE FALSE IN BETWEEN DISTINCT CHECKPOINT SHOW TABLES HAVING`) {
		keywords[k] = true
	}
}

type SyntaxError struct {
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("syntax error at %d: %s", e.Pos, e.Msg) }

func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := rune(src[i])
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '-' && strings.HasPrefix(src[i:], "--"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case unicode.IsLetter(c) || c == '_':
			j := i
			for j < len(src) && (unicode.IsLetter(rune(src[j])) || unicode.IsDigit(rune(src[j])) || src[j] == '_') {
				j++
			}
			w := src[i:j]
			if keywords[strings.ToUpper(w)] {
				toks = append(toks, token{tKeyword, strings.ToUpper(w), i})
			} else {
				toks = append(toks, token{tIdent, w, i})
			}
			i = j
		case unicode.IsDigit(c) || (c == '.' && i+1 < len(src) && unicode.IsDigit(rune(src[i+1]))):
			j := i
			isFloat := false
			for j < len(src) && (unicode.IsDigit(rune(src[j])) || src[j] == '.' || src[j] == 'e' || src[j] == 'E' ||
				((src[j] == '-' || src[j] == '+') && (src[j-1] == 'e' || src[j-1] == 'E'))) {
				if src[j] == '.' || src[j] == 'e' || src[j] == 'E' {
					isFloat = true
				}
				j++
			}
			k := tInt
			if isFloat {
				k = tFloat
			}
			toks = append(toks, token{k, src[i:j], i})
			i = j
		case c == '\'':
			var b strings.Builder
			j := i + 1
			for {
				if j >= len(src) {
					return nil, &SyntaxError{i, "unterminated string"}
				}
				if src[j] == '\'' {
					if j+1 < len(src) && src[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(src[j])
				j++
			}
			toks = append(toks, token{tString, b.String(), i})
			i = j + 1
		default:
			for _, op := range []string{"<=", ">=", "!=", "<>", "||"} {
				if strings.HasPrefix(src[i:], op) {
					toks = append(toks, token{tSym, op, i})
					i += 2
					goto next
				}
			}
			if strings.ContainsRune("(),;*=<>+-/%.", c) {
				toks = append(toks, token{tSym, string(c), i})
				i++
				continue
			}
			return nil, &SyntaxError{i, fmt.Sprintf("unexpected character %q", c)}
		next:
		}
	}
	return append(toks, token{kind: tEOF, pos: len(src)}), nil
}

// Expr is an expression tree node.
type Expr interface{ String() string }

type (
	Lit    struct{ Kind, Text string } // Kind: int, float, string, null, bool
	ColRef struct{ Name string }
	Unary  struct {
		Op string
		X  Expr
	}
	Binary struct {
		Op   string
		L, R Expr
	}
	Call struct {
		Name     string
		Args     []Expr
		Star     bool
		Distinct bool
	}
	IsNull struct {
		X   Expr
		Not bool
	}
	InList struct {
		X    Expr
		List []Expr
		Not  bool
	}
)

func (l *Lit) String() string {
	if l.Kind == "string" {
		return "'" + strings.ReplaceAll(l.Text, "'", "''") + "'"
	}
	return l.Text
}
func (c *ColRef) String() string { return c.Name }
func (u *Unary) String() string  { return "(" + u.Op + " " + u.X.String() + ")" }
func (b *Binary) String() string { return "(" + b.L.String() + " " + b.Op + " " + b.R.String() + ")" }
func (c *Call) String() string {
	if c.Star {
		return c.Name + "(*)"
	}
	parts := make([]string, len(c.Args))
	for i, a := range c.Args {
		parts[i] = a.String()
	}
	d := ""
	if c.Distinct {
		d = "DISTINCT "
	}
	return c.Name + "(" + d + strings.Join(parts, ", ") + ")"
}
func (n *IsNull) String() string {
	if n.Not {
		return n.X.String() + " IS NOT NULL"
	}
	return n.X.String() + " IS NULL"
}
func (n *InList) String() string {
	parts := make([]string, len(n.List))
	for i, a := range n.List {
		parts[i] = a.String()
	}
	not := ""
	if n.Not {
		not = "NOT "
	}
	return n.X.String() + " " + not + "IN (" + strings.Join(parts, ", ") + ")"
}

type ColumnDef struct {
	Name, Type               string
	Primary, Unique, NotNull bool
}

type SelectItem struct {
	X     Expr
	Alias string
}

type OrderItem struct {
	X    Expr
	Desc bool
}

type (
	CreateTable struct {
		Name string
		Cols []ColumnDef
	}
	CreateIndex struct {
		Name, Table, Col string
		Unique           bool
	}
	Insert struct {
		Table string
		Cols  []string
		Rows  [][]Expr
	}
	Select struct {
		Items   []SelectItem
		Star    bool
		Table   string
		Where   Expr
		GroupBy []Expr
		Having  Expr
		OrderBy []OrderItem
		Limit   int
		Offset  int
		Explain bool
	}
	Update struct {
		Table string
		Set   []string
		Exprs []Expr
		Where Expr
	}
	Delete struct {
		Table string
		Where Expr
	}
	DropTable  struct{ Name string }
	Begin      struct{}
	Commit     struct{}
	Rollback   struct{}
	Checkpoint struct{}
	ShowTables struct{}
)

type Stmt any

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

func (p *parser) is(kind tokKind, text string) bool {
	t := p.peek()
	return t.kind == kind && t.text == text
}

func (p *parser) accept(kind tokKind, text string) bool {
	if p.is(kind, text) {
		p.i++
		return true
	}
	return false
}

func (p *parser) fail(format string, args ...any) error {
	t := p.peek()
	where := "end of input"
	if t.kind != tEOF {
		where = fmt.Sprintf("%q", t.text)
	}
	return &SyntaxError{t.pos, fmt.Sprintf(format, args...) + " near " + where}
}

func (p *parser) expect(kind tokKind, text string) error {
	if !p.accept(kind, text) {
		return p.fail("expected %s", text)
	}
	return nil
}

func (p *parser) ident() (string, error) {
	t := p.peek()
	if t.kind != tIdent {
		return "", p.fail("expected a name")
	}
	p.i++
	return t.text, nil
}

// ParseScript splits src into statements at top-level semicolons.
func ParseScript(src string) ([]Stmt, []string, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, nil, err
	}
	var stmts []Stmt
	var texts []string
	p := &parser{toks: toks}
	for p.peek().kind != tEOF {
		if p.accept(tSym, ";") {
			continue
		}
		start := p.peek().pos
		s, err := p.statement()
		if err != nil {
			return stmts, texts, err
		}
		end := p.peek().pos
		if !p.accept(tSym, ";") && p.peek().kind != tEOF {
			return stmts, texts, p.fail("expected ;")
		}
		stmts = append(stmts, s)
		texts = append(texts, strings.TrimSpace(src[start:end]))
	}
	return stmts, texts, nil
}

func (p *parser) statement() (Stmt, error) {
	t := p.next()
	if t.kind != tKeyword {
		p.i--
		return nil, p.fail("expected a statement")
	}
	switch t.text {
	case "CREATE":
		unique := p.accept(tKeyword, "UNIQUE")
		switch {
		case !unique && p.accept(tKeyword, "TABLE"):
			return p.createTable()
		case p.accept(tKeyword, "INDEX"):
			name, err := p.ident()
			if err != nil {
				return nil, err
			}
			if err := p.expect(tKeyword, "ON"); err != nil {
				return nil, err
			}
			table, err := p.ident()
			if err != nil {
				return nil, err
			}
			if err := p.expect(tSym, "("); err != nil {
				return nil, err
			}
			col, err := p.ident()
			if err != nil {
				return nil, err
			}
			return &CreateIndex{name, table, col, unique}, p.expect(tSym, ")")
		}
		return nil, p.fail("expected TABLE or INDEX")
	case "DROP":
		if err := p.expect(tKeyword, "TABLE"); err != nil {
			return nil, err
		}
		name, err := p.ident()
		return &DropTable{name}, err
	case "INSERT":
		return p.insert()
	case "SELECT":
		return p.selectStmt()
	case "EXPLAIN":
		if err := p.expect(tKeyword, "SELECT"); err != nil {
			return nil, err
		}
		s, err := p.selectStmt()
		if err != nil {
			return nil, err
		}
		s.Explain = true
		return s, nil
	case "UPDATE":
		return p.update()
	case "DELETE":
		if err := p.expect(tKeyword, "FROM"); err != nil {
			return nil, err
		}
		table, err := p.ident()
		if err != nil {
			return nil, err
		}
		d := &Delete{Table: table}
		if p.accept(tKeyword, "WHERE") {
			if d.Where, err = p.expr(); err != nil {
				return nil, err
			}
		}
		return d, nil
	case "BEGIN":
		return &Begin{}, nil
	case "COMMIT":
		return &Commit{}, nil
	case "ROLLBACK":
		return &Rollback{}, nil
	case "CHECKPOINT":
		return &Checkpoint{}, nil
	case "SHOW":
		return &ShowTables{}, p.expect(tKeyword, "TABLES")
	}
	p.i--
	return nil, p.fail("unknown statement")
}

func (p *parser) createTable() (Stmt, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tSym, "("); err != nil {
		return nil, err
	}
	ct := &CreateTable{Name: name}
	for {
		var cd ColumnDef
		if cd.Name, err = p.ident(); err != nil {
			return nil, err
		}
		t := p.next()
		switch t.text {
		case "INT", "INTEGER":
			cd.Type = "INT"
		case "FLOAT", "REAL":
			cd.Type = "FLOAT"
		case "TEXT":
			cd.Type = "TEXT"
		default:
			p.i--
			return nil, p.fail("expected a column type")
		}
		for {
			switch {
			case p.accept(tKeyword, "PRIMARY"):
				if err := p.expect(tKeyword, "KEY"); err != nil {
					return nil, err
				}
				cd.Primary = true
			case p.accept(tKeyword, "UNIQUE"):
				cd.Unique = true
			case p.accept(tKeyword, "NOT"):
				if err := p.expect(tKeyword, "NULL"); err != nil {
					return nil, err
				}
				cd.NotNull = true
			default:
				goto done
			}
		}
	done:
		ct.Cols = append(ct.Cols, cd)
		if !p.accept(tSym, ",") {
			break
		}
	}
	return ct, p.expect(tSym, ")")
}

func (p *parser) insert() (Stmt, error) {
	if err := p.expect(tKeyword, "INTO"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	ins := &Insert{Table: table}
	if p.accept(tSym, "(") {
		for {
			c, err := p.ident()
			if err != nil {
				return nil, err
			}
			ins.Cols = append(ins.Cols, c)
			if !p.accept(tSym, ",") {
				break
			}
		}
		if err := p.expect(tSym, ")"); err != nil {
			return nil, err
		}
	}
	if err := p.expect(tKeyword, "VALUES"); err != nil {
		return nil, err
	}
	for {
		if err := p.expect(tSym, "("); err != nil {
			return nil, err
		}
		var row []Expr
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			row = append(row, e)
			if !p.accept(tSym, ",") {
				break
			}
		}
		if err := p.expect(tSym, ")"); err != nil {
			return nil, err
		}
		ins.Rows = append(ins.Rows, row)
		if !p.accept(tSym, ",") {
			break
		}
	}
	return ins, nil
}

func (p *parser) selectStmt() (*Select, error) {
	s := &Select{Limit: -1}
	if p.accept(tSym, "*") {
		s.Star = true
	} else {
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			it := SelectItem{X: e}
			if p.accept(tKeyword, "AS") {
				if it.Alias, err = p.ident(); err != nil {
					return nil, err
				}
			}
			s.Items = append(s.Items, it)
			if !p.accept(tSym, ",") {
				break
			}
		}
	}
	if err := p.expect(tKeyword, "FROM"); err != nil {
		return nil, err
	}
	var err error
	if s.Table, err = p.ident(); err != nil {
		return nil, err
	}
	if p.accept(tKeyword, "WHERE") {
		if s.Where, err = p.expr(); err != nil {
			return nil, err
		}
	}
	if p.accept(tKeyword, "GROUP") {
		if err := p.expect(tKeyword, "BY"); err != nil {
			return nil, err
		}
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			s.GroupBy = append(s.GroupBy, e)
			if !p.accept(tSym, ",") {
				break
			}
		}
		if p.accept(tKeyword, "HAVING") {
			if s.Having, err = p.expr(); err != nil {
				return nil, err
			}
		}
	}
	if p.accept(tKeyword, "ORDER") {
		if err := p.expect(tKeyword, "BY"); err != nil {
			return nil, err
		}
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			oi := OrderItem{X: e}
			if p.accept(tKeyword, "DESC") {
				oi.Desc = true
			} else {
				p.accept(tKeyword, "ASC")
			}
			s.OrderBy = append(s.OrderBy, oi)
			if !p.accept(tSym, ",") {
				break
			}
		}
	}
	if p.accept(tKeyword, "LIMIT") {
		t := p.next()
		n, err := strconv.Atoi(t.text)
		if t.kind != tInt || err != nil {
			p.i--
			return nil, p.fail("expected a number after LIMIT")
		}
		s.Limit = n
		if p.accept(tKeyword, "OFFSET") {
			t := p.next()
			if s.Offset, err = strconv.Atoi(t.text); t.kind != tInt || err != nil {
				p.i--
				return nil, p.fail("expected a number after OFFSET")
			}
		}
	}
	return s, nil
}

func (p *parser) update() (Stmt, error) {
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	u := &Update{Table: table}
	if err := p.expect(tKeyword, "SET"); err != nil {
		return nil, err
	}
	for {
		c, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tSym, "="); err != nil {
			return nil, err
		}
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		u.Set = append(u.Set, c)
		u.Exprs = append(u.Exprs, e)
		if !p.accept(tSym, ",") {
			break
		}
	}
	if p.accept(tKeyword, "WHERE") {
		if u.Where, err = p.expr(); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// Expression grammar, lowest precedence first:
// OR, AND, NOT, comparison/IS/LIKE/IN/BETWEEN, || + -, * / %, unary -, primary.
func (p *parser) expr() (Expr, error) { return p.or() }

func (p *parser) or() (Expr, error) {
	l, err := p.and()
	for err == nil && p.accept(tKeyword, "OR") {
		var r Expr
		r, err = p.and()
		l = &Binary{"OR", l, r}
	}
	return l, err
}

func (p *parser) and() (Expr, error) {
	l, err := p.not()
	for err == nil && p.accept(tKeyword, "AND") {
		var r Expr
		r, err = p.not()
		l = &Binary{"AND", l, r}
	}
	return l, err
}

func (p *parser) not() (Expr, error) {
	if p.accept(tKeyword, "NOT") {
		x, err := p.not()
		return &Unary{"NOT", x}, err
	}
	return p.comparison()
}

func (p *parser) comparison() (Expr, error) {
	l, err := p.additive()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	switch {
	case t.kind == tSym && strings.Contains(" = != <> < <= > >= ", " "+t.text+" "):
		p.i++
		r, err := p.additive()
		op := t.text
		if op == "<>" {
			op = "!="
		}
		return &Binary{op, l, r}, err
	case p.accept(tKeyword, "IS"):
		not := p.accept(tKeyword, "NOT")
		return &IsNull{l, not}, p.expect(tKeyword, "NULL")
	}
	not := p.accept(tKeyword, "NOT")
	switch {
	case p.accept(tKeyword, "LIKE"):
		r, err := p.additive()
		var e Expr = &Binary{"LIKE", l, r}
		if not {
			e = &Unary{"NOT", e}
		}
		return e, err
	case p.accept(tKeyword, "IN"):
		if err := p.expect(tSym, "("); err != nil {
			return nil, err
		}
		in := &InList{X: l, Not: not}
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			in.List = append(in.List, e)
			if !p.accept(tSym, ",") {
				break
			}
		}
		return in, p.expect(tSym, ")")
	case p.accept(tKeyword, "BETWEEN"):
		lo, err := p.additive()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tKeyword, "AND"); err != nil {
			return nil, err
		}
		hi, err := p.additive()
		var e Expr = &Binary{"AND", &Binary{">=", l, lo}, &Binary{"<=", l, hi}}
		if not {
			e = &Unary{"NOT", e}
		}
		return e, err
	}
	if not {
		return nil, p.fail("expected LIKE, IN, or BETWEEN after NOT")
	}
	return l, nil
}

func (p *parser) additive() (Expr, error) {
	l, err := p.multiplicative()
	for err == nil {
		t := p.peek()
		if t.kind != tSym || (t.text != "+" && t.text != "-" && t.text != "||") {
			break
		}
		p.i++
		var r Expr
		r, err = p.multiplicative()
		l = &Binary{t.text, l, r}
	}
	return l, err
}

func (p *parser) multiplicative() (Expr, error) {
	l, err := p.unary()
	for err == nil {
		t := p.peek()
		if t.kind != tSym || (t.text != "*" && t.text != "/" && t.text != "%") {
			break
		}
		p.i++
		var r Expr
		r, err = p.unary()
		l = &Binary{t.text, l, r}
	}
	return l, err
}

func (p *parser) unary() (Expr, error) {
	if p.accept(tSym, "-") {
		x, err := p.unary()
		return &Unary{"-", x}, err
	}
	return p.primary()
}

func (p *parser) primary() (Expr, error) {
	t := p.next()
	switch t.kind {
	case tInt:
		return &Lit{"int", t.text}, nil
	case tFloat:
		return &Lit{"float", t.text}, nil
	case tString:
		return &Lit{"string", t.text}, nil
	case tKeyword:
		switch t.text {
		case "NULL":
			return &Lit{"null", "NULL"}, nil
		case "TRUE":
			return &Lit{"int", "1"}, nil
		case "FALSE":
			return &Lit{"int", "0"}, nil
		}
	case tIdent:
		if p.accept(tSym, "(") {
			c := &Call{Name: strings.ToUpper(t.text)}
			if p.accept(tSym, "*") {
				c.Star = true
				return c, p.expect(tSym, ")")
			}
			c.Distinct = p.accept(tKeyword, "DISTINCT")
			if !p.is(tSym, ")") {
				for {
					e, err := p.expr()
					if err != nil {
						return nil, err
					}
					c.Args = append(c.Args, e)
					if !p.accept(tSym, ",") {
						break
					}
				}
			}
			return c, p.expect(tSym, ")")
		}
		return &ColRef{t.text}, nil
	case tSym:
		if t.text == "(" {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			return e, p.expect(tSym, ")")
		}
	}
	p.i--
	return nil, p.fail("expected an expression")
}
