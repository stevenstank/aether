package main

import (
	"fmt"
	"math/rand/v2"
)

func main() {
	r := rand.New(rand.NewPCG(1, 2))
	fmt.Println(r.IntN(100), r.IntN(100))
}
