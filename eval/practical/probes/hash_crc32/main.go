package main

import (
	"fmt"
	"hash/crc32"
)

func main() {
	fmt.Println(crc32.Checksum([]byte("hello"), crc32.MakeTable(crc32.Castagnoli)))
}
