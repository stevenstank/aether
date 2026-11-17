// aether:expect run

package main

// Reading and deleting from a nil map is allowed; assigning to an entry
// panics with gc's message, after the right side is evaluated (D-042).

func f() int { println("f"); return 1 }

func main() {
	var m map[string]int
	println(m["a"], len(m))
	delete(m, "x")
	m["k"] = f()
}
