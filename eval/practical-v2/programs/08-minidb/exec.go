package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"example.com/minidb/query"
	"example.com/minidb/store"
)

type DB struct {
	tables map[string]*store.Table
	log    *store.UndoLog
}

func NewDB() *DB { return &DB{tables: map[string]*store.Table{}, log: &store.UndoLog{}} }

var ErrNoTable = errors.New("no such table")

type ExecError struct {
	Stmt string
	Err  error
}

func (e *ExecError) Error() string { return e.Err.Error() }
func (e *ExecError) Unwrap() error { return e.Err }

func (db *DB) table(name string) (*store.Table, error) {
	t, ok := db.tables[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoTable, name)
	}
	return t, nil
}

// env evaluates expressions against one row (and optionally a group).
type env struct {
	t     *store.Table
	row   store.Row
	group []store.Row // non-nil while evaluating aggregates
}

func isAggregate(name string) bool {
	switch name {
	case "COUNT", "SUM", "AVG", "MIN", "MAX":
		return true
	}
	return false
}

func hasAggregate(e query.Expr) bool {
	switch e := e.(type) {
	case *query.Call:
		if isAggregate(e.Name) {
			return true
		}
		for _, a := range e.Args {
			if hasAggregate(a) {
				return true
			}
		}
	case *query.Binary:
		return hasAggregate(e.L) || hasAggregate(e.R)
	case *query.Unary:
		return hasAggregate(e.X)
	case *query.IsNull:
		return hasAggregate(e.X)
	}
	return false
}

func truth(v store.Value) bool {
	n, ok := v.Num()
	return ok && n != 0
}

func boolV(b bool) store.Value {
	if b {
		return store.IntV(1)
	}
	return store.IntV(0)
}

// like implements SQL LIKE with % and _ (case-insensitive).
func like(s, pat string) bool {
	s, pat = strings.ToLower(s), strings.ToLower(pat)
	var match func(si, pi int) bool
	match = func(si, pi int) bool {
		for pi < len(pat) {
			switch pat[pi] {
			case '%':
				for k := si; k <= len(s); k++ {
					if match(k, pi+1) {
						return true
					}
				}
				return false
			case '_':
				if si >= len(s) {
					return false
				}
			default:
				if si >= len(s) || s[si] != pat[pi] {
					return false
				}
			}
			si++
			pi++
		}
		return si == len(s)
	}
	return match(0, 0)
}

func arith(op string, a, b store.Value) (store.Value, error) {
	if a.K == store.Null || b.K == store.Null {
		return store.Value{}, nil
	}
	if a.K == store.Int && b.K == store.Int {
		x, y := a.I, b.I
		switch op {
		case "+":
			return store.IntV(x + y), nil
		case "-":
			return store.IntV(x - y), nil
		case "*":
			return store.IntV(x * y), nil
		case "/", "%":
			if y == 0 {
				return store.Value{}, errors.New("division by zero")
			}
			if op == "/" {
				return store.IntV(x / y), nil
			}
			return store.IntV(x % y), nil
		}
	}
	x, ok1 := a.Num()
	y, ok2 := b.Num()
	if !ok1 || !ok2 {
		return store.Value{}, fmt.Errorf("cannot apply %s to %s and %s", op, a.K, b.K)
	}
	switch op {
	case "+":
		return store.FloatV(x + y), nil
	case "-":
		return store.FloatV(x - y), nil
	case "*":
		return store.FloatV(x * y), nil
	case "/":
		if y == 0 {
			return store.Value{}, errors.New("division by zero")
		}
		return store.FloatV(x / y), nil
	}
	return store.FloatV(math.Mod(x, y)), nil
}

func (en *env) eval(e query.Expr) (store.Value, error) {
	switch e := e.(type) {
	case *query.Lit:
		switch e.Kind {
		case "int":
			n, err := strconv.ParseInt(e.Text, 10, 64)
			if err != nil {
				return store.Value{}, fmt.Errorf("integer literal %s out of range", e.Text)
			}
			return store.IntV(n), nil
		case "float":
			f, err := strconv.ParseFloat(e.Text, 64)
			if err != nil {
				return store.Value{}, fmt.Errorf("bad number %s", e.Text)
			}
			return store.FloatV(f), nil
		case "string":
			return store.TextV(e.Text), nil
		}
		return store.Value{}, nil
	case *query.ColRef:
		i := en.t.ColIndex(e.Name)
		if i < 0 {
			return store.Value{}, fmt.Errorf("no column %s in %s", e.Name, en.t.Name)
		}
		if en.row == nil {
			return store.Value{}, nil
		}
		return en.row[i], nil
	case *query.Unary:
		v, err := en.eval(e.X)
		if err != nil || v.K == store.Null {
			return v, err
		}
		if e.Op == "NOT" {
			return boolV(!truth(v)), nil
		}
		return arith("-", store.IntV(0), v)
	case *query.IsNull:
		v, err := en.eval(e.X)
		return boolV((v.K == store.Null) != e.Not), err
	case *query.InList:
		v, err := en.eval(e.X)
		if err != nil || v.K == store.Null {
			return store.Value{}, err
		}
		for _, x := range e.List {
			w, err := en.eval(x)
			if err != nil {
				return w, err
			}
			if w.K != store.Null && store.Compare(v, w) == 0 {
				return boolV(!e.Not), nil
			}
		}
		return boolV(e.Not), nil
	case *query.Binary:
		l, err := en.eval(e.L)
		if err != nil {
			return l, err
		}
		// Short-circuit three-valued AND/OR.
		if e.Op == "AND" && l.K != store.Null && !truth(l) {
			return boolV(false), nil
		}
		if e.Op == "OR" && l.K != store.Null && truth(l) {
			return boolV(true), nil
		}
		r, err := en.eval(e.R)
		if err != nil {
			return r, err
		}
		switch e.Op {
		case "AND", "OR":
			if r.K == store.Null || l.K == store.Null {
				if e.Op == "AND" && r.K != store.Null && !truth(r) {
					return boolV(false), nil
				}
				if e.Op == "OR" && r.K != store.Null && truth(r) {
					return boolV(true), nil
				}
				return store.Value{}, nil
			}
			return boolV(truth(r)), nil
		case "||":
			if l.K == store.Null || r.K == store.Null {
				return store.Value{}, nil
			}
			return store.TextV(l.String() + r.String()), nil
		case "LIKE":
			if l.K == store.Null || r.K == store.Null {
				return store.Value{}, nil
			}
			return boolV(like(l.String(), r.String())), nil
		case "=", "!=", "<", "<=", ">", ">=":
			if l.K == store.Null || r.K == store.Null {
				return store.Value{}, nil
			}
			c := store.Compare(l, r)
			var b bool
			switch e.Op {
			case "=":
				b = c == 0
			case "!=":
				b = c != 0
			case "<":
				b = c < 0
			case "<=":
				b = c <= 0
			case ">":
				b = c > 0
			default:
				b = c >= 0
			}
			return boolV(b), nil
		}
		return arith(e.Op, l, r)
	case *query.Call:
		if isAggregate(e.Name) {
			return en.aggregate(e)
		}
		args := make([]store.Value, len(e.Args))
		for i, a := range e.Args {
			v, err := en.eval(a)
			if err != nil {
				return v, err
			}
			args[i] = v
		}
		return scalar(e.Name, args)
	}
	return store.Value{}, fmt.Errorf("cannot evaluate %T", e)
}

func scalar(name string, args []store.Value) (store.Value, error) {
	want := map[string]int{"UPPER": 1, "LOWER": 1, "LENGTH": 1, "ABS": 1, "ROUND": 2, "SUBSTR": 3}
	if n, ok := want[name]; ok && len(args) != n {
		return store.Value{}, fmt.Errorf("%s takes %d arguments", name, n)
	}
	switch name {
	case "COALESCE":
		for _, a := range args {
			if a.K != store.Null {
				return a, nil
			}
		}
		return store.Value{}, nil
	}
	if len(args) > 0 && args[0].K == store.Null {
		return store.Value{}, nil
	}
	switch name {
	case "UPPER":
		return store.TextV(strings.ToUpper(args[0].String())), nil
	case "LOWER":
		return store.TextV(strings.ToLower(args[0].String())), nil
	case "LENGTH":
		return store.IntV(int64(len([]rune(args[0].String())))), nil
	case "ABS":
		if args[0].K == store.Int {
			if args[0].I < 0 {
				return store.IntV(-args[0].I), nil
			}
			return args[0], nil
		}
		f, ok := args[0].Num()
		if !ok {
			return store.Value{}, errors.New("ABS of non-number")
		}
		return store.FloatV(math.Abs(f)), nil
	case "ROUND":
		f, ok1 := args[0].Num()
		d, ok2 := args[1].Num()
		if !ok1 || !ok2 {
			return store.Value{}, errors.New("ROUND of non-number")
		}
		p := math.Pow(10, d)
		return store.FloatV(math.Round(f*p) / p), nil
	case "SUBSTR":
		r := []rune(args[0].String())
		start, ok1 := args[1].Num()
		n, ok2 := args[2].Num()
		if !ok1 || !ok2 {
			return store.Value{}, errors.New("SUBSTR bounds must be numbers")
		}
		s := min(max(int(start)-1, 0), len(r))
		e := min(s+max(int(n), 0), len(r))
		return store.TextV(string(r[s:e])), nil
	}
	return store.Value{}, fmt.Errorf("unknown function %s", name)
}

func (en *env) aggregate(c *query.Call) (store.Value, error) {
	if en.group == nil {
		return store.Value{}, fmt.Errorf("aggregate %s used outside of a query", c.Name)
	}
	if c.Star {
		if c.Name != "COUNT" {
			return store.Value{}, fmt.Errorf("%s(*) is not allowed", c.Name)
		}
		return store.IntV(int64(len(en.group))), nil
	}
	if len(c.Args) != 1 {
		return store.Value{}, fmt.Errorf("%s takes one argument", c.Name)
	}
	var vals []store.Value
	seen := map[store.Value]bool{}
	for _, r := range en.group {
		sub := &env{t: en.t, row: r}
		v, err := sub.eval(c.Args[0])
		if err != nil {
			return v, err
		}
		if v.K == store.Null || (c.Distinct && seen[v]) {
			continue
		}
		seen[v] = true
		vals = append(vals, v)
	}
	switch c.Name {
	case "COUNT":
		return store.IntV(int64(len(vals))), nil
	case "MIN", "MAX":
		if len(vals) == 0 {
			return store.Value{}, nil
		}
		best := vals[0]
		for _, v := range vals[1:] {
			cmp := store.Compare(v, best)
			if (c.Name == "MIN" && cmp < 0) || (c.Name == "MAX" && cmp > 0) {
				best = v
			}
		}
		return best, nil
	}
	if len(vals) == 0 {
		return store.Value{}, nil
	}
	allInt := true
	var isum int64
	fsum := 0.0
	for _, v := range vals {
		f, ok := v.Num()
		if !ok {
			return store.Value{}, fmt.Errorf("%s of non-number %q", c.Name, v.String())
		}
		fsum += f
		if v.K == store.Int {
			isum += v.I
		} else {
			allInt = false
		}
	}
	if c.Name == "SUM" {
		if allInt {
			return store.IntV(isum), nil
		}
		return store.FloatV(fsum), nil
	}
	return store.FloatV(fsum / float64(len(vals))), nil
}

// plan describes how a SELECT/UPDATE/DELETE finds candidate rows.
type plan struct {
	index  *store.Index
	lo, hi *store.Value
	desc   string
}

// choosePlan looks for an indexed column compared with a constant in the
// top-level AND conjuncts of where. Equality beats ranges; unique beats
// non-unique.
func choosePlan(t *store.Table, where query.Expr) plan {
	var conj []query.Expr
	var split func(e query.Expr)
	split = func(e query.Expr) {
		if b, ok := e.(*query.Binary); ok && b.Op == "AND" {
			split(b.L)
			split(b.R)
			return
		}
		conj = append(conj, e)
	}
	if where != nil {
		split(where)
	}
	best := plan{desc: "full scan of " + t.Name}
	bestScore := 0
	for _, c := range conj {
		b, ok := c.(*query.Binary)
		if !ok {
			continue
		}
		col, lit, op := b.L, b.R, b.Op
		if _, isCol := col.(*query.ColRef); !isCol {
			col, lit = b.R, b.L
			op = map[string]string{"<": ">", "<=": ">=", ">": "<", ">=": "<=", "=": "="}[op]
		}
		cr, ok1 := col.(*query.ColRef)
		l, ok2 := lit.(*query.Lit)
		if !ok1 || !ok2 || l.Kind == "null" || op == "" {
			continue
		}
		v, err := (&env{t: t}).eval(l)
		if err != nil {
			continue
		}
		ci := t.ColIndex(cr.Name)
		for _, ix := range t.Indexes {
			if ix.Col != ci {
				continue
			}
			score := 0
			p := plan{index: ix}
			vv := v
			switch op {
			case "=":
				score = 3
				if ix.Unique {
					score = 4
				}
				p.lo, p.hi = &vv, &vv
				p.desc = fmt.Sprintf("index %s lookup %s = %s", ix.Name, cr.Name, v)
			case ">", ">=":
				score = 1
				p.lo = &vv
				p.desc = fmt.Sprintf("index %s range %s %s %s", ix.Name, cr.Name, op, v)
			case "<", "<=":
				score = 1
				p.hi = &vv
				p.desc = fmt.Sprintf("index %s range %s %s %s", ix.Name, cr.Name, op, v)
			}
			if score > bestScore {
				best, bestScore = p, score
			}
		}
	}
	return best
}

// matching returns the ids of rows satisfying where, in id order.
func (db *DB) matching(t *store.Table, where query.Expr) ([]int64, plan, error) {
	p := choosePlan(t, where)
	var ids []int64
	var err error
	test := func(id int64, row store.Row) bool {
		if where != nil {
			v, e := (&env{t: t, row: row}).eval(where)
			if e != nil {
				err = e
				return false
			}
			if !truth(v) {
				return true
			}
		}
		ids = append(ids, id)
		return true
	}
	if p.index != nil {
		p.index.Scan(p.lo, p.hi, func(id int64) bool {
			row, _ := t.Get(id)
			return test(id, row)
		})
		slices.Sort(ids)
	} else {
		t.Scan(test)
	}
	return ids, p, err
}
