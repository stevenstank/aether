package main

import (
	"fmt"
)

func main() {
	for i := 0; i < 3; i++ {
		for _, s := range []string{"a", "b"} {
			if s == "b" {
				goto next
			}
			fmt.Println(i, s)
		}
		fmt.Println("unreached")
	next:
	}
}
