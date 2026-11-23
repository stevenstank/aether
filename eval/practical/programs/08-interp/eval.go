package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Env struct {
	vars   map[string]Value
	parent *Env
}

func NewEnv(parent *Env) *Env { return &Env{map[string]Value{}, parent} }

func (e *Env) lookup(name string) (*Env, bool) {
	for env := e; env != nil; env = env.parent {
		if _, ok := env.vars[name]; ok {
			return env, true
		}
	}
	return nil, false
}

// Control-flow signals, carried by panics through the tree walk.
type (
	returnSignal   struct{ val Value }
	breakSignal    struct{}
	continueSignal struct{}
	runtimeError   struct{ err *Error }
)

type Interp struct {
	out     io.Writer
	globals *Env
	depth   int
	steps   int
	MaxStep int
}

func (in *Interp) fail(line int, format string, args ...any) {
	panic(runtimeError{&Error{"runtime", line, fmt.Sprintf(format, args...)}})
}

func NewInterp(out io.Writer) *Interp {
	in := &Interp{out: out, globals: NewEnv(nil), MaxStep: 50_000_000}
	def := func(name string, fn func(line int, args []Value) Value) {
		in.globals.vars[name] = &Builtin{name, fn}
	}
	argc := func(line int, name string, args []Value, n int) {
		if len(args) != n {
			in.fail(line, "%s expects %d argument(s), got %d", name, n, len(args))
		}
	}
	def("len", func(line int, args []Value) Value {
		argc(line, "len", args, 1)
		switch v := args[0].(type) {
		case string:
			return int64(len(v))
		case *List:
			return int64(len(v.Elems))
		case *Map:
			return int64(len(v.M))
		}
		in.fail(line, "len of %s", typeName(args[0]))
		return nil
	})
	def("push", func(line int, args []Value) Value {
		if len(args) < 2 {
			in.fail(line, "push expects a list and values")
		}
		l, ok := args[0].(*List)
		if !ok {
			in.fail(line, "push to %s", typeName(args[0]))
		}
		l.Elems = append(l.Elems, args[1:]...)
		return l
	})
	def("pop", func(line int, args []Value) Value {
		argc(line, "pop", args, 1)
		l, ok := args[0].(*List)
		if !ok || len(l.Elems) == 0 {
			in.fail(line, "pop from empty or non-list value")
		}
		v := l.Elems[len(l.Elems)-1]
		l.Elems = l.Elems[:len(l.Elems)-1]
		return v
	})
	def("keys", func(line int, args []Value) Value {
		argc(line, "keys", args, 1)
		m, ok := args[0].(*Map)
		if !ok {
			in.fail(line, "keys of %s", typeName(args[0]))
		}
		return &List{sortedKeys(m)}
	})
	def("str", func(line int, args []Value) Value {
		argc(line, "str", args, 1)
		return show(args[0], false)
	})
	def("int", func(line int, args []Value) Value {
		argc(line, "int", args, 1)
		switch v := args[0].(type) {
		case int64:
			return v
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return nil
			}
			return n
		case bool:
			if v {
				return int64(1)
			}
			return int64(0)
		}
		in.fail(line, "cannot convert %s to int", typeName(args[0]))
		return nil
	})
	def("type", func(line int, args []Value) Value {
		argc(line, "type", args, 1)
		return typeName(args[0])
	})
	def("range", func(line int, args []Value) Value {
		argc(line, "range", args, 1)
		n, ok := args[0].(int64)
		if !ok || n < 0 || n > 10_000_000 {
			in.fail(line, "bad range argument %s", show(args[0], true))
		}
		l := &List{make([]Value, n)}
		for i := range l.Elems {
			l.Elems[i] = int64(i)
		}
		return l
	})
	def("split", func(line int, args []Value) Value {
		argc(line, "split", args, 2)
		s, ok1 := args[0].(string)
		sep, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			in.fail(line, "split expects two strings")
		}
		parts := strings.Split(s, sep)
		l := &List{make([]Value, len(parts))}
		for i, p := range parts {
			l.Elems[i] = p
		}
		return l
	})
	def("join", func(line int, args []Value) Value {
		argc(line, "join", args, 2)
		l, ok1 := args[0].(*List)
		sep, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			in.fail(line, "join expects a list and a string")
		}
		parts := make([]string, len(l.Elems))
		for i, e := range l.Elems {
			parts[i] = show(e, false)
		}
		return strings.Join(parts, sep)
	})
	def("error", func(line int, args []Value) Value {
		argc(line, "error", args, 1)
		in.fail(line, "%s", show(args[0], false))
		return nil
	})
	return in
}

// Run executes a program and returns the first runtime error.
func (in *Interp) Run(prog []Stmt) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch r := r.(type) {
			case runtimeError:
				err = r.err
			case returnSignal:
				err = &Error{"runtime", 0, "return outside function"}
			case breakSignal, continueSignal:
				err = &Error{"runtime", 0, "break or continue outside loop"}
			default:
				panic(r)
			}
		}
	}()
	in.execBlock(prog, in.globals)
	return nil
}

func (in *Interp) execBlock(stmts []Stmt, env *Env) {
	for _, s := range stmts {
		in.exec(s, env)
	}
}

func (in *Interp) exec(s Stmt, env *Env) {
	in.steps++
	if in.steps > in.MaxStep {
		in.fail(0, "step limit exceeded")
	}
	switch s := s.(type) {
	case *LetStmt:
		if f, ok := s.Init.(*FuncLit); ok && f.Name != "" {
			env.vars[s.Name] = nil // allow recursion
		}
		env.vars[s.Name] = in.eval(s.Init, env)
	case *AssignStmt:
		in.assign(s, env)
	case *ExprStmt:
		in.eval(s.X, env)
	case *PrintStmt:
		parts := make([]string, len(s.Args))
		for i, a := range s.Args {
			parts[i] = show(in.eval(a, env), false)
		}
		fmt.Fprintln(in.out, strings.Join(parts, " "))
	case *IfStmt:
		if truthy(in.eval(s.Cond, env)) {
			in.execBlock(s.Then, NewEnv(env))
		} else if s.Else != nil {
			in.execBlock(s.Else, NewEnv(env))
		}
	case *WhileStmt:
		for truthy(in.eval(s.Cond, env)) {
			if in.loopBody(s.Body, NewEnv(env)) {
				break
			}
		}
	case *ForStmt:
		var items []Value
		switch seq := in.eval(s.Seq, env).(type) {
		case *List:
			items = append(items, seq.Elems...)
		case *Map:
			items = sortedKeys(seq)
		case string:
			for _, r := range seq {
				items = append(items, string(r))
			}
		default:
			in.fail(s.Line, "cannot iterate over %s", typeName(seq))
		}
		for _, it := range items {
			body := NewEnv(env)
			body.vars[s.Var] = it
			if in.loopBody(s.Body, body) {
				break
			}
		}
	case *ReturnStmt:
		var v Value
		if s.Val != nil {
			v = in.eval(s.Val, env)
		}
		panic(returnSignal{v})
	case *BreakStmt:
		panic(breakSignal{})
	case *ContinueStmt:
		panic(continueSignal{})
	}
}

// loopBody runs one iteration and reports whether the loop should stop.
func (in *Interp) loopBody(body []Stmt, env *Env) (stop bool) {
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case breakSignal:
				stop = true
			case continueSignal:
				stop = false
			default:
				panic(r)
			}
		}
	}()
	in.execBlock(body, env)
	return false
}

func (in *Interp) assign(s *AssignStmt, env *Env) {
	val := in.eval(s.Val, env)
	combine := func(old Value) Value {
		switch s.Op {
		case "+=":
			return in.binop(s.Line, "+", old, val)
		case "-=":
			return in.binop(s.Line, "-", old, val)
		}
		return val
	}
	switch t := s.Target.(type) {
	case *Name:
		scope, ok := env.lookup(t.Name)
		if !ok {
			in.fail(s.Line, "assignment to undeclared variable %s", t.Name)
		}
		scope.vars[t.Name] = combine(scope.vars[t.Name])
	case *Index:
		container := in.eval(t.X, env)
		key := in.eval(t.I, env)
		switch c := container.(type) {
		case *List:
			i := in.listIndex(t.Line, c, key)
			c.Elems[i] = combine(c.Elems[i])
		case *Map:
			in.checkKey(t.Line, key)
			c.M[key] = combine(c.M[key])
		default:
			in.fail(s.Line, "cannot index-assign %s", typeName(container))
		}
	}
}

func (in *Interp) listIndex(line int, l *List, key Value) int {
	i, ok := key.(int64)
	if !ok {
		in.fail(line, "list index must be int, not %s", typeName(key))
	}
	if i < 0 {
		i += int64(len(l.Elems))
	}
	if i < 0 || i >= int64(len(l.Elems)) {
		in.fail(line, "index %d out of range for list of length %d", key, len(l.Elems))
	}
	return int(i)
}

func (in *Interp) checkKey(line int, k Value) {
	switch k.(type) {
	case int64, string:
	default:
		in.fail(line, "map key must be int or string, not %s", typeName(k))
	}
}

func (in *Interp) eval(e Expr, env *Env) Value {
	switch e := e.(type) {
	case *IntLit:
		return e.Val
	case *StrLit:
		return e.Val
	case *BoolLit:
		return e.Val
	case *NilLit:
		return nil
	case *Name:
		scope, ok := env.lookup(e.Name)
		if !ok {
			in.fail(e.Line, "undefined: %s", e.Name)
		}
		return scope.vars[e.Name]
	case *Unary:
		x := in.eval(e.X, env)
		if e.Op == "not" {
			return !truthy(x)
		}
		n, ok := x.(int64)
		if !ok {
			in.fail(e.Line, "cannot negate %s", typeName(x))
		}
		return -n
	case *Binary:
		switch e.Op {
		case "and":
			x := in.eval(e.X, env)
			if !truthy(x) {
				return x
			}
			return in.eval(e.Y, env)
		case "or":
			x := in.eval(e.X, env)
			if truthy(x) {
				return x
			}
			return in.eval(e.Y, env)
		}
		return in.binop(e.Line, e.Op, in.eval(e.X, env), in.eval(e.Y, env))
	case *ListLit:
		l := &List{make([]Value, len(e.Elems))}
		for i, x := range e.Elems {
			l.Elems[i] = in.eval(x, env)
		}
		return l
	case *MapLit:
		m := &Map{map[Value]Value{}}
		for i := range e.Keys {
			k := in.eval(e.Keys[i], env)
			in.checkKey(e.Line, k)
			m.M[k] = in.eval(e.Vals[i], env)
		}
		return m
	case *FuncLit:
		return &Closure{e, env}
	case *Index:
		x, k := in.eval(e.X, env), in.eval(e.I, env)
		switch c := x.(type) {
		case *List:
			return c.Elems[in.listIndex(e.Line, c, k)]
		case *Map:
			return c.M[k]
		case string:
			i, ok := k.(int64)
			if !ok || i < 0 || i >= int64(len(c)) {
				in.fail(e.Line, "string index %s out of range", show(k, true))
			}
			return c[i : i+1]
		}
		in.fail(e.Line, "cannot index %s", typeName(x))
	case *Field:
		m, ok := in.eval(e.X, env).(*Map)
		if !ok {
			in.fail(e.Line, "field access on non-map")
		}
		return m.M[e.Name]
	case *Call:
		fn := in.eval(e.Fn, env)
		args := make([]Value, len(e.Args))
		for i, a := range e.Args {
			args[i] = in.eval(a, env)
		}
		return in.call(e.Line, fn, args)
	}
	in.fail(e.line(), "unknown expression")
	return nil
}

func (in *Interp) call(line int, fn Value, args []Value) (result Value) {
	switch f := fn.(type) {
	case *Builtin:
		return f.Fn(line, args)
	case *Closure:
		if len(args) != len(f.Fn.Params) {
			in.fail(line, "%s expects %d argument(s), got %d", show(f, false), len(f.Fn.Params), len(args))
		}
		in.depth++
		if in.depth > 2000 {
			in.fail(line, "call depth exceeded")
		}
		env := NewEnv(f.Env)
		for i, p := range f.Fn.Params {
			env.vars[p] = args[i]
		}
		defer func() {
			in.depth--
			if r := recover(); r != nil {
				ret, ok := r.(returnSignal)
				if !ok {
					panic(r)
				}
				result = ret.val
			}
		}()
		in.execBlock(f.Fn.Body, env)
		return nil
	}
	in.fail(line, "cannot call %s", typeName(fn))
	return nil
}

func (in *Interp) binop(line int, op string, x, y Value) Value {
	switch op {
	case "==":
		return equal(x, y)
	case "!=":
		return !equal(x, y)
	}
	switch a := x.(type) {
	case int64:
		b, ok := y.(int64)
		if !ok {
			break
		}
		switch op {
		case "+":
			return a + b
		case "-":
			return a - b
		case "*":
			return a * b
		case "/", "%":
			if b == 0 {
				in.fail(line, "division by zero")
			}
			if op == "/" {
				return a / b
			}
			return a % b
		case "<":
			return a < b
		case "<=":
			return a <= b
		case ">":
			return a > b
		case ">=":
			return a >= b
		}
	case string:
		switch b := y.(type) {
		case string:
			switch op {
			case "+":
				return a + b
			case "<":
				return a < b
			case "<=":
				return a <= b
			case ">":
				return a > b
			case ">=":
				return a >= b
			}
		case int64:
			if op == "*" {
				if b < 0 || b > 1_000_000 {
					in.fail(line, "bad repeat count %d", b)
				}
				return strings.Repeat(a, int(b))
			}
		}
	case *List:
		if b, ok := y.(*List); ok && op == "+" {
			out := &List{append(append([]Value{}, a.Elems...), b.Elems...)}
			return out
		}
	}
	in.fail(line, "invalid operation: %s %s %s", typeName(x), op, typeName(y))
	return nil
}
