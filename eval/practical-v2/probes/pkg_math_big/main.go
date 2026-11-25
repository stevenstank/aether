package main

import (
	"fmt"
	"math/big"
)

func main() {
	a := new(big.Int).Exp(big.NewInt(2), big.NewInt(100), nil)
	q := new(big.Rat).SetFrac64(1, 3)
	f, _ := q.Float64()
	r := new(big.Rat)
	r.SetFloat64(0.1)
	fmt.Println(a, q.RatString(), f, r, a.Text(16), new(big.Int).Sqrt(a))
}
