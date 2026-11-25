package main

import "fmt"

func main() {
	limit, offset := 20, 0
	params := []struct {
		name string
		dst  *int
	}{{"limit", &limit}, {"offset", &offset}}
	for _, p := range params {
		*p.dst += len(p.name)
	}
	fmt.Println(limit, offset)
}
