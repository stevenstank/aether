package main

import (
	"fmt"
	"os"
)

func main() {
	b, err := os.ReadFile("main.go")
	fmt.Println(len(b) > 0, err)
}
