package main

import (
	"fmt"
)

func main() {
	k := 2
	fmt.Println([...]string{"NULL", "INT", "FLOAT", "TEXT"}[k], map[string]string{"<": ">"}["<"])
}
