// aether:expect run

package main

// One function per integer type prints every operator and comparison of
// its arguments; main calls each with the type's boundary values. The
// expected output is gc's (TestDifferentialAgainstGc).

func tint8(x, y int8) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func tint16(x, y int16) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func tint32(x, y int32) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func tint64(x, y int64) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func tuint8(x, y uint8) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func tuint16(x, y uint16) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func tuint32(x, y uint32) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func tuint64(x, y uint64) {
	println(x+y, x-y, x*y, x&y, x|y, x^y, x&^y, -x, ^x, +y)
	println(x == y, x != y, x < y, x <= y, x > y, x >= y)
	z := x
	z += y
	z *= x
	z -= 1
	z &= ^y
	z |= 5
	z ^= x
	z &^= 3
	z++
	z--
	z++
	println(z)
}

func main() {
	var i8lo, i8hi int8 = -128, 127
	tint8(-128, 1)
	tint8(127, 127-1)
	tint8(0, 2)
	tint8(1, -128)
	tint8(2, 7)
	tint8(7, 127)
	tint8(-1, -1)
	tint8(-128+1, 0)
	tint8(127-1, -128+1)
	tint8(i8lo, i8hi)
	tint8(i8hi, i8hi)
	var i6lo, i6hi int16 = -32768, 32767
	tint16(-32768, 1)
	tint16(32767, 32767-1)
	tint16(0, 2)
	tint16(1, -32768)
	tint16(2, 7)
	tint16(7, 32767)
	tint16(-1, -1)
	tint16(-32768+1, 0)
	tint16(32767-1, -32768+1)
	tint16(i6lo, i6hi)
	tint16(i6hi, i6hi)
	var i2lo, i2hi int32 = -2147483648, 2147483647
	tint32(-2147483648, 1)
	tint32(2147483647, 2147483647-1)
	tint32(0, 2)
	tint32(1, -2147483648)
	tint32(2, 7)
	tint32(7, 2147483647)
	tint32(-1, -1)
	tint32(-2147483648+1, 0)
	tint32(2147483647-1, -2147483648+1)
	tint32(i2lo, i2hi)
	tint32(i2hi, i2hi)
	var i4lo, i4hi int64 = -9223372036854775808, 9223372036854775807
	tint64(-9223372036854775808, 1)
	tint64(9223372036854775807, 9223372036854775807-1)
	tint64(0, 2)
	tint64(1, -9223372036854775808)
	tint64(2, 7)
	tint64(7, 9223372036854775807)
	tint64(-1, -1)
	tint64(-9223372036854775808+1, 0)
	tint64(9223372036854775807-1, -9223372036854775808+1)
	tint64(i4lo, i4hi)
	tint64(i4hi, i4hi)
	var u8lo, u8hi uint8 = 0, 255
	tuint8(0, 1)
	tuint8(255, 128)
	tuint8(0, 2)
	tuint8(1, 0)
	tuint8(2, 7)
	tuint8(7, 255)
	tuint8(255-1, 255-1)
	tuint8(127, 0)
	tuint8(128, 127)
	tuint8(u8lo, u8hi)
	tuint8(u8hi, u8hi)
	var u6lo, u6hi uint16 = 0, 65535
	tuint16(0, 1)
	tuint16(65535, 32768)
	tuint16(0, 2)
	tuint16(1, 0)
	tuint16(2, 7)
	tuint16(7, 65535)
	tuint16(65535-1, 65535-1)
	tuint16(32767, 0)
	tuint16(32768, 32767)
	tuint16(u6lo, u6hi)
	tuint16(u6hi, u6hi)
	var u2lo, u2hi uint32 = 0, 4294967295
	tuint32(0, 1)
	tuint32(4294967295, 2147483648)
	tuint32(0, 2)
	tuint32(1, 0)
	tuint32(2, 7)
	tuint32(7, 4294967295)
	tuint32(4294967295-1, 4294967295-1)
	tuint32(2147483647, 0)
	tuint32(2147483648, 2147483647)
	tuint32(u2lo, u2hi)
	tuint32(u2hi, u2hi)
	var u4lo, u4hi uint64 = 0, 18446744073709551615
	tuint64(0, 1)
	tuint64(18446744073709551615, 9223372036854775808)
	tuint64(0, 2)
	tuint64(1, 0)
	tuint64(2, 7)
	tuint64(7, 18446744073709551615)
	tuint64(18446744073709551615-1, 18446744073709551615-1)
	tuint64(9223372036854775807, 0)
	tuint64(9223372036854775808, 9223372036854775807)
	tuint64(u4lo, u4hi)
	tuint64(u4hi, u4hi)

	// The aliases and the other 64-bit types.
	var b byte = 200
	var r rune = -5
	var u uint = 18446744073709551615
	var p uintptr = 9223372036854775808
	println(b+b, r*r, u+1, p+p, ^b, ^r, ^u, ^p)
}
