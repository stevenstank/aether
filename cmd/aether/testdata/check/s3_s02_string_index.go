// aether:expect run

package main

// Indexing strings: the first and last bytes, bytes of multibyte runes,
// NUL and invalid bytes, indices of every integer type, the byte's type in
// arithmetic, and indexing inside expressions whose other operands branch
// (&&, ||, division, and other index expressions), so that values computed
// before a bounds check survive it.

func bytesOf(s string) {
	for i := 0; i < len(s); i++ {
		print(s[i], " ")
	}
	println(len(s))
}

func at(s string, i int) byte { return s[i] }

func main() {
	s := "héllo\x00\xff世"
	bytesOf(s)
	bytesOf("")
	bytesOf("a")
	var i8 int8 = 1
	var i16 int16 = 2
	var i32 int32 = 3
	var i64 int64 = 4
	var u8 uint8 = 5
	var u16 uint16 = 6
	var u32 uint32 = 7
	var u64 uint64 = 8
	var ui uint = 9
	var up uintptr = 10
	println(s[i8], s[i16], s[i32], s[i64], s[u8], s[u16], s[u32], s[u64], s[ui], s[up], s[len(s)-1])
	println("xyz"[0], "xyz"[2], "xyz"[i8], greeting()[0])
	// A byte is a uint8: arithmetic wraps at 8 bits.
	b := s[7]
	b++
	println(b, s[7]+s[7], s[0]-s[1], s[0]*3, s[0]/7, s[0]>>1, ^s[0], s[0] == 104, int(s[7])+1)
	// Operands that branch, before and after the index.
	x, y := 7, 3
	t := true
	println(s[0], t && x > y, s[1], x/y, s[2], s[s[0]-104], x%y, s[x/y])
	println(t || s[0] > 0, s[len(s)-1], at(s, x/y), at("ab", 1), x/y+int(s[0]))
	c := s[0] + s[1]
	c += s[2]
	println(c, s[0] > s[1] && s[1] < s[2], s[0] == s[0] || x/(y-3) > 0)
	switch s[0] {
	case 104:
		println("h")
	}
	if s[5] == 0 {
		println("nul")
	}
}

func greeting() string { return "hi" }
