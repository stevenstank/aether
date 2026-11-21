// aether:expect unsupported generic-instance

package main

// For T = [1]int the index of the copy is a constant out of range, which
// gc accepts in the generic code and reports when it runs. The instance
// errors the copy may have are only those of type tests (s5_g06).

func last[T [1]int | [2]int](a T) int {
	defer func() { recover() }()
	return a[len(a)]
}

func main() {
	println(last([1]int{}))
}
