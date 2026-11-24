package main

import "sync"

// gc's CompareAndDelete panics while holding the map's lock when the value
// is not comparable, so the panic is left unrecovered here (Batch 1).
func main() {
	var m sync.Map
	m.Store("k", []int{1})
	println(m.CompareAndDelete("k", 1), m.CompareAndDelete("missing", []int{1}))
	m.CompareAndDelete("k", []int{1})
}
