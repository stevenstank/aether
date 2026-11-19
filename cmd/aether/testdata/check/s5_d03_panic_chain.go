// aether:expect run

package main

// A deferred call that panics while a panic unwinds adds a later panic;
// gc writes them oldest first. Values with an Error or String method are
// written as its result, strings with a tab after each newline, and other
// values with their type and data word (D-043).

type E struct{ code int }

func (e E) Error() string { println("Error called", e.code); return "error " + string(rune('0'+e.code)) }

type N struct{ n int }

func (n *N) String() string { return "stringer\nline two" }

func main() {
	var np *int
	defer func() { panic(np) }()
	defer func() { panic(&N{2}) }()
	defer func() { panic("two\nlines") }()
	defer func() { panic(E{4}) }()
	defer println("first deferred")
	panic(1.25)
}
