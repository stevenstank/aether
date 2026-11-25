package main

import (
	"fmt"
	"strconv"
)

func main() {
	u, err := strconv.Unquote(`"a\tb"`)
	f, _ := strconv.ParseFloat("1e3", 64)
	fmt.Println(u, err, f, strconv.FormatFloat(0.1, 'g', -1, 64), strconv.Quote("x"))
}
