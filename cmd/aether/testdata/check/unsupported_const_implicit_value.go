// aether:expect unsupported const-implicit-value

package main

func main() {
	const (
		a = 1
		b
	)
	println(a, b)
}
