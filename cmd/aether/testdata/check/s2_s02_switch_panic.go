// aether:expect run

package main

// A case value is evaluated only if no earlier one matched, so the
// division by zero runs only for a tag other than 1.
func pick(tag, d int) int {
	switch tag {
	case 1, 10 / d:
		return 1
	}
	return 0
}

func main() {
	println(pick(1, 0))
	println(pick(5, 2))
	print(pick(2, 0))
}
