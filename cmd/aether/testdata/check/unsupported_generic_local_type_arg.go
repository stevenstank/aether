// aether:expect unsupported generic-instance

package main

func id[T any](x T) T { return x }

// A type declared in a generic function that uses its type parameter
// cannot be moved to the package level (D-057).
func wrap[T any](x T) T {
	type local struct{ v T }
	return id(local{x}).v
}

func main() {
	println(wrap(1))
}
