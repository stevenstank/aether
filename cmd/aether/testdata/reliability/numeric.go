// Generic numeric code: vectors and statistics over several integer and
// float types, a generic matrix that embeds a generic vector and
// promotes its methods, type switches on type parameters to pick a
// formatting, type assertions that are impossible for some instances,
// and integer overflow and division panics, recovered and reported.
package main

import (
	"fmt"
	"math"
	"strings"
)

type Number interface {
	~int | ~int8 | ~int32 | ~int64 | ~uint8 | ~float32 | ~float64
}

type Celsius float64

func (c Celsius) String() string { return fmt.Sprintf("%.1fC", float64(c)) }

type Vec[T Number] struct{ xs []T }

func V[T Number](xs ...T) Vec[T] { return Vec[T]{xs} }

func (v Vec[T]) Sum() T {
	var s T
	for _, x := range v.xs {
		s += x
	}
	return s
}

func (v Vec[T]) Max() (m T) {
	for i, x := range v.xs {
		if i == 0 || x > m {
			m = x
		}
	}
	return m
}

func (v Vec[T]) Scale(k T) Vec[T] {
	out := make([]T, len(v.xs))
	for i, x := range v.xs {
		out[i] = x * k
	}
	return Vec[T]{out}
}

type Matrix[T Number] struct {
	Vec[T]
	rows, cols int
}

func NewMatrix[T Number](rows, cols int, xs ...T) Matrix[T] {
	return Matrix[T]{V(xs...), rows, cols}
}

func (m Matrix[T]) At(r, c int) T { return m.xs[r*m.cols+c] }

func (m Matrix[T]) Mul(o Matrix[T]) Matrix[T] {
	out := make([]T, m.rows*o.cols)
	for r := 0; r < m.rows; r++ {
		for c := 0; c < o.cols; c++ {
			var s T
			for k := 0; k < m.cols; k++ {
				s += m.At(r, k) * o.At(k, c)
			}
			out[r*o.cols+c] = s
		}
	}
	return NewMatrix(m.rows, o.cols, out...)
}

func format[T Number](x T) string {
	switch v := any(x).(type) {
	case fmt.Stringer:
		return v.String()
	case float64, float32:
		return fmt.Sprintf("%.3g", v)
	case T:
		return fmt.Sprintf("%v", v)
	case int:
		return fmt.Sprintf("int %d", v)
	}
	return "?"
}

// asStringer's assertion is impossible for the instances whose type has
// no String method; it then reports false, or panics with gc's message.
type Stringer interface{ String() string }

func asStringer[T Number](x T, must bool) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "panic: " + fmt.Sprint(r)
		}
	}()
	var i interface{ Sum() T } = V(x)
	if must {
		return i.(Stringer).String()
	}
	if st, ok := i.(Stringer); ok {
		return st.String()
	}
	_, ok := i.(Vec[T])
	return fmt.Sprint("not a Stringer, Vec: ", ok)
}

func mean[T Number](v Vec[T]) (m float64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("mean: %v", r)
		}
	}()
	return float64(v.Sum()) / float64(len(v.xs)), nil
}

func divide[T ~int | ~int8 | ~int64](a, b T) (q T, err string) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Sprint(r)
		}
	}()
	return a / b, ""
}

func row[T Number](label string, vs ...T) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = format(v)
	}
	return label + ": " + strings.Join(parts, " ")
}

func main() {
	vi := V(3, 1, 4, 1, 5, 9, 2, 6)
	vb := V[uint8](200, 100, 7)
	vf := V(0.5, 1.25, -3.0)
	vc := V[Celsius](21.5, 19, 23.25)
	v8 := V[int8](100, 27, 1)
	fmt.Println(row("ints", vi.Sum(), vi.Max(), vi.Scale(3).Sum()))
	fmt.Println(row("bytes", vb.Sum(), vb.Max(), vb.Scale(2).Max()))
	fmt.Println(row("floats", vf.Sum(), vf.Max(), vf.Scale(-2).Max()))
	fmt.Println(row("celsius", vc.Sum(), vc.Max()))
	fmt.Println(row("int8", v8.Sum(), v8.Max(), v8.Scale(2).Sum()))
	for _, m := range []func() (float64, error){
		func() (float64, error) { return mean(vi) },
		func() (float64, error) { return mean(vc) },
		func() (float64, error) { return mean(V[int]()) },
	} {
		x, err := m()
		fmt.Println("mean", x, math.IsNaN(x), err)
	}
	a := NewMatrix(2, 3, 1, 2, 3, 4, 5, 6)
	b := NewMatrix(3, 2, 7, 8, 9, 10, 11, 12)
	p := a.Mul(b)
	fmt.Println("product", p.xs, p.Sum(), p.Max(), p.At(1, 1))
	fm := NewMatrix(1, 2, Celsius(1.5), 2).Mul(NewMatrix(2, 1, Celsius(2), 4))
	fmt.Println("celsius product", fm.At(0, 0), format(fm.Sum()))
	fmt.Println(asStringer(Celsius(5), true), "|", asStringer(Celsius(5), false))
	fmt.Println(asStringer(7, false), "|", asStringer(7, true))
	fmt.Println(divide(7, 2))
	fmt.Println(divide(7, 0))
	fmt.Println(divide[int8](-128, -1))
	fmt.Println(divide[int64](math.MinInt64, -1))
}
