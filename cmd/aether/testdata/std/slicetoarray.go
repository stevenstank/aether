package main

import "fmt"

type pt struct{ x, y int }

func try(name string, f func()) {
	defer func() {
		r := recover()
		if e, ok := r.(error); ok {
			fmt.Printf("%s: %v (%T)\n", name, e, r)
		} else if r != nil {
			fmt.Println(name, r)
		}
	}()
	f()
}

func main() {
	s := []byte{1, 2, 3, 4, 5}
	a := [4]byte(s)
	s[0] = 9
	fmt.Println(a, s)
	p := (*[2]byte)(s)
	p[1] = 7
	fmt.Println(*p, s)
	var nilS []int
	fmt.Println([0]int(nilS), (*[0]int)(nilS) == nil, (*[0]int)([]int{}) != nil)
	ps := []pt{{1, 2}, {3, 4}}
	fmt.Println([2]pt(ps), [1]pt(ps[1:]), len([3]string([]string{"a", "b", "c", "d"})))
	type A [3]int
	fmt.Println(A([]int{7, 8, 9}), fmt.Sprint([2]int(make([]int, 2, 10)), [16]byte(make([]byte, 20))))
	try("short", func() { fmt.Println([6]byte(s)) })
	try("short ptr", func() { fmt.Println((*[3]int)(nilS)) })
	x := [5]int(append([]int{1}, 2, 3, 4, 5))
	fmt.Println(x, [2]int(s2i(s)), len(s))
	var arr [3]byte = [3]byte(s[2:])
	fmt.Println(arr)
}

func s2i(b []byte) []int {
	out := make([]int, len(b))
	for i, c := range b {
		out[i] = int(c)
	}
	return out
}
