// aether:expect run

package main

// Package initialization may call main: Go forbids referring to init, not
// to main. Init runs first, its call of main runs main once, and then main
// runs again as the program's entry.

var calls int

var n = f()

func f() int {
	println(100, calls)
	main()
	return 7
}

func main() {
	calls++
	println(200, calls)
}
