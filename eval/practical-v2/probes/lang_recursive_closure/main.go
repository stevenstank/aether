package main

import "fmt"

func main() {
	var walk func(n int) int
	walk = func(n int) int {
		if n == 0 {
			return 0
		}
		return n + walk(n-1)
	}
	fmt.Println(walk(10))
}
