// aether:expect run

package main

// Elements of array, struct, slice, map, string, and func types: copied
// out, appended to, counted, concatenated, swapped, and called (D-042).

type T struct {
	a [20]int
	s []int
	m map[string]int
}

func g(m map[string]T, k string) T { return m[k] }

func main() {
	m := map[string]T{}
	var t T
	t.a[19] = 5
	t.s = []int{1, 2}
	t.m = map[string]int{"z": 26}
	m["a"] = t
	t.a[19] = 6
	println(m["a"].a[19], t.a[19], len(m["a"].s), m["a"].m["z"])
	u := g(m, "a")
	println(u.a[19], g(m, "b").m == nil)
	sm := map[int][]int{}
	sm[1] = append(sm[1], 3)
	sm[1] = append(sm[1], 4)
	println(len(sm[1]), sm[1][1])
	cnt := map[string]int{}
	words := []string{"a", "b", "a", "c", "b", "a"}
	for i := 0; i < len(words); i++ {
		cnt[words[i]]++
	}
	println(cnt["a"], cnt["b"], cnt["c"])
	ss := map[string]string{}
	ss["x"] += "abc"
	ss["x"] += "def"
	println(ss["x"])
	m2 := map[int]int{1: 1}
	m2[1], m2[2] = m2[2], m2[1]
	println(m2[1], m2[2], len(m2))
	fm := map[string]func(int) int{"dbl": func(x int) int { return 2 * x }}
	println(fm["dbl"](21), fm["no"] == nil)
}
