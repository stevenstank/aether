// largearray checks arrays larger than 16 MiB at the package level and on
// the heap, which a frame does not hold (D-057).
package main

import "fmt"

type Buffer [1 << 25]byte

var global Buffer

func fill(b *Buffer, step int) int {
	sum := 0
	for i := 0; i < len(b); i += step {
		b[i] = byte(i / step)
		sum += int(b[i])
	}
	return sum
}

func main() {
	fmt.Println(fill(&global, 1<<16), global[1<<16], len(global))
	h := new([1 << 22]int64)
	for i := 0; i < len(h); i += 1 << 12 {
		h[i] = int64(i)
	}
	h[len(h)-2] = -2
	s := h[len(h)-3:]
	fmt.Println(s, cap(s), h[1<<21])
	p := &struct {
		a [1 << 22]int32
		b string
	}{b: "tail"}
	p.a[len(p.a)-1] = 7
	fmt.Println(p.b, p.a[len(p.a)-1])
}
