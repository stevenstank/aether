// aether:expect run

package main

func sign(x int) int {
	if x < 0 {
		return -1
	} else if x == 0 {
		return 0
	}
	return 1
}

// The result is returned from inside an endless loop.
func firstSquareAbove(n int) int {
	for i := 0; ; i++ {
		if i*i > n {
			return i
		}
	}
}

func main() {
	println(sign(-5), sign(0), sign(7))
	println(firstSquareAbove(50))

	// The three for forms, break, and continue.
	sum := 0
	for i := 0; i < 10; i++ {
		if i == 3 {
			continue
		}
		if i == 8 {
			break
		}
		sum += i
	}
	println(sum)
	n := 5
	for n > 0 {
		n -= 2
	}
	println(n)
	k := 0
	for {
		k++
		if k >= 4 {
			break
		}
	}
	println(k)

	// Nested loops: break and continue apply to the innermost.
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == i {
				continue
			}
			if j > i {
				break
			}
			println(i, j)
		}
	}

	// if with an init statement, shadowing, and else chains.
	if x := sum * 2; x > 100 {
		println(x, true)
	} else if y := x - 50; y < 0 {
		println(x, y)
	} else {
		println(y)
	}

	// Loop variables wrap at the int boundary like any other.
	big := 9223372036854775806
	for i := big; i > 0; i++ {
		println(i)
	}
	return
	println(0)
}
