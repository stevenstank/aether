package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
)

type Celsius float64

func (c Celsius) String() string { return fmt.Sprintf("%.1f°C", float64(c)) }

type Temp = Celsius
type Pair = struct {
	K string
	V int
}
type Pairs = []Pair
type Lookup = map[string]Pair
type IntFn = func(int) int
type Err = error
type Alias2 = Temp

func (p *Point) Move(dx int) { p.X += dx }

type Point struct{ X, Y int }
type P = Point

func apply(f IntFn, x int) int { return f(x) }

func kind(v any) string {
	switch v.(type) {
	case Temp:
		return "temp"
	case Pair:
		return "pair"
	case Err:
		return "error"
	}
	return "other"
}

func max2[T int | Celsius](a, b T) T { return max(a, b) }

func main() {
	var t Temp = 21.5
	var a2 Alias2 = t + 1
	fmt.Println(t, a2, Celsius(3) == Temp(3))
	fmt.Printf("%T %T %v\n", t, a2, t.String())
	ps := Pairs{{"a", 1}, {"b", 2}}
	ps = append(ps, Pair{K: "c", V: 3})
	m := Lookup{"x": ps[1]}
	fmt.Printf("%v %T %v %T\n", ps, ps, m, m)
	fmt.Println(apply(func(x int) int { return x * 3 }, 5))
	var e Err = errors.New("boom")
	fmt.Println(kind(t), kind(ps[0]), kind(e), kind(3))
	p := P{1, 2}
	p.Move(5)
	var pp *P = &p
	fmt.Printf("%v %T %+v\n", p, pp, *pp)
	fmt.Println(max2(Temp(4), Temp(9)), slices.Index([]Temp{1, 2}, 2))
	var mode os.FileMode = 0644 | fs.ModeDir
	var fm fs.FileMode = mode
	fmt.Println(mode, fm.IsDir(), fmt.Sprintf("%T", mode))
	_, err := os.Open("/nonexistent/file")
	var pe *fs.PathError
	var ope *os.PathError
	fmt.Println(errors.As(err, &pe), errors.As(err, &ope), pe == ope, pe.Op, errors.Is(err, fs.ErrNotExist), os.IsNotExist(err))
	type local = [2]string
	var l local = local{"p", "q"}
	fmt.Println(l, len(l))
}
