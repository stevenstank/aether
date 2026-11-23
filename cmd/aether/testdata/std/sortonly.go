// sort.Slice in a program without fmt, so without package reflect: the
// type descriptors are full only because internal/reflectlite is in the
// program (Reliability R.6). Elements hold pointers and the less function
// allocates; a non-slice argument panics with gc's message.
package main

import "sort"

type R struct {
	Name string
	N    int16
	P    *R
}

func main() {
	xs := []R{{"c", 3, nil}, {"a", 1, nil}, {"b", 2, nil}, {"a", 0, nil}}
	for i := range xs {
		xs[i].P = &R{Name: xs[i].Name + "!"}
	}
	sort.SliceStable(xs, func(i, j int) bool {
		_ = make([]byte, 128)
		return xs[i].Name < xs[j].Name
	})
	for _, x := range xs {
		println(x.Name, x.N, x.P.Name)
	}
	bs := []int8{5, -1, 3}
	sort.Slice(bs, func(i, j int) bool { return bs[i] < bs[j] })
	println(bs[0], bs[1], bs[2], sort.SliceIsSorted(bs, func(i, j int) bool { return bs[i] < bs[j] }))
	sort.Slice(7, nil)
}
