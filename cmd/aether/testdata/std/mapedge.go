// mapedge checks maps where Aether's representation matters (D-042,
// D-054): NaN keys, each its own entry, hashed apart; float keys, -0 and
// +0 one key; and ranges that begin after deleted entries, in insertion
// order in Aether; only what does not depend on gc's random order is
// printed.
package main

import (
	"fmt"
	"math"
)

func main() {
	m := map[float64]int{}
	nan := math.NaN()
	for i := 0; i < 1000; i++ {
		m[nan] = i
		m[float64(i)/4] = i
	}
	m[math.Copysign(0, -1)] = -1
	_, okNaN := m[nan]
	v, ok := m[0]
	fmt.Println(len(m), okNaN, v, ok, m[2.5], m[249.75])
	nans, sum := 0, 0
	for k, v := range m {
		if k != k {
			nans++
			sum += v
		}
	}
	fmt.Println(nans, sum)
	delete(m, nan)
	fmt.Println(len(m))
	clear(m)
	fmt.Println(len(m))

	d := map[int]string{}
	for i := 0; i < 200; i++ {
		d[i] = fmt.Sprint(i)
	}
	// Delete the first key a range visits, repeatedly.
	var order []int
	for len(d) > 150 {
		for k := range d {
			order = append(order, k)
			delete(d, k)
			break
		}
	}
	d[-1] = "back"
	for i := 0; i < 400; i++ {
		d[1000+i] = "grow"
	}
	n := 0
	for range d {
		n++
	}
	fmt.Println(len(order), n, len(d), d[-1])
	small := map[string]int{"a": 1, "b": 2, "c": 3}
	delete(small, "a")
	small["a"] = 4
	fmt.Println(small)
}
