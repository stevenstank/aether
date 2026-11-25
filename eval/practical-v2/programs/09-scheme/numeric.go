package main

import (
	"math"
	"math/big"
	"math/bits"
)

// The numeric tower: int64 fixnums overflow into *big.Int; exact division
// produces *big.Rat; float64 is inexact and contagious.

func isNumber(v Value) bool {
	switch v.(type) {
	case int64, *big.Int, *big.Rat, float64:
		return true
	}
	return false
}

func normBig(b *big.Int) Value {
	if b.IsInt64() {
		return b.Int64()
	}
	return b
}

func normRat(q *big.Rat) Value {
	if q.IsInt() {
		return normBig(new(big.Int).Set(q.Num()))
	}
	return q
}

func toBig(v Value) *big.Int {
	switch x := v.(type) {
	case int64:
		return big.NewInt(x)
	case *big.Int:
		return x
	}
	throw("not an integer: %s", Write(v))
	return nil
}

func toRat(v Value) *big.Rat {
	switch x := v.(type) {
	case int64:
		return new(big.Rat).SetInt64(x)
	case *big.Int:
		return new(big.Rat).SetInt(x)
	case *big.Rat:
		return x
	}
	throw("not an exact number: %s", Write(v))
	return nil
}

func toFloat(v Value) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case *big.Int:
		f, _ := new(big.Float).SetInt(x).Float64()
		return f
	case *big.Rat:
		f, _ := x.Float64()
		return f
	case float64:
		return x
	}
	throw("not a number: %s", Write(v))
	return 0
}

// rank: 0 fixnum, 1 bignum, 2 rational, 3 float
func rank(v Value) int {
	switch v.(type) {
	case int64:
		return 0
	case *big.Int:
		return 1
	case *big.Rat:
		return 2
	case float64:
		return 3
	}
	throw("not a number: %s", Write(v))
	return -1
}

func arith(op byte, a, b Value) Value {
	r := max(rank(a), rank(b))
	if r == 0 {
		x, y := a.(int64), b.(int64)
		switch op {
		case '+':
			s := x + y
			if (s > x) == (y > 0) {
				return s
			}
		case '-':
			s := x - y
			if (s < x) == (y > 0) {
				return s
			}
		case '*':
			hi, lo := bits.Mul64(uint64(absI(x)), uint64(absI(y)))
			if hi == 0 && lo < 1<<63 && x != math.MinInt64 && y != math.MinInt64 {
				p := int64(lo)
				if (x < 0) != (y < 0) {
					p = -p
				}
				return p
			}
		}
		r = 1
	}
	switch r {
	case 1:
		x, y := toBig(a), toBig(b)
		z := new(big.Int)
		switch op {
		case '+':
			z.Add(x, y)
		case '-':
			z.Sub(x, y)
		case '*':
			z.Mul(x, y)
		case '/':
			return divide(a, b)
		}
		return normBig(z)
	case 2:
		x, y := toRat(a), toRat(b)
		z := new(big.Rat)
		switch op {
		case '+':
			z.Add(x, y)
		case '-':
			z.Sub(x, y)
		case '*':
			z.Mul(x, y)
		case '/':
			return divide(a, b)
		}
		return normRat(z)
	}
	x, y := toFloat(a), toFloat(b)
	switch op {
	case '+':
		return x + y
	case '-':
		return x - y
	case '*':
		return x * y
	}
	return x / y
}

func absI(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func divide(a, b Value) Value {
	if rank(a) == 3 || rank(b) == 3 {
		return toFloat(a) / toFloat(b)
	}
	y := toRat(b)
	if y.Sign() == 0 {
		throw("/: division by zero")
	}
	return normRat(new(big.Rat).Quo(toRat(a), y))
}

func compareNum(a, b Value) int {
	if rank(a) == 3 || rank(b) == 3 {
		x, y := toFloat(a), toFloat(b)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	if x, ok := a.(int64); ok {
		if y, ok := b.(int64); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	}
	return toRat(a).Cmp(toRat(b))
}

// intDiv implements quotient, remainder, and modulo on exact integers.
func intDiv(op string, a, b Value) Value {
	if x, ok := a.(int64); ok {
		if y, ok := b.(int64); ok && !(x == math.MinInt64 && y == -1) {
			if y == 0 {
				throw("%s: division by zero", op)
			}
			switch op {
			case "quotient":
				return x / y
			case "remainder":
				return x % y
			}
			m := x % y
			if m != 0 && (m < 0) != (y < 0) {
				m += y
			}
			return m
		}
	}
	x, y := toBig(a), toBig(b)
	if y.Sign() == 0 {
		throw("%s: division by zero", op)
	}
	q, m := new(big.Int).QuoRem(x, y, new(big.Int))
	switch op {
	case "quotient":
		return normBig(q)
	case "remainder":
		return normBig(m)
	}
	if m.Sign() != 0 && (m.Sign() < 0) != (y.Sign() < 0) {
		m.Add(m, y)
	}
	return normBig(m)
}

func exptNum(base, exp Value) Value {
	if e, ok := exp.(int64); ok && rank(base) < 3 {
		if e >= 0 {
			if rank(base) == 2 {
				q := toRat(base)
				n := new(big.Int).Exp(q.Num(), big.NewInt(e), nil)
				d := new(big.Int).Exp(q.Denom(), big.NewInt(e), nil)
				return normRat(new(big.Rat).SetFrac(n, d))
			}
			return normBig(new(big.Int).Exp(toBig(base), big.NewInt(e), nil))
		}
		return divide(int64(1), exptNum(base, -e))
	}
	return math.Pow(toFloat(base), toFloat(exp))
}

func exactToInexact(v Value) Value { return toFloat(v) }

func inexactToExact(v Value) Value {
	f, ok := v.(float64)
	if !ok {
		return v
	}
	if math.IsInf(f, 0) || math.IsNaN(f) {
		throw("inexact->exact: no exact representation of %s", formatFloat(f))
	}
	q := new(big.Rat)
	q.SetFloat64(f)
	return normRat(q)
}

func numSqrt(v Value) Value {
	if rank(v) <= 1 && compareNum(v, int64(0)) >= 0 {
		r := new(big.Int).Sqrt(toBig(v))
		if new(big.Int).Mul(r, r).Cmp(toBig(v)) == 0 {
			return normBig(r)
		}
	}
	return math.Sqrt(toFloat(v))
}
