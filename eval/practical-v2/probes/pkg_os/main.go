package main

import (
	"fmt"
	"os"
)

func main() {
	b, err := os.ReadFile("/nonexistent")
	f, _ := os.CreateTemp("", "probe-*")
	os.Remove(f.Name())
	fmt.Println(len(b), err != nil, len(os.Args) > 0)
}
