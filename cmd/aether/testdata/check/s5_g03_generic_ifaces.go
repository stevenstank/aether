// aether:expect run

package main

// Generic code over interfaces (D-045): interface type arguments, ==
// under comparable on interfaces holding uncomparable values (a run-time
// error, recovered), type switches and assertions on type parameters'
// values, constraints with pointer methods (PT interface{ *T; ... }),
// method calls through a constraint on values and pointers, and defer
// and recover in generic code with named results.

type Shape interface {
	Area() float64
	Name() string
}

type Rect struct{ w, h float64 }

func (r Rect) Area() float64 { return r.w * r.h }
func (r Rect) Name() string  { return "rect" }

type Square struct{ s float64 }

func (q *Square) Area() float64 { return q.s * q.s }
func (q *Square) Name() string  { return "square" }

func Total[S Shape](xs []S) float64 {
	t := 0.0
	for i := 0; i < len(xs); i++ {
		t += xs[i].Area()
	}
	return t
}

func Names[S Shape](xs []S) string {
	r := ""
	for i := 0; i < len(xs); i++ {
		r += xs[i].Name() + ";"
	}
	return r
}

type Setter[T any] interface {
	*T
	Set(int)
	Get() int
}

type Box struct{ v int }

func (b *Box) Set(v int) { b.v = v * 10 }
func (b *Box) Get() int  { return b.v }

func Build[T any, PT Setter[T]](vs []int) []T {
	r := make([]T, len(vs))
	for i := 0; i < len(vs); i++ {
		PT(&r[i]).Set(vs[i])
	}
	return r
}

func Kind[T any](v T) string {
	switch x := any(v).(type) {
	case int:
		return "int " + string(rune('0'+x%10))
	case string:
		return "string " + x
	case Shape:
		return "shape " + x.Name()
	case nil:
		return "nil"
	default:
		return "other"
	}
}

func Equal[T comparable](a, b T) (eq bool, err string) {
	defer func() {
		if e := recover(); e != nil {
			err = e.(error).Error()
		}
	}()
	return a == b, ""
}

func As[T any](v any) (T, bool) {
	t, ok := v.(T)
	return t, ok
}

func Must[T any](v any) (r T) {
	defer func() {
		if e := recover(); e != nil {
			println("recovered:", e.(error).Error())
		}
	}()
	return v.(T)
}

type Number interface{ ~int | ~float64 }

func Clamp[T Number](x, lo, hi T) (r T) {
	defer func() {
		if r < lo {
			r = lo
		}
	}()
	if x > hi {
		return hi
	}
	return x
}

func main() {
	println(Total([]Rect{{1, 2}, {3, 4}}), Names([]Rect{{}}))
	println(Total([]*Square{{2}, {3}}), Names([]*Square{{1}, {1}}))
	println(Total([]Shape{Rect{1, 1}, &Square{4}}), Names([]Shape{&Square{}, Rect{}}))
	bs := Build[Box]([]int{1, 2, 3})
	println(len(bs), bs[0].v, bs[2].Get())
	println(Kind(42), Kind("go"), Kind(Rect{}), Kind[Shape](nil), Kind(1.5), Kind[any](nil))
	println(Equal(1, 1))
	println(Equal("a", "b"))
	println(Equal[any](1, 1))
	println(Equal[any](1, "1"))
	println(Equal[any]([]int{1}, []int{1}))
	println(Equal[Shape](Rect{1, 2}, Rect{1, 2}))
	println(Equal[any](map[int]int{}, 1))
	i, ok := As[int](7)
	s, ok2 := As[string](7)
	sh, ok3 := As[Shape](Rect{2, 2})
	println(i, ok, s == "", ok2, sh.Area(), ok3)
	println(Must[int](3))
	println(Must[string](3) == "")
	println(Must[Shape](&Square{3}).Area())
	println(Clamp(5, 0, 3), Clamp(-2, 0, 3), Clamp(1.5, 0, 3), Clamp(-0.5, -1, 1))
}
