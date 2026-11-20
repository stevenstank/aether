// aether:expect run

package main

// Defined types of every supported underlying type (D-049): arithmetic,
// constants, conversions, methods with value and pointer receivers,
// interfaces, type switches, map keys, println, and comparisons.

type Celsius float64
type Count int
type Small uint8
type Name string
type Flag bool
type Ints []int
type Table map[Name]Count
type Op func(Count) Count
type Pipe chan Count
type PInt *int

const Boiling Celsius = 100
const Many Count = 1 << 10

func (c Celsius) Fahrenheit() Celsius { return c*9/5 + 32 }
func (c *Count) Inc()                 { *c++ }
func (n Name) Greet() Name            { return "hi " + n }
func (f Flag) Not() Flag              { return !f }
func (s Ints) Sum() (t Count) {
	for i := 0; i < len(s); i++ {
		t += Count(s[i])
	}
	return
}
func (t Table) Add(n Name, c Count) { t[n] += c }
func (o Op) Twice(c Count) Count    { return o(o(c)) }

type Stringer interface{ Greet() Name }

type Wrap struct {
	Name
	Ints
}

type Summer interface{ Sum() Count }

func describe(v any) {
	switch x := v.(type) {
	case Count:
		println("Count", x, int(x)+1)
	case Name:
		println("Name", x, len(x))
	case Celsius:
		println("Celsius", x)
	case Small:
		println("Small", x)
	case Flag:
		println("Flag", x)
	case int:
		println("int", x)
	default:
		println("other")
	}
}

func main() {
	var c Count = 3
	c.Inc()
	(&c).Inc()
	c += Many
	println(c, c/2, c%7, c<<1, c > Many, Many)
	var s Small = 250
	s += 10
	println(s, Small(300&0xff), ^s)
	println(Boiling.Fahrenheit(), Celsius(37).Fahrenheit(), float64(Boiling)/3)
	n := Name("go")
	println(n.Greet(), n+"!", n < "h", n[0], string(n[1:]), len(n))
	var f Flag
	println(f, f.Not(), !f.Not())
	xs := Ints{1, 2, 3}
	xs = append(xs, 4)
	println(xs.Sum(), len(xs), cap(xs) >= 4, xs[3])
	t := Table{}
	t.Add("a", 1)
	t.Add("a", 2)
	t["b"] = 5
	println(len(t), t["a"], t["b"], t["zz"])
	var op Op = func(c Count) Count { return c * 3 }
	println(op.Twice(2), op(1), op != nil)
	p := make(Pipe, 1)
	p <- 7
	println(<-p, len(p), cap(p))
	v := 9
	var pi PInt = &v
	*pi = 10
	println(v, pi != nil)
	var g Stringer = n
	var su Summer = xs
	println(g.Greet(), su.Sum())
	g2 := n.Greet
	h := Name.Greet
	k := Ints.Sum
	w := Wrap{"w", Ints{5, 6}}
	var ws Summer = w
	var wp Stringer = &w
	println(g2(), h("x"), k(xs), w.Greet(), w.Sum(), ws.Sum(), wp.Greet())
	describe(c)
	describe(n)
	describe(Boiling)
	describe(s)
	describe(Flag(true))
	describe(5)
	describe(xs)
	var a, b any = Count(1), 1
	println(a == b, a == any(Count(1)), Name("x") == Name("x"))
	m := map[Count]Name{1: "one", 2: "two"}
	println(m[1], m[2], m[3] == "")
	defer func() {
		r := recover()
		e, ok := r.(Name)
		println("recovered", e, ok)
		panic(Count(42))
	}()
	panic(Name("boom\nline"))
}
