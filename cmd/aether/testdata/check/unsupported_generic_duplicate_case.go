// aether:expect unsupported generic-instance

package main

// For T = int the cases of the copy are duplicates, which gc accepts in
// the generic code.

func kind[T any](x any) int {
	switch x.(type) {
	case int:
		return 1
	case T:
		return 2
	}
	return 0
}

func main() {
	println(kind[string]("s"), kind[int](1))
}
