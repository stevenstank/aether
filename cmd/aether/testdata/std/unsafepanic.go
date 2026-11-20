package main

import "unsafe"

type P unsafe.Pointer

func main() {
	defer func() {
		r := recover()
		println(r.(P) == nil)
		panic(unsafe.Pointer(nil))
	}()
	panic(P(nil))
}
