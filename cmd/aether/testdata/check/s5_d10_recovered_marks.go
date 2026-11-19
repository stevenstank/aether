// aether:expect run

package main

// A panic recovered and still listed is written with " [recovered]" (D-043).

func main() {
	defer func() {
		r := recover()
		println("got", r.(string))
		panic("again")
	}()
	panic("first")
}
