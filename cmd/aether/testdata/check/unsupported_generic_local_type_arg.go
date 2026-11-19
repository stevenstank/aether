// aether:expect unsupported generic-instance

package main

func id[T any](x T) T { return x }

func main() {
	type local struct{ a int }
	println(id(local{1}).a)
}
