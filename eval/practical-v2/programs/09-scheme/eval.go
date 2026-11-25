package main

import (
	"fmt"
)

var (
	symQuote   = Symbol("quote")
	symQuasi   = Symbol("quasiquote")
	symUnquote = Symbol("unquote")
	symSplice  = Symbol("unquote-splicing")
	symElse    = Symbol("else")
	symArrow   = Symbol("=>")
	symDefine  = Symbol("define")
)

// escape is panicked by an escape continuation created with call/ec.
type escape struct {
	id    *int
	value Value
}

type Interp struct {
	global   *Env
	depth    int
	maxDepth int
	steps    int64
	out      func(string)
}

func car(v Value) Value {
	p, ok := v.(*Pair)
	if !ok {
		throw("car: not a pair: %s", Write(v))
	}
	return p.Car
}

func cdr(v Value) Value {
	p, ok := v.(*Pair)
	if !ok {
		throw("cdr: not a pair: %s", Write(v))
	}
	return p.Cdr
}

func (in *Interp) makeLambda(name string, params Value, body Value, env *Env) *Closure {
	c := &Closure{Name: name, Env: env, Body: listToSlice(body)}
	if len(c.Body) == 0 {
		throw("lambda: empty body")
	}
	for {
		switch p := params.(type) {
		case *Pair:
			s, ok := p.Car.(Symbol)
			if !ok {
				throw("lambda: parameter is not a symbol: %s", Write(p.Car))
			}
			c.Params = append(c.Params, s)
			params = p.Cdr
			continue
		case Symbol:
			c.Rest = p
		case *emptyList:
		default:
			throw("lambda: bad parameter list")
		}
		return c
	}
}

// bind creates the frame for a call to c.
func bind(c *Closure, args []Value) *Env {
	if len(args) < len(c.Params) || (c.Rest == "" && len(args) > len(c.Params)) {
		name := c.Name
		if name == "" {
			name = "#<procedure>"
		}
		throw("%s: expected %d argument(s), got %d", name, len(c.Params), len(args))
	}
	env := &Env{vars: make(map[Symbol]Value, len(c.Params)+1), parent: c.Env}
	for i, p := range c.Params {
		env.vars[p] = args[i]
	}
	if c.Rest != "" {
		env.vars[c.Rest] = list(args[len(c.Params):]...)
	}
	return env
}

// Apply calls a procedure with already-evaluated arguments.
func (in *Interp) Apply(f Value, args []Value) Value {
	switch fn := f.(type) {
	case *Primitive:
		if len(args) < fn.MinArgs || (fn.MaxArgs >= 0 && len(args) > fn.MaxArgs) {
			throw("%s: wrong number of arguments (%d)", fn.Name, len(args))
		}
		return fn.Fn(args)
	case *Closure:
		env := bind(fn, args)
		for _, x := range fn.Body[:len(fn.Body)-1] {
			in.Eval(x, env)
		}
		return in.Eval(fn.Body[len(fn.Body)-1], env)
	}
	throw("not a procedure: %s", Write(f))
	return nil
}

// Eval evaluates x in env. Tail positions loop instead of recursing, so
// tail calls run in constant Go stack.
func (in *Interp) Eval(x Value, env *Env) Value {
	in.depth++
	defer func() { in.depth-- }()
	if in.depth > in.maxDepth {
		throw("recursion too deep (limit %d)", in.maxDepth)
	}
	for {
		in.steps++
		switch v := x.(type) {
		case Symbol:
			return env.Lookup(v)
		case *Pair:
		default:
			if v == Empty {
				throw("missing procedure expression: ()")
			}
			return x // self-evaluating
		}
		p := x.(*Pair)
		if s, ok := p.Car.(Symbol); ok {
			switch s {
			case "quote":
				return car(p.Cdr)
			case "quasiquote":
				return in.quasi(car(p.Cdr), env, 1)
			case "if":
				args := listToSlice(p.Cdr)
				if len(args) < 2 || len(args) > 3 {
					throw("if: bad syntax")
				}
				if truthy(in.Eval(args[0], env)) {
					x = args[1]
				} else if len(args) == 3 {
					x = args[2]
				} else {
					return Unspecified
				}
				continue
			case "define":
				target := car(p.Cdr)
				if sig, ok := target.(*Pair); ok {
					name, ok := sig.Car.(Symbol)
					if !ok {
						throw("define: bad procedure name")
					}
					env.Define(name, in.makeLambda(string(name), sig.Cdr, cdr(p.Cdr), env))
					return Unspecified
				}
				name, ok := target.(Symbol)
				if !ok {
					throw("define: not a symbol: %s", Write(target))
				}
				var val Value = Unspecified
				if rest := cdr(p.Cdr); rest != Empty {
					val = in.Eval(car(rest), env)
				}
				if c, ok := val.(*Closure); ok && c.Name == "" {
					c.Name = string(name)
				}
				env.Define(name, val)
				return Unspecified
			case "set!":
				name, ok := car(p.Cdr).(Symbol)
				if !ok {
					throw("set!: not a symbol")
				}
				env.Set(name, in.Eval(car(cdr(p.Cdr)), env))
				return Unspecified
			case "lambda":
				return in.makeLambda("", car(p.Cdr), cdr(p.Cdr), env)
			case "begin":
				body := listToSlice(p.Cdr)
				if len(body) == 0 {
					return Unspecified
				}
				for _, b := range body[:len(body)-1] {
					in.Eval(b, env)
				}
				x = body[len(body)-1]
				continue
			case "let", "let*", "letrec", "letrec*":
				x, env = in.let(s, p, env)
				continue
			case "cond":
				var next Value
				found := false
				for _, clause := range listToSlice(p.Cdr) {
					parts := listToSlice(clause)
					if len(parts) == 0 {
						throw("cond: empty clause")
					}
					var test Value = true
					if parts[0] != symElse {
						test = in.Eval(parts[0], env)
					}
					if !truthy(test) {
						continue
					}
					found = true
					switch {
					case len(parts) == 1:
						return test
					case parts[1] == symArrow:
						f := in.Eval(parts[2], env)
						return in.Apply(f, []Value{test})
					}
					for _, b := range parts[1 : len(parts)-1] {
						in.Eval(b, env)
					}
					next = parts[len(parts)-1]
					break
				}
				if !found {
					return Unspecified
				}
				x = next
				continue
			case "case":
				key := in.Eval(car(p.Cdr), env)
				var next Value
				for _, clause := range listToSlice(cdr(p.Cdr)) {
					parts := listToSlice(clause)
					match := parts[0] == symElse
					if !match {
						for _, d := range listToSlice(parts[0]) {
							if eqv(d, key) {
								match = true
								break
							}
						}
					}
					if match {
						for _, b := range parts[1 : len(parts)-1] {
							in.Eval(b, env)
						}
						next = parts[len(parts)-1]
						break
					}
				}
				if next == nil {
					return Unspecified
				}
				x = next
				continue
			case "and":
				args := listToSlice(p.Cdr)
				if len(args) == 0 {
					return true
				}
				for _, a := range args[:len(args)-1] {
					if v := in.Eval(a, env); !truthy(v) {
						return v
					}
				}
				x = args[len(args)-1]
				continue
			case "or":
				args := listToSlice(p.Cdr)
				if len(args) == 0 {
					return false
				}
				for _, a := range args[:len(args)-1] {
					if v := in.Eval(a, env); truthy(v) {
						return v
					}
				}
				x = args[len(args)-1]
				continue
			case "when", "unless":
				test := truthy(in.Eval(car(p.Cdr), env))
				if test != (s == "when") {
					return Unspecified
				}
				x = &Pair{Symbol("begin"), cdr(p.Cdr)}
				continue
			case "do":
				return in.do(p, env)
			case "delay":
				body := car(p.Cdr)
				done := false
				var val Value
				return &Primitive{Name: "promise", MaxArgs: 0, Fn: func([]Value) Value {
					if !done {
						val = in.Eval(body, env)
						done = true
					}
					return val
				}}
			case "assert":
				if !truthy(in.Eval(car(p.Cdr), env)) {
					throw("assertion failed: %s", Write(car(p.Cdr)))
				}
				return Unspecified
			}
		}
		// Application.
		f := in.Eval(p.Car, env)
		var args []Value
		for a := p.Cdr; a != Empty; {
			ap, ok := a.(*Pair)
			if !ok {
				throw("bad application: improper argument list")
			}
			args = append(args, in.Eval(ap.Car, env))
			a = ap.Cdr
		}
		if c, ok := f.(*Closure); ok {
			env = bind(c, args)
			for _, b := range c.Body[:len(c.Body)-1] {
				in.Eval(b, env)
			}
			x = c.Body[len(c.Body)-1]
			continue
		}
		return in.Apply(f, args)
	}
}

func (in *Interp) let(kind Symbol, p *Pair, env *Env) (Value, *Env) {
	first := car(p.Cdr)
	body := cdr(p.Cdr)
	if name, ok := first.(Symbol); ok && kind == "let" {
		// Named let: (let loop ((var init) ...) body)
		bindings := listToSlice(car(body))
		var params, inits []Value
		for _, b := range bindings {
			params = append(params, car(b))
			inits = append(inits, in.Eval(car(cdr(b)), env))
		}
		loopEnv := NewEnv(env)
		proc := in.makeLambda(string(name), list(params...), cdr(body), loopEnv)
		loopEnv.Define(name, proc)
		return &Pair{proc, quoteAll(inits)}, loopEnv
	}
	bindings := listToSlice(first)
	newEnv := NewEnv(env)
	switch kind {
	case "let":
		for _, b := range bindings {
			newEnv.Define(car(b).(Symbol), in.Eval(car(cdr(b)), env))
		}
	case "let*":
		for _, b := range bindings {
			newEnv.Define(car(b).(Symbol), in.Eval(car(cdr(b)), newEnv))
			newEnv = NewEnv(newEnv)
		}
	default: // letrec, letrec*
		for _, b := range bindings {
			newEnv.Define(car(b).(Symbol), Unspecified)
		}
		for _, b := range bindings {
			v := in.Eval(car(cdr(b)), newEnv)
			if c, ok := v.(*Closure); ok && c.Name == "" {
				c.Name = string(car(b).(Symbol))
			}
			newEnv.Define(car(b).(Symbol), v)
		}
	}
	return &Pair{Symbol("begin"), body}, newEnv
}

func quoteAll(vals []Value) Value {
	q := make([]Value, len(vals))
	for i, v := range vals {
		q[i] = list(symQuote, v)
	}
	return list(q...)
}

// (do ((var init step) ...) (test expr ...) body ...)
func (in *Interp) do(p *Pair, env *Env) Value {
	specs := listToSlice(car(p.Cdr))
	exit := listToSlice(car(cdr(p.Cdr)))
	body := listToSlice(cdr(cdr(p.Cdr)))
	loop := NewEnv(env)
	for _, s := range specs {
		loop.Define(car(s).(Symbol), in.Eval(car(cdr(s)), env))
	}
	for {
		if truthy(in.Eval(exit[0], loop)) {
			var result Value = Unspecified
			for _, e := range exit[1:] {
				result = in.Eval(e, loop)
			}
			return result
		}
		for _, b := range body {
			in.Eval(b, loop)
		}
		next := NewEnv(env)
		for _, s := range specs {
			name := car(s).(Symbol)
			if steps := cdr(cdr(s)); steps != Empty {
				next.Define(name, in.Eval(car(steps), loop))
			} else {
				next.Define(name, loop.Lookup(name))
			}
		}
		loop = next
	}
}

func (in *Interp) quasi(x Value, env *Env, depth int) Value {
	p, ok := x.(*Pair)
	if !ok {
		if v, ok := x.(*Vector); ok {
			return &Vector{listToSlice(in.quasi(list(v.items...), env, depth))}
		}
		return x
	}
	if p.Car == symUnquote {
		if depth == 1 {
			return in.Eval(car(p.Cdr), env)
		}
		return list(symUnquote, in.quasi(car(p.Cdr), env, depth-1))
	}
	if p.Car == symQuasi {
		return list(symQuasi, in.quasi(car(p.Cdr), env, depth+1))
	}
	if inner, ok := p.Car.(*Pair); ok && inner.Car == symSplice && depth == 1 {
		spliced := in.Eval(car(inner.Cdr), env)
		rest := in.quasi(p.Cdr, env, depth)
		items := listToSlice(spliced)
		for i := len(items) - 1; i >= 0; i-- {
			rest = &Pair{items[i], rest}
		}
		return rest
	}
	return &Pair{in.quasi(p.Car, env, depth), in.quasi(p.Cdr, env, depth)}
}

// callEC implements call-with-escape-continuation using panic/recover.
func (in *Interp) callEC(f Value) (result Value) {
	id := new(int)
	k := &Primitive{Name: "escape", MinArgs: 0, MaxArgs: -1, Fn: func(args []Value) Value {
		var v Value = Unspecified
		if len(args) == 1 {
			v = args[0]
		} else if len(args) > 1 {
			v = list(args...)
		}
		panic(&escape{id: id, value: v})
	}}
	saved := in.depth
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*escape); ok && e.id == id {
				in.depth = saved
				result = e.value
				return
			}
			panic(r)
		}
	}()
	return in.Apply(f, []Value{k})
}

// withHandler runs thunk; a Scheme error is passed to handler instead.
func (in *Interp) withHandler(handler, thunk Value) (result Value) {
	saved := in.depth
	defer func() {
		if r := recover(); r != nil {
			se, ok := r.(*SchemeError)
			if !ok {
				panic(r)
			}
			in.depth = saved
			var cond Value
			if se.Payload != nil {
				cond = se.Payload
			} else {
				cond = &Pair{&MString{[]rune(se.Msg)}, list(se.Irritants...)}
			}
			result = in.Apply(handler, []Value{cond})
		}
	}()
	return in.Apply(thunk, nil)
}

func (in *Interp) dynamicWind(before, thunk, after Value) Value {
	in.Apply(before, nil)
	defer in.Apply(after, nil)
	return in.Apply(thunk, nil)
}

func (in *Interp) String() string { return fmt.Sprintf("interp(%d steps)", in.steps) }
