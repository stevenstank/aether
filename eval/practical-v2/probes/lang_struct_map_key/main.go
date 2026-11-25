package main

import (
	"fmt"
)

func main() {
	type Value struct {
		K int
		S string
		F float64
	}
	seen := map[Value]bool{}
	seen[Value{1, "a", 0}] = true
	fmt.Println(seen[Value{1, "a", 0}], seen[Value{2, "a", 0}])
}
