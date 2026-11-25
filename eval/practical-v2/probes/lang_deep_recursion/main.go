package main

import (
	"fmt"
)

func depth(n int) int {
	if n == 0 {
		return 0
	}
	var pad [8]int
	pad[n%8] = n
	return depth(n-1) + 1 + pad[0]*0
}

func main() {
	fmt.Println(depth(200000))
}
