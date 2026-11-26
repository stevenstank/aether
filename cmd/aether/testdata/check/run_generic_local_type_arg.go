// aether:expect run

package main

// A type declared in a function, as a type argument: moved to the package
// level for the instance (D-057).

func id[T any](x T) T { return x }

func main() {
	type local struct{ a int }
	println(id(local{1}).a)
}
