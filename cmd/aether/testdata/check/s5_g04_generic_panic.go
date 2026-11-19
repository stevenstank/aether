// aether:expect run

package main

// A panic that passes through generic code (D-045): deferred calls in
// instances run while it unwinds, and gc's message names the instance of
// a generic error type, through its Error method, and of a generic type
// in an interface conversion that is not recovered.

type NotFound[K comparable] struct{ key K }

func (e NotFound[K]) Error() string { return "not found" }

type Table[K comparable, V any] struct{ m map[K]V }

func (t *Table[K, V]) Get(k K) V {
	v, ok := t.m[k]
	if !ok {
		panic(NotFound[K]{k})
	}
	return v
}

func Try[K comparable, V any](t *Table[K, V], k K) (v V, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
		}
	}()
	return t.Get(k), nil
}

func Trace[T any](v T, f func(T)) {
	defer println("leaving Trace")
	f(v)
}

type Box[T any] struct{ v T }

func main() {
	t := &Table[string, int]{map[string]int{"a": 1}}
	v, err := Try(t, "a")
	println(v, err == nil)
	v, err = Try(t, "b")
	nf, isNF := err.(NotFound[string])
	println(v, err.Error(), isNF, nf.key)
	Trace(3, func(i int) { println("in f", i) })
	Trace(Box[int]{4}, func(b Box[int]) {
		var e any = b
		println(e.(Box[string]).v)
	})
}
