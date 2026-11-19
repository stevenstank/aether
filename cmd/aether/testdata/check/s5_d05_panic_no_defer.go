// aether:expect run

package main

// In a program without defer statements, panic writes its value and ends
// the program at once (D-043): an error's Error method is called first.

type E struct{ msg string }

func (e *E) Error() string { println("Error"); return e.msg }

func check(n int) {
	if n > 2 {
		var err error = &E{"too big"}
		panic(err)
	}
	println("ok", n)
}

func main() {
	for i := 0; i < 5; i++ {
		check(i)
	}
}
