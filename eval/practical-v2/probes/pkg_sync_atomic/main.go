package main

import (
	"fmt"
	"sync/atomic"
)

func main() {
	var c atomic.Int64
	c.Add(2)
	fmt.Println(c.Load())
}
