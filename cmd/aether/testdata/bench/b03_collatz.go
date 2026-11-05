package main

// Loop-dominated, no calls in the inner loop: Collatz trajectories.

func main() {
	total, best, arg := 0, 0, uint64(0)
	for n := uint64(1); n < 600000; n++ {
		x := n
		k := 0
		for x != 1 {
			if x&1 == 0 {
				x >>= 1
			} else {
				x = 3*x + 1
			}
			k++
		}
		total += k
		if k > best {
			best = k
			arg = n
		}
	}
	println(total, best, arg)
}
