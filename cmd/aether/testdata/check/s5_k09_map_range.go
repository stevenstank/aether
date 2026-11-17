// aether:expect run

package main

// Ranges over maps whose output does not depend on the order: sums and
// counts, every form of the range clause, break and continue, deleting
// or changing the entry being visited, clear, a nil map, and range
// variables new in each iteration, captured and addressed (D-042).

type P struct {
	x int
	s string
}

func main() {
	m := map[int]int{}
	for i := 0; i < 100; i++ {
		m[i*3] = i
	}
	s, c := 0, 0
	for k, v := range m {
		s += k * v
		c++
	}
	println(s, c)
	n := 0
	for range m {
		n++
	}
	for k := range m {
		if k%2 == 0 {
			delete(m, k)
		}
	}
	println(n, len(m))
	for k, v := range m {
		m[k] = v * 2
	}
	s = 0
	for _, v := range m {
		s += v
	}
	println(s)
	var k2, v2 int
	c = 0
	for k2, v2 = range m {
		if v2 > 100 {
			continue
		}
		c++
	}
	println(c, k2 >= 0)
	for k := range m {
		if k > 1000 {
			break
		}
		clear(m)
	}
	println(len(m))
	var nm map[string]int
	for k, v := range nm {
		println(k, v)
	}
	ps := map[P][2]string{{1, "a"}: {"x", "y"}, {2, "b"}: {"z", "w"}}
	t := ""
	for p, a := range ps {
		a[0] += p.s
		t += a[0] + a[1]
		ps[p] = a
	}
	println(len(t), ps[P{1, "a"}][0], ps[P{2, "b"}][0])
	var fs []func() int
	var ptrs []*int
	for k, v := range map[int]int{1: 10, 2: 20, 3: 30} {
		fs = append(fs, func() int { return k + v })
		ptrs = append(ptrs, &v)
	}
	s = 0
	for i := 0; i < len(fs); i++ {
		s += fs[i]() + *ptrs[i]
	}
	println(s)
	nan := 0.0
	nan /= nan
	fm := map[float64]int{nan: 1, 2: 2}
	fm[nan] = 3
	s = 0
	for k, v := range fm {
		if k != k {
			s += v
		}
	}
	println(len(fm), s)
	am := map[any]string{1: "a", "b": "c", P{}: "d"}
	t = ""
	for k, v := range am {
		if x, ok := k.(int); ok && x == 1 {
			t += v
		}
	}
	println(t)
}
