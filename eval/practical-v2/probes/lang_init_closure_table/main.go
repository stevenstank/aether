package main

import (
	"fmt"
	"strings"
)

var table map[string]func(string) string

func init() {
	table = map[string]func(string) string{
		"up":    strings.ToUpper,
		"twice": func(s string) string { return table["up"](s) + s },
	}
}

func main() {
	fmt.Println(table["up"]("abc"), table["twice"]("x"))
}
