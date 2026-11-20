package main

import "math/bits"

func main() {
	x := uint64(0xf0f0_0000_1234_8000)
	println(bits.LeadingZeros64(x), bits.TrailingZeros64(x), bits.OnesCount64(x), bits.Len64(x), bits.Len(7))
	println(bits.RotateLeft32(0x80000001, 3), bits.Reverse8(1), bits.ReverseBytes16(0x1234), bits.Reverse64(x))
	hi, lo := bits.Mul64(x, x)
	sum, carry := bits.Add64(x, x, 1)
	diff, borrow := bits.Sub64(1, x, 0)
	q, r := bits.Div64(1, lo, x)
	println(hi, lo, sum, carry, diff, borrow, q, r, bits.UintSize)
	defer func() {
		e := recover().(error)
		println(e.Error())
	}()
	bits.Div64(5, 0, 3)
}
