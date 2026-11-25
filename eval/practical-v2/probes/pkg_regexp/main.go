package main

import (
	"fmt"
	"regexp"
)

func main() {
	re := regexp.MustCompile(`(\d+)-(\w+)`)
	fmt.Println(re.FindStringSubmatch("12-ab"), re.ReplaceAllString("1-x 2-y", "$2"))
}
