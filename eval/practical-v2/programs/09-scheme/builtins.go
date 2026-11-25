package main

import (
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func eqv(a, b Value) bool {
	switch x := a.(type) {
	case int64, float64, bool, Symbol, Char:
		return a == b
	case *big.Int:
		y, ok := b.(*big.Int)
		return ok && x.Cmp(y) == 0
	case *big.Rat:
		y, ok := b.(*big.Rat)
		return ok && x.Cmp(y) == 0
	case *MString:
		y, ok := b.(*MString)
		return ok && (x == y || (len(x.s) == 0 && len(y.s) == 0))
	}
	return a == b
}

func equal(a, b Value) bool {
	switch x := a.(type) {
	case *Pair:
		y, ok := b.(*Pair)
		return ok && equal(x.Car, y.Car) && equal(x.Cdr, y.Cdr)
	case *MString:
		y, ok := b.(*MString)
		return ok && string(x.s) == string(y.s)
	case *Vector:
		y, ok := b.(*Vector)
		return ok && slices.EqualFunc(x.items, y.items, equal)
	}
	return eqv(a, b)
}

// hashKey maps a Scheme value to a comparable Go key for hash tables
// (equal?-based for strings and numbers).
func hashKey(v Value) any {
	switch x := v.(type) {
	case *big.Int:
		return "i" + x.String()
	case *big.Rat:
		return "r" + x.RatString()
	case *MString:
		return "s" + string(x.s)
	case *Pair, *Vector:
		return "p" + Write(x)
	}
	return v
}

func num(name string, v Value) Value {
	if !isNumber(v) {
		throw("%s: not a number: %s", name, Write(v))
	}
	return v
}

func str(name string, v Value) *MString {
	s, ok := v.(*MString)
	if !ok {
		throw("%s: not a string: %s", name, Write(v))
	}
	return s
}

func intArg(name string, v Value) int {
	n, ok := v.(int64)
	if !ok {
		throw("%s: not a small integer: %s", name, Write(v))
	}
	return int(n)
}

func (in *Interp) installBuiltins() {
	def := func(name string, min, max int, fn func([]Value) Value) {
		in.global.Define(Symbol(name), &Primitive{Name: name, MinArgs: min, MaxArgs: max, Fn: fn})
	}
	fold := func(name string, op byte, unit Value) {
		def(name, 0, -1, func(a []Value) Value {
			if len(a) == 0 {
				return unit
			}
			acc := num(name, a[0])
			if len(a) == 1 && (op == '-' || op == '/') {
				return arith(op, unit, acc)
			}
			for _, x := range a[1:] {
				acc = arith(op, acc, num(name, x))
			}
			return acc
		})
	}
	fold("+", '+', int64(0))
	fold("*", '*', int64(1))
	fold("-", '-', int64(0))
	fold("/", '/', int64(1))
	compare := func(name string, ok func(int) bool) {
		def(name, 1, -1, func(a []Value) Value {
			for i := 0; i+1 < len(a); i++ {
				if !ok(compareNum(num(name, a[i]), num(name, a[i+1]))) {
					return false
				}
			}
			return true
		})
	}
	compare("=", func(c int) bool { return c == 0 })
	compare("<", func(c int) bool { return c < 0 })
	compare(">", func(c int) bool { return c > 0 })
	compare("<=", func(c int) bool { return c <= 0 })
	compare(">=", func(c int) bool { return c >= 0 })
	for _, op := range []string{"quotient", "remainder", "modulo"} {
		def(op, 2, 2, func(a []Value) Value { return intDiv(op, a[0], a[1]) })
	}
	def("expt", 2, 2, func(a []Value) Value { return exptNum(num("expt", a[0]), num("expt", a[1])) })
	def("sqrt", 1, 1, func(a []Value) Value { return numSqrt(num("sqrt", a[0])) })
	def("exact->inexact", 1, 1, func(a []Value) Value { return exactToInexact(num("exact->inexact", a[0])) })
	def("inexact->exact", 1, 1, func(a []Value) Value { return inexactToExact(num("inexact->exact", a[0])) })
	def("exact?", 1, 1, func(a []Value) Value { return rank(num("exact?", a[0])) < 3 })
	def("integer?", 1, 1, func(a []Value) Value {
		switch x := a[0].(type) {
		case int64, *big.Int:
			return true
		case float64:
			return x == math.Trunc(x)
		}
		return false
	})
	def("rational?", 1, 1, func(a []Value) Value { return isNumber(a[0]) && rank(a[0]) < 3 })
	def("number?", 1, 1, func(a []Value) Value { return isNumber(a[0]) })
	def("numerator", 1, 1, func(a []Value) Value { return normBig(new(big.Int).Set(toRat(a[0]).Num())) })
	def("denominator", 1, 1, func(a []Value) Value { return normBig(new(big.Int).Set(toRat(a[0]).Denom())) })
	def("floor", 1, 1, func(a []Value) Value {
		switch x := a[0].(type) {
		case float64:
			return math.Floor(x)
		case *big.Rat:
			q, m := new(big.Int).DivMod(x.Num(), x.Denom(), new(big.Int))
			_ = m
			return normBig(q)
		}
		return num("floor", a[0])
	})
	def("round", 1, 1, func(a []Value) Value {
		if f, ok := a[0].(float64); ok {
			return math.RoundToEven(f)
		}
		return num("round", a[0])
	})
	def("exp", 1, 1, func(a []Value) Value { return math.Exp(toFloat(a[0])) })
	def("log", 1, 1, func(a []Value) Value { return math.Log(toFloat(a[0])) })
	def("sin", 1, 1, func(a []Value) Value { return math.Sin(toFloat(a[0])) })
	def("atan", 1, 2, func(a []Value) Value {
		if len(a) == 2 {
			return math.Atan2(toFloat(a[0]), toFloat(a[1]))
		}
		return math.Atan(toFloat(a[0]))
	})
	def("number->string", 1, 2, func(a []Value) Value {
		if len(a) == 2 {
			base := intArg("number->string", a[1])
			return &MString{[]rune(toBig(a[0]).Text(base))}
		}
		return &MString{[]rune(Display(num("number->string", a[0])))}
	})
	def("string->number", 1, 1, func(a []Value) Value {
		s := string(str("string->number", a[0]).s)
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
		if b, ok := new(big.Int).SetString(s, 10); ok {
			return normBig(b)
		}
		if q, ok := new(big.Rat).SetString(s); ok && strings.Contains(s, "/") {
			return normRat(q)
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return false
	})

	// Pairs and lists.
	def("cons", 2, 2, func(a []Value) Value { return &Pair{a[0], a[1]} })
	def("car", 1, 1, func(a []Value) Value { return car(a[0]) })
	def("cdr", 1, 1, func(a []Value) Value { return cdr(a[0]) })
	def("set-car!", 2, 2, func(a []Value) Value { a[0].(*Pair).Car = a[1]; return Unspecified })
	def("set-cdr!", 2, 2, func(a []Value) Value { a[0].(*Pair).Cdr = a[1]; return Unspecified })
	def("list", 0, -1, func(a []Value) Value { return list(a...) })
	def("length", 1, 1, func(a []Value) Value { return int64(len(listToSlice(a[0]))) })
	def("reverse", 1, 1, func(a []Value) Value {
		items := listToSlice(a[0])
		slices.Reverse(items)
		return list(items...)
	})
	def("append", 0, -1, func(a []Value) Value {
		if len(a) == 0 {
			return Empty
		}
		res := a[len(a)-1]
		for i := len(a) - 2; i >= 0; i-- {
			items := listToSlice(a[i])
			for j := len(items) - 1; j >= 0; j-- {
				res = &Pair{items[j], res}
			}
		}
		return res
	})
	def("list-tail", 2, 2, func(a []Value) Value {
		v := a[0]
		for k := intArg("list-tail", a[1]); k > 0; k-- {
			v = cdr(v)
		}
		return v
	})
	def("pair?", 1, 1, func(a []Value) Value { _, ok := a[0].(*Pair); return ok })
	def("null?", 1, 1, func(a []Value) Value { return a[0] == Empty })
	def("list?", 1, 1, func(a []Value) Value {
		for v := a[0]; ; {
			switch p := v.(type) {
			case *emptyList:
				return true
			case *Pair:
				v = p.Cdr
			default:
				return false
			}
		}
	})
	def("symbol?", 1, 1, func(a []Value) Value { _, ok := a[0].(Symbol); return ok })
	def("string?", 1, 1, func(a []Value) Value { _, ok := a[0].(*MString); return ok })
	def("procedure?", 1, 1, func(a []Value) Value {
		switch a[0].(type) {
		case *Closure, *Primitive:
			return true
		}
		return false
	})
	def("boolean?", 1, 1, func(a []Value) Value { _, ok := a[0].(bool); return ok })
	def("vector?", 1, 1, func(a []Value) Value { _, ok := a[0].(*Vector); return ok })
	def("eq?", 2, 2, func(a []Value) Value { return eqv(a[0], a[1]) })
	def("eqv?", 2, 2, func(a []Value) Value { return eqv(a[0], a[1]) })
	def("equal?", 2, 2, func(a []Value) Value { return equal(a[0], a[1]) })
	def("not", 1, 1, func(a []Value) Value { return a[0] == false })

	// Strings, characters, symbols.
	def("string-length", 1, 1, func(a []Value) Value { return int64(len(str("string-length", a[0]).s)) })
	def("string-ref", 2, 2, func(a []Value) Value {
		s, i := str("string-ref", a[0]), intArg("string-ref", a[1])
		if i < 0 || i >= len(s.s) {
			throw("string-ref: index %d out of range", i)
		}
		return Char(s.s[i])
	})
	def("substring", 2, 3, func(a []Value) Value {
		s := str("substring", a[0])
		start, end := intArg("substring", a[1]), len(s.s)
		if len(a) == 3 {
			end = intArg("substring", a[2])
		}
		if start < 0 || end > len(s.s) || start > end {
			throw("substring: range %d..%d out of bounds", start, end)
		}
		return &MString{slices.Clone(s.s[start:end])}
	})
	def("string-append", 0, -1, func(a []Value) Value {
		var out []rune
		for _, x := range a {
			out = append(out, str("string-append", x).s...)
		}
		return &MString{out}
	})
	def("string=?", 2, 2, func(a []Value) Value { return string(str("string=?", a[0]).s) == string(str("string=?", a[1]).s) })
	def("string<?", 2, 2, func(a []Value) Value { return string(str("string<?", a[0]).s) < string(str("string<?", a[1]).s) })
	def("string-upcase", 1, 1, func(a []Value) Value {
		return &MString{[]rune(strings.ToUpper(string(str("string-upcase", a[0]).s)))}
	})
	def("string->list", 1, 1, func(a []Value) Value {
		var cs []Value
		for _, r := range str("string->list", a[0]).s {
			cs = append(cs, Char(r))
		}
		return list(cs...)
	})
	def("list->string", 1, 1, func(a []Value) Value {
		var rs []rune
		for _, c := range listToSlice(a[0]) {
			ch, ok := c.(Char)
			if !ok {
				throw("list->string: not a character: %s", Write(c))
			}
			rs = append(rs, rune(ch))
		}
		return &MString{rs}
	})
	def("string->symbol", 1, 1, func(a []Value) Value { return Symbol(string(str("string->symbol", a[0]).s)) })
	def("symbol->string", 1, 1, func(a []Value) Value {
		s, ok := a[0].(Symbol)
		if !ok {
			throw("symbol->string: not a symbol")
		}
		return &MString{[]rune(string(s))}
	})
	def("string-split", 2, 2, func(a []Value) Value {
		parts := strings.Split(string(str("string-split", a[0]).s), string(str("string-split", a[1]).s))
		out := make([]Value, len(parts))
		for i, p := range parts {
			out[i] = &MString{[]rune(p)}
		}
		return list(out...)
	})
	def("char->integer", 1, 1, func(a []Value) Value { return int64(a[0].(Char)) })
	def("integer->char", 1, 1, func(a []Value) Value { return Char(rune(intArg("integer->char", a[0]))) })
	def("char-alphabetic?", 1, 1, func(a []Value) Value { return unicode.IsLetter(rune(a[0].(Char))) })
	def("char-numeric?", 1, 1, func(a []Value) Value { return unicode.IsDigit(rune(a[0].(Char))) })
	def("char-upcase", 1, 1, func(a []Value) Value { return Char(unicode.ToUpper(rune(a[0].(Char)))) })

	// Vectors and hash tables.
	def("vector", 0, -1, func(a []Value) Value { return &Vector{slices.Clone(a)} })
	def("make-vector", 1, 2, func(a []Value) Value {
		n := intArg("make-vector", a[0])
		if n < 0 {
			throw("make-vector: negative length")
		}
		var fill Value = int64(0)
		if len(a) == 2 {
			fill = a[1]
		}
		v := &Vector{make([]Value, n)}
		for i := range v.items {
			v.items[i] = fill
		}
		return v
	})
	vec := func(name string, v Value) *Vector {
		x, ok := v.(*Vector)
		if !ok {
			throw("%s: not a vector: %s", name, Write(v))
		}
		return x
	}
	def("vector-length", 1, 1, func(a []Value) Value { return int64(len(vec("vector-length", a[0]).items)) })
	def("vector-ref", 2, 2, func(a []Value) Value {
		v, i := vec("vector-ref", a[0]), intArg("vector-ref", a[1])
		if i < 0 || i >= len(v.items) {
			throw("vector-ref: index %d out of range for length %d", i, len(v.items))
		}
		return v.items[i]
	})
	def("vector-set!", 3, 3, func(a []Value) Value {
		v, i := vec("vector-set!", a[0]), intArg("vector-set!", a[1])
		if i < 0 || i >= len(v.items) {
			throw("vector-set!: index %d out of range for length %d", i, len(v.items))
		}
		v.items[i] = a[2]
		return Unspecified
	})
	def("vector->list", 1, 1, func(a []Value) Value { return list(vec("vector->list", a[0]).items...) })
	def("list->vector", 1, 1, func(a []Value) Value { return &Vector{listToSlice(a[0])} })
	def("make-hash-table", 0, 0, func([]Value) Value { return &Table{m: map[any]Value{}, keys: map[any]Value{}} })
	tbl := func(v Value) *Table {
		t, ok := v.(*Table)
		if !ok {
			throw("not a hash table: %s", Write(v))
		}
		return t
	}
	def("hash-table-set!", 3, 3, func(a []Value) Value {
		t, k := tbl(a[0]), hashKey(a[1])
		t.m[k] = a[2]
		t.keys[k] = a[1]
		return Unspecified
	})
	def("hash-table-ref", 2, 3, func(a []Value) Value {
		if v, ok := tbl(a[0]).m[hashKey(a[1])]; ok {
			return v
		}
		if len(a) == 3 {
			return in.Apply(a[2], nil)
		}
		throw("hash-table-ref: key not found: %s", Write(a[1]))
		return nil
	})
	def("hash-table-ref/default", 3, 3, func(a []Value) Value {
		if v, ok := tbl(a[0]).m[hashKey(a[1])]; ok {
			return v
		}
		return a[2]
	})
	def("hash-table-delete!", 2, 2, func(a []Value) Value {
		t, k := tbl(a[0]), hashKey(a[1])
		delete(t.m, k)
		delete(t.keys, k)
		return Unspecified
	})
	def("hash-table-count", 1, 1, func(a []Value) Value { return int64(len(tbl(a[0]).m)) })
	def("hash-table-keys", 1, 1, func(a []Value) Value {
		t := tbl(a[0])
		keys := make([]Value, 0, len(t.keys))
		for _, k := range t.keys {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return Write(keys[i]) < Write(keys[j]) })
		return list(keys...)
	})

	// Control.
	def("apply", 1, -1, func(a []Value) Value {
		args := slices.Clone(a[1 : len(a)-1])
		if len(a) > 1 {
			args = append(args, listToSlice(a[len(a)-1])...)
		}
		return in.Apply(a[0], args)
	})
	def("call/ec", 1, 1, func(a []Value) Value { return in.callEC(a[0]) })
	def("call-with-escape-continuation", 1, 1, func(a []Value) Value { return in.callEC(a[0]) })
	def("with-exception-handler", 2, 2, func(a []Value) Value { return in.withHandler(a[0], a[1]) })
	def("dynamic-wind", 3, 3, func(a []Value) Value { return in.dynamicWind(a[0], a[1], a[2]) })
	def("force", 1, 1, func(a []Value) Value { return in.Apply(a[0], nil) })
	def("error", 1, -1, func(a []Value) Value {
		msg := Display(a[0])
		panic(&SchemeError{Msg: msg, Irritants: slices.Clone(a[1:])})
	})
	def("raise", 1, 1, func(a []Value) Value { panic(&SchemeError{Msg: "raise", Payload: a[0]}) })
	def("error-object-message", 1, 1, func(a []Value) Value { return car(a[0]) })
	def("error-object-irritants", 1, 1, func(a []Value) Value { return cdr(a[0]) })
	def("sort", 2, 2, func(a []Value) Value {
		items := listToSlice(a[0])
		sort.SliceStable(items, func(i, j int) bool { return truthy(in.Apply(a[1], []Value{items[i], items[j]})) })
		return list(items...)
	})

	// Output.
	def("display", 1, 1, func(a []Value) Value { in.out(Display(a[0])); return Unspecified })
	def("write", 1, 1, func(a []Value) Value { in.out(Write(a[0])); return Unspecified })
	def("newline", 0, 0, func([]Value) Value { in.out("\n"); return Unspecified })
	def("runtime-steps", 0, 0, func([]Value) Value { return in.steps })
}

// prelude defines library procedures in Scheme itself.
const prelude = `
(define (caar x) (car (car x)))
(define (cadr x) (car (cdr x)))
(define (cdar x) (cdr (car x)))
(define (cddr x) (cdr (cdr x)))
(define (caddr x) (car (cddr x)))
(define (zero? n) (= n 0))
(define (positive? n) (> n 0))
(define (negative? n) (< n 0))
(define (even? n) (= (remainder n 2) 0))
(define (odd? n) (not (even? n)))
(define (abs n) (if (< n 0) (- n) n))
(define (min x . xs) (fold-left (lambda (a b) (if (< b a) b a)) x xs))
(define (max x . xs) (fold-left (lambda (a b) (if (> b a) b a)) x xs))
(define (gcd a b) (if (= b 0) (abs a) (gcd b (remainder a b))))
(define (lcm a b) (if (or (= a 0) (= b 0)) 0 (quotient (abs (* a b)) (gcd a b))))
(define (list-ref lst k) (car (list-tail lst k)))
(define (fold-left f acc lst)
  (if (null? lst) acc (fold-left f (f acc (car lst)) (cdr lst))))
(define (fold-right f init lst)
  (if (null? lst) init (f (car lst) (fold-right f init (cdr lst)))))
(define (reduce f init lst)
  (if (null? lst) init (fold-left f (car lst) (cdr lst))))
(define (map f lst . more)
  (if (null? more)
      (let loop ((l lst) (acc '()))
        (if (null? l) (reverse acc) (loop (cdr l) (cons (f (car l)) acc))))
      (let loop ((ls (cons lst more)) (acc '()))
        (if (memq #t (map null? ls))
            (reverse acc)
            (loop (map cdr ls) (cons (apply f (map car ls)) acc))))))
(define (for-each f lst . more)
  (if (null? more)
      (let loop ((l lst)) (when (pair? l) (f (car l)) (loop (cdr l))))
      (begin (apply map f lst more) #t)))
(define (filter pred lst)
  (cond ((null? lst) '())
        ((pred (car lst)) (cons (car lst) (filter pred (cdr lst))))
        (else (filter pred (cdr lst)))))
(define (memq x lst) (cond ((null? lst) #f) ((eq? x (car lst)) lst) (else (memq x (cdr lst)))))
(define (member x lst) (cond ((null? lst) #f) ((equal? x (car lst)) lst) (else (member x (cdr lst)))))
(define (assq k al) (cond ((null? al) #f) ((eq? k (caar al)) (car al)) (else (assq k (cdr al)))))
(define (assoc k al) (cond ((null? al) #f) ((equal? k (caar al)) (car al)) (else (assoc k (cdr al)))))
(define (iota n . start)
  (let ((s (if (null? start) 0 (car start))))
    (let loop ((i (- n 1)) (acc '()))
      (if (< i 0) acc (loop (- i 1) (cons (+ s i) acc))))))
(define (string-join strs sep)
  (if (null? strs) ""
      (fold-left (lambda (acc s) (string-append acc sep s)) (car strs) (cdr strs))))
(define (vector-map f v) (list->vector (map f (vector->list v))))
(define (vector-fill! v x)
  (do ((i 0 (+ i 1))) ((= i (vector-length v))) (vector-set! v i x)))
`
