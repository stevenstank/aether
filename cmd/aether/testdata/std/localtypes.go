// localtypes checks types declared in functions as type arguments of
// generic code (D-057): moved to the package level, named as gc names
// them, tc alone and tc·N in an instance's name, by reflect and fmt, and
// encoded by encoding/json with their tags.
package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
)

type Box[T any] struct{ v T }

func f() {
	type tc struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	type pair struct{ a, b tc }
	out := []tc{{"b", 1}, {"a", 2}, {"c", 2}}
	slices.SortFunc(out, func(a, b tc) int { return b.Count - a.Count })
	fmt.Printf("%v %T %T %T\n", out, out[0], Box[tc]{}, Box[pair]{})
	fmt.Println(reflect.TypeOf(Box[tc]{}).Name(), reflect.TypeOf(out[0]).Name())
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	var x any = Box[tc]{}
	_, ok := x.(Box[tc])
	fmt.Println(ok)
}

func g() {
	type tc int
	fmt.Printf("%T %T %v\n", tc(1), Box[tc]{}, slices.Max([]tc{3, 9, 2}))
	m := map[tc]Box[tc]{}
	m[2] = Box[tc]{5}
	fmt.Println(m)
}

func main() { f(); g() }
