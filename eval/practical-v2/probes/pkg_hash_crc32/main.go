package main

import (
	"fmt"
	"hash/crc32"
)

func main() {
	fmt.Println(crc32.ChecksumIEEE([]byte("abc")))
}
