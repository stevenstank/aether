package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	r, n := utf8.DecodeRuneInString("\xff")
	l, _ := utf8.DecodeLastRuneInString("é")
	fmt.Println(r, n, l, utf8.RuneCountInString("héllo"), utf8.RuneLen('☺'))
}
