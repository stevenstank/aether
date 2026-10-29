// aether:expect unsupported label

package main

func main() {
outer:
	for {
		break outer
	}
}
