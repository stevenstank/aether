package main

import "fmt"

type I interface{ M() int }
type T int

func (t T) M() int { return int(t) }

type list []string

func (l list) show(args ...any) { fmt.Println(len(l), args) }

func pair() (any, I) { return 1, T(7) }

func main() {
	list{"a"}.show(pair())
	var nilI I
	f := func() (I, I) { return nilI, T(2) }
	list{"b", "c"}.show(f())
}
