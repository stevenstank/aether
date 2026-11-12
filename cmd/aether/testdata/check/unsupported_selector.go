// aether:expect unsupported selector

package main

type T struct{}

func (T) m() {}

func main() {
	var t T
	g := t.m
	g()
}
