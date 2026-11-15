// aether:expect run

package main

// Conversions between strings and []byte copy the bytes, so a later change
// of either does not show in the other; append and copy take a string's
// bytes.

type rec struct {
	b []byte
	s string
}

func main() {
	s := "hello"
	b := []byte(s)
	b[0] = 72
	println(s, string(b), len(b))
	t := string(b)
	b[1] = 69
	println(t, string(b))
	println(string(b[1:3]), len(string(b[:0])), len([]byte("")), []byte("") == nil)
	var nb []byte
	println(len(string(nb)), string(nb)+"!")
	r := rec{b: []byte("abc"), s: "xyz"}
	r.s = string(r.b) + r.s
	r.b[2] = 90
	println(r.s, string(r.b))
	c := []uint8(s + s)
	println(len(c), c[5], c[9])
	d := append([]byte{}, s...)
	d = append(d, " world"...)
	d = append(d, ""...)
	d = append(d, s[1:3]...)
	println(string(d), len(d))
	n := copy(d, "HELLO")
	println(n, string(d))
	n = copy(d[6:], s)
	println(n, string(d))
	n = copy(d, "")
	println(n, copy(d[:2], "xyz"), string(d))
	e := make([]byte, 3, 10)
	f := append(e, "ab"...)
	g := append(e, "cd"...)
	println(string(f), string(g))
	println(string([]byte(string([]byte("round trip")))))
}
