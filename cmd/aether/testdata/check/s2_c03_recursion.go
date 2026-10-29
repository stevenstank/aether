// aether:expect run

package main

// sum recurses n levels deep: far below both the interpreter's depth limit
// and a quarter of the native stack, and far below gc's limit.
func sum(n int) int {
	if n == 0 {
		return 0
	}
	return n + sum(n-1)
}

func even(n int) bool {
	if n == 0 {
		return true
	}
	return odd(n - 1)
}

func odd(n int) bool {
	if n == 0 {
		return false
	}
	return even(n - 1)
}

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	println(sum(50000), sum(1), sum(0))
	println(even(10001), odd(10001))
	println(fib(20))
}
