// aether:expect run

package main

// Complex numbers as values (D-049, increment 11.8): constants, complex,
// real, imag, conversions, comparison, interfaces, map keys, fields, and
// elements; no arithmetic.
type C complex64

type pt struct {
	c complex128
	n int
}

func parts(c complex128) (float64, float64) { return real(c), imag(c) }

func mk(r, i float32) complex64 { return complex(r, i) }

func main() {
	var c complex128 = 1 + 2i
	d := complex64(c)
	println(real(c), imag(c), real(d), imag(d), c == complex(1, 2), c != 0)
	r, i := parts(complex(3, -4))
	println(r, i)
	var a any = c
	println(a == any(complex(1.0, 2.0)), a.(complex128) == c)
	switch a.(type) {
	case complex64:
		println("c64")
	case complex128:
		println("c128")
	}
	m := map[complex64]int{mk(1, 2): 3}
	m[complex64(1+2i)]++
	println(m[mk(1, 2)], len(m))
	p := pt{c: 5i, n: 1}
	q := p
	println(q == p, imag(q.c), real(complex128(C(mk(7, 8)))))
	arr := [2]complex64{1, 2i}
	println(real(arr[0]), imag(arr[1]))
	var z complex128
	println(z == 0, real(z))
}
