package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type Shape interface {
	Area() float64
	Name() string
}

type sq struct{ s float64 }

func (q sq) Area() float64 { return q.s * q.s }
func (q sq) Name() string  { return "square" }

// Labeled embeds Shape and overrides Name.
type Labeled struct {
	Shape
	label string
}

func (l Labeled) Name() string { return l.label + ":" + l.Shape.Name() }

type counter struct {
	io.Writer
	n int
}

func (c *counter) Write(b []byte) (int, error) {
	c.n += len(b)
	return c.Writer.Write(b)
}

type rev struct{ sort.Interface }

func (r rev) Less(i, j int) bool { return r.Interface.Less(j, i) }

type wrapErr struct {
	error
	code int
}

type deep struct {
	*Labeled
	extra int
}

func main() {
	l := Labeled{Shape: sq{3}, label: "big"}
	fmt.Println(l.Area(), l.Name(), l.Shape.Area())
	var s Shape = l
	fmt.Println(s.Area(), s.Name())
	var sp Shape = &l
	fmt.Println(sp.Area(), sp.Name())
	f := l.Area
	g := Labeled.Area
	h := (*Labeled).Area
	fmt.Println(f(), g(l), h(&l))

	c := &counter{Writer: os.Stdout}
	fmt.Fprintf(c, "hello %d\n", 42)
	var w io.Writer = c
	io.WriteString(w, "again\n")
	fmt.Println(c.n)

	xs := []int{3, 1, 2}
	sort.Sort(rev{sort.IntSlice(xs)})
	fmt.Println(xs)
	sort.Sort(sort.Reverse(sort.StringSlice([]string{"a"})))

	e := wrapErr{errors.New("boom"), 7}
	var err error = e
	fmt.Println(err.Error(), e.Error(), e.code, err)
	fmt.Println(strings.Repeat("=", 3))

	d := deep{&l, 1}
	fmt.Println(d.Area(), d.Name())
	var ds Shape = d
	fmt.Println(ds.Name())

	defer func() { fmt.Println("recovered:", recover()) }()
	var empty Labeled
	fmt.Println(empty.Area())
}
