// aether:expect unsupported selector

package main

func main() {
	var s struct{ f int }
	println(s.f)
}
