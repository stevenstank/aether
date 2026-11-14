// aether:expect unsupported loop-variable-address

package main

// Go 1.22 gives each iteration its own copy of a variable declared in a for
// statement's init, which a pointer can observe (D-035).

func main() {
	var last *int
	for i := 0; i < 3; i++ {
		last = &i
	}
	println(*last)
}
