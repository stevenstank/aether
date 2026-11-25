package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type filterFunc func(v any, args []any) (any, error)

var filters map[string]filterFunc

func init() {
	filters = map[string]filterFunc{
		"upper": func(v any, _ []any) (any, error) { return strings.ToUpper(toString(v)), nil },
		"lower": func(v any, _ []any) (any, error) { return strings.ToLower(toString(v)), nil },
		"title": func(v any, _ []any) (any, error) {
			words := strings.Fields(toString(v))
			for i, w := range words {
				r, n := utf8.DecodeRuneInString(w)
				words[i] = string(unicode.ToTitle(r)) + strings.ToLower(w[n:])
			}
			return strings.Join(words, " "), nil
		},
		"trim": func(v any, _ []any) (any, error) { return strings.TrimSpace(toString(v)), nil },
		"len": func(v any, _ []any) (any, error) {
			switch x := v.(type) {
			case []any:
				return float64(len(x)), nil
			case map[string]any:
				return float64(len(x)), nil
			case nil:
				return 0.0, nil
			}
			return float64(utf8.RuneCountInString(toString(v))), nil
		},
		"default": func(v any, args []any) (any, error) {
			if len(args) != 1 {
				return nil, errors.New("default takes 1 argument")
			}
			if !truthy(v) {
				return args[0], nil
			}
			return v, nil
		},
		"join": func(v any, args []any) (any, error) {
			list, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("join: want a list, got %s", typeName(v))
			}
			sep := ", "
			if len(args) > 0 {
				sep = toString(args[0])
			}
			parts := make([]string, len(list))
			for i, x := range list {
				parts[i] = toString(x)
			}
			return strings.Join(parts, sep), nil
		},
		"truncate": func(v any, args []any) (any, error) {
			if len(args) != 1 {
				return nil, errors.New("truncate takes 1 argument")
			}
			n, ok := args[0].(float64)
			if !ok || n < 0 {
				return nil, fmt.Errorf("truncate: bad length %v", args[0])
			}
			r := []rune(toString(v))
			if len(r) <= int(n) {
				return string(r), nil
			}
			return strings.TrimRightFunc(string(r[:int(n)]), unicode.IsSpace) + "…", nil
		},
		"money": func(v any, args []any) (any, error) {
			f, err := toNumber(v)
			if err != nil {
				return nil, fmt.Errorf("money: %w", err)
			}
			cur := "$"
			if len(args) > 0 {
				cur = toString(args[0])
			}
			neg := f < 0
			cents := int64(math.Round(math.Abs(f) * 100))
			whole := strconv.FormatInt(cents/100, 10)
			var b strings.Builder
			for i, c := range whole {
				if i > 0 && (len(whole)-i)%3 == 0 {
					b.WriteByte(',')
				}
				b.WriteRune(c)
			}
			s := fmt.Sprintf("%s%s.%02d", cur, b.String(), cents%100)
			if neg {
				s = "-" + s
			}
			return s, nil
		},
		"date": func(v any, args []any) (any, error) {
			t, err := time.Parse(time.RFC3339, toString(v))
			if err != nil {
				return nil, fmt.Errorf("date: %w", err)
			}
			layout := "Jan 2, 2006"
			if len(args) > 0 {
				layout = toString(args[0])
			}
			return t.Format(layout), nil
		},
		"add": func(v any, args []any) (any, error) {
			a, err := toNumber(v)
			if err != nil || len(args) != 1 {
				return nil, fmt.Errorf("add: bad operands")
			}
			b, err := toNumber(args[0])
			if err != nil {
				return nil, fmt.Errorf("add: %w", err)
			}
			return a + b, nil
		},
		"sortby": func(v any, args []any) (any, error) {
			list, ok := v.([]any)
			if !ok || len(args) != 1 {
				return nil, errors.New("sortby: want a list and a key")
			}
			key := toString(args[0])
			out := append([]any(nil), list...)
			sort.SliceStable(out, func(i, j int) bool {
				a, _ := lookupPath(out[i], strings.Split(key, "."))
				b, _ := lookupPath(out[j], strings.Split(key, "."))
				return compare(a, b) < 0
			})
			return out, nil
		},
		"json": func(v any, _ []any) (any, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
		"eq": func(v any, args []any) (any, error) {
			if len(args) != 1 {
				return nil, errors.New("eq takes 1 argument")
			}
			return compare(v, args[0]) == 0, nil
		},
		"gt": func(v any, args []any) (any, error) {
			if len(args) != 1 {
				return nil, errors.New("gt takes 1 argument")
			}
			return compare(v, args[0]) > 0, nil
		},
		"pluralize": func(v any, args []any) (any, error) {
			n, err := toNumber(v)
			if err != nil || len(args) != 2 {
				return nil, errors.New("pluralize: want a number and two words")
			}
			if n == 1 {
				return toString(args[0]), nil
			}
			return toString(args[1]), nil
		},
	}
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case []any:
		return "list"
	case map[string]any:
		return "object"
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case bool:
		return "bool"
	}
	return fmt.Sprintf("%T", v)
}

func toNumber(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case json.Number:
		return x.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(x), 64)
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("not a number: %s", typeName(v))
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = toString(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case map[string]any:
		b, _ := json.Marshal(x)
		return string(b)
	}
	return fmt.Sprint(v)
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case json.Number:
		f, _ := x.Float64()
		return f != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func compare(a, b any) int {
	fa, ea := toNumber(a)
	fb, eb := toNumber(b)
	if ea == nil && eb == nil {
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
		return 0
	}
	return strings.Compare(toString(a), toString(b))
}

func lookupPath(v any, path []string) (any, bool) {
	for _, p := range path {
		switch x := v.(type) {
		case map[string]any:
			var ok bool
			if v, ok = x[p]; !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// scope is one level of variable bindings; lookups fall back to parents.
type scope struct {
	vars   map[string]any
	dot    any
	parent *scope
}

type Engine struct {
	partials map[string]*Template
	strict   bool
	depth    int
}

func (s *scope) lookup(path []string) (any, bool) {
	for sc := s; sc != nil; sc = sc.parent {
		if v, ok := sc.vars[path[0]]; ok {
			return lookupPath(v, path[1:])
		}
		if v, ok := lookupPath(sc.dot, path[:1]); ok {
			return lookupPath(v, path[1:])
		}
	}
	return nil, false
}

func (e *Engine) eval(s *scope, x expr, p Position) (any, error) {
	if x.isLit {
		return x.literal, nil
	}
	if x.path == nil {
		return s.dot, nil
	}
	v, ok := s.lookup(x.path)
	if !ok && e.strict {
		return nil, &TemplateError{Pos: p, Exec: true, Msg: fmt.Sprintf("undefined value %q", strings.Join(x.path, "."))}
	}
	return v, nil
}

func (e *Engine) pipeline(s *scope, x expr, fs []filterCall, p Position) (any, error) {
	v, err := e.eval(s, x, p)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		args := make([]any, len(f.args))
		for i, a := range f.args {
			if args[i], err = e.eval(s, a, p); err != nil {
				return nil, err
			}
		}
		if v, err = filters[f.name](v, args); err != nil {
			return nil, &TemplateError{Pos: p, Exec: true, Msg: "filter " + f.name, Err: err}
		}
	}
	return v, nil
}

func (e *Engine) Execute(w *strings.Builder, t *Template, data any) error {
	return e.exec(w, t.root, &scope{vars: map[string]any{}, dot: data})
}

func (e *Engine) exec(w *strings.Builder, nodes []node, s *scope) error {
	for _, n := range nodes {
		switch n := n.(type) {
		case *textNode:
			w.WriteString(n.text)
		case *outputNode:
			v, err := e.pipeline(s, n.value, n.filters, n.p)
			if err != nil {
				return err
			}
			if n.raw {
				w.WriteString(toString(v))
			} else {
				w.WriteString(html.EscapeString(toString(v)))
			}
		case *setNode:
			v, err := e.pipeline(s, n.value, n.filters, n.p)
			if err != nil {
				return err
			}
			s.vars[n.name] = v
		case *ifNode:
			v, err := e.pipeline(s, n.cond, n.filters, n.p)
			if err != nil {
				return err
			}
			body := n.elseBody
			if truthy(v) != n.negate {
				body = n.then
			}
			if err := e.exec(w, body, &scope{vars: map[string]any{}, dot: s.dot, parent: s}); err != nil {
				return err
			}
		case *eachNode:
			if err := e.each(w, n, s); err != nil {
				return err
			}
		case *partialNode:
			pt, ok := e.partials[n.name]
			if !ok {
				return &TemplateError{Pos: n.p, Exec: true, Msg: fmt.Sprintf("no partial named %q", n.name)}
			}
			if e.depth >= 20 {
				return &TemplateError{Pos: n.p, Exec: true, Msg: "partials nested too deeply"}
			}
			ctx := s.dot
			if n.ctx != nil {
				v, err := e.eval(s, *n.ctx, n.p)
				if err != nil {
					return err
				}
				ctx = v
			}
			e.depth++
			err := e.exec(w, pt.root, &scope{vars: map[string]any{}, dot: ctx, parent: s})
			e.depth--
			if err != nil {
				return err
			}
		default:
			panic(fmt.Sprintf("unexpected node %T", n))
		}
	}
	return nil
}

func (e *Engine) each(w *strings.Builder, n *eachNode, s *scope) error {
	v, err := e.eval(s, n.list, n.p)
	if err != nil {
		return err
	}
	type entry struct {
		key string
		val any
	}
	var entries []entry
	switch x := v.(type) {
	case []any:
		for i, item := range x {
			entries = append(entries, entry{strconv.Itoa(i), item})
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			entries = append(entries, entry{k, x[k]})
		}
	case nil:
	default:
		return &TemplateError{Pos: n.p, Exec: true, Msg: fmt.Sprintf("cannot iterate over %s", typeName(v))}
	}
	if len(entries) == 0 {
		return e.exec(w, n.empty, &scope{vars: map[string]any{}, dot: s.dot, parent: s})
	}
	for i, en := range entries {
		vars := map[string]any{
			"@index": float64(i),
			"@key":   en.key,
			"@first": i == 0,
			"@last":  i == len(entries)-1,
		}
		dot := en.val
		if n.as != "" {
			vars[n.as] = en.val
			dot = s.dot
		}
		if err := e.exec(w, n.body, &scope{vars: vars, dot: dot, parent: s}); err != nil {
			return err
		}
	}
	return nil
}
