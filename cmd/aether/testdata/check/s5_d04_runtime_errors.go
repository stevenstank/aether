// aether:expect run

package main

// Run-time errors unwind like panic: deferred calls run, and their
// messages are written in the list of panics (D-043).

func idx(s []int, i int) int { return s[i] }

func div(a, b int8) int8 { return a / b }

func main() {
	defer func() {
		var m map[string]int
		m["x"] = 1
	}()
	defer func() {
		var x any = "s"
		println(x.(int))
	}()
	defer func() {
		s := []int{1, 2, 3}
		var i uint8 = 200
		t := s[1:i]
		println(len(t))
	}()
	defer func() {
		var i int8 = -3
		s := []int{1}
		t := s[i:]
		println(len(t))
	}()
	defer func() { println(div(1, 0)) }()
	defer func() {
		var f func()
		f()
	}()
	println(idx([]int{1, 2}, 5))
}
