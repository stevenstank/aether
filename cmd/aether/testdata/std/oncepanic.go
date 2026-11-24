package main

import "sync"

// A function made by OnceFunc panics with f's value on every call; the
// second panic is left unrecovered (Batch 1).
func main() {
	f := sync.OnceFunc(func() { panic("once") })
	func() {
		defer func() { println("recovered:", recover().(string)) }()
		f()
	}()
	println("again")
	f()
}
