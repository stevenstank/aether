// aether:expect unsupported index-expression

package main

func g[T any]() {}

func main() {
	g[int]()
}
