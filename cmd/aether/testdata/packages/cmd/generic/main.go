// A program instantiating another package's generic code (D-049,
// increment 11.7), directly and from its own generic code.
package main

import (
	"example.com/pk/base"
	"example.com/pk/gen"
)

func double[T gen.Number](xs []T) T {
	return gen.Sum(xs...) * 2
}

func main() {
	println(gen.Sum(1, 2, 3), gen.Sum(1.5, 2.25), gen.Sum[int64]())
	println(double([]int{4, 5}), double([]float64{0.5}))
	strs := gen.Map([]int{1, 2, 3}, func(i int) string { return string(rune('a' + i)) })
	println(len(strs), strs[0], strs[2])

	var s gen.Stack[string]
	s.Push("x")
	s.Push("y")
	v, ok := s.Pop()
	println(v, ok, s.Len())
	ints := &gen.Stack[int]{}
	_, ok = ints.Pop()
	println(ok)

	p := gen.Pair[string, gen.Level]{"k", 3}
	for _, q := range append(gen.Levels(), p) {
		println(q.Key, q.Val)
	}
	ks := gen.Kinds(7)
	println(len(ks), int(ks[1]), ks[0].String())
	println(gen.Sum(base.Kind(2), base.Kind(3)).String())
}
