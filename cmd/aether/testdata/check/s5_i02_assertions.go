// aether:expect run

package main

// Type assertions and type switches (D-041): to concrete and interface
// types, the comma-ok form with the zero value of every kind, cases of
// several types, nil, and default, and the switch's variable of each
// clause's type.

type Animal interface{ Sound() string }

type Walker interface {
	Animal
	Legs() int
}

type Dog struct{ name string }

func (d Dog) Sound() string { return d.name + ": woof" }
func (d Dog) Legs() int      { return 4 }

type Fish struct{}

func (Fish) Sound() string { return "blub" }

type Point struct{ x, y int }

func kind(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int:
		return "int " + string(rune('0'+x%10))
	case string:
		return "string " + x
	case int8, int16:
		return "small int"
	case Walker:
		return "walker " + x.Sound()
	case Animal:
		return "animal " + x.Sound()
	case Point:
		return "point"
	case *Point:
		if x == nil {
			return "nil *point"
		}
		return "*point"
	case []int:
		return "slice"
	case error:
		return "error"
	default:
		return "other"
	}
}

func main() {
	vals := []any{nil, 7, "s", int8(1), int16(2), Dog{"rex"}, Fish{}, Point{1, 2}, &Point{}, (*Point)(nil), []int{}, 2.5, true}
	for i := 0; i < len(vals); i++ {
		println(i, kind(vals[i]))
	}

	var a Animal = Dog{"fido"}
	w, ok := a.(Walker)
	println(ok, w.Legs(), w.Sound())
	a = Fish{}
	w, ok = a.(Walker)
	println(ok, w == nil)
	d, ok := a.(Dog)
	println(ok, len(d.name))
	f, ok := a.(Fish)
	println(ok, f == Fish{})

	var x any = Point{3, 4}
	p, ok := x.(Point)
	println(ok, p.x, p.y)
	q, ok := x.(*Point)
	println(ok, q == nil)
	s, ok := x.(string)
	println(ok, len(s))
	n, ok := x.(int)
	println(ok, n)
	arr, ok := x.([3]int)
	println(ok, arr[0], arr[2])
	an, ok := x.(Animal)
	println(ok, an == nil)
	e, ok := x.(any)
	println(ok, e == x)
	x = nil
	e, ok = x.(any)
	println(ok, e == nil)

	switch x.(type) {
	case int:
		println("int")
	default:
		println("default")
	}
	var b any = false
	switch v := b.(type) {
	case bool, string:
		println("bool or string", v == false)
	}
	println(x == nil, b.(bool))
}
