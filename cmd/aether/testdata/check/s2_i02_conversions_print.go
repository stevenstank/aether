// aether:expect run

package main

// conv prints x converted to every integer type.
func conv(x int64) {
	println(int8(x), int16(x), int32(x), int64(x), uint8(x), uint16(x), uint32(x), uint64(x))
}

// uconv prints x converted to every integer type.
func uconv(x uint64) {
	println(int8(x), int16(x), int32(x), int64(x), uint8(x), uint16(x), uint32(x), uint64(x))
}

func main() {
	conv(0)
	conv(-1)
	conv(127)
	conv(128)
	conv(-129)
	conv(255)
	conv(256)
	conv(32767)
	conv(-32769)
	conv(65536)
	conv(2147483648)
	conv(-2147483649)
	conv(4294967295)
	conv(-9223372036854775808)
	conv(9223372036854775807)
	uconv(18446744073709551615)
	uconv(9223372036854775808)
	uconv(4294967296 + 65535)

	// Conversions of narrower types widen with their sign.
	var a int8 = -100
	var b uint8 = 156
	println(int64(a), uint64(a), int64(b), int16(int8(b)), uint32(a), int32(uint16(a)))
	println(int8(a+a), uint8(b+b), int8(b) == a, uint8(a) == b)

	// print of every integer type and bool: no separators, no newline.
	var i16 int16 = -32768
	var i32 int32 = 2147483647
	var u16 uint16 = 65535
	var u32 uint32 = 4294967295
	var u64 uint64 = 18446744073709551615
	print(a, b, i16, i32, u16, u32, u64, true, false, -1, 0)
	print()
	println()
	print(1, 2)
	println(3, 4)
	print(u64, a)
	println()
}
