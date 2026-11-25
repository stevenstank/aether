package main

import (
	"fmt"
	"strings"
	"unicode"
)

func main() {
	fmt.Println(strings.Fields(" a b "), strings.TrimRightFunc("x  ", unicode.IsSpace), strings.EqualFold("A", "a"))
	a, b, ok := strings.Cut("k=v", "=")
	s, found := strings.CutPrefix("Bearer t", "Bearer ")
	fmt.Println(a, b, ok, s, found)
}
