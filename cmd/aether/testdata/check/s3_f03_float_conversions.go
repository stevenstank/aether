// aether:expect run

package main

// Conversions between floats and every integer type, at and beyond each
// boundary. In range they truncate toward zero; out of range, the result is
// implementation-specific in Go, and Aether matches gc on amd64 (D-032).

func toInts(x float64) {
	println(x, int8(x), int16(x), int32(x), int64(x), int(x))
	println(uint8(x), uint16(x), uint32(x), uint64(x), uint(x), uintptr(x))
}

func toInts32(x float32) {
	println(x, int8(x), int16(x), int32(x), int64(x), uint8(x), uint16(x), uint32(x), uint64(x))
}

func fromInts(i8 int8, i16 int16, i32 int32, i64 int64, u8 uint8, u16 uint16, u32 uint32, u64 uint64) {
	println(float64(i8), float64(i16), float64(i32), float64(i64), float64(u8), float64(u16), float64(u32), float64(u64))
	println(float32(i8), float32(i16), float32(i32), float32(i64), float32(u8), float32(u16), float32(u32), float32(u64))
}

func main() {
	zero := 0.0
	toInts(0)
	toInts(-zero)
	toInts(0.9)
	toInts(-0.9)
	toInts(127.9)
	toInts(128)
	toInts(-128.9)
	toInts(-129)
	toInts(255.5)
	toInts(256)
	toInts(-1)
	toInts(32767.5)
	toInts(32768)
	toInts(65535.9)
	toInts(65536)
	toInts(-70000)
	toInts(2147483647.9)
	toInts(2147483648)
	toInts(-2147483648.9)
	toInts(-2147483649)
	toInts(4294967295.5)
	toInts(4294967296)
	toInts(4.5e9)
	toInts(-3e9)
	toInts(9223372036854774784)
	toInts(9223372036854775808)
	toInts(-9223372036854775808)
	toInts(-9223372036854777856)
	toInts(9.3e18)
	toInts(-9.3e18)
	toInts(18446744073709549568)
	toInts(18446744073709551616)
	toInts(1e300)
	toInts(-1e300)
	toInts(1 / zero)
	toInts(-1 / zero)
	toInts(zero / zero)
	toInts(5e-324)
	var z32 float32
	toInts32(0.5)
	toInts32(-128.5)
	toInts32(70000)
	toInts32(-70000)
	toInts32(3e9)
	toInts32(-3e9)
	toInts32(9.2233715e18)
	toInts32(1.8446743e19)
	toInts32(1e20)
	toInts32(1 / z32)
	toInts32(z32 / z32)
	fromInts(127, 32767, 2147483647, 9223372036854775807, 255, 65535, 4294967295, 18446744073709551615)
	fromInts(-128, -32768, -2147483648, -9223372036854775808, 0, 0, 0, 0)
	fromInts(-1, -1, 16777217, 9007199254740993, 1, 1, 16777217, 9007199254740993)
	fromInts(0, 0, 0, 1<<53+1, 0, 0, 0, 1<<63+1<<11)
	fromInts(0, 0, 0, -(1<<53 + 1), 0, 0, 0, 1<<63+1<<11+1)
	fromInts(0, 0, 0, 1<<62+1<<9, 0, 0, 0, 1<<64-1<<11)
}
