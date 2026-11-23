package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Value is a run-time value: nil, int64, string, bool, *List, *Map,
// *Closure, or *Builtin.
type Value any

type List struct{ Elems []Value }

// Map is a mutable map with string or int keys, printed in key order.
type Map struct{ M map[Value]Value }

type Closure struct {
	Fn  *FuncLit
	Env *Env
}

type Builtin struct {
	Name string
	Fn   func(line int, args []Value) Value
}

func typeName(v Value) string {
	switch v.(type) {
	case nil:
		return "nil"
	case int64:
		return "int"
	case string:
		return "string"
	case bool:
		return "bool"
	case *List:
		return "list"
	case *Map:
		return "map"
	case *Closure, *Builtin:
		return "function"
	}
	return fmt.Sprintf("%T", v)
}

func show(v Value, quote bool) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case int64:
		return strconv.FormatInt(v, 10)
	case string:
		if quote {
			return strconv.Quote(v)
		}
		return v
	case bool:
		return strconv.FormatBool(v)
	case *List:
		parts := make([]string, len(v.Elems))
		for i, e := range v.Elems {
			parts[i] = show(e, true)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Map:
		keys := sortedKeys(v)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = show(k, true) + ": " + show(v.M[k], true)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *Closure:
		if v.Fn.Name != "" {
			return "<fn " + v.Fn.Name + ">"
		}
		return "<fn>"
	case *Builtin:
		return "<builtin " + v.Name + ">"
	}
	return "?"
}

// sortedKeys orders ints before strings, each in natural order.
func sortedKeys(m *Map) []Value {
	keys := make([]Value, 0, len(m.M))
	for k := range m.M {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, aInt := keys[i].(int64)
		b, bInt := keys[j].(int64)
		switch {
		case aInt && bInt:
			return a < b
		case aInt != bInt:
			return aInt
		}
		return keys[i].(string) < keys[j].(string)
	})
	return keys
}

func truthy(v Value) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case int64:
		return v != 0
	case string:
		return v != ""
	}
	return true
}

func equal(a, b Value) bool {
	switch a := a.(type) {
	case *List:
		bl, ok := b.(*List)
		if !ok || len(a.Elems) != len(bl.Elems) {
			return false
		}
		for i := range a.Elems {
			if !equal(a.Elems[i], bl.Elems[i]) {
				return false
			}
		}
		return true
	case *Map, *Closure, *Builtin:
		return a == b
	}
	return a == b
}
