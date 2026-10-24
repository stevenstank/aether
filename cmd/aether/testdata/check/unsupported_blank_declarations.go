// aether:expect unsupported blank-identifier

package main

func main() {
	var _ = 1
	var _ int
	const _ = 2
}
