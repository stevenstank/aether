package main

import (
	"fmt"
	"hash/fnv"
)

func main() {
	h := fnv.New64a()
	h.Write([]byte("abc"))
	fmt.Println(h.Sum64())
}
