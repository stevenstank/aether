// aether:expect run

package main

// Rune literals and string(x) of an integer: the UTF-8 encoding of the
// value, or of U+FFFD for a value that is not a code point: negative,
// above U+10FFFF, or a surrogate.

const smile = '😀'

var lits = [10]rune{'a', '\x00', '\n', '\'', '\\', '\x7f', 'é', '\377', '世', '\U0010FFFF'}

func show(s string) {
	print(len(s), ":")
	for i := 0; i < len(s); i++ {
		print(" ", s[i])
	}
	println()
}

func main() {
	for i := 0; i < len(lits); i++ {
		print(lits[i], " ")
	}
	println(smile, 'a'+1, 'z'-'a', '0' <= '5' && '5' <= '9')
	var b byte = 'A'
	var i64 int64 = 'é'
	x := 'x'
	x++
	println(b, i64, x, string(x), string(b))

	rs := [24]rune{0, 1, 0x7f, 0x80, 0xff, 0x100, 0x7ff, 0x800, 0xfff, 0xd7ff, 0xd800, 0xdbff, 0xdc00, 0xdfff, 0xe000,
		0xfffd, 0xffff, 0x10000, 0x1f600, 0x10ffff, 0x110000, -1, -0x80000000, 0x7fffffff}
	for i := 0; i < len(rs); i++ {
		show(string(rs[i]))
	}

	// Every integer type, signed and unsigned, and values above the
	// int64 range.
	var i8 int8 = -1
	var u8 uint8 = 0xe9
	var i16 int16 = -0x8000
	var u16 uint16 = 0xd800
	var u32 uint32 = 0x10ffff
	var u64 uint64 = 1<<64 - 1
	var u uint = 1 << 63
	var big int64 = 1<<32 + 'a'
	var p uintptr = 0x263a
	show(string(i8))
	show(string(u8))
	show(string(i16))
	show(string(u16))
	show(string(u32))
	show(string(u64))
	show(string(u))
	show(string(big))
	show(string(p))

	// Bytes of a string, converted one by one, are code points 0 to 255.
	s := "aé\xff"
	t := ""
	for i := 0; i < len(s); i++ {
		t += string(s[i])
	}
	show(t)
	println(t, string(rune(65)), string(rune(0x4e16)) == "世", string(rune(-1)) == "�")
}
