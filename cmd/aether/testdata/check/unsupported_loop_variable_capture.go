// aether:expect unsupported loop-variable-address

package main

// Go 1.22 gives each iteration of a for statement its own copy of a
// variable declared in its init, which a closure capturing it observes
// (D-040), as a pointer to it does (D-035).

func main() {
	var fs []func() int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i })
	}
	println(fs[0](), fs[2]())
}
