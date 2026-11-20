// aether:expect unsupported non-int-type
// aether:gc invalid gc reports "channel element type too large (>64kB)"; the spec sets no limit

package main

// A channel whose element is larger than gc accepts (64 KiB), sent on.
func main() {
	var ch chan [1 << 16]byte
	ch <- [1 << 16]byte{}
}
