// aether:expect run

package main

// UTF-8 over whole ranges: every string of two bytes; after each lead
// byte whose second byte has its own range, and a few others, every second
// byte, with the third and fourth bytes at the edges of their ranges; and the encoding of code points
// across the whole range, decoded back. Each line is a checksum.

func sum(rs []rune) int {
	h := len(rs)
	for i := 0; i < len(rs); i++ {
		h = h*31 + int(rs[i])
	}
	return h
}

func main() {
	b := make([]byte, 4)
	for x := 0; x < 256; x++ {
		h := 0
		for y := 0; y < 256; y++ {
			b[0], b[1] = byte(x), byte(y)
			h = h*7 + sum([]rune(string(b[:2])))
		}
		print(x, ":", h, " ")
		if x%8 == 7 {
			println()
		}
	}
	edges := []byte{0x00, 0x7f, 0x80, 0x8f, 0x90, 0x9f, 0xa0, 0xbf, 0xc0, 0xff}
	leads := []int{0xe0, 0xe1, 0xed, 0xee, 0xf0, 0xf3, 0xf4, 0xf5}
	for l := 0; l < len(leads); l++ {
		x := leads[l]
		h := 0
		for y := 0; y < 256; y++ {
			for z := 0; z < len(edges); z++ {
				b[0], b[1], b[2], b[3] = byte(x), byte(y), edges[z], 0x80
				h = h*7 + sum([]rune(string(b[:3]))) + sum([]rune(string(b)))
			}
		}
		println(x, h)
	}
	h, n := 0, 0
	for r := -3; r < 0x110010; r += 251 {
		s := string(rune(r))
		rs := []rune(s)
		if len(rs) != 1 || (rs[0] != rune(r) && rs[0] != 0xfffd) {
			println("bad", r)
		}
		h = h*31 + len(s)*int(s[len(s)-1]) + sum(rs)
		n++
	}
	println(n, h)
}
