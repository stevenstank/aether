// aether:expect unsupported println-argument-type

package main

// gc prints a pointer's address, which varies from run to run.

func main() {
	x := 1
	println(&x)
}
