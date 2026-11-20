// Failed assertions between types of the same name: gc tells the types of
// different packages from those of different scopes.
package main

import (
	one "example.com/pk/dup/one"
	two "example.com/pk/dup/two"
)

type T struct{ X int }

func try(f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(r.(error).Error())
		}
	}()
	f()
}

func main() {
	var x any = one.T{1}
	try(func() { _ = x.(two.T) })
	try(func() { _ = x.(one.T); println("same type") })
	type local struct{ X int }
	var y any = local{2}
	try(func() {
		type local struct{ X int }
		_ = y.(local)
	})
	var z any = T{3}
	try(func() { _ = z.(one.T) })
	try(func() { _ = z.(int) })
}
