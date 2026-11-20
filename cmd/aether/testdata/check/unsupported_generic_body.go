// aether:expect unsupported fallthrough

package main

// The copies of generic code are checked against the subset; a
// rejection is reported once, at the generic code's position.
func pick[T int | float64](x T) T {
	switch {
	case x > 0:
		fallthrough
	default:
		x++
	}
	return x
}

func main() {
	println(pick(1), pick(2.5))
}
