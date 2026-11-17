// aether:expect unsupported non-int-type

package main

// An interface embedded in a struct would promote its methods through the
// struct (D-041).

type Namer interface{ Name() string }

type T struct {
	Namer
	n int
}

func main() {
	var t T
	println(t.n)
}
