// aether:expect run

package main

// panic(nil) panics with gc's PanicNilError message, and a nil func value
// deferred panics only when the call runs (D-043).

func main() {
	var f func(int)
	defer println("last")
	defer f(1)
	defer println("before the nil call")
	var v any
	panic(v)
}
