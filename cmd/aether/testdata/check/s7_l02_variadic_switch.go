// aether:expect run

package main

// Variadic functions, called with arguments or a slice and ..., and
// expression switches on an interface value (D-049, increment 11.5).

type code int

func (c code) Error() string { return "code" }

var errA error = code(1)

var errB = code(2)

func sum(base int, xs ...int) int {
	println(len(xs), cap(xs), xs == nil)
	for _, x := range xs {
		base += x
	}
	return base
}

func join(sep string, parts ...string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += sep
		}
		s += p
	}
	return s
}

func any2(xs ...any) int { return len(xs) }

func trace(s string) int {
	println("eval", s)
	return len(s)
}

func describe(err error) string {
	switch err {
	case nil:
		return "nil"
	case errA:
		return "A"
	case errB, code(3):
		return "B or 3"
	}
	return "other"
}

func which(v any) int {
	switch v {
	case 1:
		return 1
	case "one", int8(1):
		return 2
	case errB:
		return 3
	}
	return 0
}

func main() {
	println(sum(1))
	println(sum(1, 2, 3))
	s := []int{10, 20}
	println(sum(0, s...))
	s2 := s[:1]
	println(sum(0, s2...), s[1])
	println(join("-", "a", "b", "c"), join(","), any2(1, "x", nil, 2.5), any2())
	println(sum(trace("a"), trace("bb"), trace("ccc")))
	println(describe(nil), describe(errA), describe(code(2)), describe(code(3)), describe(code(4)))
	println(which(1), which("one"), which(int8(1)), which(code(2)), which(2), which(nil))
}
