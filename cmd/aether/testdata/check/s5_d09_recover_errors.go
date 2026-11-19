// aether:expect run

package main

// A recovered run-time error is an error with RuntimeError, of gc's
// dynamic type, whose Error is the message gc writes (D-043).

type RE interface {
	Error() string
	RuntimeError()
}

func try(n int, f func()) {
	defer func() {
		r := recover()
		e, ok := r.(error)
		re, isRE := r.(RE)
		if isRE {
			re.RuntimeError()
		}
		println(n, ok, isRE)
		if ok {
			println(e.Error())
		}
		switch v := r.(type) {
		case string:
			println("string", v)
		case error:
			println("error")
		}
		defer func() { println(recover().(error).Error()) }()
		println(r.(int))
	}()
	f()
}

func div(a, b int) int { return a / b }

func main() {
	try(1, func() { var s []int; i := 5; println(s[i]) })
	try(2, func() { println(div(1, 0)) })
	try(3, func() { var p *int; println(*p) })
	try(4, func() { var m map[int]int; m[1] = 2 })
	try(5, func() { var x any = "s"; println(x.(int)) })
	try(6, func() { var e error; println(e.Error()) })
	try(7, func() { var a, b any = []int{}, []int{}; println(a == b) })
	try(8, func() { m := map[any]int{}; m[[]int{}] = 1 })
	try(9, func() { m := map[any]int{}; println(m[[]int{}]) })
	try(10, func() { n := -1; s := make([]int, n); println(len(s)) })
	try(11, func() { panic(nil) })
	try(12, func() { s := []int{1}; i := uint8(3); println(len(s[i:])) })
	try(13, func() { x, n := 1, -1; println(x << n) })
	try(14, func() { var x any = 1; println(x.(interface{ M() }) == nil) })
	try(15, func() { s := []int{1}; i := -2; println(s[i]) })
	try(16, func() { s := []int{1, 2, 3}; i, j := 2, 1; println(len(s[i:j])) })
	try(17, func() { panic("plain") })
	var d1, d2, t1, t2 any
	func() { defer func() { d1 = recover() }(); println(div(2, 0)) }()
	func() { defer func() { d2 = recover() }(); println(div(3, 0)) }()
	func() { defer func() { t1 = recover() }(); var x any = 1; println(x.(string)) }()
	func() { defer func() { t2 = recover() }(); var x any = 1; println(x.(string)) }()
	println(d1 == d2, t1 == t2, t1 == t1, d1 != t1)
}
