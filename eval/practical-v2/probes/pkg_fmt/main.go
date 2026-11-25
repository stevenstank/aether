package main

import (
	"fmt"
)

func main() {
	type P struct{ X int }
	fmt.Printf("%T %v %+v %q %-5s| %6.2f %x %08x\n", P{}, P{1}, &P{2}, "q", "ab", 3.14159, 255, 7)
}
