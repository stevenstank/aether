package main

import (
	"cmp"
	"fmt"
)

func main() {
	fmt.Println(cmp.Compare(1, 2), cmp.Compare("b", "a"), cmp.Less(1.0, 2.0))
}
