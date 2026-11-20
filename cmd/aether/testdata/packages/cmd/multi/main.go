// A program of two files: package-level variables initialized across them,
// and an init function in each.
package main

var A = B + 1

var log []string

func note(s string) int {
	log = append(log, s)
	return len(log)
}

func init() { note("main.go init") }

func main() {
	println(A, B, C, D)
	for _, s := range log {
		println(s)
	}
	println(twice(A))
}
