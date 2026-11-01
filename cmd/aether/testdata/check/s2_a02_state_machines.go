// aether:expect run

package main

// State machines and a small bytecode interpreter driven by switch, with
// their state in package-level variables.

var a, b int64 // the interpreter's registers
var executed int

// run executes the 4-bit opcodes packed into code, lowest first, until
// opcode 0 or the 16th opcode, and returns the program counter it stopped
// at. A backward jump is taken at most limit times.
func run(code uint64, limit int) int {
	pc := 0
	for pc < 16 {
		op := code >> (4 * uint(pc)) & 0xf
		executed++
		pc++
		switch op {
		case 0:
			return pc - 1
		case 1:
			a++
		case 2:
			a *= 2
		case 3:
			a, b = b, a
		case 4:
			b += a
		case 5:
			a--
		case 6:
			println(a, b)
		case 7:
			// Skip the next opcode when a is zero.
			if a == 0 {
				pc++
			}
		case 8:
			// Jump back to the start.
			if limit == 0 {
				break
			}
			limit--
			pc = 0
		default:
			print(op)
			println()
		}
	}
	return pc
}

// mod3 runs a three-state automaton over the bits of x, most significant
// first, and returns x mod 3 without dividing.
func mod3(x uint32) int {
	state := 0
	for i := 31; i >= 0; i-- {
		bit := int(x >> uint(i) & 1)
		switch state {
		case 0:
			state = bit
		case 1:
			switch bit {
			case 0:
				state = 2
			case 1:
				state = 0
			}
		case 2:
			if bit == 0 {
				state = 1
			}
		}
	}
	return state
}

func daysIn(month, year int) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		switch {
		case year%400 == 0:
			return 29
		case year%100 == 0:
			return 28
		case year%4 == 0:
			return 29
		}
		return 28
	}
	return 31
}

// classify counts the runs of equal decimal digits in n.
func classify(n uint64) (int, int) {
	runs, longest := 0, 0
	prev, length := uint64(10), 0
	for n > 0 {
		d := n % 10
		n /= 10
		switch d {
		case prev:
			length++
		default:
			runs++
			length = 1
			prev = d
		}
		if length > longest {
			longest = length
		}
	}
	return runs, longest
}

func main() {
	// a = 3, then a doubled into b three times through a loop.
	println(run(0x0, 0))
	// executed is printed in a statement of its own (SEMANTICS.md V-4).
	println(run(0x6_4_2_1_1_1, 0))
	println(executed)
	a, b = 0, 0
	println(run(0x6_8_6_4_1, 2))
	println(a, b)
	a, b = 0, 5
	println(run(0x6_9_7_6_3, 0))
	println(run(0x6_1_7_5_1_6, 0))
	println(executed)

	ok := true
	for x := uint32(0); x < 2000; x += 7 {
		if mod3(x) != int(x%3) {
			ok = false
		}
	}
	println(ok, mod3(4294967295), mod3(4294967294), mod3(1<<31))

	total := 0
	for m := 1; m <= 12; m++ {
		total += daysIn(m, 2024)
	}
	println(total, daysIn(2, 1900), daysIn(2, 2000), daysIn(2, 2023))

	println(classify(0))
	println(classify(1122233334))
	println(classify(18446744073709551615))
}
