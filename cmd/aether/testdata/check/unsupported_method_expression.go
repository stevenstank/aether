// aether:expect unsupported method-expression

package main

type T struct{ x int }

func (t *T) inc() { t.x++ }

func main() {
	var t T
	inc := (*T).inc
	inc(&t)
	println(t.x)
}
