package main

import (
	"fmt"
	"unicode"
)

func main() {
	fmt.Println(unicode.SimpleFold('k'), unicode.IsLetter('é'), unicode.Is(unicode.ASCII_Hex_Digit, 'f'), string(unicode.ToTitle('a')))
}
