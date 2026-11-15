// aether:expect run

package main

// Slicing strings: two-index slice expressions with every combination of
// bounds, of constants, variables, fields, and results, and of the empty
// string; a slice shares the bytes, which a string never changes.

type holder struct {
	s string
}

func get() string { return "returned" }

func bounds() (int, int) { return 2, 5 }

func main() {
	s := "abcdefgh"
	println(s[2:5], s[:3], s[5:], s[:], s[8:], s[0:0], len(s[3:3]))
	i, j := bounds()
	println(s[i:j], s[i:], s[:j], len(s[j:j]))
	var u8 uint8 = 7
	var i16 int16 = 1
	println(s[i16:u8], s[u8:])
	h := holder{"held string"}
	println(h.s[5:], get()[2:4], "constant"[3:], ("ab" + s)[1:4])
	t := s[1:7]
	t = t[1:5]
	println(t, len(t), t[0], t[3])
	t = t[2:]
	println(t)
	var e string
	println(len(e[:]), len(e[0:]), e[:0]+"x")
	r := s
	s = "changed"
	println(r[1:3], s[1:3])
}
