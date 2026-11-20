package main

import "reflect"

type Point struct {
	X, Y int
	name string
	Tags []string
}

func (p Point) String() string { return "pt" }

type Stringer interface{ String() string }

// Blank fields stay zero, though their elements are evaluated
// (go.dev/issue/31546).
var blank = struct{ a, _, c int }{1, 2, 3}

var calls int

func count() int { calls++; return calls }

type Blanks struct {
	x, _ int
	_    *[2]int
	y    int
}

func main() {
	p := Point{1, 2, "a", []string{"x", "y"}}
	v := reflect.ValueOf(p)
	t := v.Type()
	println(t.String(), v.Kind() == reflect.Struct, v.NumField(), t.Field(2).Name, t.Field(2).PkgPath != "")
	println(v.Field(0).Int(), v.Field(2).String(), v.Field(2).CanInterface(), v.Field(3).Len(), v.Field(3).Index(1).String())
	var s Stringer = p
	sv := reflect.ValueOf(&s).Elem()
	println(sv.Kind().String(), sv.Elem().Type().String(), sv.Elem().Field(1).Int())
	any1 := v.Interface()
	str, ok := any1.(Stringer)
	println(ok, str.String(), any1.(Point).Y)
	m := map[string]int{"b": 2, "a": 1}
	it := reflect.ValueOf(m).MapRange()
	n := 0
	for it.Next() {
		n += int(it.Value().Int()) + len(it.Key().String())
	}
	println(n, reflect.ValueOf(m).Len(), reflect.TypeOf(m).Key().String(), reflect.TypeOf(m).Elem().Kind().String())
	var np *Point
	pv := reflect.ValueOf(np)
	println(pv.Kind().String(), pv.IsNil(), reflect.ValueOf(&p).Elem().Field(1).Int(), reflect.ValueOf(&p).Elem().CanAddr())
	println(reflect.ValueOf(3.5).Float(), reflect.ValueOf(uint8(7)).Uint(), reflect.ValueOf(true).Bool(), reflect.ValueOf("hey").Index(1).Uint(), reflect.ValueOf(complex64(2+3i)).Complex() == 2+3i)
	println(reflect.TypeOf([3]int{}).Len(), reflect.TypeOf(int16(0)).Bits(), reflect.ValueOf([]byte("ab")).Bytes()[1], reflect.TypeOf(nil) == nil, reflect.ValueOf(nil).IsValid())
	println(reflect.ValueOf(p).String(), reflect.ValueOf(42).Interface().(int))
	b := Blanks{count(), count(), &[2]int{count(), 0}, count()}
	println(reflect.ValueOf(blank).Field(1).Int(), b.x, b.y, calls, reflect.ValueOf(b).Field(1).Int(), reflect.ValueOf(b).Field(2).IsNil())
}
