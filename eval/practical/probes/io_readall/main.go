package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	b, err := io.ReadAll(os.Stdin)
	fmt.Println(len(b), err)
}
