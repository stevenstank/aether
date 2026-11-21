// Deferred calls, recover, re-panics, panics with error and custom
// values, method values in defers, and runtime errors, ending in a nil
// map write that is not recovered.
package main

import (
	"errors"
	"fmt"
	"strings"
)

type logger struct {
	prefix string
	lines  []string
}

func (l *logger) log(s string) { l.lines = append(l.lines, l.prefix+s) }

type myErr struct{ code int }

func (e myErr) Error() string { return fmt.Sprintf("myErr %d", e.code) }

func try(l *logger, name string, f func()) {
	defer func() {
		r := recover()
		switch v := r.(type) {
		case nil:
			l.log(name + ": ok")
		case error:
			e := v
			for e != nil {
				if _, ok := e.(myErr); ok {
					break
				}
				e = errors.Unwrap(e)
			}
			if me, ok := e.(myErr); ok {
				l.log(fmt.Sprintf("%s: myErr code %d", name, me.code))
			} else {
				l.log(name + ": error " + v.Error())
			}
		case string:
			l.log(name + ": string " + v)
		default:
			l.log(fmt.Sprintf("%s: %T %v", name, v, v))
		}
	}()
	f()
}

func nested(l *logger, depth int) {
	defer l.log(fmt.Sprint("unwind ", depth))
	if depth == 0 {
		var a []int
		_ = a[depth+3]
	}
	nested(l, depth-1)
}

func rethrow() {
	defer func() {
		r := recover()
		panic(fmt.Sprint("re: ", r))
	}()
	panic("first")
}

func deferOrder(l *logger) (n int) {
	for i := range 4 {
		defer func() { n = n*10 + i }()
	}
	return 5
}

func main() {
	l := &logger{prefix: "> "}
	try(l, "plain", func() {})
	try(l, "string", func() { panic("boom") })
	try(l, "error", func() { panic(errors.New("bad")) })
	try(l, "custom", func() { panic(myErr{7}) })
	try(l, "wrapped", func() { panic(fmt.Errorf("ctx: %w", myErr{9})) })
	try(l, "int", func() { panic(42) })
	try(l, "index", func() { nested(l, 3) })
	try(l, "divide", func() {
		x := 0
		fmt.Println(10 / x)
	})
	try(l, "nil deref", func() {
		var p *logger
		p.log("x")
	})
	try(l, "conversion", func() {
		var x any = "s"
		_ = x.(int)
	})
	try(l, "rethrow", rethrow)
	try(l, "slice bounds", func() {
		s := []int{1, 2, 3}
		i := 5
		_ = s[1:i]
	})
	try(l, "closed chan", func() {
		c := make(chan int)
		close(c)
		close(c)
	})
	l.log(fmt.Sprint("defer order: ", deferOrder(l)))
	fmt.Println(strings.Join(l.lines, "\n"))
	var m map[string]int
	defer func() { fmt.Println("main deferred") }()
	m["x"] = 1
}
