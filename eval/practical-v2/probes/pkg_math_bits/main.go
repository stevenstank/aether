package main

import (
	"fmt"
	"math/bits"
)

func main() {
	hi, lo := bits.Mul64(1<<63, 4)
	fmt.Println(hi, lo)
}
