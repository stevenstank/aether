package main

import (
	"fmt"
	"io"
	"strings"
)

func main() {
	b, _ := io.ReadAll(io.LimitReader(strings.NewReader("abcdef"), 3))
	fmt.Println(string(b), io.EOF, io.ErrUnexpectedEOF)
}
