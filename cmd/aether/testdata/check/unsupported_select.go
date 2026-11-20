// aether:expect unsupported label

package main

// A labeled break out of a select statement: labels are not supported.
func main() {
L:
	for {
		select {
		default:
			break L
		}
	}
}
