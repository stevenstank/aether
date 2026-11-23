package main

import (
	"fmt"
	"strconv"
)

func main() { fmt.Println(strconv.QuotedPrefix(`"a\"b" rest`)) }
