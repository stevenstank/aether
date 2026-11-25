package main

import (
	"fmt"
)

func pairs(n int) func(yield func(int, string) bool) {
	return func(yield func(int, string) bool) {
		for i := 0; i < n; i++ {
			if !yield(i, fmt.Sprint("v", i)) {
				return
			}
		}
	}
}

func main() {
	for k, v := range pairs(3) {
		fmt.Println(k, v)
		if k == 1 {
			break
		}
	}
}
