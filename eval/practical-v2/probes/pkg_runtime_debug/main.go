package main

import (
	"fmt"
	"runtime/debug"
)

func main() {
	fmt.Println(debug.SetMaxStack(1<<30) > 0)
}
