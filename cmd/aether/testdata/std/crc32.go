package main

import (
	"fmt"
	"hash/crc32"
)

func main() {
	inputs := [][]byte{nil, []byte("a"), []byte("hello, world"), []byte("The quick brown fox jumps over the lazy dog")}
	big := make([]byte, 20000)
	for i := range big {
		big[i] = byte(i*131 + i>>7)
	}
	inputs = append(inputs, big, big[3:4099], big[1:17])
	tables := []*crc32.Table{crc32.IEEETable, crc32.MakeTable(crc32.IEEE), crc32.MakeTable(crc32.Castagnoli), crc32.MakeTable(crc32.Koopman)}
	for _, in := range inputs {
		fmt.Printf("%d %08x", len(in), crc32.ChecksumIEEE(in))
		for _, t := range tables {
			fmt.Printf(" %08x", crc32.Checksum(in, t))
		}
		fmt.Println()
	}
	// Incremental updates match one-shot checksums, at every split.
	c := crc32.MakeTable(crc32.Castagnoli)
	data := big[:300]
	bad := 0
	for split := 0; split <= len(data); split++ {
		if crc32.Update(crc32.Update(0, c, data[:split]), c, data[split:]) != crc32.Checksum(data, c) {
			bad++
		}
		if crc32.Update(crc32.Update(0, crc32.IEEETable, data[:split]), crc32.IEEETable, data[split:]) != crc32.ChecksumIEEE(data) {
			bad++
		}
	}
	fmt.Println("bad splits:", bad)
	// hash.Hash32.
	h := crc32.New(c)
	h.Write([]byte("hello, "))
	h.Write([]byte("world"))
	fmt.Printf("%08x %x %d %d\n", h.Sum32(), h.Sum([]byte{0xAA}), h.Size(), h.BlockSize())
	h.Reset()
	fmt.Printf("%08x\n", h.Sum32())
	ih := crc32.NewIEEE()
	ih.Write(big)
	fmt.Printf("%08x\n", ih.Sum32())
}
