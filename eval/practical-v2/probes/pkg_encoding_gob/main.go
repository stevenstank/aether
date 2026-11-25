package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

func main() {
	type T struct {
		A int
		B []string
	}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode([]T{{1, []string{"x"}}})
	var out []T
	err := gob.NewDecoder(&buf).Decode(&out)
	fmt.Println(out, err)
}
