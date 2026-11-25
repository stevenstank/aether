package main

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Value is any Scheme value. Concrete types:
//
//	int64 (fixnum), *big.Int, *big.Rat, float64, bool, Symbol, *MString,
//	Char, *Pair, Empty, *Closure, *Primitive, *Vector, *Table, Unspecified
type Value any

type Symbol string

type MString struct{ s []rune }

type Char rune

type Pair struct {
	Car, Cdr Value
}

type emptyList struct{}

var Empty = &emptyList{}

type unspecified struct{}

var Unspecified = &unspecified{}

type Closure struct {
	Name   string
	Params []Symbol
	Rest   Symbol // "" when there is no rest parameter
	Body   []Value
	Env    *Env
}

type Primitive struct {
	Name    string
	MinArgs int
	MaxArgs int // -1 for variadic
	Fn      func(args []Value) Value
}

type Vector struct{ items []Value }

type Table struct {
	m    map[any]Value
	keys map[any]Value // original key values, for iteration
}

// SchemeError is raised by (error ...) and by primitive failures.
type SchemeError struct {
	Msg       string
	Irritants []Value
	Payload   Value // set by (raise obj)
}

func (e *SchemeError) Error() string {
	if e.Payload != nil {
		return "uncaught raise: " + Write(e.Payload)
	}
	var b strings.Builder
	b.WriteString(e.Msg)
	for _, x := range e.Irritants {
		b.WriteByte(' ')
		b.WriteString(Write(x))
	}
	return b.String()
}

func throw(format string, args ...any) {
	panic(&SchemeError{Msg: fmt.Sprintf(format, args...)})
}

func list(items ...Value) Value {
	var v Value = Empty
	for i := len(items) - 1; i >= 0; i-- {
		v = &Pair{items[i], v}
	}
	return v
}

func listToSlice(v Value) []Value {
	var out []Value
	for {
		switch p := v.(type) {
		case *Pair:
			out = append(out, p.Car)
			v = p.Cdr
		case *emptyList:
			return out
		default:
			throw("not a proper list: %s", Write(v))
		}
	}
}

func truthy(v Value) bool {
	b, ok := v.(bool)
	return !ok || b
}

func formatFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "+inf.0"
	case math.IsInf(f, -1):
		return "-inf.0"
	case math.IsNaN(f):
		return "+nan.0"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

var charNames = map[rune]string{' ': "space", '\n': "newline", '\t': "tab", 0: "nul"}

// Display renders v as display would; Write as write would.
func Display(v Value) string { return render(v, false) }
func Write(v Value) string   { return render(v, true) }

func render(v Value, quoted bool) string {
	var b strings.Builder
	renderTo(&b, v, quoted, 0)
	return b.String()
}

func renderTo(b *strings.Builder, v Value, quoted bool, depth int) {
	if depth > 200 {
		b.WriteString("...")
		return
	}
	switch x := v.(type) {
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case *big.Int:
		b.WriteString(x.String())
	case *big.Rat:
		b.WriteString(x.RatString())
	case float64:
		b.WriteString(formatFloat(x))
	case bool:
		if x {
			b.WriteString("#t")
		} else {
			b.WriteString("#f")
		}
	case Symbol:
		b.WriteString(string(x))
	case *MString:
		if !quoted {
			b.WriteString(string(x.s))
			return
		}
		b.WriteByte('"')
		for _, r := range x.s {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\n':
				b.WriteString(`\n`)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
	case Char:
		if !quoted {
			b.WriteRune(rune(x))
		} else if name, ok := charNames[rune(x)]; ok {
			b.WriteString(`#\` + name)
		} else {
			b.WriteString(`#\` + string(rune(x)))
		}
	case *emptyList:
		b.WriteString("()")
	case *Pair:
		if s, ok := x.Car.(Symbol); ok {
			if q, ok := x.Cdr.(*Pair); ok && q.Cdr == Empty {
				prefix := map[Symbol]string{"quote": "'", "quasiquote": "`", "unquote": ",", "unquote-splicing": ",@"}[s]
				if prefix != "" {
					b.WriteString(prefix)
					renderTo(b, q.Car, quoted, depth+1)
					return
				}
			}
		}
		b.WriteByte('(')
		first := true
		var cur Value = x
		for {
			p, ok := cur.(*Pair)
			if !ok {
				break
			}
			if !first {
				b.WriteByte(' ')
			}
			first = false
			renderTo(b, p.Car, quoted, depth+1)
			cur = p.Cdr
		}
		if cur != Empty {
			b.WriteString(" . ")
			renderTo(b, cur, quoted, depth+1)
		}
		b.WriteByte(')')
	case *Vector:
		b.WriteString("#(")
		for i, it := range x.items {
			if i > 0 {
				b.WriteByte(' ')
			}
			renderTo(b, it, quoted, depth+1)
		}
		b.WriteByte(')')
	case *Closure:
		if x.Name != "" {
			b.WriteString("#<procedure " + x.Name + ">")
		} else {
			b.WriteString("#<procedure>")
		}
	case *Primitive:
		b.WriteString("#<primitive " + x.Name + ">")
	case *Table:
		fmt.Fprintf(b, "#<hash-table %d>", len(x.m))
	case *unspecified:
		b.WriteString("#<unspecified>")
	default:
		fmt.Fprintf(b, "#<go %T>", v)
	}
}

// Env is a lexical environment frame.
type Env struct {
	vars   map[Symbol]Value
	parent *Env
}

func NewEnv(parent *Env) *Env { return &Env{vars: map[Symbol]Value{}, parent: parent} }

func (e *Env) Lookup(s Symbol) Value {
	for f := e; f != nil; f = f.parent {
		if v, ok := f.vars[s]; ok {
			return v
		}
	}
	throw("unbound variable: %s", s)
	return nil
}

func (e *Env) Set(s Symbol, v Value) {
	for f := e; f != nil; f = f.parent {
		if _, ok := f.vars[s]; ok {
			f.vars[s] = v
			return
		}
	}
	throw("set!: unbound variable: %s", s)
}

func (e *Env) Define(s Symbol, v Value) { e.vars[s] = v }
