package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

type rw struct {
	io.ReadWriter // Read is declared by io.Reader, not io.ReadWriter
	reads         int
}

func (r *rw) Read(p []byte) (int, error) {
	r.reads++
	return r.ReadWriter.Read(p)
}

type Named interface{ Name() string }

type nm string

func (n nm) Name() string { return string(n) }

type pair struct {
	Named
	n int
}

type Box[T any] struct {
	fmt.Stringer
	v T
}

type str int

func (s str) String() string { return fmt.Sprintf("str(%d)", int(s)) }

type outer struct {
	inner
}

type inner struct {
	fmt.Stringer
	error
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "panic:", r)
		}
	}()
	f()
}

func main() {
	var buf bytes.Buffer
	x := &rw{ReadWriter: &buf}
	fmt.Fprint(x, "data in buffer")
	b, err := io.ReadAll(x)
	fmt.Println(string(b), err, x.reads > 0)
	var r io.Reader = x
	_, _ = r.Read(make([]byte, 1))

	// Comparison and map keys use the embedded interface's equality.
	p1, p2, p3 := pair{nm("a"), 1}, pair{nm("a"), 1}, pair{nm("b"), 1}
	fmt.Println(p1 == p2, p1 == p3, p1.Name())
	m := map[pair]int{p1: 1}
	m[p2]++
	m[p3]++
	fmt.Println(len(m), m[p1])
	fmt.Printf("%v %+v\n", p1, p3)

	bx := Box[int]{str(5), 9}
	fmt.Println(bx.String(), bx, bx.v)
	var s fmt.Stringer = bx
	fmt.Println(s.String())

	o := outer{inner{str(1), errors.New("e1")}}
	fmt.Println(o.String(), o.Error())
	var e error = o
	fmt.Println(e.Error(), e)

	try("nil call", func() {
		var p pair
		_ = p.Name()
	})
	try("nil method value", func() {
		var p pair
		f := p.Name
		fmt.Println("not reached", f)
	})
	try("nil via iface", func() {
		var n Named = pair{}
		fmt.Println(n.Name())
	})
	try("nil ptr struct", func() {
		var pp *pair
		fmt.Println(pp.Name())
	})
	f := pair.Name
	g := (*pair).Name
	fmt.Println(f(p3), g(&p1))
	try("method expr nil", func() { fmt.Println(f(pair{})) })
	h := p1.Name
	p1.Named = nm("changed")
	fmt.Println(h(), p1.Name())

	sr := strings.NewReader("abc")
	type rc struct {
		io.Reader
		io.Closer
	}
	var rcv io.ReadCloser = rc{Reader: sr}
	bb := make([]byte, 2)
	n, _ := rcv.Read(bb)
	fmt.Println(n, string(bb))
	try("nil closer", func() { rcv.Close() })
}
