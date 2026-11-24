package main

import "errors"

type E struct{}

func (*E) Error() string { return "e" }

// errors.As with a target that is not a pointer to an interface or an
// error type panics with gc's message (Batch 1).
func main() {
	var e *E
	println(errors.As(&E{}, &e), e != nil)
	var n int
	errors.As(&E{}, &n)
}
