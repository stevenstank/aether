package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func main() {
	type T struct {
		N int    `json:"n"`
		S string `json:"s,omitempty"`
	}
	b, _ := json.MarshalIndent(T{N: 1}, "", "  ")
	fmt.Println(string(b))
	var v any
	dec := json.NewDecoder(strings.NewReader(`{"a":[1,2]}`))
	dec.DisallowUnknownFields()
	fmt.Println(dec.Decode(&v), v)
}
