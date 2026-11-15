// aether:expect unsupported for-range

package main

func main() {
	for i := range []int{1, 2} {
		println(i)
	}
}
