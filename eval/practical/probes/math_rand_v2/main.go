package main

import (
	"fmt"
	"math/rand/v2"
)

func main() { fmt.Println(rand.New(rand.NewPCG(1, 2)).IntN(100)) }
