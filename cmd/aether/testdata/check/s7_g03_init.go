// aether:expect run

package main

// init functions (D-049): run after every package variable is
// initialized, in source order, before main; several may be declared, and
// one may panic and recover, start goroutines, or set variables.

var order []string

var a = record("var a")

func record(s string) int {
	order = append(order, s)
	return len(order)
}

func init() {
	record("init 1")
	a *= 10
}

var b = record("var b")

func init() {
	defer func() { record("recovered " + recover().(string)) }()
	panic("in init 2")
}

var done = make(chan bool)

func init() {
	go func() {
		record("goroutine")
		done <- true
	}()
	<-done
}

func main() {
	record("main")
	for _, s := range order {
		println(s)
	}
	println(a, b)
}
