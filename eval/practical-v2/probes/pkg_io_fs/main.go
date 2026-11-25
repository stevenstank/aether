package main

import (
	"fmt"
	"io/fs"
	"os"
)

func main() {
	_, err := os.ReadDir("/nonexistent")
	var pe *fs.PathError
	fmt.Println(err != nil, pe == nil)
}
