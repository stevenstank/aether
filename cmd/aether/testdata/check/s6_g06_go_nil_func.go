// aether:expect run

package main

// go of a nil func value of no parameters and no results is gc's fatal
// error at the go statement, and no deferred call runs (D-048).

func main() {
	var g func()
	defer println("not run")
	println("before")
	go g()
	println("not reached")
}
