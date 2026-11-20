package main

import (
	"errors"
	"fmt"
	"os"
)

type Point struct {
	X, Y int
	name string
}

type Celsius float64

func (c Celsius) String() string { return fmt.Sprintf("%.1f°C", float64(c)) }

type myErr struct{ code int }

func (e *myErr) Error() string { return fmt.Sprint("code ", e.code) }

func main() {
	fmt.Println(1, "two", 3.5, true, nil, 'x', uint8(7))
	fmt.Printf("%d %5d %-5d| %05d %x %X %o %b %c %q %U\n", 42, 42, 42, -42, 255, 255, 8, 5, 'A', 'A', 'A')
	fmt.Printf("%s %q %v %10s|%-10s| %x\n", "hi", "a\tb", "v", "r", "l", "hex")
	fmt.Printf("%f %.2f %e %g %g %8.3f|\n", 3.14159, 2.5, 123456.789, 1e21, 0.000001, -1.5)
	p := Point{1, 2, "p"}
	fmt.Printf("%v %+v %#v %T\n", p, p, p, p)
	fmt.Printf("%v %v %p\n", &p == nil, []int{1, 2, 3}, nil)
	fmt.Println(map[string]int{"b": 2, "a": 1, "c": 3}, map[int]bool{3: true, 1: false})
	fmt.Println([]string{"x", "y"}, [2]bool{true}, struct{}{}, []byte("ab"))
	fmt.Println(Celsius(21.5), []Celsius{1, 2})
	var e error = &myErr{7}
	w := fmt.Errorf("wrapped: %w", e)
	fmt.Println(w, errors.Unwrap(w) == e)
	s := fmt.Sprintf("%d-%s", 5, "x")
	fmt.Println(s, len(s), fmt.Sprint("a", 1, 2, "b"), fmt.Sprintln("end"))
	fmt.Printf("%!d %d\n", "str")
	fmt.Printf("%v %v\n", complex(1, -2), int64(-1)<<63)
	fmt.Fprintln(os.Stderr, "to stderr")
	n, err := fmt.Fprintf(os.Stdout, "%s\n", "count")
	fmt.Println(n, err)
	var ip *int
	var iface any
	fmt.Println(ip, iface, [][]int{{1}, {2, 3}}, map[Point]string{{1, 2, "a"}: "pt"})
	fmt.Printf("%6.2v|%t|%08.3f|%+d|% d\n", 3.14159, false, -3.5, 5, 7)
	// Scanning: a string, and the standard input, which is empty.
	var bs []byte
	var k int
	var c complex128
	n, err = fmt.Sscan("hello 12 (1+2i)", &bs, &k, &c)
	fmt.Println(n, err, string(bs), k, real(c), imag(c))
	n, err = fmt.Scan(&k)
	fmt.Println(n, err)
	_, err = fmt.Sscanf("x", "%d", &k)
	fmt.Println(err)
}
