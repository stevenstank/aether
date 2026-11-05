package main

// Narrow integer types: a bitwise CRC-32 over a stream of generated
// bytes, with uint8, uint16, and int32 arithmetic.

func crc32(crc uint32, b uint8) uint32 {
	crc ^= uint32(b)
	for k := 0; k < 8; k++ {
		if crc&1 != 0 {
			crc = crc>>1 ^ 0xedb88320
		} else {
			crc >>= 1
		}
	}
	return crc
}

func main() {
	crc := ^uint32(0)
	var seed uint16 = 0xace1
	var acc int32
	for i := 0; i < 3000000; i++ {
		bit := (seed ^ seed>>2 ^ seed>>3 ^ seed>>5) & 1
		seed = seed>>1 | bit<<15
		b := uint8(seed) + uint8(i)
		crc = crc32(crc, b)
		acc += int32(int8(b)) * 3
	}
	println(^crc, seed, acc)
}
