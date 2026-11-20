package main

import "errors"

type myErr struct{ s string }

func (e myErr) Error() string { return e.s }

type sliceErr []int

func (e sliceErr) Error() string { return "slice" }

type wrap struct{ err error }

func (w *wrap) Error() string { return "wrap: " + w.err.Error() }
func (w *wrap) Unwrap() error { return w.err }

type isAll struct{}

func (isAll) Error() string        { return "all" }
func (isAll) Is(target error) bool { return true }

var errA = errors.New("a")
var errB = errors.New("b")

func main() {
	w := &wrap{&wrap{errA}}
	println(errors.Is(w, errA), errors.Is(w, errB), errors.Unwrap(w) != nil, errors.Unwrap(errA) == nil)
	println(errors.Is(nil, nil), errors.Is(nil, errA), errors.Is(errA, nil))
	println(errors.Is(&wrap{myErr{"x"}}, myErr{"x"}), errors.Is(myErr{"x"}, myErr{"y"}))
	// An uncomparable target is never compared.
	println(errors.Is(sliceErr{1}, sliceErr{1}), errors.Is(&wrap{sliceErr{1}}, isAll{}), errors.Is(isAll{}, sliceErr{2}))
	j := errors.Join(nil, errA, nil, &wrap{errB})
	println(j.Error())
	println(errors.Is(j, errB), errors.Is(j, myErr{}), errors.Join(nil, nil) == nil)
	println(errors.Join(errA).Error())
}
