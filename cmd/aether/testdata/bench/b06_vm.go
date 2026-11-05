package main

// Switch dispatch with state in package-level variables: a small
// accumulator machine whose program is packed into a uint64.

var acc, aux, steps int64

func step(op uint64) {
	switch op {
	case 0:
		acc++
	case 1:
		acc += aux
	case 2:
		aux, acc = acc, aux
	case 3:
		acc ^= acc >> 3
	case 4:
		aux -= 3
	case 5:
		acc *= 5
	case 6:
		acc &= 0xffffff
	default:
		aux += acc & 7
	}
	steps++
}

func main() {
	code := uint64(0x0123456701234567)
	for r := 0; r < 1000000; r++ {
		for pc := uint(0); pc < 16; pc++ {
			step(code >> (4 * pc) & 0xf % 8)
		}
		code = code*6364136223846793005 + 1442695040888963407
	}
	println(acc, aux, steps)
}
